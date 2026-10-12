#!/bin/bash
# Installs whatsapp-claude on macOS for the current user. No sudo, no Xcode, no Homebrew.
# Safe to run again (it upgrades in place and keeps the WhatsApp link + messages).
set -euo pipefail

PKG="$(cd "$(dirname "$0")/.." && pwd)"
HOME_DIR="${WA_CLAUDE_HOME:-$HOME/.whatsapp-claude}"
PORT="${WA_CLAUDE_PORT:-47321}"
BIN="$HOME_DIR/bin/whatsapp-claude"
LABEL="com.ysresearch.whatsapp-claude"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

step() { printf '\n==> %s\n' "$1"; }

mkdir -p "$HOME_DIR/bin" "$HOME_DIR/logs" "$HOME/Library/LaunchAgents"

# 1. Go toolchain (private copy inside ~/.whatsapp-claude unless a recent Go exists)
step "1/5 Go toolchain"
GO=""
if command -v go >/dev/null 2>&1; then
  v="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
  major="${v%%.*}"; rest="${v#*.}"; minor="${rest%%.*}"
  if [ "${major:-0}" -ge 1 ] && [ "${minor:-0}" -ge 25 ]; then GO="$(command -v go)"; fi
fi
if [ -z "$GO" ]; then
  GO="$HOME_DIR/toolchain/go/bin/go"
  if [ ! -x "$GO" ]; then
    case "$(uname -m)" in arm64) ARCH=arm64 ;; *) ARCH=amd64 ;; esac
    VER="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1)"
    echo "downloading $VER for darwin-$ARCH"
    mkdir -p "$HOME_DIR/toolchain"
    curl -fSL --progress-bar "https://go.dev/dl/$VER.darwin-$ARCH.tar.gz" -o "$HOME_DIR/toolchain/go.tar.gz"
    rm -rf "$HOME_DIR/toolchain/go"
    tar -xzf "$HOME_DIR/toolchain/go.tar.gz" -C "$HOME_DIR/toolchain"
    rm -f "$HOME_DIR/toolchain/go.tar.gz"
  fi
fi
"$GO" version

# 2. Build (pure Go, no C compiler needed)
step "2/5 Build"
export GOPATH="$HOME_DIR/toolchain/gopath" GOCACHE="$HOME_DIR/toolchain/cache" CGO_ENABLED=0 GOFLAGS="-mod=mod -modcacherw"
(cd "$PKG/engine" && "$GO" build -o "$BIN.new" .)
mv -f "$BIN.new" "$BIN"
"$BIN" version

# 3. Background service (starts at login, restarts if it stops)
step "3/5 Background service"
cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN</string><string>serve</string></array>
  <key>EnvironmentVariables</key><dict>
    <key>WA_CLAUDE_HOME</key><string>$HOME_DIR</string>
    <key>WA_CLAUDE_PORT</key><string>$PORT</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$HOME_DIR/logs/service.log</string>
  <key>StandardErrorPath</key><string>$HOME_DIR/logs/service.log</string>
</dict></plist>
EOF
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
sleep 1
launchctl bootstrap "gui/$(id -u)" "$PLIST"
for i in $(seq 1 20); do
  if curl -fs "http://127.0.0.1:$PORT/status" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fs "http://127.0.0.1:$PORT/status" || { echo "service did not start; see $HOME_DIR/logs/service.log"; tail -20 "$HOME_DIR/logs/service.log"; exit 1; }
echo

# 4. Claude Code: MCP server + read-only permissions + skill
step "4/5 Claude Code"
CLAUDE=""
for c in claude "$HOME/.local/bin/claude" "$HOME/.claude/local/claude" /opt/homebrew/bin/claude /usr/local/bin/claude; do
  if command -v "$c" >/dev/null 2>&1; then CLAUDE="$(command -v "$c")"; break; fi
done
if [ -n "$CLAUDE" ] && { "$CLAUDE" mcp remove --scope user whatsapp >/dev/null 2>&1 || true; } \
   && "$CLAUDE" mcp add --scope user whatsapp -- "$BIN" mcp; then
  "$BIN" register
else
  echo "claude command not usable here; writing the MCP entry into ~/.claude.json directly"
  "$BIN" register --mcp
fi
mkdir -p "$HOME/.claude/skills/whatsapp"
cp "$PKG/skill/SKILL.md" "$HOME/.claude/skills/whatsapp/SKILL.md"
echo "skill installed in ~/.claude/skills/whatsapp"
cp "$PKG/scripts/uninstall-mac.sh" "$HOME_DIR/uninstall.sh" && chmod +x "$HOME_DIR/uninstall.sh"

# 5. Done
step "5/5 Status"
"$BIN" status
echo
echo "INSTALL_OK  link page: http://127.0.0.1:$PORT/link"
