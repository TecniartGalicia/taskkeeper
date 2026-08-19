// Turns the raw stream-json events of a run into a readable transcript model.
//
// This is the whole point of the result view: the events are correct but noisy
// (thinking-token deltas, base64 signatures, internal quota fields). Here they
// are parsed by type into a small, ordered model that a view can render as a
// conversation. Pure: no vscode, no I/O — so it is unit-tested against real
// captured events. Secrets are already redacted by the worker before storage.
import type { Run, RunEvent } from './model';

export type ItemKind = 'system' | 'say' | 'tool' | 'thought' | 'final' | 'log' | 'notice';

export interface TranscriptItem {
  kind: ItemKind;
  who?: string; // "Claude" / "Codex" / tool name
  text?: string; // say / final / system / log / notice
  tool?: string; // tool name (kind === 'tool')
  command?: string; // summarised tool input
  output?: string; // tool result, truncated
  error?: boolean; // final: the run reported an error
}

export interface TranscriptSummary {
  state: string;
  costUSD?: number;
  turns?: number;
  files: number;
  errorCode?: string;
  session?: string;
  cwd?: string; // the folder the run worked in (where its conversation lives)
  decision?: string;
  isolated?: boolean; // ran in a worktree (true) or directly in the repo (false)
}

export interface Transcript {
  summary: TranscriptSummary;
  items: TranscriptItem[];
}

const MAX_OUTPUT = 600; // characters of tool output to keep

export function buildTranscript(run: Run, events: RunEvent[]): Transcript {
  const items: TranscriptItem[] = [];
  const toolIndexById = new Map<string, number>();
  let turns: number | undefined;
  let costFromResult: number | undefined;

  const agentName = run.agente === 'codex' ? 'Codex' : 'Claude';

  for (const ev of events) {
    let p: Record<string, unknown>;
    try {
      p = JSON.parse(ev.payload) as Record<string, unknown>;
    } catch {
      // Not JSON: keep it as a plain log line rather than guessing.
      pushLog(items, ev.payload);
      continue;
    }
    const type = String(p.type ?? ev.tipo ?? '');
    const subtype = String(p.subtype ?? '');

    if (type === 'system' && subtype === 'init') {
      const cwd = String(p.cwd ?? '');
      items.push({
        kind: 'system',
        text: cwd.includes('worktrees')
          ? 'Started in an isolated worktree.'
          : 'Started directly in the repository.',
      });
      continue;
    }
    if (type === 'system' && subtype === 'thinking_tokens') continue; // pure noise
    if (ev.tipo === 'aviso' || type === 'aviso') {
      const aviso = String((p as { aviso?: unknown }).aviso ?? ev.payload);
      items.push({ kind: 'notice', text: aviso });
      continue;
    }
    if (type === 'rate_limit_event') {
      const info = (p.rate_limit_info ?? {}) as { status?: string };
      if (info.status && info.status !== 'allowed') {
        items.push({ kind: 'notice', text: `Rate limit: ${info.status}.` });
      }
      continue;
    }
    if (type === 'assistant') {
      const content = (((p.message ?? {}) as { content?: unknown }).content ?? []) as Array<Record<string, unknown>>;
      for (const block of Array.isArray(content) ? content : []) {
        const bt = String(block.type ?? '');
        if (bt === 'text') {
          const text = String(block.text ?? '').trim();
          if (text && !isDuplicateSay(items, text)) items.push({ kind: 'say', who: agentName, text });
        } else if (bt === 'thinking') {
          const th = String(block.thinking ?? '').trim();
          if (th) items.push({ kind: 'thought', who: agentName, text: th });
        } else if (bt === 'tool_use') {
          const name = String(block.name ?? 'tool');
          items.push({ kind: 'tool', tool: name, who: name, command: summariseInput(block.input) });
          const id = String(block.id ?? '');
          if (id) toolIndexById.set(id, items.length - 1);
        }
      }
      continue;
    }
    if (type === 'user') {
      const content = (((p.message ?? {}) as { content?: unknown }).content ?? []) as Array<Record<string, unknown>>;
      for (const block of Array.isArray(content) ? content : []) {
        if (String(block.type ?? '') !== 'tool_result') continue;
        const id = String(block.tool_use_id ?? '');
        const out = toolResultText(block.content);
        const idx = toolIndexById.get(id);
        if (idx != null && items[idx]) items[idx].output = truncate(out, MAX_OUTPUT);
        else if (out) items.push({ kind: 'tool', tool: 'result', output: truncate(out, MAX_OUTPUT) });
      }
      continue;
    }
    if (type === 'result') {
      const text = String(p.result ?? '').trim();
      const isErr = p.is_error === true;
      if (typeof p.total_cost_usd === 'number') costFromResult = p.total_cost_usd as number;
      if (typeof p.num_turns === 'number') turns = p.num_turns as number;
      items.push({ kind: 'final', who: 'Result', text: text || (isErr ? 'The run ended with an error.' : 'Done.'), error: isErr });
      continue;
    }
    // Any other event: keep the raw line so nothing is silently lost.
    pushLog(items, ev.payload);
  }

  const summary: TranscriptSummary = {
    state: run.estado,
    costUSD: run.coste_usd ?? costFromResult,
    turns,
    files: run.ficheros?.length ?? 0,
    errorCode: run.codigo_error || undefined,
    session: run.sesion_proveedor || undefined,
    cwd: run.ruta_proyecto || undefined,
    decision: run.decision || undefined,
    // Per-run: un worktree registrado significa que ESA ejecución fue aislada,
    // aunque la tarea se edite luego (el worktree manda). Sin worktree, el modo
    // desempata en ambos sentidos: 'direct' → en el repo; 'isolated' → aislada
    // que falló antes de crear el worktree; desconocido → sin etiqueta.
    isolated: run.worktree
      ? true
      : run.workspace_mode === 'direct'
        ? false
        : run.workspace_mode === 'isolated'
          ? true
          : undefined,
  };
  return { summary, items };
}

function pushLog(items: TranscriptItem[], raw: string): void {
  const text = raw.trim();
  if (text) items.push({ kind: 'log', text: truncate(text, 300) });
}

function isDuplicateSay(items: TranscriptItem[], text: string): boolean {
  for (let i = items.length - 1; i >= 0; i--) {
    if (items[i].kind === 'say') return items[i].text === text;
    if (items[i].kind === 'tool' || items[i].kind === 'final') return false;
  }
  return false;
}

/** A compact one-line-ish description of a tool call's input. */
function summariseInput(input: unknown): string {
  if (input == null) return '';
  if (typeof input === 'string') return input;
  const o = input as Record<string, unknown>;
  if (typeof o.command === 'string') return o.command;
  if (typeof o.file_path === 'string') return String(o.file_path);
  if (typeof o.path === 'string') return String(o.path);
  if (typeof o.pattern === 'string') return String(o.pattern);
  try {
    return truncate(JSON.stringify(o), 200);
  } catch {
    return '';
  }
}

/** tool_result content may be a string or an array of blocks. */
function toolResultText(content: unknown): string {
  if (content == null) return '';
  if (typeof content === 'string') return content;
  if (Array.isArray(content)) {
    return content
      .map((b) => (b && typeof b === 'object' && 'text' in b ? String((b as { text?: unknown }).text ?? '') : typeof b === 'string' ? b : ''))
      .join('')
      .trim();
  }
  return '';
}

function truncate(s: string, n: number): string {
  const t = s.trim();
  return t.length > n ? `${t.slice(0, n)}…` : t;
}
