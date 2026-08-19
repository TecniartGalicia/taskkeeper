# Changelog


## 0.1.1 — 2026-08-19

Corrección de un fallo encontrado en uso real:

- **Continuar/derivar una conversación ya funciona.** En 0.1.0 una tarea con «Resume» o «Fork» arrancaba el agente sin el id de la conversación (`--resume` vacío) y fallaba al instante, sin gastar nada. Ahora el runner resuelve el id de sesión guardado y lo pasa al agente; si la tarea no tiene una referencia de sesión válida, falla con un motivo claro (`sin_referencia_sesion`) en vez del error críptico del agente.

## 0.1.0 — 2026-08-19 (pre-release)

First public release. Windows x64.

- Schedule Claude Code or Codex tasks (daily, weekly, once) registered with Windows Task Scheduler; runs with VS Code closed.
- Every run in its own Git worktree from the resolved base commit; the main checkout is never touched.
- Morning inbox: diff in VS Code's editor, files changed, cost; accept (local merge, no push) or reject (worktree and branch removed).
- New / resume / fork conversation modes for Claude Code; new for Codex.
- Two permission profiles applied explicitly on every run; agent defaults are never trusted.
- Per-run spending cap; provider quota and expired sign-in recognised and reported instead of hanging.
- Machine-wide concurrency slot (default 1), cancellation that kills the whole process tree, missed-run policy per task.
- Full local audit trail. English and Spanish UI.
