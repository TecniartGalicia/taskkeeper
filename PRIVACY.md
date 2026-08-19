# Privacy

TaskKeeper is local-first. It has no account, no telemetry, and no network service of its own.

## What stays on your machine

- **Your prompts, the agents' output, diffs, cost and run history** live in a local SQLite database under your user profile (`%LOCALAPPDATA%\Argalla\TaskKeeper` on Windows). TaskKeeper never uploads any of it.
- **Scheduled tasks** are entries in your operating system's own scheduler, under a TaskKeeper-only folder/label prefix. TaskKeeper never reads or changes tasks it did not create.
- **Secrets** that an agent happens to print (API keys, tokens, private keys) are redacted before they are written to the database, at a single choke point.

## What does leave your machine — and who is responsible for it

TaskKeeper launches **Claude Code** (Anthropic) and/or **Codex** (OpenAI) using the installation and credentials **you** already have. When a run executes, those agents send your prompt and repository context to their respective providers, exactly as they do when you run them by hand. That traffic is governed by **their** privacy policies, not this one:

- Anthropic — https://www.anthropic.com/legal/privacy
- OpenAI — https://openai.com/policies/privacy-policy

TaskKeeper adds no telemetry on top and sends nothing to Argalla or anyone else.

## Contact

Tecniart Galicia SL (Argalla) — info@tecniartgalicia.com
