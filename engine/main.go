// whatsapp-claude: a personal WhatsApp link for Claude Code.
//
//	whatsapp-claude serve   background service (links as a WhatsApp "linked device",
//	                        stores incoming/history messages locally, HTTP on 127.0.0.1)
//	whatsapp-claude mcp     stdio MCP server for Claude Code (talks to the service)
//	whatsapp-claude status  one-line status check
//	whatsapp-claude unlink  log this device out of WhatsApp
//	whatsapp-claude register [--mcp]  add permissions (and the MCP server) to Claude Code
//
// All data stays on this computer in ~/.whatsapp-claude (override: WA_CLAUDE_HOME).
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const version = "1.0.0"

func homeDir() string {
	if h := os.Getenv("WA_CLAUDE_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".whatsapp-claude")
}

func port() string {
	if p := os.Getenv("WA_CLAUDE_PORT"); p != "" {
		return p
	}
	return "47321"
}

func baseURL() string { return "http://127.0.0.1:" + port() }

// token guards the read/send endpoints against other local programs.
func loadOrCreateToken() string {
	path := filepath.Join(homeDir(), "token")
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return strings.TrimSpace(string(b))
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	tok := hex.EncodeToString(buf)
	_ = os.MkdirAll(homeDir(), 0o700)
	_ = os.WriteFile(path, []byte(tok), 0o600)
	return tok
}

func main() {
	cmd := "help"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		serve()
	case "mcp":
		runMCP()
	case "status":
		os.Exit(cliStatus())
	case "unlink":
		os.Exit(cliUnlink())
	case "register":
		// register        -> permissions only (MCP added with `claude mcp add`)
		// register --mcp  -> also writes the MCP server into ~/.claude.json
		os.Exit(registerClaude(len(os.Args) > 2 && os.Args[2] == "--mcp"))
	case "version":
		fmt.Println(version)
	default:
		fmt.Println("usage: whatsapp-claude serve | mcp | status | unlink | register [--mcp] | version")
	}
}
