import * as assert from 'assert';
import { execFileSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import { stableBinDir } from '../../../core/binaries';
import { Ctl } from '../../../core/ctl';

const EXT_ID = 'argalla.taskkeeper';

function git(cwd: string, ...args: string[]): void {
  execFileSync('git', args, { cwd, stdio: 'ignore' });
}

describe('TaskKeeper extension', function () {
  this.timeout(90_000);

  it('activates and registers its commands', async () => {
    const ext = vscode.extensions.getExtension(EXT_ID);
    assert.ok(ext, `extension ${EXT_ID} not found`);
    await ext!.activate();
    const cmds = await vscode.commands.getCommands(true);
    for (const c of ['taskkeeper.newTask', 'taskkeeper.refresh', 'taskkeeper.accept', 'taskkeeper.reject', 'taskkeeper.runNow', 'taskkeeper.openDiff', 'taskkeeper.showEnvironment']) {
      assert.ok(cmds.includes(c), `command ${c} not registered`);
    }
  });

  it('installs the binaries to the stable folder and they answer', async () => {
    const ext = vscode.extensions.getExtension(EXT_ID)!;
    await ext.activate();
    const dir = stableBinDir();
    const exe = path.join(dir, process.platform === 'win32' ? 'taskkeeper-ctl.exe' : 'taskkeeper-ctl');
    assert.ok(fs.existsSync(exe), `ctl not installed at ${exe}`);
    const out = execFileSync(exe, ['--json', 'version'], { env: { ...process.env } }).toString();
    const parsed = JSON.parse(out);
    assert.strictEqual(parsed.ok, true);
    assert.strictEqual(parsed.data.version, ext.packageJSON.version, 'installed ctl version must match the extension');
  });

  it('creates, lists and deletes a task through ctl using the hermetic data dir', async function () {
    if (process.platform !== 'win32') this.skip(); // the trigger registration is Windows-only in this release
    const home = process.env.TASKKEEPER_HOME!;
    const tmp = process.env.TASKKEEPER_IT_TMP!;
    const repo = path.join(tmp, 'repo');
    fs.mkdirSync(repo, { recursive: true });
    git(repo, 'init', '-q', '-b', 'main');
    git(repo, '-c', 'user.name=t', '-c', 'user.email=t@l', 'commit', '-q', '--allow-empty', '-m', 'init');

    const ctl = new Ctl({ exe: path.join(stableBinDir(), 'taskkeeper-ctl.exe'), env: { TASKKEEPER_HOME: home } });

    const created = await ctl.create({
      proyecto: repo,
      nombre: 'integration test',
      agente: 'claude',
      prompt: 'do nothing',
      regla: 'daily',
      hora: '03:33',
      zona: 'Europe/Madrid',
      perfil: 'auditoria',
    });
    try {
      assert.ok(created.id);
      const list = await ctl.tasks();
      assert.strictEqual(list.length, 1);
      assert.strictEqual(list[0].nombre, 'integration test');
      assert.strictEqual(list[0].proxima_local.slice(11), '03:33');
      const inbox = await ctl.inbox();
      assert.deepStrictEqual(inbox, []);
      // The environment points at our hermetic home, never the real one.
      const env = await ctl.environment();
      assert.strictEqual(path.resolve(env.raiz).toLowerCase(), path.resolve(home).toLowerCase());
    } finally {
      await ctl.delete(created.id);
    }
    assert.strictEqual((await ctl.tasks()).length, 0);
  });

  it('shows the views without throwing', async () => {
    await vscode.commands.executeCommand('workbench.view.extension.taskkeeper');
    await vscode.commands.executeCommand('taskkeeper.refresh');
  });
});
