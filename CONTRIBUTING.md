# Contributing

Thanks for helping. TaskKeeper is a monorepo: a Go worker/CLI that does the dangerous work (spawning agents, worktrees, killing process trees, the database) and a TypeScript VS Code extension that only ever *reads* that database and drives the wizard. Keep the safety-critical parts small and boring.

## Layout

- `apps/worker` — the short-lived process the OS scheduler launches for one run.
- `apps/ctl` — `taskkeeper-ctl`, the JSON contract the extension consumes (`--json`).
- `packages/runner` — the run lifecycle (slot, worktree, agent launch, events, outcome).
- `packages/scheduler` — DST-safe occurrence math (UTC monotonic), misfire policy.
- `packages/store` — SQLite schema, state machine, idempotency, the change marker.
- `packages/redact` — secret redaction, applied at the **single** event writer.
- `packages/platform` — OS layer: Windows Task Scheduler + Job Objects, macOS launchd + process groups.
- `adapters` — Claude Code and Codex command builders (never shell strings) and event parsers.
- `apps/vscode-extension` — the extension. `src/core/` never imports `vscode`.

## Ground rules

- The agent's work is only ever merged into your branch on an **explicit accept**. Reject deletes the worktree and branch, leaving nothing behind. Accept merges **locally** — it never pushes.
- Commands are built as argv arrays, never shell strings. No `sh -c`.
- Cancelling or timing out a run must kill the **whole** process tree (Job Object on Windows, process group on macOS).
- Secrets are redacted at the single event writer in `packages/store`. Nothing downstream re-derives raw text.
- Every user-facing string in the extension goes through `vscode.l10n.t` and gets a Spanish entry in `l10n/bundle.l10n.es.json`. `node scripts/l10n-sync.mjs` reports missing/orphan keys; the unit tests fail on both.
- Nothing leaves the machine except the agents' own API calls. No telemetry, no account, no network of ours.

## Dev loop

```bash
# Go worker/ctl (from the repo root; needs Go per go.mod and git on PATH)
go build ./...
go test ./...

# Extension
cd apps/vscode-extension
npm install
npm run check              # typecheck + lint + unit tests + l10n
npm run test:integration   # downloads VS Code once, runs the hermetic end-to-end suite
node scripts/build-bin.mjs # compile the worker into bin/<platform>-<arch>/
npm run package            # win32-x64 .vsix
```

Press F5 in VS Code (with the `apps/vscode-extension` folder open) to launch an Extension Development Host.

## Platforms

Windows x64 is verified. The macOS layer (launchd + process groups) is written and cross-compiles, but ships as its own platform package only after it is verified on real hardware — matching the honesty in the README.
