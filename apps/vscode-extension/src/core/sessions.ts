// Discovers Claude Code sessions for the "resume / fork" picker.
//
// The Agent SDK's listSessions() would be the sanctioned way, but its licence
// is "all rights reserved, subject to the legal agreements", so we do not
// redistribute it inside the VSIX (see TUS-TAREAS.md). What we rely on instead
// is only what Anthropic documents publicly for its own users: sessions live
// under ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl (or under
// $CLAUDE_CONFIG_DIR). The file name is the session id and the mtime is when
// it was last touched — no interpretation of the transcript format needed.
//
// To show a title we peek at the transcript for the first user message; if
// that ever fails, the picker still works showing id + date. Nothing else
// depends on this.
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

export interface DiscoveredSession {
  id: string;
  cwd: string;
  lastModified: Date;
  title: string; // first user prompt or '' if unreadable
  file: string;
}

export function claudeConfigDir(env: NodeJS.ProcessEnv = process.env): string {
  return env.CLAUDE_CONFIG_DIR || path.join(os.homedir(), '.claude');
}

/** Same rule Claude Code documents: every non-alphanumeric char → '-'. */
export function encodeCwd(cwd: string): string {
  return cwd.replace(/[^A-Za-z0-9]/g, '-');
}

/**
 * Lists sessions whose project folder matches `cwd`. The folder keeps the case
 * of the cwd the session was created with, so the match is case-insensitive.
 */
export function listSessions(cwd: string, env: NodeJS.ProcessEnv = process.env, limit = 50): DiscoveredSession[] {
  const root = path.join(claudeConfigDir(env), 'projects');
  let dirs: string[];
  try {
    dirs = fs.readdirSync(root);
  } catch {
    return [];
  }
  const want = encodeCwd(cwd).toLowerCase();
  const out: DiscoveredSession[] = [];
  for (const d of dirs) {
    // Long cwds are truncated to 200 chars plus a hash by Claude Code, so we
    // match on the prefix rather than equality.
    const dl = d.toLowerCase();
    if (dl !== want && !(want.length > 200 && dl.startsWith(want.slice(0, 200)))) continue;
    const full = path.join(root, d);
    let files: string[];
    try {
      files = fs.readdirSync(full);
    } catch {
      continue;
    }
    for (const f of files) {
      if (!f.endsWith('.jsonl')) continue;
      const id = f.slice(0, -'.jsonl'.length);
      if (!/^[0-9a-f-]{36}$/i.test(id)) continue;
      const file = path.join(full, f);
      let st: fs.Stats;
      try {
        st = fs.statSync(file);
      } catch {
        continue;
      }
      out.push({ id, cwd, lastModified: st.mtime, title: '', file });
    }
  }
  out.sort((a, b) => b.lastModified.getTime() - a.lastModified.getTime());
  const top = out.slice(0, limit);
  for (const s of top) s.title = peekTitle(s.file);
  return top;
}

export interface SessionLocation {
  cwd: string; // the real folder the session lives in
  file: string;
  lastModified: Date;
}

/**
 * Finds the folder(s) a session id lives in, so the panel can set the project
 * from the conversation instead of asking the user to pick it.
 *
 * A session file is stored under encodeCwd(cwd), and that encoding is lossy
 * (every non-alphanumeric char becomes '-'), so the folder name cannot be
 * decoded directly. Instead we confirm a candidate: inside the transcript, the
 * `cwd` values are recorded, and the real folder is the one whose encoding
 * equals the folder name. Usually one folder; a conversation resumed across
 * folders can have several (newest first).
 */
export function foldersForSession(sessionId: string, env: NodeJS.ProcessEnv = process.env): SessionLocation[] {
  if (!/^[0-9a-f-]{6,}$/i.test(sessionId)) return [];
  const root = path.join(claudeConfigDir(env), 'projects');
  let dirs: string[];
  try {
    dirs = fs.readdirSync(root);
  } catch {
    return [];
  }
  const out: SessionLocation[] = [];
  for (const d of dirs) {
    const file = path.join(root, d, `${sessionId}.jsonl`);
    let st: fs.Stats;
    try {
      st = fs.statSync(file);
    } catch {
      continue;
    }
    const cwd = cwdMatchingDir(file, d, st.size);
    if (cwd && isDir(cwd)) out.push({ cwd, file, lastModified: st.mtime });
  }
  out.sort((a, b) => b.lastModified.getTime() - a.lastModified.getTime());
  return out;
}

function isDir(p: string): boolean {
  try {
    return fs.statSync(p).isDirectory();
  } catch {
    return false;
  }
}

/** Scans a transcript for a `cwd` whose encoding matches the folder name. */
function cwdMatchingDir(file: string, dirName: string, fileSize: number, maxBytes = 20 * 1024 * 1024): string | undefined {
  const want = dirName.toLowerCase();
  let text: string;
  try {
    const fd = fs.openSync(file, 'r');
    try {
      const size = Math.min(fileSize, maxBytes);
      const buf = Buffer.alloc(size);
      // readSync can return a short read; loop until the buffer is filled.
      let read = 0;
      while (read < size) {
        const n = fs.readSync(fd, buf, read, size - read, read);
        if (n <= 0) break;
        read += n;
      }
      text = buf.subarray(0, read).toString('utf8');
    } finally {
      fs.closeSync(fd);
    }
  } catch {
    return undefined;
  }
  const seen = new Set<string>();
  for (const line of text.split('\n')) {
    if (!line.includes('"cwd"')) continue;
    let obj: any;
    try {
      obj = JSON.parse(line);
    } catch {
      continue;
    }
    const cwd = typeof obj?.cwd === 'string' ? obj.cwd : '';
    if (!cwd || seen.has(cwd)) continue;
    seen.add(cwd);
    const enc = encodeCwd(cwd).toLowerCase();
    // Claude Code truncates long encoded names to 200 chars + a hash suffix, so
    // for those match on the 200-char prefix, like listSessions does.
    if (enc === want || (want.length > 200 && enc.startsWith(want.slice(0, 200)))) return cwd;
  }
  return undefined;
}

/** Reads at most 64 KB looking for the first user message. Never throws. */
export function peekTitle(file: string, maxBytes = 64 * 1024): string {
  try {
    const fd = fs.openSync(file, 'r');
    try {
      const buf = Buffer.alloc(maxBytes);
      const n = fs.readSync(fd, buf, 0, maxBytes, 0);
      const text = buf.subarray(0, n).toString('utf8');
      for (const line of text.split('\n')) {
        if (!line.trim()) continue;
        let obj: any;
        try {
          obj = JSON.parse(line);
        } catch {
          continue;
        }
        if (obj?.type !== 'user') continue;
        const c = obj?.message?.content;
        let s = '';
        if (typeof c === 'string') s = c;
        else if (Array.isArray(c)) {
          const t = c.find((b: any) => b && b.type === 'text' && typeof b.text === 'string');
          s = t ? t.text : '';
        }
        s = s.replace(/\s+/g, ' ').trim();
        if (s) return s.length > 90 ? `${s.slice(0, 89)}…` : s;
      }
    } finally {
      fs.closeSync(fd);
    }
  } catch {
    /* unreadable: caller shows id + date */
  }
  return '';
}
