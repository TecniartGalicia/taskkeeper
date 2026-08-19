// Diff and details for a run.
//
// The morning review is the product, so it uses VS Code's own diff editor
// rather than a homemade viewer: left side is the file at the base commit
// (served through a read-only content provider that runs `git show`), right
// side is the real file inside the worktree.
import { execFile } from 'node:child_process';
import * as path from 'node:path';
import * as vscode from 'vscode';
import type { Ctl } from '../core/ctl';
import type { Run } from '../core/model';
import { stateLabel } from './trees';

const t = vscode.l10n.t;
export const BASE_SCHEME = 'taskkeeper-base';
export const RUN_SCHEME = 'taskkeeper-run';

function gitShow(repo: string, commit: string, file: string): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile('git', ['-C', repo, 'show', `${commit}:${file}`], { maxBuffer: 32 * 1024 * 1024, windowsHide: true }, (err, stdout, stderr) => {
      if (err) {
        // A file that did not exist at the base commit is a legitimate "new file".
        if (/exists on disk, but not in|does not exist in/.test(stderr)) return resolve('');
        return reject(new Error(stderr || err.message));
      }
      resolve(stdout);
    });
  });
}

/** Encodes repo, commit and path in the URI so the provider is stateless. */
export function baseUri(run: Run, file: string): vscode.Uri {
  const q = new URLSearchParams({ repo: run.raiz_git, commit: run.commit_base }).toString();
  return vscode.Uri.from({ scheme: BASE_SCHEME, path: `/${run.id}/${file}`, query: q });
}

export class BaseContentProvider implements vscode.TextDocumentContentProvider {
  provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const q = new URLSearchParams(uri.query);
    const repo = q.get('repo') ?? '';
    const commit = q.get('commit') ?? '';
    // path is "/<runId>/<file>"
    const file = uri.path.replace(/^\/[^/]+\//, '');
    if (!repo || !commit) return Promise.resolve('');
    return gitShow(repo, commit, file);
  }
}

export async function openFileDiff(run: Run, file: string): Promise<void> {
  const right = vscode.Uri.file(path.join(run.worktree, file));
  const left = baseUri(run, file);
  const title = t('{0} — {1} (base ↔ agent)', file, run.tarea);
  await vscode.commands.executeCommand('vscode.diff', left, right, title, { preview: true });
}

/** Opens the first changed file, or the multi-diff editor when available. */
export async function openRunDiff(run: Run): Promise<void> {
  if (!run.ficheros.length) {
    vscode.window.showInformationMessage(t('This run did not change any file.'));
    return;
  }
  const resources = run.ficheros.map((f) => [baseUri(run, f), vscode.Uri.file(path.join(run.worktree, f))] as [vscode.Uri, vscode.Uri]);
  try {
    // Multi-file diff (VS Code ≥ 1.85). Falls back to the first file if the host lacks it.
    await vscode.commands.executeCommand('vscode.changes', t('{0} — changes', run.tarea), resources);
  } catch {
    await openFileDiff(run, run.ficheros[0]);
  }
}

// ---------- run details as a rendered markdown document ----------

export class RunDetailsProvider implements vscode.TextDocumentContentProvider {
  private readonly emitter = new vscode.EventEmitter<vscode.Uri>();
  readonly onDidChange = this.emitter.event;
  constructor(private readonly ctl: () => Ctl | undefined) {}

  fire(runId: string): void {
    this.emitter.fire(detailsUri(runId));
  }

  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const id = uri.path.replace(/^\//, '').replace(/\.md$/, '');
    const c = this.ctl();
    if (!c) return t('TaskKeeper is not ready.');
    let r: Run;
    try {
      r = await c.run(id);
    } catch (e) {
      return `# ${t('Run not found')}\n\n${(e as Error).message}`;
    }
    const events = await c.events(id).catch(() => []);
    const lines: string[] = [];
    lines.push(`# ${r.tarea}`);
    lines.push('');
    lines.push(`**${stateLabel(r.estado)}**${r.codigo_error ? ` — \`${r.codigo_error}\`` : ''}`);
    lines.push('');
    lines.push(`| | |`);
    lines.push(`|---|---|`);
    lines.push(`| ${t('Project')} | ${r.proyecto} (${r.ruta_proyecto}) |`);
    lines.push(`| ${t('Agent')} | ${r.agente} |`);
    lines.push(`| ${t('Scheduled')} | ${r.prevista_utc} |`);
    if (r.inicio) lines.push(`| ${t('Started')} | ${r.inicio} |`);
    if (r.fin) lines.push(`| ${t('Finished')} | ${r.fin} |`);
    if (r.rama) lines.push(`| ${t('Branch')} | \`${r.rama}\` |`);
    if (r.commit_base) lines.push(`| ${t('Base commit')} | \`${r.commit_base}\` |`);
    if (r.worktree) lines.push(`| ${t('Worktree')} | \`${r.worktree}\` |`);
    if (r.coste_usd != null) lines.push(`| ${t('Cost')} | ${r.coste_usd.toFixed(2)} USD |`);
    if (r.sesion_proveedor) lines.push(`| ${t('Provider session')} | \`${r.sesion_proveedor}\` |`);
    if (r.decision) lines.push(`| ${t('Your decision')} | ${r.decision} |`);
    lines.push('');
    if (r.ficheros.length) {
      lines.push(`## ${t('Files changed')} (${r.ficheros.length})`);
      lines.push('');
      for (const f of r.ficheros) lines.push(`- \`${f}\``);
      lines.push('');
    }
    if (events.length) {
      lines.push(`## ${t('Timeline')}`);
      lines.push('');
      lines.push('```');
      for (const e of events.slice(-200)) {
        const p = e.payload.length > 300 ? `${e.payload.slice(0, 300)}…` : e.payload;
        lines.push(`${e.en}  ${e.tipo.padEnd(16)} ${p}`);
      }
      lines.push('```');
    }
    return lines.join('\n');
  }
}

export function detailsUri(runId: string): vscode.Uri {
  return vscode.Uri.from({ scheme: RUN_SCHEME, path: `/${runId}.md` });
}

export async function showRunDetails(runId: string): Promise<void> {
  const uri = detailsUri(runId);
  await vscode.commands.executeCommand('markdown.showPreview', uri);
}
