# WhatsApp for Claude Code

Lets Claude Code read, search and, only with your explicit OK, send messages on **your own WhatsApp**.
It connects as a "linked device" (like WhatsApp Web). Everything runs and stays on your computer.

**To install:** open Claude Code and paste the prompt from [PROMPT.md](PROMPT.md) (or tell Claude: "Read INSTALL.md in this repo and install it for me"). Your only step is scanning one QR code with your phone.

## What you can ask Claude afterwards

- "מה כתבו היום בקבוצה של המשפחה?"
- "תסכם לי את השיחה עם דנה מהשבוע האחרון"
- "תחפש איפה דיברנו על ההצעה"
- "תנסח הודעה לאבא שאני מאחרת", and Claude shows you the text first and sends only after you say yes.

## Privacy and safety

- Messages are stored only on your computer (`~/.whatsapp-claude/`). Nothing goes to any server of ours.
- The service listens only on this computer (`127.0.0.1`), behind a local access token.
- Claude Code asks before every send; the skill forbids sending without your yes for that exact message.
- Remove everything at any time: ask Claude "תסיר את התוסף של WhatsApp" (it runs `~/.whatsapp-claude/uninstall.sh`, which also unlinks the device from your phone).

## Contents

| Path | What |
|---|---|
| `INSTALL.md` | Step-by-step instructions written for Claude Code |
| `PROMPT.md` | The prompt to paste into Claude Code |
| `engine/` | The program (Go): background service + MCP server + QR page |
| `scripts/` | Installers for macOS / Windows, uninstaller |
| `skill/SKILL.md` | How Claude should use WhatsApp (tools, sending rules, fixes) |

Built on [whatsmeow](https://github.com/tulir/whatsmeow) (MPL-2.0). Unofficial; not affiliated with WhatsApp or Meta.
