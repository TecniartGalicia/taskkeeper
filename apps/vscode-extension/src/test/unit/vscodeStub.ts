// Minimal stub so pure modules that import 'vscode' types can be unit-tested
// under plain mocha. Only what the tested modules touch at load time.
import Module from 'node:module';

const stub = {
  l10n: { t: (s: string, ...args: unknown[]) => s.replace(/\{(\d+)\}/g, (_, i) => String(args[Number(i)] ?? '')) },
  Uri: {
    file: (p: string) => ({ scheme: 'file', fsPath: p, path: p, toString: () => `file://${p}` }),
    from: (o: { scheme: string; path: string; query?: string }) => ({ ...o, fsPath: o.path, toString: () => `${o.scheme}:${o.path}?${o.query ?? ''}` }),
  },
  ThemeIcon: class {
    static File = { id: 'file' };
    constructor(public id: string, public color?: unknown) {}
  },
  ThemeColor: class {
    constructor(public id: string) {}
  },
  TreeItem: class {
    constructor(public label: string, public collapsibleState?: number) {}
  },
  TreeItemCollapsibleState: { None: 0, Collapsed: 1, Expanded: 2 },
  MarkdownString: class {
    isTrusted = false;
    constructor(public value: string) {}
  },
  EventEmitter: class {
    event = () => ({ dispose() {} });
    fire() {}
  },
};

const origLoad = (Module as any)._load;
(Module as any)._load = function (request: string, ...rest: unknown[]) {
  if (request === 'vscode') return stub;
  return origLoad.call(this, request, ...rest);
};
