# Third-party notices

TaskKeeper's VS Code extension bundles no third-party runtime code of its own (its `dependencies` are empty; the build tools are dev-only). What it ships alongside the extension are the TaskKeeper Go binaries (`taskkeeper-worker`, `taskkeeper-ctl`). Those binaries statically link the following open-source Go modules. Each is used under its own license; the full license texts travel with each module's source.

| Module | License |
|---|---|
| `modernc.org/sqlite` | BSD-3-Clause |
| `modernc.org/libc` | BSD-3-Clause |
| `modernc.org/memory` | BSD-3-Clause |
| `modernc.org/mathutil` | BSD-3-Clause |
| `github.com/remyoudompheng/bigfft` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |
| `github.com/dustin/go-humanize` | MIT |
| `github.com/mattn/go-isatty` | MIT |
| `github.com/ncruces/go-strftime` | MIT |

The pure-Go SQLite driver (`modernc.org/sqlite`) is used so the binaries need no cgo and no system SQLite.

TaskKeeper launches, but does not redistribute, **Claude Code** (Anthropic) and **Codex** (OpenAI). Those are installed and licensed separately by the user; TaskKeeper invokes whatever the user already has installed.
