#!/bin/bash
# Removes whatsapp-claude. Pass --keep-data to keep the WhatsApp link and stored messages.
set -uo pipefail

HOME_DIR="${WA_CLAUDE_HOME:-$HOME/.whatsapp-claude}"
LABEL="com.ysresearch.whatsapp-claude"

if [ "${1:-}" != "--keep-data" ] && [ -x "$HOME_DIR/bin/whatsapp-claude" ]; then
  "$HOME_DIR/bin/whatsapp-claude" unlink || true   # removes the linked device from the phone too
fi
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
rm -f "$HOME/Library/LaunchAgents/$LABEL.plist"
command -v claude >/dev/null 2>&1 && claude mcp remove --scope user whatsapp || true
rm -rf "$HOME/.claude/skills/whatsapp"
chmod -R u+w "$HOME_DIR/toolchain" 2>/dev/null || true
if [ "${1:-}" = "--keep-data" ]; then
  rm -rf "$HOME_DIR/bin" "$HOME_DIR/toolchain"
else
  rm -rf "$HOME_DIR"
fi
echo "UNINSTALL_OK"
