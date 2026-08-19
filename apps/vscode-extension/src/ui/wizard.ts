// The "New task" flow: a chain of quick picks and input boxes.
//
// Seven steps, as in the specification: goal, project, agent, context, prompt,
// schedule, safety. Each step validates before moving on, and the last one
// shows a plain summary with "Create" and "Create and run now".
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import type { Ctl, CreateParams } from '../core/ctl';
import type { Agent } from '../core/model';
import { ISO_WEEKDAYS, isValidHHMM, isValidLocalDateTime, systemTimezone } from '../core/schedule';
import { listSessions } from '../core/sessions';

const t = vscode.l10n.t;

const PROMPT_TEMPLATE = [
  'Treat the current repository as the source of truth and re-read the relevant files first.',
  'Goal: <what must be true when you finish>.',
  'Allowed: read the code, run the tests, change files inside this worktree.',
  'Not allowed: git push, merge, deploy, or touching anything outside this worktree.',
  'When done, summarise: what changed, what you tested, risks and anything you left undone.',
].join('\n');

export interface WizardResult {
  params: CreateParams;
  runNow: boolean;
}

export async function runNewTaskWizard(ctl: Ctl, agents: Agent[], defaultTz: string): Promise<WizardResult | undefined> {
  // 1. Goal
  const nombre = await vscode.window.showInputBox({
    title: t('New task (1/7) · Name'),
    prompt: t('A short name you will recognise in the morning'),
    placeHolder: t('Weekly maintenance'),
    ignoreFocusOut: true,
    validateInput: (v) => (v.trim().length < 3 ? t('Give it at least three characters') : /[\\/:]/.test(v) ? t('No slashes or colons, please') : undefined),
  });
  if (!nombre) return undefined;

  // 2. Project
  const proyecto = await pickProject();
  if (!proyecto) return undefined;

  // 3. Agent
  const installed = agents.filter((a) => a.instalado);
  if (!installed.length) {
    vscode.window.showErrorMessage(t('Neither Claude Code nor Codex was found on this machine. Install one of them in VS Code and try again.'));
    return undefined;
  }
  const agentPick = await vscode.window.showQuickPick(
    installed.map((a) => ({ label: a.nombre === 'claude' ? 'Claude Code' : 'Codex', description: `v${a.version}`, detail: a.ruta, agent: a })),
    { title: t('New task (3/7) · Agent'), ignoreFocusOut: true },
  );
  if (!agentPick) return undefined;
  const agent = agentPick.agent;

  // 4. Context
  type Ctx = { label: string; detail: string; modo: 'new' | 'resume' | 'fork' };
  const ctxItems: Ctx[] = [{ label: t('New conversation'), detail: t('Starts fresh. Best for recurring reports and audits.'), modo: 'new' }];
  if (agent.retomar) ctxItems.push({ label: t('Resume an existing conversation'), detail: t('Appends to its history. For a one-off follow-up.'), modo: 'resume' });
  if (agent.derivar) ctxItems.push({ label: t('Fork an existing conversation'), detail: t('Copies the context into a new conversation. Best for code changes and recurring work.'), modo: 'fork' });
  const ctxPick = await vscode.window.showQuickPick(ctxItems, { title: t('New task (4/7) · Context'), ignoreFocusOut: true });
  if (!ctxPick) return undefined;
  let sesion: string | undefined;
  if (ctxPick.modo !== 'new') {
    sesion = await pickSession(agent.nombre, proyecto);
    if (!sesion) return undefined;
  }

  // 5. Prompt
  const prompt = await vscode.window.showInputBox({
    title: t('New task (5/7) · Prompt'),
    prompt: t('What should the agent do? Say the goal, what it may and may not do, and how it should report. You can edit it later.'),
    value: PROMPT_TEMPLATE,
    ignoreFocusOut: true,
    validateInput: (v) => (v.trim().length < 10 ? t('Say a bit more') : undefined),
  });
  if (!prompt) return undefined;

  // 6. Schedule
  const sched = await pickSchedule(defaultTz || systemTimezone());
  if (!sched) return undefined;

  // 7. Safety
  const perfilPick = await vscode.window.showQuickPick(
    [
      { label: t('Audit (read only)'), detail: t('The agent can read the repository and write a report. It cannot change files.'), perfil: 'auditoria' },
      { label: t('Isolated changes'), detail: t('The agent may edit files inside its own worktree. Nothing reaches your branch until you accept.'), perfil: 'cambios_aislados' },
    ],
    { title: t('New task (7/7) · Permissions'), ignoreFocusOut: true },
  );
  if (!perfilPick) return undefined;

  const politicaPick = await vscode.window.showQuickPick(
    [
      { label: t('Skip it'), detail: t('If the computer was off at that time, do nothing.'), politica: 'skip' },
      { label: t('Run when the computer wakes, if less than 2 hours late'), detail: t('Recommended for one-off tasks.'), politica: 'run_if_late' },
      { label: t('Wait for me to decide'), detail: t('Leave it pending in the inbox.'), politica: 'manual' },
    ],
    { title: t('If the scheduled time is missed…'), ignoreFocusOut: true },
  );
  if (!politicaPick) return undefined;

  const budgetStr = await vscode.window.showInputBox({
    title: t('Spending cap per run (USD)'),
    prompt: t('The agent stops when it reaches this. Leave empty for no cap.'),
    placeHolder: '2.00',
    ignoreFocusOut: true,
    validateInput: (v) => (v.trim() === '' || /^\d+(\.\d{1,2})?$/.test(v.trim()) ? undefined : t('A number like 2.00, or empty')),
  });
  if (budgetStr === undefined) return undefined;

  const params: CreateParams = {
    proyecto,
    nombre: nombre.trim(),
    agente: agent.nombre,
    prompt,
    regla: sched.regla,
    hora: sched.hora,
    dias: sched.dias,
    zona: sched.zona,
    perfil: perfilPick.perfil,
    modo: ctxPick.modo,
    sesion,
    politica: politicaPick.politica,
    retrasoMax: 7200,
    timeout: 3600,
    presupuesto: budgetStr.trim() ? Number(budgetStr.trim()) : undefined,
  };

  // Summary
  const summary = [
    `${params.nombre} — ${path.basename(proyecto)}`,
    `${agentPick.label} · ${ctxPick.label}`,
    `${sched.human} · ${sched.zona}`,
    `${perfilPick.label} · ${politicaPick.label}`,
    params.presupuesto ? t('Cap {0} USD per run', params.presupuesto.toFixed(2)) : t('No spending cap'),
  ].join('\n');
  const go = await vscode.window.showQuickPick(
    [
      { label: t('Create'), detail: t('Registers the schedule with the operating system.'), runNow: false },
      { label: t('Create and run now'), detail: t('Also launches a test run right away, without touching the schedule.'), runNow: true },
    ],
    { title: t('Ready?'), placeHolder: summary.replace(/\n/g, '  ·  '), ignoreFocusOut: true },
  );
  if (!go) return undefined;
  void ctl; // reserved for future validation calls
  return { params, runNow: go.runNow };
}

async function pickProject(): Promise<string | undefined> {
  const folders = vscode.workspace.workspaceFolders ?? [];
  const items = folders.map((f) => ({ label: f.name, description: f.uri.fsPath, path: f.uri.fsPath }));
  const browse = { label: t('Browse…'), description: '', path: '' };
  const pick = await vscode.window.showQuickPick([...items, browse], { title: t('New task (2/7) · Repository'), ignoreFocusOut: true });
  if (!pick) return undefined;
  let p = pick.path;
  if (!p) {
    const sel = await vscode.window.showOpenDialog({ canSelectFolders: true, canSelectFiles: false, canSelectMany: false, title: t('Choose the Git repository') });
    if (!sel || !sel.length) return undefined;
    p = sel[0].fsPath;
  }
  if (!fs.existsSync(path.join(p, '.git'))) {
    const again = await vscode.window.showWarningMessage(t('{0} does not look like a Git repository. TaskKeeper needs Git to create an isolated worktree.', p), t('Choose another'));
    if (again) return pickProject();
    return undefined;
  }
  return p;
}

async function pickSession(agent: string, cwd: string): Promise<string | undefined> {
  if (agent === 'claude') {
    const found = listSessions(cwd);
    if (found.length) {
      const pick = await vscode.window.showQuickPick(
        [
          ...found.map((s) => ({ label: s.title || s.id, description: s.lastModified.toLocaleString(), detail: s.id, id: s.id })),
          { label: t('Type a session id…'), description: '', detail: '', id: '' },
        ],
        { title: t('Which conversation?'), matchOnDetail: true, ignoreFocusOut: true },
      );
      if (!pick) return undefined;
      if (pick.id) return pick.id;
    }
  }
  return vscode.window.showInputBox({
    title: t('Session id'),
    prompt: t('Paste the conversation id to continue from'),
    ignoreFocusOut: true,
    validateInput: (v) => (v.trim().length < 6 ? t('That does not look like an id') : undefined),
  });
}

interface SchedulePick {
  regla: 'daily' | 'weekly' | 'once';
  hora: string;
  dias?: number[];
  zona: string;
  human: string;
}

async function pickSchedule(defaultTz: string): Promise<SchedulePick | undefined> {
  const kind = await vscode.window.showQuickPick(
    [
      { label: t('Every day'), regla: 'daily' as const },
      { label: t('Some days of the week'), regla: 'weekly' as const },
      { label: t('Once, on a date'), regla: 'once' as const },
    ],
    { title: t('New task (6/7) · When'), ignoreFocusOut: true },
  );
  if (!kind) return undefined;

  let dias: number[] | undefined;
  if (kind.regla === 'weekly') {
    const picks = await vscode.window.showQuickPick(
      ISO_WEEKDAYS.map((w) => ({ label: w.long, iso: w.iso, picked: w.iso === 1 })),
      { title: t('Which days?'), canPickMany: true, ignoreFocusOut: true },
    );
    if (!picks || !picks.length) return undefined;
    dias = picks.map((p) => p.iso);
  }

  const hora = await vscode.window.showInputBox({
    title: kind.regla === 'once' ? t('Date and time') : t('Time'),
    prompt: kind.regla === 'once' ? t('Local date and time, like 2026-09-01T03:00') : t('Local time, 24 h, like 03:00'),
    value: kind.regla === 'once' ? '' : '03:00',
    placeHolder: kind.regla === 'once' ? '2026-09-01T03:00' : '03:00',
    ignoreFocusOut: true,
    validateInput: (v) => (kind.regla === 'once' ? (isValidLocalDateTime(v) ? undefined : t('Use YYYY-MM-DDTHH:MM')) : isValidHHMM(v) ? undefined : t('Use HH:MM')),
  });
  if (!hora) return undefined;

  const zona = await vscode.window.showInputBox({
    title: t('Time zone'),
    prompt: t('IANA name. Task times are interpreted in this zone, including daylight-saving changes.'),
    value: defaultTz,
    ignoreFocusOut: true,
    validateInput: (v) => (/^[A-Za-z_]+\/[A-Za-z_\-/]+$|^UTC$/.test(v.trim()) ? undefined : t('Something like Europe/Madrid')),
  });
  if (!zona) return undefined;

  const human =
    kind.regla === 'daily'
      ? t('daily at {0}', hora)
      : kind.regla === 'weekly'
        ? t('{0} at {1}', (dias ?? []).map((d) => ISO_WEEKDAYS.find((w) => w.iso === d)?.short ?? d).join(', '), hora)
        : t('once, {0}', hora);
  return { regla: kind.regla, hora: hora.trim(), dias, zona: zona.trim(), human };
}
