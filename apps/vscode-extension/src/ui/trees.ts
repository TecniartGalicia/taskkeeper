import * as vscode from 'vscode';
import type { Ctl } from '../core/ctl';
import { formatCost, isActive, runContext, shortTime, stateIcon, type Run, type Task } from '../core/model';
import { describeRule, parseRule } from '../core/schedule';

const t = vscode.l10n.t;

// ---------- shared ----------

export class RunItem extends vscode.TreeItem {
  constructor(readonly run: Run, kind: 'inbox' | 'history') {
    super(run.tarea, run.ficheros.length ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None);
    this.id = `${kind}:${run.id}`;
    this.contextValue = runContext(run);
    const ic = stateIcon(run.estado);
    this.iconPath = new vscode.ThemeIcon(ic.id, ic.color ? new vscode.ThemeColor(ic.color) : undefined);
    this.description = describeRun(run);
    this.tooltip = tooltipRun(run);
    this.command = { command: 'taskkeeper.showRun', title: t('Show Run Details'), arguments: [run.id] };
  }
}

export class FileItem extends vscode.TreeItem {
  constructor(readonly run: Run, readonly file: string) {
    super(file, vscode.TreeItemCollapsibleState.None);
    this.id = `file:${run.id}:${file}`;
    this.contextValue = 'run.file';
    this.iconPath = vscode.ThemeIcon.File;
    this.resourceUri = vscode.Uri.file(`${run.worktree}/${file}`);
    this.command = { command: 'taskkeeper.openFileDiff', title: t('Show File Diff'), arguments: [run.id, file] };
  }
}

export class TaskItem extends vscode.TreeItem {
  constructor(readonly task: Task) {
    super(task.nombre, vscode.TreeItemCollapsibleState.None);
    this.id = `task:${task.id}`;
    this.contextValue = task.activa ? 'task.active' : 'task.paused';
    this.iconPath = new vscode.ThemeIcon(task.activa ? 'calendar' : 'debug-pause', task.activa ? undefined : new vscode.ThemeColor('descriptionForeground'));
    const rule = describeRule(parseRule(task.regla), t);
    this.description = task.activa
      ? t('{0} · {1} · next {2}', task.agente, rule, task.proxima_local || '—')
      : t('{0} · {1} · paused', task.agente, rule);
    this.tooltip = new vscode.MarkdownString(
      [
        `**${task.nombre}** — ${task.proyecto}`,
        '',
        `- ${t('Agent')}: ${task.agente} · ${t('mode')}: ${task.modo}`,
        `- ${t('Schedule')}: ${rule} (${task.zona})`,
        `- ${t('Permissions')}: ${task.perfil}`,
        `- ${t('If missed')}: ${task.politica}`,
        task.ultimo_estado ? `- ${t('Last run')}: ${task.ultimo_estado} (${shortTime(task.ultima_ejecucion)})` : '',
        '',
        '```',
        task.prompt.length > 400 ? `${task.prompt.slice(0, 400)}…` : task.prompt,
        '```',
      ].join('\n'),
    );
  }
}

export class InfoItem extends vscode.TreeItem {
  constructor(label: string, icon = 'info') {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.iconPath = new vscode.ThemeIcon(icon);
    this.contextValue = 'info';
  }
}

function describeRun(r: Run): string {
  const parts: string[] = [];
  const when = shortTime(r.fin || r.inicio || r.prevista_utc);
  if (isActive(r.estado)) parts.push(t('running'));
  else parts.push(stateLabel(r.estado));
  if (when) parts.push(when);
  if (r.ficheros.length) parts.push(t('{0} file(s)', r.ficheros.length));
  const c = formatCost(r.coste_usd);
  if (c) parts.push(c);
  return parts.join(' · ');
}

export function stateLabel(s: string): string {
  switch (s) {
    case 'awaiting_review':
      return t('waiting for review');
    case 'accepted':
      return t('accepted');
    case 'rejected':
      return t('rejected');
    case 'completed':
      return t('Done');
    case 'skipped':
      return t('skipped');
    case 'failed':
      return t('failed');
    case 'failed_verification':
      return t('checks failed');
    case 'failed_quota':
      return t('quota exhausted');
    case 'failed_auth':
      return t('sign in to the agent');
    case 'cancelled':
      return t('cancelled');
    case 'queued':
      return t('queued');
    case 'preflight':
      return t('preparing');
    case 'running':
      return t('running');
    case 'verifying':
      return t('verifying');
    default:
      return s;
  }
}

function tooltipRun(r: Run): vscode.MarkdownString {
  const lines = [
    `**${r.tarea}** — ${r.proyecto} · ${r.agente}`,
    '',
    `- ${t('State')}: ${stateLabel(r.estado)}${r.codigo_error ? ` (${r.codigo_error})` : ''}`,
    `- ${t('Scheduled')}: ${r.prevista_utc}`,
    r.inicio ? `- ${t('Started')}: ${r.inicio}` : '',
    r.fin ? `- ${t('Finished')}: ${r.fin}` : '',
    r.rama ? `- ${t('Branch')}: \`${r.rama}\`` : '',
    r.commit_base ? `- ${t('Base commit')}: \`${r.commit_base.slice(0, 10)}\`` : '',
    r.coste_usd != null ? `- ${t('Cost')}: ${formatCost(r.coste_usd)}` : '',
  ].filter(Boolean);
  const md = new vscode.MarkdownString(lines.join('\n'));
  md.isTrusted = false;
  return md;
}

// ---------- providers ----------

type InboxNode = RunItem | FileItem | InfoItem;

export class InboxProvider implements vscode.TreeDataProvider<InboxNode> {
  private readonly emitter = new vscode.EventEmitter<InboxNode | undefined>();
  readonly onDidChangeTreeData = this.emitter.event;
  private cache: Run[] = [];
  private lastError: string | undefined;

  constructor(private readonly ctl: () => Ctl | undefined) {}

  refresh(): void {
    this.emitter.fire(undefined);
  }
  get runs(): Run[] {
    return this.cache;
  }

  getTreeItem(el: InboxNode): vscode.TreeItem {
    return el;
  }

  async getChildren(el?: InboxNode): Promise<InboxNode[]> {
    if (el instanceof RunItem) return el.run.ficheros.map((f) => new FileItem(el.run, f));
    if (el) return [];
    const c = this.ctl();
    if (!c) return [new InfoItem(t('TaskKeeper is not ready on this platform yet.'), 'warning')];
    try {
      this.cache = await c.inbox();
      this.lastError = undefined;
    } catch (e) {
      this.lastError = (e as Error).message;
      return [new InfoItem(t('Could not read the inbox: {0}', this.lastError), 'error')];
    }
    return this.cache.map((r) => new RunItem(r, 'inbox'));
  }
}

export class HistoryProvider implements vscode.TreeDataProvider<InboxNode> {
  private readonly emitter = new vscode.EventEmitter<InboxNode | undefined>();
  readonly onDidChangeTreeData = this.emitter.event;
  constructor(private readonly ctl: () => Ctl | undefined) {}
  refresh(): void {
    this.emitter.fire(undefined);
  }
  getTreeItem(el: InboxNode): vscode.TreeItem {
    return el;
  }
  async getChildren(el?: InboxNode): Promise<InboxNode[]> {
    if (el instanceof RunItem) return el.run.ficheros.map((f) => new FileItem(el.run, f));
    if (el) return [];
    const c = this.ctl();
    if (!c) return [];
    try {
      const runs = await c.history(100);
      return runs.map((r) => new RunItem(r, 'history'));
    } catch (e) {
      return [new InfoItem(t('Could not read the history: {0}', (e as Error).message), 'error')];
    }
  }
}

export class TasksProvider implements vscode.TreeDataProvider<TaskItem | InfoItem> {
  private readonly emitter = new vscode.EventEmitter<TaskItem | InfoItem | undefined>();
  readonly onDidChangeTreeData = this.emitter.event;
  private cache: Task[] = [];
  constructor(private readonly ctl: () => Ctl | undefined) {}
  refresh(): void {
    this.emitter.fire(undefined);
  }
  get tasks(): Task[] {
    return this.cache;
  }
  getTreeItem(el: TaskItem | InfoItem): vscode.TreeItem {
    return el;
  }
  async getChildren(el?: TaskItem | InfoItem): Promise<(TaskItem | InfoItem)[]> {
    if (el) return [];
    const c = this.ctl();
    if (!c) return [new InfoItem(t('TaskKeeper is not ready on this platform yet.'), 'warning')];
    try {
      this.cache = await c.tasks();
    } catch (e) {
      return [new InfoItem(t('Could not read the tasks: {0}', (e as Error).message), 'error')];
    }
    return this.cache.map((tk) => new TaskItem(tk));
  }
}
