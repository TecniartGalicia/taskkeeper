#!/usr/bin/env bash
# P-00 · Deteccion de agentes (FR-001)
# Ni claude ni codex estan en el PATH del sistema en una instalacion tipica de
# VS Code: viven dentro de la carpeta de la extension, con la version en la ruta.
# El runner NO puede depender de PATH.
set -uo pipefail
EXT="$HOME/.vscode/extensions"

newest() { ls -d $1 2>/dev/null | sort -V | tail -1; }

CLAUDE_DIR=$(newest "$EXT/anthropic.claude-code-*")
CODEX_DIR=$(newest "$EXT/openai.chatgpt-*")
CLAUDE="$CLAUDE_DIR/resources/native-binary/claude.exe"
CODEX="$CODEX_DIR/bin/windows-x86_64/codex.exe"

echo "claude_dir=$CLAUDE_DIR"
echo "codex_dir=$CODEX_DIR"
for b in "$CLAUDE" "$CODEX"; do
  if [ -x "$b" ]; then echo "OK   $b"; else echo "FALTA $b"; fi
done
echo "--- versiones ---"
[ -x "$CLAUDE" ] && echo "claude: $("$CLAUDE" --version 2>&1 | head -1)"
[ -x "$CODEX" ]  && echo "codex : $("$CODEX" --version 2>&1 | head -1)"
