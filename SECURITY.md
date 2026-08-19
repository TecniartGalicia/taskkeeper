# Security

TaskKeeper runs coding agents (Claude Code, Codex) against your Git repository on a schedule, each in an **isolated worktree** created from a base commit, and stores every run's events, cost and outcome in a local SQLite database. It writes to your repository only when you **accept** a finished run.

Please report anything that could:

- run an agent, or merge its work, **without an explicit user action**;
- escape the worktree isolation and touch your working checkout, another repository, or files outside the project;
- fail to kill the agent's process tree on cancel or timeout;
- leak a secret into the stored events or the review UI (the worker redacts known secret shapes at the single event writer — a bypass is a vulnerability);
- be tricked by repository content (paths, symlinks, `.git` configuration, hooks) into acting outside the worktree;
- register or alter an OS scheduled task other than TaskKeeper's own (under `Argalla\TaskKeeper` on Windows, the `com.argalla.taskkeeper.` label prefix on macOS).

How to report:

- Email: info@tecniartgalicia.com (subject "taskkeeper security")
- Or a private security advisory on GitHub.

We aim to acknowledge within 3 business days. Please don't open public issues for security reports until a fix is released.
