// Builds the Go binaries into bin/<platform>-<arch>/ so `vsce package --target`
// ships exactly one platform per VSIX. The version is stamped from package.json
// so the extension can check it matches at activation.
//
//   node scripts/build-bin.mjs                 → host platform only
//   node scripts/build-bin.mjs --all           → win32-x64, darwin-x64, darwin-arm64
//   node scripts/build-bin.mjs win32-x64       → just that target (cross-compiled; used by CI)
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const REPO = path.resolve(ROOT, '..', '..');
const pkg = JSON.parse(fs.readFileSync(path.join(ROOT, 'package.json'), 'utf8'));

const TARGETS = {
  'win32-x64': { GOOS: 'windows', GOARCH: 'amd64', exe: '.exe' },
  'darwin-x64': { GOOS: 'darwin', GOARCH: 'amd64', exe: '' },
  'darwin-arm64': { GOOS: 'darwin', GOARCH: 'arm64', exe: '' },
};
const host = `${process.platform}-${process.arch}`;
const explicit = process.argv.slice(2).filter((a) => !a.startsWith('--'));
const wanted = process.argv.includes('--all')
  ? Object.keys(TARGETS)
  : explicit.length > 0
    ? explicit
    : [host];

for (const target of wanted) {
  const t = TARGETS[target];
  if (!t) {
    console.error(`no Go target for ${target}`);
    process.exit(1);
  }
  const outDir = path.join(ROOT, 'bin', target);
  fs.mkdirSync(outDir, { recursive: true });
  for (const [app, name] of [
    ['./apps/worker', 'taskkeeper-worker'],
    ['./apps/ctl', 'taskkeeper-ctl'],
  ]) {
    const out = path.join(outDir, `${name}${t.exe}`);
    execFileSync('go', ['build', '-trimpath', '-ldflags', `-s -w -X main.version=${pkg.version}`, '-o', out, app], {
      cwd: REPO,
      stdio: 'inherit',
      env: { ...process.env, GOOS: t.GOOS, GOARCH: t.GOARCH, CGO_ENABLED: '0', GOTOOLCHAIN: 'local' },
    });
    const kb = Math.round(fs.statSync(out).size / 1024);
    console.log(`${target}  ${name}${t.exe}  ${kb} KB`);
  }
}
