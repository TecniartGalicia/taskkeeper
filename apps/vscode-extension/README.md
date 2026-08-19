# TaskKeeper

**Your coding agents work the night shift without touching your repository. In the morning you review the diff and decide.**

TaskKeeper runs [Claude Code](https://code.claude.com) or [Codex](https://developers.openai.com/codex) on a schedule, **inside an isolated Git worktree**, and puts the result in a review inbox: the files it changed, what it cost, and two buttons — accept into your branch, or discard.

Scheduling prompts is something several tools already do, including the agents themselves. What none of them do is keep the agent's work **isolated and reviewable**. Every other scheduler runs the agent on your working directory; if it gets something wrong at 3 a.m., it gets it wrong on your code.

> Free, local-first, no telemetry, no account. Not affiliated with Anthropic or OpenAI.

## What you get

| | |
|---|---|
| **Isolated worktree per run** | The agent works on a copy created from the exact base commit. Your checkout is never touched. |
| **The morning inbox** | Each finished run shows its diff (VS Code's own diff editor), the files it changed and its cost. Accept merges locally — never pushes. Reject deletes the worktree and branch, leaving nothing behind. |
| **Runs with VS Code closed** | Schedules are registered with the operating system's own scheduler (Windows Task Scheduler). No background daemon of ours; if your machine is on, the task runs — and on Windows it can wake the machine. |
| **Resume or fork a conversation** | A task can start fresh, continue an existing Claude Code conversation, or fork it. |
| **Claude Code and Codex, one model of safety** | Two permission profiles — *audit (read only)* and *isolated changes* — applied explicitly on every run. TaskKeeper does not rely on the agent's defaults: it measured them and they were too permissive. |
| **Spending caps and quota awareness** | A cap per run; a provider quota exhaustion is recognised and retried later instead of burning the night against a wall. |
| **A record of everything** | Which agent, which repository and commit, which permissions, what it changed, what it cost, who accepted it. |

## Getting started

1. Install the extension. Have Claude Code and/or Codex installed in VS Code and signed in.
2. Open the **TaskKeeper** view in the Activity Bar → **New task**. Seven short steps: name, repository, agent, context, prompt, schedule, permissions.
3. Choose **Create and run now** to test it immediately. Watch the **Last night** view; when it finishes, open the diff and accept or reject.

The prompt matters more than the schedule. TaskKeeper pre-fills a durable structure: goal, what is allowed, what is not, how to report. Edit it any time — earlier runs keep the version they used.

## How it works

- Each task is one entry in the OS scheduler, under its own folder (`Argalla\TaskKeeper`). TaskKeeper never touches any other scheduled task.
- At the scheduled time a short-lived worker starts, takes a machine-wide "slot" (one concurrent run by default), creates a worktree from the base commit, launches the agent with an explicit permission profile inside a Windows Job Object (so cancelling kills every descendant process), and records events, cost and outcome in a local SQLite database.
- The extension only reads that database (through the bundled `taskkeeper-ctl`) and refreshes in well under a second when the worker touches a marker file. No polling, no daemon.
- Missed runs follow the policy you chose per task: skip, run if less than *n* hours late, or wait for you.

## Requirements and limitations (read this)

- **Windows 10/11, x64** in this release. macOS support is built and unverified on real hardware; it will ship as its own platform package once it is.
- The machine must be **on and your session signed in** at the scheduled time. Task Scheduler can wake a sleeping PC if *wake timers* are allowed in your power plan — on battery they usually are not; TaskKeeper tells you when it creates the task.
- The agent CLI must be **already signed in**. A scheduled run has no terminal to answer a login or a "trust this folder?" prompt; TaskKeeper detects both and fails fast with a clear reason instead of hanging.
- Your usage of Claude Code / Codex is subject to their terms. TaskKeeper does not store or transmit your credentials; it launches the same binaries you use by hand.

## Privacy

Everything stays on your machine: tasks, prompts, transcripts of runs, worktrees. There is no telemetry, no account, no network call from TaskKeeper itself. Secrets that look like tokens or keys are redacted from stored logs.

## Support

Issues and questions: <https://github.com/TecniartGalicia/taskkeeper/issues>. Made by [Argalla](https://argalla.com) (Tecniart Galicia, S.L.), Galicia, Spain.
