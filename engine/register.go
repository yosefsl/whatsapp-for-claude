package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Read-only tools are pre-approved so Claude can read without a prompt each time.
// The two send tools are left out on purpose: Claude Code asks before every send.
var allowedTools = []string{
	"mcp__whatsapp__whatsapp_status",
	"mcp__whatsapp__whatsapp_link",
	"mcp__whatsapp__whatsapp_list_chats",
	"mcp__whatsapp__whatsapp_find",
	"mcp__whatsapp__whatsapp_read",
	"mcp__whatsapp__whatsapp_search",
}

// registerClaude adds the MCP server to ~/.claude.json (only used when the
// `claude` command is not available) and the read-only permissions to
// ~/.claude/settings.json. Both edits keep every other key as it was.
func registerClaude(withMCP bool) int {
	home, _ := os.UserHomeDir()
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)

	if withMCP {
		path := filepath.Join(home, ".claude.json")
		cfg := map[string]any{}
		if err := readJSONFile(path, &cfg); err != nil {
			fmt.Println("cannot read", path, err)
			return 1
		}
		servers, _ := cfg["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		servers["whatsapp"] = map[string]any{"type": "stdio", "command": self, "args": []string{"mcp"}, "env": map[string]any{}}
		cfg["mcpServers"] = servers
		if err := writeJSONFile(path, cfg); err != nil {
			fmt.Println("cannot write", path, err)
			return 1
		}
		fmt.Println("registered MCP server 'whatsapp' in", path)
	}

	path := filepath.Join(home, ".claude", "settings.json")
	cfg := map[string]any{}
	if err := readJSONFile(path, &cfg); err != nil {
		fmt.Println("cannot read", path, err)
		return 1
	}
	perms, _ := cfg["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	allow, _ := perms["allow"].([]any)
	have := map[string]bool{}
	for _, a := range allow {
		if s, ok := a.(string); ok {
			have[s] = true
		}
	}
	for _, t := range allowedTools {
		if !have[t] {
			allow = append(allow, t)
		}
	}
	perms["allow"] = allow
	cfg["permissions"] = perms
	if err := writeJSONFile(path, cfg); err != nil {
		fmt.Println("cannot write", path, err)
		return 1
	}
	fmt.Println("allowed read-only WhatsApp tools in", path)
	return 0
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

func writeJSONFile(path string, v any) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if b, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak-whatsapp-claude", b, 0o600)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
