package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

// ---- tiny HTTP client to the background service ----

func callService(method, path string, body any) (string, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, baseURL()+path, rd)
	req.Header.Set("Authorization", "Bearer "+loadOrCreateToken())
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("the WhatsApp service is not running (%v). Restart it per INSTALL.md, step 'Service is down'", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s", bytes.TrimSpace(b))
	}
	return string(b), nil
}

func openBrowser(u string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", u).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		_ = exec.Command("xdg-open", u).Start()
	}
}

func cliStatus() int {
	out, err := callService("GET", "/status", nil)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Print(out)
	return 0
}

func cliUnlink() int {
	out, err := callService("POST", "/unlink", nil)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Print(out)
	return 0
}

// ---- MCP over stdio (JSON-RPC 2.0, one message per line) ----

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

var readOnly = map[string]any{"readOnlyHint": true}

var tools = []tool{
	{"whatsapp_status", "Is WhatsApp linked and connected? Returns state, linked phone and how many messages are stored locally.",
		schema(map[string]any{}), readOnly},
	{"whatsapp_link", "Open the QR page in the browser so the user can link WhatsApp (phone: Settings > Linked devices > Link a device). Use when status is not 'connected'.",
		schema(map[string]any{}), nil},
	{"whatsapp_list_chats", "List recent chats (people and groups) with the last message, newest first.",
		schema(map[string]any{"limit": num("max chats, default 30")}), readOnly},
	{"whatsapp_find", "Find a contact or group by name or phone number. Returns ids to use in other tools.",
		schema(map[string]any{"query": str("name, part of a name, or phone number")}, "query"), readOnly},
	{"whatsapp_read", "Read messages of one chat, oldest first.",
		schema(map[string]any{
			"chat":        str("chat id, phone number, or contact/group name"),
			"limit":       num("max messages, default 50"),
			"since_hours": num("only messages from the last N hours"),
		}, "chat"), readOnly},
	{"whatsapp_search", "Search message text across all chats (or one chat).",
		schema(map[string]any{
			"query": str("text to look for"),
			"chat":  str("optional: limit to one chat (id, phone or name)"),
			"limit": num("max results, default 30"),
		}, "query"), readOnly},
	{"whatsapp_send", "Send a text message FROM THE USER'S OWN WhatsApp. Only after the user saw the exact text and recipient and said yes.",
		schema(map[string]any{
			"to":      str("chat id, phone number with country code, or exact contact/group name"),
			"message": str("the exact text to send"),
		}, "to", "message"), map[string]any{"destructiveHint": true}},
	{"whatsapp_send_file", "Send a file or image from this computer FROM THE USER'S OWN WhatsApp. Only after the user approved recipient, file and caption.",
		schema(map[string]any{
			"to":      str("chat id, phone number with country code, or exact contact/group name"),
			"path":    str("absolute path of the file on this computer"),
			"caption": str("optional caption"),
		}, "to", "path"), map[string]any{"destructiveHint": true}},
}

func runMCP() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var req rpcReq
		if err := json.Unmarshal(in.Bytes(), &req); err != nil || len(req.ID) == 0 {
			continue // notifications need no answer
		}
		result, rpcErr := handleRPC(req)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		_ = out.Encode(resp)
	}
}

func handleRPC(req rpcReq) (any, map[string]any) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "whatsapp", "version": version},
			"instructions":    "The user's personal WhatsApp. Reading is fine. Never send anything without showing the exact text and recipient and getting an explicit yes for that message.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		text, err := callTool(p.Name, p.Arguments)
		if err != nil {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": "Error: " + err.Error()}}, "isError": true}, nil
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}, nil
	}
	return nil, map[string]any{"code": -32601, "message": "method not found: " + req.Method}
}

func callTool(name string, a map[string]any) (string, error) {
	s := func(k string) string { v, _ := a[k].(string); return v }
	n := func(k string) string {
		if v, ok := a[k].(float64); ok {
			return strconv.Itoa(int(v))
		}
		return ""
	}
	q := func(kv ...string) string {
		v := url.Values{}
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] != "" {
				v.Set(kv[i], kv[i+1])
			}
		}
		return "?" + v.Encode()
	}
	switch name {
	case "whatsapp_status":
		return callService("GET", "/status", nil)
	case "whatsapp_link":
		st, err := callService("GET", "/status", nil)
		if err != nil {
			return "", err
		}
		openBrowser(baseURL() + "/link")
		return "Opened " + baseURL() + "/link in the browser. Ask the user to scan the QR with their phone (WhatsApp > Settings > Linked devices > Link a device), then call whatsapp_status until state is 'connected'. Current status: " + st, nil
	case "whatsapp_list_chats":
		return callService("GET", "/chats"+q("limit", n("limit")), nil)
	case "whatsapp_find":
		return callService("GET", "/find"+q("q", s("query")), nil)
	case "whatsapp_read":
		return callService("GET", "/messages"+q("chat", s("chat"), "limit", n("limit"), "since_hours", n("since_hours")), nil)
	case "whatsapp_search":
		return callService("GET", "/search"+q("q", s("query"), "chat", s("chat"), "limit", n("limit")), nil)
	case "whatsapp_send":
		return callService("POST", "/send", map[string]string{"to": s("to"), "message": s("message")})
	case "whatsapp_send_file":
		return callService("POST", "/send", map[string]string{"to": s("to"), "path": s("path"), "caption": s("caption")})
	}
	return "", fmt.Errorf("unknown tool %s", name)
}
