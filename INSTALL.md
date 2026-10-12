# INSTALL.md: instructions for Claude Code

You are installing **whatsapp-claude** for a user with no technical background.
Goal: the user does exactly one thing, scanning a QR code with their phone. You do everything else.

## Ground rules for this install

- Talk to the user in their language (Hebrew if they wrote in Hebrew), short and plain. No commands, no paths, no jargon in chat.
- Never ask the user to open a terminal, type anything, or install anything themselves.
- If Claude Code asks permission for a command, that is the only click you may ask for: tell them "אם מופיעה בקשת אישור, ללחוץ Allow / אישור".
- macOS may show a notification "Background item added" / "פריט רקע נוסף". It is expected; tell the user it is fine.
- Do not send any WhatsApp message during the install, not even a test.
- Run the install script in **one** shell call. It is idempotent: running it again upgrades in place.

## What gets installed (all inside the user's home folder, no admin password)

| Piece | Where |
|---|---|
| Private Go toolchain (only if no Go ≥ 1.25 exists) | `~/.whatsapp-claude/toolchain/` |
| The program | `~/.whatsapp-claude/bin/whatsapp-claude` |
| Background service (starts at login, restarts itself) | macOS: `~/Library/LaunchAgents/com.ysresearch.whatsapp-claude.plist`; Windows: Startup folder `whatsapp-claude.vbs` |
| WhatsApp link + local message store | `~/.whatsapp-claude/device.db`, `messages.db` |
| MCP server `whatsapp` (user scope) | via `claude mcp add`, or `~/.claude.json` if the CLI is not usable |
| Read-only tool permissions | `~/.claude/settings.json` → `permissions.allow` (send tools stay on "ask") |
| Skill | `~/.claude/skills/whatsapp/SKILL.md` |

The service listens only on `127.0.0.1:47321`. Data never leaves the computer except messages the user approves sending.

## Steps

### 1. Get the folder

If this file is already on disk (you are reading it from a local folder), use that folder as `PKG` and skip to step 2.

Otherwise download it **without git** (a fresh Mac has no git, and calling `git` would pop up an Xcode install window the user cannot handle):

```bash
mkdir -p ~/.whatsapp-claude/src && cd ~/.whatsapp-claude/src \
 && curl -fsSL "<ZIP_URL>" -o pkg.zip && rm -rf pkg && mkdir pkg && unzip -q pkg.zip -d pkg && rm pkg.zip \
 && find pkg -name INSTALL.md | head -1
```

`<ZIP_URL>` is the link given in the user's prompt (normally `https://github.com/yosefsl/whatsapp-for-claude/archive/refs/heads/main.zip`). If the prompt says the zip is in Downloads instead, unzip `~/Downloads/whatsapp-claude*.zip` the same way.
`PKG` = the folder that contains this `INSTALL.md` (printed by the last command).

### 2. Run the installer

macOS (one call, can take 2-4 minutes on first run because it downloads Go and builds):

```bash
bash "$PKG/scripts/install-mac.sh"
```

Windows (PowerShell):

```powershell
powershell -ExecutionPolicy Bypass -File "$PKG\scripts\install-windows.ps1"
```

Success = the last line starts with `INSTALL_OK`. If not, read the output, fix, and run again (see Troubleshooting). Do not stop and hand the error to the user.

### 3. Link WhatsApp (the user's only action)

1. Open the link page in the browser: macOS `open http://127.0.0.1:47321/link`, Windows `start http://127.0.0.1:47321/link`.
2. Tell the user, in these words or close to them (Hebrew example):

   > נפתח עכשיו חלון עם קוד QR.
   > בטלפון: פותחים WhatsApp → הגדרות → מכשירים מקושרים → קישור מכשיר, ומכוונים את המצלמה לקוד.
   > (באנדרואיד: שלוש הנקודות למעלה → מכשירים מקושרים.)
   > כשמופיע ✓ ירוק, זהו.

3. Poll every ~5 seconds, up to 6 minutes: `~/.whatsapp-claude/bin/whatsapp-claude status` until `"state":"connected"`.
   - If the page says the code expired, it has a button "להציג קוד חדש"; tell the user to press it, or reload the page yourself with `open` again.
4. After `connected`, wait ~30 seconds (WhatsApp sends recent history), run `status` again and note `messages_stored`.

### 4. Verify (read-only)

```bash
curl -s -H "Authorization: Bearer $(cat ~/.whatsapp-claude/token)" "http://127.0.0.1:47321/chats?limit=5"
```

You should see recent chats. Do not paste their content to the user; just confirm it works ("אני רואה את הצ'אטים האחרונים שלך").

### 5. Finish

Tell the user, short:

> הכול מותקן ומחובר. מהשיחה הבאה עם Claude אפשר לבקש למשל: "מה כתבו היום בקבוצה X", "תסכם לי את השיחה עם Y", "תנסח הודעה ל-Z".
> Claude אף פעם לא ישלח הודעה בלי להראות לך אותה קודם ולקבל ממך "כן".
> כדי שהכלים ייטענו, צריך לפתוח שיחה חדשה ב-Claude פעם אחת.

The MCP tools load in a **new** Claude Code session; in the current session use the shell (`whatsapp-claude status`) only.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `curl: (6) Could not resolve host` | No internet. Ask the user to check Wi-Fi, then rerun |
| Build error mentioning `go: download` | Network hiccup; rerun the script |
| `service did not start` | `tail -50 ~/.whatsapp-claude/logs/service.log`; if port 47321 is taken: `lsof -i :47321`, then set `WA_CLAUDE_PORT=47322` before the script and use that port everywhere |
| `launchctl bootstrap` error 5 / "Input/output error" | `launchctl bootout gui/$(id -u)/com.ysresearch.whatsapp-claude; sleep 2` then rerun the script |
| `claude mcp add` fails | The script falls back to editing `~/.claude.json` by itself; nothing to do |
| QR expired 3 times | `launchctl kickstart -k gui/$(id -u)/com.ysresearch.whatsapp-claude`, then step 3 again |
| Phone says "couldn't link device" | WhatsApp allows 4 linked devices; ask the user to remove an old one in Linked devices, then step 3 again |
| Status `not_linked` days later | The phone unlinked it (or 14+ days without the phone online). Step 3 again |

## Uninstall

macOS: `bash ~/.whatsapp-claude/uninstall.sh` (unlinks from the phone and deletes all local data; `--keep-data` keeps them).
Windows: delete the Startup file `whatsapp-claude.vbs`, stop the `whatsapp-claude` process, `claude mcp remove --scope user whatsapp`, delete `%USERPROFILE%\.whatsapp-claude` and `%USERPROFILE%\.claude\skills\whatsapp`.
