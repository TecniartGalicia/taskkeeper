/**
 * Scripted screenshot harness (not part of the test suites). Seeds a hermetic
 * TaskKeeper home with real scheduled tasks through the ctl binary, opens the
 * views, and pauses on each frame while an external window recorder captures.
 * Nothing is faked for the camera: the tasks are created through the same
 * command the product uses.
 */
import { execFileSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import { stableBinDir } from '../../core/binaries';

const OUT = process.env.TK_DEMO_OUT!;
const HOME = process.env.TASKKEEPER_HOME!;
const REPO = process.env.TK_DEMO_REPO!;
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
const mark = (name: string) => fs.appendFileSync(path.join(OUT, 'marks.txt'), `${Date.now()} ${name}\n`);

function ctl(...args: string[]): void {
  const exe = path.join(stableBinDir(), 'taskkeeper-ctl.exe');
  const out = execFileSync(exe, ['--json', ...args], { env: { ...process.env, TASKKEEPER_HOME: HOME } }).toString();
  const p = JSON.parse(out);
  if (!p.ok) throw new Error(p.error);
}

async function tidy(): Promise<void> {
  for (const c of ['workbench.action.closeAuxiliaryBar', 'notifications.clearAll', 'notifications.hideToasts', 'workbench.action.closePanel']) {
    await vscode.commands.executeCommand(c).then(undefined, () => undefined);
  }
}

export async function run(): Promise<void> {
  fs.mkdirSync(OUT, { recursive: true });
  const ext = vscode.extensions.getExtension('argalla.taskkeeper')!;
  await ext.activate();

  ctl('crear', '--proyecto', REPO, '--nombre', 'Weekly dependency review', '--agente', 'claude', '--prompt', 'Review dependencies, run the tests, and leave a report.', '--regla', 'weekly', '--dias', '1', '--hora', '03:00', '--perfil', 'auditoria');
  ctl('crear', '--proyecto', REPO, '--nombre', 'Nightly bug hunt', '--agente', 'claude', '--prompt', 'Hunt for bugs in changed files and prepare a fix in the worktree.', '--regla', 'daily', '--hora', '02:30', '--perfil', 'cambios_aislados');
  ctl('crear', '--proyecto', REPO, '--nombre', 'PR follow-up (Codex)', '--agente', 'codex', '--prompt', 'Check the open PR and address review comments.', '--regla', 'weekly', '--dias', '2,4', '--hora', '05:00', '--perfil', 'cambios_aislados');

  await wait(2500);
  await tidy();
  await vscode.commands.executeCommand('workbench.view.extension.taskkeeper');
  await vscode.commands.executeCommand('taskkeeper.refresh');
  await wait(3000);
  await tidy();
  mark('tasks');
  await wait(4000);

  fs.writeFileSync(path.join(OUT, 'stop.flag'), 'done');
  await wait(1500);
}
