// Watches the marker file the worker touches on every state change.
//
// There is no daemon and no channel: the worker writes to SQLite and then
// touches `cambios.marca`. Watching that one file (debounced) is what keeps
// the tree views fresh in well under a second without polling the database.
import * as fs from 'node:fs';
import * as path from 'node:path';

export type Unsubscribe = () => void;

export function watchMarker(markerPath: string, onChange: () => void, debounceMs = 250): Unsubscribe {
  const dir = path.dirname(markerPath);
  const name = path.basename(markerPath);
  fs.mkdirSync(dir, { recursive: true });
  if (!fs.existsSync(markerPath)) fs.writeFileSync(markerPath, '');

  let timer: NodeJS.Timeout | undefined;
  const fire = () => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = undefined;
      onChange();
    }, debounceMs);
  };

  // Watch the directory rather than the file: on Windows an editor-style
  // replace (write temp + rename) would otherwise detach a file watcher.
  let watcher: fs.FSWatcher | undefined;
  try {
    watcher = fs.watch(dir, { persistent: false }, (_ev, changed) => {
      if (!changed || changed.toString() === name) fire();
    });
  } catch {
    // Fall back to polling the mtime if fs.watch is unavailable.
    let last = 0;
    const iv = setInterval(() => {
      try {
        const m = fs.statSync(markerPath).mtimeMs;
        if (m !== last) {
          last = m;
          fire();
        }
      } catch {
        /* marker not there yet */
      }
    }, 1000);
    return () => clearInterval(iv);
  }
  return () => {
    if (timer) clearTimeout(timer);
    watcher?.close();
  };
}
