package main

import (
	"database/sql"

	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// msgStore keeps a local copy of messages so Claude can read and search them.
type msgStore struct{ db *sql.DB }

type storedMsg struct {
	ID       string `json:"id"`
	Chat     string `json:"chat"`
	ChatName string `json:"chat_name,omitempty"`
	Sender   string `json:"sender"`
	Name     string `json:"sender_name,omitempty"`
	FromMe   bool   `json:"from_me"`
	Time     string `json:"time"`
	Text     string `json:"text"`
}

func openMsgStore(path string) (*msgStore, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS messages (
  id TEXT NOT NULL, chat TEXT NOT NULL, sender TEXT, sender_name TEXT,
  from_me INTEGER, ts INTEGER, text TEXT,
  PRIMARY KEY (chat, id));
CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(chat, ts);
CREATE TABLE IF NOT EXISTS chats (jid TEXT PRIMARY KEY, name TEXT);`)
	if err != nil {
		return nil, err
	}
	return &msgStore{db: db}, nil
}

func (s *msgStore) put(m storedMsg, ts time.Time) {
	if strings.TrimSpace(m.Text) == "" {
		return
	}
	_, _ = s.db.Exec(`INSERT INTO messages(id,chat,sender,sender_name,from_me,ts,text) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(chat,id) DO UPDATE SET text=excluded.text`,
		m.ID, m.Chat, m.Sender, m.Name, boolInt(m.FromMe), ts.Unix(), m.Text)
}

func (s *msgStore) setChatName(jid, name string) {
	if name == "" {
		return
	}
	_, _ = s.db.Exec(`INSERT INTO chats(jid,name) VALUES(?,?) ON CONFLICT(jid) DO UPDATE SET name=excluded.name`, jid, name)
}

func (s *msgStore) chatName(jid string) string {
	var n string
	_ = s.db.QueryRow(`SELECT name FROM chats WHERE jid=?`, jid).Scan(&n)
	return n
}

type chatRow struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	LastTime string `json:"last_time"`
	LastText string `json:"last_text"`
	Count    int    `json:"messages_stored"`
}

func (s *msgStore) chats(limit int) ([]chatRow, error) {
	rows, err := s.db.Query(`
SELECT m.chat, COALESCE(c.name,''), MAX(m.ts), COUNT(*),
  (SELECT text FROM messages x WHERE x.chat=m.chat ORDER BY ts DESC LIMIT 1)
FROM messages m LEFT JOIN chats c ON c.jid=m.chat
GROUP BY m.chat ORDER BY MAX(m.ts) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chatRow{}
	for rows.Next() {
		var r chatRow
		var ts int64
		if err := rows.Scan(&r.JID, &r.Name, &ts, &r.Count, &r.LastText); err != nil {
			return nil, err
		}
		r.LastTime = fmtTime(ts)
		r.LastText = clip(r.LastText, 120)
		out = append(out, r)
	}
	return out, nil
}

func (s *msgStore) messages(chat string, limit int, since time.Time) ([]storedMsg, error) {
	rows, err := s.db.Query(`SELECT id,chat,sender,COALESCE(sender_name,''),from_me,ts,text FROM messages
WHERE chat=? AND ts>=? ORDER BY ts DESC LIMIT ?`, chat, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	out, err := scanMsgs(rows)
	// oldest first reads naturally
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}

func (s *msgStore) search(q, chat string, limit int) ([]storedMsg, error) {
	query := `SELECT id,chat,sender,COALESCE(sender_name,''),from_me,ts,text FROM messages WHERE text LIKE ?`
	args := []any{"%" + q + "%"}
	if chat != "" {
		query += ` AND chat=?`
		args = append(args, chat)
	}
	query += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return scanMsgs(rows)
}

func (s *msgStore) count() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n)
	return n
}

func scanMsgs(rows *sql.Rows) ([]storedMsg, error) {
	defer rows.Close()
	out := []storedMsg{}
	for rows.Next() {
		var m storedMsg
		var fromMe int
		var ts int64
		if err := rows.Scan(&m.ID, &m.Chat, &m.Sender, &m.Name, &fromMe, &ts, &m.Text); err != nil {
			return nil, err
		}
		m.FromMe = fromMe == 1
		m.Time = fmtTime(ts)
		out = append(out, m)
	}
	return out, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func fmtTime(ts int64) string { return time.Unix(ts, 0).Local().Format("2006-01-02 15:04") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
