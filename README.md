# TaskKeeper

**Your coding agents work the night shift without touching your repository. In the morning you review the diff and decide.**

TaskKeeper is a free, local-first VS Code extension that runs [Claude Code](https://code.claude.com) or [Codex](https://developers.openai.com/codex) on a schedule, **inside an isolated Git worktree**, and puts the result in a review inbox: the files it changed, what it cost, and two buttons — accept into your branch, or discard.

Scheduling prompts is something several tools already do, including the agents themselves. What none of them do is keep the agent's work **isolated and reviewable**. Every other scheduler runs the agent on your working directory; if it gets something wrong at 3 a.m., it gets it wrong on your code. TaskKeeper runs it on a worktree created from a base commit and never merges anything until you accept.

> Free · local-first · no telemetry · no account. Not affiliated with Anthropic or OpenAI.

## Install

Search **TaskKeeper** in the VS Code Extensions view (Marketplace or Open VSX), or:

```
code --install-extension argalla.taskkeeper
```

Windows 10/11 x64 in this release. You need Claude Code and/or Codex installed in VS Code and signed in.

The user-facing guide lives in [`apps/vscode-extension/README.md`](apps/vscode-extension/README.md).

## How it works

- Each task is one entry in the operating system's own scheduler (Windows Task Scheduler), under a TaskKeeper-only folder. **No background daemon of ours.**
- At the scheduled time a short-lived Go worker starts, takes a machine-wide slot, creates a worktree from the base commit, launches the agent with an explicit permission profile inside a Windows Job Object (so cancelling kills every descendant process), and records events, cost and outcome in a local SQLite database.
- The extension only **reads** that database, through the bundled `taskkeeper-ctl`, and refreshes in well under a second when the worker touches a marker file. No polling.
- Secrets an agent may print are redacted at the single event writer before anything is stored.

## Repository layout (monorepo)

| Path | What |
|---|---|
| `apps/worker` | The short-lived process the OS scheduler launches for one run |
| `apps/ctl` | `taskkeeper-ctl` — the JSON contract the extension reads |
| `apps/vscode-extension` | The VS Code extension (TypeScript) |
| `packages/runner` | Run lifecycle: slot, worktree, agent launch, events, outcome |
| `packages/scheduler` | DST-safe occurrence math and misfire policy |
| `packages/store` | SQLite schema, state machine, idempotency, change marker |
| `packages/redact` | Secret redaction at the single event writer |
| `packages/platform` | OS layer: Windows Task Scheduler + Job Objects; macOS launchd + process groups |
| `adapters` | Claude Code and Codex command builders and event parsers |
| `docs/PLAN.md` | The authoritative build plan and the per-phase audits |

## Build from source

```bash
# Worker + CLI (needs Go per go.mod, and git on PATH)
go build ./...
go test ./...

# Extension
cd apps/vscode-extension
npm ci
npm run check            # typecheck + lint + unit tests + l10n
node scripts/build-bin.mjs   # compile the worker into bin/<platform>-<arch>/
npm run package          # win32-x64 .vsix
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Platforms

Windows x64 is verified and shipped. The macOS layer (launchd + process groups) is written and cross-compiles; it ships as its own platform package only after it is verified on real hardware.

## Privacy & security

Nothing leaves your machine except the agents' own API calls, made with the credentials you already have — see [`PRIVACY.md`](PRIVACY.md). To report a vulnerability, see [`SECURITY.md`](SECURITY.md).

## License

MIT © Tecniart Galicia SL (Argalla). See [`LICENSE`](LICENSE) and [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).
