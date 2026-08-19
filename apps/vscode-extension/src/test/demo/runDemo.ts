/**
 * Launches a VS Code window with the extension loaded on a throwaway repo and
 * plays demo.ts while an external recorder captures the window. Only used to
 * produce the store screenshot; not a test.
 */
import { execFileSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { runTests } from '@vscode/test-electron';

function git(cwd: string, ...args: string[]): void {
  execFileSync('git', args, { cwd, stdio: 'ignore', env: { ...process.env, GIT_CONFIG_NOSYSTEM: '1' } });
}

async function main(): Promise<void> {
  delete process.env.ELECTRON_RUN_AS_NODE;
  const root = path.resolve(__dirname, '../../../');
  const out = process.env.TK_DEMO_OUT || path.join(os.tmpdir(), 'tk-demo-out');
  fs.mkdirSync(out, { recursive: true });
  for (const f of ['stop.flag', 'marks.txt']) fs.rmSync(path.join(out, f), { force: true });

  const home = path.join(out, 'home');
  fs.rmSync(home, { recursive: true, force: true });
  fs.mkdirSync(home, { recursive: true });

  const repo = path.join(out, 'shop-api');
  fs.rmSync(repo, { recursive: true, force: true });
  fs.mkdirSync(path.join(repo, 'src'), { recursive: true });
  fs.writeFileSync(path.join(repo, 'README.md'), '# shop-api\n\nDemo project.\n');
  fs.writeFileSync(path.join(repo, 'src', 'orders.py'), 'def create_order(items):\n    return {"items": items, "status": "new"}\n');
  git(repo, 'init', '-q', '-b', 'main');
  git(repo, 'config', 'user.email', 'demo@example.com');
  git(repo, 'config', 'user.name', 'Demo');
  git(repo, 'add', '-A');
  git(repo, 'commit', '-q', '-m', 'initial');

  const userDataDir = path.join(out, 'user-data');
  fs.mkdirSync(path.join(userDataDir, 'User'), { recursive: true });
  fs.copyFileSync(path.resolve(__dirname, '../../../src/test/demo/settings.json'), path.join(userDataDir, 'User', 'settings.json'));

  await runTests({
    extensionDevelopmentPath: root,
    extensionTestsPath: path.resolve(__dirname, './index'),
    launchArgs: [repo, `--user-data-dir=${userDataDir}`, '--disable-extensions', '--disable-workspace-trust', '--new-window'],
    extensionTestsEnv: { TASKKEEPER_HOME: home, TK_DEMO_OUT: out, TK_DEMO_REPO: repo },
  });
}

main().catch((e) => {
  console.error('demo failed', e);
  process.exit(1);
});
