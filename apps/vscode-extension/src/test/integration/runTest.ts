import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { runTests } from '@vscode/test-electron';

/**
 * Hermetic integration run: a temp data dir (TASKKEEPER_HOME) so the suite
 * never touches the developer's real tasks or Task Scheduler entries beyond
 * the ones it creates and removes itself, an isolated user-data-dir, and no
 * other extensions.
 */
async function main(): Promise<void> {
  // From a terminal inside VS Code the extension host sets ELECTRON_RUN_AS_NODE=1;
  // the test VS Code would inherit it and start as plain Node.
  delete process.env.ELECTRON_RUN_AS_NODE;

  const extensionDevelopmentPath = path.resolve(__dirname, '../../../');
  const extensionTestsPath = path.resolve(__dirname, './suite/index');
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'taskkeeper-it-'));
  const home = path.join(tmp, 'home');
  const workspace = path.join(tmp, 'workspace');
  fs.mkdirSync(home, { recursive: true });
  fs.mkdirSync(workspace, { recursive: true });

  const vscodeExecutablePath = process.env.TASKKEEPER_VSCODE_EXE || undefined;
  try {
    await runTests({
      ...(vscodeExecutablePath ? { vscodeExecutablePath } : {}),
      extensionDevelopmentPath,
      extensionTestsPath,
      launchArgs: [workspace, `--user-data-dir=${path.join(extensionDevelopmentPath, '.vscode-test', 'user-data')}`, '--disable-extensions'],
      extensionTestsEnv: { TASKKEEPER_HOME: home, TASKKEEPER_IT_TMP: tmp },
    });
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

main().catch((err) => {
  console.error('Integration tests failed', err);
  process.exit(1);
});
