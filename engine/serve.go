package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type engine struct {
	client *whatsmeow.Client
	store  *msgStore
	token  string

	mu      sync.Mutex
	qrCode  string // current QR payload while pairing
	pairing bool   // a pairing round is running
}

var ctx = context.Background()

func serve() {
	home := homeDir()
	_ = os.MkdirAll(home, 0o700)
	logf("starting whatsapp-claude %s, data in %s", version, home)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(home, "device.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		fatal(err)
	}
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Stdout("DB", "WARN", false))
	if err := container.Upgrade(ctx); err != nil {
		fatal(err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		fatal(err)
	}
	ms, err := openMsgStore(filepath.Join(home, "messages.db"))
	if err != nil {
		fatal(err)
	}

	e := &engine{store: ms, token: loadOrCreateToken()}
	e.client = whatsmeow.NewClient(device, waLog.Stdout("WA", "WARN", false))
	e.client.AddEventHandler(e.onEvent)

	if e.client.Store.ID == nil {
		go e.pair()
	} else if err := e.client.Connect(); err != nil {
		logf("connect failed: %v", err)
	}

	e.routes()
	logf("listening on %s", baseURL())
	if err := http.ListenAndServe("127.0.0.1:"+port(), nil); err != nil {
		fatal(err)
	}
}

// pair runs up to 3 QR rounds (~2 minutes each). The QR page shows the live code.
func (e *engine) pair() {
	e.mu.Lock()
	if e.pairing || e.client.Store.ID != nil {
		e.mu.Unlock()
		return
	}
	e.pairing = true
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.pairing = false; e.qrCode = ""; e.mu.Unlock() }()

	for round := 0; round < 3; round++ {
		if e.client.IsConnected() {
			e.client.Disconnect()
		}
		qrChan, err := e.client.GetQRChannel(ctx)
		if err != nil {
			logf("qr channel: %v", err)
			return
		}
		if err := e.client.Connect(); err != nil {
			logf("connect for pairing: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		for evt := range qrChan {
			switch evt.Event {
			case whatsmeow.QRChannelEventCode:
				e.mu.Lock()
				e.qrCode = evt.Code
				e.mu.Unlock()
			case "success":
				logf("linked to WhatsApp")
				return
			default:
				logf("pairing event: %s", evt.Event)
			}
		}
	}
	logf("pairing stopped after 3 rounds; open the QR page to start again")
}

func (e *engine) state() string {
	switch {
	case e.client.Store.ID != nil && e.client.IsLoggedIn():
		return "connected"
	case e.client.Store.ID != nil:
		return "linked_reconnecting"
	case e.pairing:
		return "waiting_for_qr_scan"
	default:
		return "not_linked"
	}
}

func (e *engine) onEvent(evt any) {
	switch v := evt.(type) {
	case *events.Message:
		e.saveMessage(v)
	case *events.HistorySync:
		n := 0
		for _, conv := range v.Data.GetConversations() {
			chat, err := types.ParseJID(conv.GetID())
			if err != nil {
				continue
			}
			e.store.setChatName(e.normalize(chat).String(), conv.GetName())
			for _, hm := range conv.GetMessages() {
				if m, err := e.client.ParseWebMessage(chat, hm.GetMessage()); err == nil {
					e.saveMessage(m)
					n++
				}
			}
		}
		logf("history sync: %d messages", n)
	case *events.GroupInfo:
		if v.Name != nil {
			e.store.setChatName(v.JID.String(), v.Name.Name)
		}
	case *events.LoggedOut:
		// Unlinked from the phone: forget the device and wait for a new QR scan.
		logf("logged out by WhatsApp (reason %v); clearing device", v.Reason)
		_ = e.client.Store.Delete(ctx)
		os.Exit(0) // the service manager restarts us in pairing mode
	case *events.Connected:
		logf("connected")
		go e.refreshGroupNames()
	}
}

func (e *engine) refreshGroupNames() {
	groups, err := e.client.GetJoinedGroups(ctx)
	if err != nil {
		return
	}
	for _, g := range groups {
		e.store.setChatName(g.JID.String(), g.Name)
	}
}

// normalize maps hidden "@lid" ids to phone-number ids when WhatsApp knows them.
func (e *engine) normalize(j types.JID) types.JID {
	if j.Server == types.HiddenUserServer {
		if pn, err := e.client.Store.LIDs.GetPNForLID(ctx, j); err == nil && !pn.IsEmpty() {
			return pn.ToNonAD()
		}
	}
	return j.ToNonAD()
}

func (e *engine) saveMessage(m *events.Message) {
	text := messageText(m.Message)
	if text == "" {
		return
	}
	chat := e.normalize(m.Info.Chat)
	sender := e.normalize(m.Info.Sender)
	e.store.put(storedMsg{
		ID: m.Info.ID, Chat: chat.String(), Sender: sender.String(),
		Name: m.Info.PushName, FromMe: m.Info.IsFromMe, Text: text,
	}, m.Info.Timestamp)
}

func messageText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if msg.GetEphemeralMessage() != nil {
		return messageText(msg.GetEphemeralMessage().GetMessage())
	}
	if msg.GetViewOnceMessage() != nil {
		return messageText(msg.GetViewOnceMessage().GetMessage())
	}
	switch {
	case msg.GetConversation() != "":
		return msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetText()
	case msg.GetImageMessage() != nil:
		return strings.TrimSpace("[image] " + msg.GetImageMessage().GetCaption())
	case msg.GetVideoMessage() != nil:
		return strings.TrimSpace("[video] " + msg.GetVideoMessage().GetCaption())
	case msg.GetAudioMessage() != nil:
		return "[voice note]"
	case msg.GetDocumentMessage() != nil:
		d := msg.GetDocumentMessage()
		return strings.TrimSpace("[file: " + d.GetFileName() + "] " + d.GetCaption())
	case msg.GetStickerMessage() != nil:
		return "[sticker]"
	case msg.GetContactMessage() != nil:
		return "[contact] " + msg.GetContactMessage().GetDisplayName()
	case msg.GetLocationMessage() != nil:
		return "[location]"
	case msg.GetPollCreationMessage() != nil:
		return "[poll] " + msg.GetPollCreationMessage().GetName()
	}
	return ""
}

// displayName finds a human name for a chat or person.
func (e *engine) displayName(jid string) string {
	if n := e.store.chatName(jid); n != "" {
		return n
	}
	j, err := types.ParseJID(jid)
	if err != nil {
		return ""
	}
	if c, err := e.client.Store.Contacts.GetContact(ctx, j); err == nil && c.Found {
		for _, n := range []string{c.FullName, c.FirstName, c.BusinessName, c.PushName} {
			if n != "" {
				return n
			}
		}
	}
	return ""
}

type candidate struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// findChats matches a name or phone against contacts and groups.
func (e *engine) findChats(q string) []candidate {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	if strings.Contains(q, "@") {
		return []candidate{{JID: q, Name: e.displayName(q), Kind: "id"}}
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q)
	if len(digits) >= 8 && len(digits) >= len([]rune(q))-4 {
		jid := digits + "@s.whatsapp.net"
		return []candidate{{JID: jid, Name: e.displayName(jid), Kind: "phone"}}
	}
	lq := strings.ToLower(q)
	seen := map[string]bool{}
	out := []candidate{}
	add := func(jid, name, kind string) {
		if !seen[jid] && strings.Contains(strings.ToLower(name), lq) {
			seen[jid] = true
			out = append(out, candidate{JID: jid, Name: name, Kind: kind})
		}
	}
	rows, err := e.store.db.Query(`SELECT jid, name FROM chats`)
	if err == nil {
		for rows.Next() {
			var jid, name string
			_ = rows.Scan(&jid, &name)
			kind := "chat"
			if strings.HasSuffix(jid, "@g.us") {
				kind = "group"
			}
			add(jid, name, kind)
		}
		rows.Close()
	}
	if contacts, err := e.client.Store.Contacts.GetAllContacts(ctx); err == nil {
		for j, c := range contacts {
			if j.Server != types.DefaultUserServer {
				continue
			}
			for _, n := range []string{c.FullName, c.FirstName, c.BusinessName, c.PushName} {
				if n != "" {
					add(j.String(), n, "contact")
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// resolve turns "name / phone / id" into exactly one chat id or an error listing options.
func (e *engine) resolve(q string) (string, error) {
	c := e.findChats(q)
	if len(c) == 1 {
		return c[0].JID, nil
	}
	for _, x := range c {
		if strings.EqualFold(x.Name, q) {
			return x.JID, nil
		}
	}
	if len(c) == 0 {
		return "", fmt.Errorf("no chat or contact matches %q", q)
	}
	b, _ := json.Marshal(c)
	return "", fmt.Errorf("%q matches several chats, use one of these ids: %s", q, b)
}

// ---- HTTP ----

func (e *engine) routes() {
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		st := map[string]any{"state": e.state(), "version": version, "messages_stored": e.store.count()}
		if id := e.client.Store.ID; id != nil {
			st["phone"] = "+" + id.User
			st["name"] = e.client.Store.PushName
		}
		writeJSON(w, st)
	})
	http.HandleFunc("/link", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(linkPage))
	})
	http.HandleFunc("/link.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && e.client.Store.ID == nil {
			go e.pair()
			time.Sleep(1500 * time.Millisecond)
		}
		e.mu.Lock()
		code := e.qrCode
		e.mu.Unlock()
		out := map[string]any{"state": e.state()}
		if code != "" && e.client.Store.ID == nil {
			if png, err := qrcode.Encode(code, qrcode.Medium, 320); err == nil {
				out["qr"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
			}
		}
		writeJSON(w, out)
	})

	api := func(path string, h func(w http.ResponseWriter, r *http.Request)) {
		http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+e.token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		})
	}
	api("/chats", func(w http.ResponseWriter, r *http.Request) {
		rows, err := e.store.chats(intParam(r, "limit", 30))
		if err != nil {
			httpErr(w, err)
			return
		}
		for i := range rows {
			if rows[i].Name == "" {
				rows[i].Name = e.displayName(rows[i].JID)
			}
		}
		writeJSON(w, rows)
	})
	api("/find", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, e.findChats(r.URL.Query().Get("q")))
	})
	api("/messages", func(w http.ResponseWriter, r *http.Request) {
		chat, err := e.resolve(r.URL.Query().Get("chat"))
		if err != nil {
			httpErr(w, err)
			return
		}
		since := time.Time{}
		if h := intParam(r, "since_hours", 0); h > 0 {
			since = time.Now().Add(-time.Duration(h) * time.Hour)
		}
		msgs, err := e.store.messages(chat, intParam(r, "limit", 50), since)
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"chat": chat, "chat_name": e.displayName(chat), "messages": e.named(msgs)})
	})
	api("/search", func(w http.ResponseWriter, r *http.Request) {
		chat := ""
		if c := r.URL.Query().Get("chat"); c != "" {
			var err error
			if chat, err = e.resolve(c); err != nil {
				httpErr(w, err)
				return
			}
		}
		msgs, err := e.store.search(r.URL.Query().Get("q"), chat, intParam(r, "limit", 30))
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, e.named(msgs))
	})
	api("/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ To, Message, Path, Caption string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err)
			return
		}
		res, err := e.send(req.To, req.Message, req.Path, req.Caption)
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, res)
	})
	api("/unlink", func(w http.ResponseWriter, r *http.Request) {
		if err := e.client.Logout(ctx); err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]string{"status": "unlinked"})
		go func() { time.Sleep(time.Second); os.Exit(0) }()
	})
}

func (e *engine) named(msgs []storedMsg) []storedMsg {
	for i := range msgs {
		if msgs[i].FromMe {
			msgs[i].Name = "me"
		} else if msgs[i].Name == "" {
			msgs[i].Name = e.displayName(msgs[i].Sender)
		}
		msgs[i].ChatName = e.displayName(msgs[i].Chat)
	}
	return msgs
}

func (e *engine) send(to, text, path, caption string) (map[string]any, error) {
	if !e.client.IsLoggedIn() {
		return nil, fmt.Errorf("WhatsApp is not connected (state %s)", e.state())
	}
	jidStr, err := e.resolve(to)
	if err != nil {
		return nil, err
	}
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return nil, err
	}
	var msg *waE2E.Message
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
		if mt == "" {
			mt = "application/octet-stream"
		}
		if strings.HasPrefix(mt, "image/") && (strings.HasSuffix(mt, "jpeg") || strings.HasSuffix(mt, "png")) {
			up, err := e.client.Upload(ctx, data, whatsmeow.MediaImage)
			if err != nil {
				return nil, err
			}
			msg = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
				Mimetype: proto.String(mt), FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
				FileLength: proto.Uint64(up.FileLength), Caption: optStr(caption),
			}}
		} else {
			up, err := e.client.Upload(ctx, data, whatsmeow.MediaDocument)
			if err != nil {
				return nil, err
			}
			name := filepath.Base(path)
			msg = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
				URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
				Mimetype: proto.String(mt), FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
				FileLength: proto.Uint64(up.FileLength), FileName: proto.String(name), Title: proto.String(name),
				Caption: optStr(caption),
			}}
		}
	} else {
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("empty message")
		}
		msg = &waE2E.Message{Conversation: proto.String(text)}
	}
	resp, err := e.client.SendMessage(ctx, jid, msg)
	if err != nil {
		return nil, err
	}
	saved := text
	if path != "" {
		saved = strings.TrimSpace("[file: " + filepath.Base(path) + "] " + caption)
	}
	e.store.put(storedMsg{ID: resp.ID, Chat: jid.String(), Sender: "me", FromMe: true, Text: saved}, resp.Timestamp)
	return map[string]any{"status": "sent", "to": jid.String(), "to_name": e.displayName(jid.String()), "id": resp.ID}, nil
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return proto.String(s)
}

func intParam(r *http.Request, k string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(k)); err == nil && v > 0 {
		return v
	}
	return def
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error) { http.Error(w, err.Error(), http.StatusBadRequest) }

func logf(f string, a ...any) {
	fmt.Printf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(f, a...))
}

func fatal(err error) {
	logf("fatal: %v", err)
	os.Exit(1)
}
