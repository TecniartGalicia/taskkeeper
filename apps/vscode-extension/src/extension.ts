import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import { ensureInstalled, type Binaries } from './core/binaries';
import { Ctl, CtlError } from './core/ctl';
import { isActive, type Run } from './core/model';
import { watchMarker } from './core/watch';
import { BASE_SCHEME, BaseContentProvider, RUN_SCHEME, RunDetailsProvider, openFileDiff, openRunDiff, showRunDetails } from './ui/diff';
import { FileItem, HistoryProvider, InboxProvider, RunItem, TaskItem, TasksProvider, stateLabel } from './ui/trees';
import { runNewTaskWizard } from './ui/wizard';

const t = vscode.l10n.t;

let ctl: Ctl | undefined;
let bins: Binaries | undefined;
let out: vscode.OutputChannel;
const seen = new Map<string, string>(); // runId → last state, for notifications

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  out = vscode.window.createOutputChannel('TaskKeeper');
  context.subscriptions.push(out);
  const log = (s: string) => out.appendLine(`${new Date().toISOString()} ${s}`);

  // 1. Binaries: install to the stable folder and check the version matches.
  const version = (context.extension.packageJSON as { version: string }).version;
  try {
    bins = ensureInstalled(context.extensionPath, log);
    ctl = new Ctl({ exe: bins.ctl, env: envFor() });
    const v = await ctl.version();
    log(`taskkeeper-ctl ${v.version} at ${bins.ctl}`);
    if (v.version !== version && v.version !== 'dev') {
      log(`version mismatch: extension ${version}, ctl ${v.version} (a running worker may have blocked the update; will retry next start)`);
    }
  } catch (e) {
    log(`binaries unavailable: ${(e as Error).message}`);
    ctl = undefined;
  }

  // 2. Views.
  const inbox = new InboxProvider(() => ctl);
  const tasks = new TasksProvider(() => ctl);
  const history = new HistoryProvider(() => ctl);
  const inboxView = vscode.window.createTreeView('taskkeeper.inbox', { treeDataProvider: inbox, showCollapseAll: false });
  const tasksView = vscode.window.createTreeView('taskkeeper.tasks', { treeDataProvider: tasks });
  const historyView = vscode.window.createTreeView('taskkeeper.history', { treeDataProvider: history });
  context.subscriptions.push(inboxView, tasksView, historyView);

  const details = new RunDetailsProvider(() => ctl);
  context.subscriptions.push(
    vscode.workspace.registerTextDocumentContentProvider(BASE_SCHEME, new BaseContentProvider()),
    vscode.workspace.registerTextDocumentContentProvider(RUN_SCHEME, details),
  );

  const refreshAll = () => {
    inbox.refresh();
    tasks.refresh();
    history.refresh();
    void updateBadge(inbox, inboxView);
  };

  // 3. Live updates: watch the marker file the worker touches.
  if (ctl) {
    try {
      const env = await ctl.environment();
      log(`data dir ${env.raiz}`);
      context.subscriptions.push({ dispose: watchMarker(env.marca, () => onMarker(inbox, refreshAll)) });
      if (env.aviso_reactivacion) log(`wake: ${env.aviso_reactivacion}`);
    } catch (e) {
      log(`environment: ${(e as Error).message}`);
    }
  }

  // 4. Commands.
  const reg = (id: string, fn: (...a: any[]) => unknown) =>
    context.subscriptions.push(vscode.commands.registerCommand(id, async (...a: any[]) => {
      try {
        return await fn(...a);
      } catch (e) {
        const msg = e instanceof CtlError ? e.message : (e as Error).message ?? String(e);
        log(`${id}: ${msg}`);
        vscode.window.showErrorMessage(t('TaskKeeper: {0}', msg));
        return undefined;
      }
    }));

  reg('taskkeeper.refresh', refreshAll);

  reg('taskkeeper.newTask', async () => {
    const c = need();
    if (!vscode.workspace.isTrusted) {
      vscode.window.showWarningMessage(t('Creating a task runs an agent on that repository, so it needs a trusted workspace.'));
      return;
    }
    const agents = await c.agents();
    const cfg = vscode.workspace.getConfiguration('taskkeeper');
    const res = await runNewTaskWizard(c, agents, cfg.get<string>('defaultTimezone', ''));
    if (!res) return;
    const created = await c.create(res.params);
    refreshAll();
    for (const a of created.avisos) vscode.window.showWarningMessage(t('TaskKeeper: {0}', a));
    if (res.runNow) {
      await c.runNow(created.id);
      vscode.window.showInformationMessage(t('Task created and launched. Next scheduled run: {0}. Watch the "Last night" view.', created.next_run_local));
    } else {
      vscode.window.showInformationMessage(t('Task created. Next run: {0}.', created.next_run_local));
    }
  });

  reg('taskkeeper.runNow', async (item?: TaskItem) => {
    const c = need();
    const id = item?.task.id ?? (await pickTaskId(c));
    if (!id) return;
    await c.runNow(id);
    vscode.window.showInformationMessage(t('Launched in the background. It will show up in "Last night" as it progresses.'));
    setTimeout(refreshAll, 800);
  });

  reg('taskkeeper.pauseTask', async (item?: TaskItem) => {
    const c = need();
    const id = item?.task.id ?? (await pickTaskId(c));
    if (!id) return;
    await c.pause(id);
    refreshAll();
  });
  reg('taskkeeper.resumeTask', async (item?: TaskItem) => {
    const c = need();
    const id = item?.task.id ?? (await pickTaskId(c));
    if (!id) return;
    await c.resume(id);
    refreshAll();
  });

  reg('taskkeeper.editPrompt', async (item?: TaskItem) => {
    const c = need();
    const id = item?.task.id ?? (await pickTaskId(c));
    if (!id) return;
    const task = await c.task(id);
    // The prompt is edited in a real editor: multi-line, with the user's own
    // keybindings. Saving is explicit through the notification action.
    const doc = await vscode.workspace.openTextDocument({ language: 'markdown', content: task.prompt });
    await vscode.window.showTextDocument(doc, { preview: false });
    const choice = await vscode.window.showInformationMessage(
      t('Editing the prompt of "{0}". When you are done, choose Save prompt.', task.nombre),
      { modal: false },
      t('Save prompt'),
      t('Discard'),
    );
    if (choice === t('Save prompt')) {
      const text = doc.getText();
      await c.edit(id, { prompt: text });
      refreshAll();
      vscode.window.showInformationMessage(t('Prompt saved as a new revision. Earlier runs keep the one they used.'));
    }
  });

  reg('taskkeeper.deleteTask', async (item?: TaskItem) => {
    const c = need();
    const id = item?.task.id ?? (await pickTaskId(c));
    if (!id) return;
    const name = item?.task.nombre ?? id;
    const ok = await vscode.window.showWarningMessage(t('Delete "{0}" and its schedule? Past runs and their worktrees are kept.', name), { modal: true }, t('Delete'));
    if (ok !== t('Delete')) return;
    await c.delete(id);
    refreshAll();
  });

  reg('taskkeeper.accept', async (item?: RunItem) => {
    const c = need();
    const run = item?.run ?? (await pickRun(inbox.runs, 'awaiting_review'));
    if (!run) return;
    const ok = await vscode.window.showInformationMessage(
      t('Merge "{0}" into {1} on this machine? Nothing is pushed. {2} file(s) will change.', run.tarea, run.rama_base, run.ficheros.length),
      { modal: true },
      t('Accept'),
    );
    if (ok !== t('Accept')) return;
    await c.accept(run.id);
    refreshAll();
    vscode.window.showInformationMessage(t('Accepted into {0}. Review, then push when you decide.', run.rama_base));
  });

  reg('taskkeeper.reject', async (item?: RunItem) => {
    const c = need();
    const run = item?.run ?? (await pickRun(inbox.runs, 'awaiting_review'));
    if (!run) return;
    const ok = await vscode.window.showWarningMessage(t('Discard the work of "{0}"? The worktree and its branch are deleted; your repository is untouched.', run.tarea), { modal: true }, t('Discard'));
    if (ok !== t('Discard')) return;
    await c.reject(run.id);
    refreshAll();
  });

  reg('taskkeeper.cancel', async (item?: RunItem) => {
    const c = need();
    const run = item?.run ?? (await pickRun(inbox.runs, 'active'));
    if (!run) return;
    await c.cancel(run.id);
    vscode.window.showInformationMessage(t('Cancellation requested. The agent and its processes stop within a few seconds.'));
    setTimeout(refreshAll, 3500);
  });

  reg('taskkeeper.openDiff', async (item?: RunItem) => {
    const c = need();
    const run = item?.run ?? (await pickRun(inbox.runs, 'any'));
    if (!run) return;
    await openRunDiff(await c.run(run.id));
  });
  reg('taskkeeper.openFileDiff', async (a?: string | FileItem, file?: string) => {
    const c = need();
    const runId = a instanceof FileItem ? a.run.id : a;
    const f = a instanceof FileItem ? a.file : file;
    if (!runId || !f) return;
    await openFileDiff(await c.run(runId), f);
  });
  reg('taskkeeper.showRun', async (a?: string | RunItem) => {
    const id = a instanceof RunItem ? a.run.id : a;
    if (!id) return;
    details.fire(id);
    await showRunDetails(id);
  });
  reg('taskkeeper.openWorktree', async (item?: RunItem) => {
    const run = item?.run;
    if (!run?.worktree || !fs.existsSync(run.worktree)) {
      vscode.window.showInformationMessage(t('This run has no worktree on disk any more.'));
      return;
    }
    await vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(run.worktree));
  });

  reg('taskkeeper.setConcurrency', async () => {
    const c = need();
    const cur = await c.concurrency();
    const v = await vscode.window.showInputBox({
      title: t('Concurrent runs on this machine'),
      prompt: t('How many tasks may run at the same time. One is the safe default: two agents compete for CPU, disk and your provider quota.'),
      value: String(cur.cupo),
      validateInput: (s) => (/^[1-9]\d?$/.test(s.trim()) ? undefined : t('A number from 1 to 99')),
    });
    if (!v) return;
    await c.setConcurrency(Number(v));
    vscode.window.showInformationMessage(t('Concurrent runs set to {0}. The next run already respects it.', v));
  });

  reg('taskkeeper.showEnvironment', async () => {
    const c = need();
    const env = await c.environment();
    const agents = await c.agents();
    out.show(true);
    out.appendLine('--- environment ---');
    out.appendLine(`extension ${version} · ctl ${env.version}`);
    out.appendLine(`data     ${env.raiz}`);
    out.appendLine(`worker   ${env.worker}`);
    for (const a of agents) out.appendLine(`${a.nombre.padEnd(8)} ${a.instalado ? `v${a.version}  ${a.ruta}` : t('not found')}`);
    if (env.aviso_reactivacion) out.appendLine(`wake     ${env.aviso_reactivacion}`);
  });

  refreshAll();
  log(`activated ${version}`);
}

export function deactivate(): void {
  /* nothing residual: no daemon */
}

// ---------- helpers ----------

function need(): Ctl {
  if (!ctl) throw new Error(t('the bundled binaries are not available on this platform yet ({0}-{1})', process.platform, process.arch));
  return ctl;
}

function envFor(): NodeJS.ProcessEnv {
  const cfg = vscode.workspace.getConfiguration('taskkeeper');
  const dir = cfg.get<string>('dataDir', '').trim();
  return dir ? { TASKKEEPER_HOME: dir } : {};
}

async function pickTaskId(c: Ctl): Promise<string | undefined> {
  const list = await c.tasks();
  if (!list.length) {
    vscode.window.showInformationMessage(t('There are no tasks yet.'));
    return undefined;
  }
  const pick = await vscode.window.showQuickPick(list.map((x) => ({ label: x.nombre, description: `${x.agente} · ${x.proyecto}`, id: x.id })), { title: t('Which task?') });
  return pick?.id;
}

async function pickRun(runs: Run[], filter: 'awaiting_review' | 'active' | 'any'): Promise<Run | undefined> {
  const list = runs.filter((r) => (filter === 'any' ? true : filter === 'active' ? isActive(r.estado) : r.estado === filter));
  if (!list.length) {
    vscode.window.showInformationMessage(t('Nothing to choose from.'));
    return undefined;
  }
  const pick = await vscode.window.showQuickPick(list.map((r) => ({ label: r.tarea, description: `${stateLabel(r.estado)} · ${r.proyecto}`, run: r })), { title: t('Which run?') });
  return pick?.run;
}

async function updateBadge(inbox: InboxProvider, view: vscode.TreeView<unknown>): Promise<void> {
  // The badge counts what needs a decision; running items are not "pending" for you.
  const n = inbox.runs.filter((r) => r.estado === 'awaiting_review' || r.estado.startsWith('failed')).length;
  view.badge = n ? { value: n, tooltip: t('{0} run(s) waiting for you', n) } : undefined;
}

let markerTimer: NodeJS.Timeout | undefined;
function onMarker(inbox: InboxProvider, refreshAll: () => void): void {
  refreshAll();
  // After the refresh, notify about transitions the user cares about.
  if (markerTimer) clearTimeout(markerTimer);
  markerTimer = setTimeout(() => {
    const cfg = vscode.workspace.getConfiguration('taskkeeper');
    if (!cfg.get<boolean>('notifications', true)) return;
    for (const r of inbox.runs) {
      const prev = seen.get(r.id);
      seen.set(r.id, r.estado);
      if (prev === r.estado || prev === undefined) continue;
      if (r.estado === 'awaiting_review') {
        void vscode.window
          .showInformationMessage(t('"{0}" finished and is waiting for your review.', r.tarea), t('Show diff'), t('Later'))
          .then((a) => (a === t('Show diff') ? vscode.commands.executeCommand('taskkeeper.openDiff', new RunItem(r, 'inbox')) : undefined));
      } else if (r.estado === 'failed_auth') {
        void vscode.window.showWarningMessage(t('"{0}" could not run: sign in to {1} again.', r.tarea, r.agente));
      } else if (r.estado === 'failed_quota') {
        void vscode.window.showWarningMessage(t('"{0}" hit the {1} usage limit and will retry later.', r.tarea, r.agente));
      } else if (r.estado === 'failed' || r.estado === 'failed_verification') {
        void vscode.window.showWarningMessage(t('"{0}" failed ({1}). See the run details.', r.tarea, r.codigo_error || stateLabel(r.estado)));
      }
    }
  }, 900);
}

// Keep the import used even when no file-level code references it (esbuild tree-shakes safely).
void path;
