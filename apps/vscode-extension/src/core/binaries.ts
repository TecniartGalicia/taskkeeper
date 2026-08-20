// Locates and installs the Go binaries.
//
// The VSIX ships taskkeeper-worker and taskkeeper-ctl under bin/<platform>/.
// They are COPIED to a stable folder on first activation and on every version
// change, and the extension always invokes ctl from that stable folder.
//
// Why not run them from the extension folder: the Windows Task Scheduler
// entries point at the worker by absolute path, and the extension folder
// changes name on every update (…/argalla.taskkeeper-0.1.0 → -0.2.0). We hit
// exactly that problem with claude and codex in Phase 0 — their path dies on
// every update. Ours must not.
import { execFileSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

export interface Binaries {
  ctl: string;
  worker: string;
  dir: string;
}

const EXE = process.platform === 'win32' ? '.exe' : '';

export function platformDirName(platform = process.platform, arch = process.arch): string {
  return `${platform}-${arch}`;
}

/** Stable per-user folder, independent of the extension version. */
export function stableBinDir(): string {
  if (process.platform === 'win32') {
    const base = process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData', 'Local');
    return path.join(base, 'Argalla', 'TaskKeeper', 'bin');
  }
  const base = process.env.XDG_DATA_HOME || path.join(os.homedir(), '.local', 'share');
  return path.join(base, 'argalla', 'taskkeeper', 'bin');
}

export function bundledBinDir(extensionPath: string): string {
  return path.join(extensionPath, 'bin', platformDirName());
}

function fileEqual(a: string, b: string): boolean {
  try {
    const sa = fs.statSync(a);
    const sb = fs.statSync(b);
    if (sa.size !== sb.size) return false;
    // Same size: compare bytes. Binaries are ~11 MB; this runs once per activation.
    return fs.readFileSync(a).equals(fs.readFileSync(b));
  } catch {
    return false;
  }
}

/**
 * Ensures the stable folder holds the binaries shipped with this extension
 * version. Returns the paths to use. Throws if the platform has no bundled
 * binaries (for example an unsupported OS).
 */
export function ensureInstalled(extensionPath: string, log: (s: string) => void = () => {}): Binaries {
  const src = bundledBinDir(extensionPath);
  const names = [`taskkeeper-ctl${EXE}`, `taskkeeper-worker${EXE}`];
  for (const n of names) {
    if (!fs.existsSync(path.join(src, n))) {
      throw new Error(`TaskKeeper does not ship binaries for ${platformDirName()} (missing ${n}).`);
    }
  }
  const dir = stableBinDir();
  fs.mkdirSync(dir, { recursive: true });
  for (const n of names) {
    const from = path.join(src, n);
    const to = path.join(dir, n);
    if (!fileEqual(from, to)) {
      try {
        // Copy to a temp name and rename: atomic on the same volume, and if the
        // old worker is running the rename fails instead of corrupting it.
        const tmp = `${to}.new`;
        fs.copyFileSync(from, tmp);
        try {
          fs.renameSync(tmp, to);
        } catch (e) {
          // On Windows a running exe cannot be replaced. Keep the old one; the
          // next activation retries. ctl still works because it is short-lived.
          fs.rmSync(tmp, { force: true });
          throw e;
        }
        log(`installed ${n} → ${to}`);
      } catch (e) {
        log(`could not update ${n}: ${(e as Error).message}`);
        if (!fs.existsSync(to)) throw e;
      }
    }
    // POSIX: the exec bit is not preserved through the VSIX, and can be lost by a
    // Time Machine / Migration Assistant copy, so ensure it EVERY activation — not
    // only when the file changed. macOS: the binaries are Developer ID signed but
    // not notarized, so strip any quarantine attribute (best-effort).
    if (process.platform !== 'win32') {
      try {
        fs.chmodSync(to, 0o755);
      } catch (e) {
        log(`chmod ${n}: ${(e as Error).message}`);
      }
      if (process.platform === 'darwin') {
        try {
          execFileSync('xattr', ['-d', 'com.apple.quarantine', to], { stdio: 'ignore' });
        } catch {
          /* not quarantined: nothing to remove */
        }
      }
    }
  }
  return { ctl: path.join(dir, `taskkeeper-ctl${EXE}`), worker: path.join(dir, `taskkeeper-worker${EXE}`), dir };
}
