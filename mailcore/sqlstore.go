package mailcore

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // pure Go: comms-maild stays cgo-free
)

// The on-disk cache is one SQLite database, mail.db, beside the raw .eml
// blobs. It replaced a dozen JSON files that every save rewrote whole —
// 8.6 MB of messages.json on a real Inbox, for every message marked read,
// with the store lock held. Per-folder sync state (UIDVALIDITY, UIDNEXT,
// HIGHESTMODSEQ) is in it too, in folder_meta.
//
// LocalStore still works on its in-memory slices; the database is where
// they persist. A save writes only what changed since the last one: each
// message row carries a fingerprint of its fields, and the small
// collections (accounts, folders, tags, …) are rewritten only when their
// encoding differs. All of one save is a single transaction, so a crash
// leaves either the old state or the new one.
//
// Search reads a full-text index, message_text: each downloaded message's
// display text (the HTML rendered to text for HTML-only mail) in trigrams,
// so any substring of three or more characters is looked up, not scanned.
// It is written in the same transaction as the message row, which records
// in "indexed" which text the index holds for it. The index keeps no copy
// of the text (contentless); the messages table has it.

const (
	dbFileName = "mail.db"
	// dbVersion is PRAGMA user_version for the schema below. 2 added the
	// search index (message_text, messages.indexed).
	dbVersion = 2
)

const dbSchema = `
CREATE TABLE IF NOT EXISTS kv (
	key   TEXT PRIMARY KEY,
	value BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id     TEXT PRIMARY KEY,
	folder TEXT NOT NULL,
	data   BLOB NOT NULL,
	body   TEXT NOT NULL DEFAULT '',
	html   TEXT NOT NULL DEFAULT '',
	indexed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_folder ON messages(folder);
CREATE TABLE IF NOT EXISTS folder_meta (
	folder TEXT PRIMARY KEY,
	data   BLOB NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS message_text USING fts5(
	text, content='', contentless_delete=1, tokenize='trigram'
);
`

// sqlCache is LocalStore's handle on mail.db and what it last wrote there.
type sqlCache struct {
	db *sql.DB
	// stamps is each message row's fingerprint as last written.
	stamps map[MessageID]uint64
	// kv is each collection's encoding as last written.
	kv map[string][]byte
	// text is, for each message, which text the search index holds for it
	// (textStamp; 0 or absent: none).
	text map[MessageID]int64
}

// openSQLCache opens (creating) dir/mail.db with the schema in place. The
// file is created 0600 before SQLite opens it; SQLite gives its -wal and
// -shm files the database file's mode.
func openSQLCache(dir string) (*sqlCache, error) {
	path := filepath.Join(dir, dbFileName)
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err == nil {
		_ = f.Close()
	} else {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	// journal_size_limit: a WAL grown by one large write (the first index,
	// a big sync) is cut back after its checkpoint, not kept at its largest.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=journal_size_limit(8388608)")
	if err != nil {
		return nil, err
	}
	// One connection: every write already happens under LocalStore.mu, and
	// SQLite serialises writers anyway.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(dbSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mail: %s: %w", path, err)
	}
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mail: %s: %w", path, err)
	}
	if v > dbVersion {
		_ = db.Close()
		return nil, fmt.Errorf("mail: %s has schema %d, newer than this comms-maild (%d)", path, v, dbVersion)
	}
	var hasIndexed int
	if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('messages') WHERE name = 'indexed'").Scan(&hasIndexed); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mail: %s: %w", path, err)
	}
	if hasIndexed == 0 {
		// From schema 1. The rows are kept; the index fills on the next
		// save, as every message with text then differs from what it
		// holds (nothing).
		if _, err := db.Exec("ALTER TABLE messages ADD COLUMN indexed INTEGER NOT NULL DEFAULT 0"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("mail: %s: %w", path, err)
		}
	}
	if v < dbVersion {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", dbVersion)); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &sqlCache{db: db, stamps: map[MessageID]uint64{}, kv: map[string][]byte{}, text: map[MessageID]int64{}}, nil
}

// kvSlots are LocalStore's small collections and where each lives.
func (s *LocalStore) kvSlots() []struct {
	key  string
	dest any
} {
	return []struct {
		key  string
		dest any
	}{
		{"accounts", &s.accounts},
		{"identities", &s.identities},
		{"folders", &s.Folders},
		{"tags", &s.tags},
		{"rules", &s.rules},
		{"smart", &s.feat.smart},
		{"vip", &s.feat.vips},
		{"muted", &s.feat.muted},
		{"categories", &s.feat.cats},
		{"notify", &s.feat.notify},
		{"outbox", &s.feat.outbox},
		{"invites", &s.feat.answers},
		{"imageSenders", &s.feat.imageSenders},
		{"addressBook", &s.feat.saved},
	}
}

// loadSQL fills s from the database. Rows that do not decode are skipped
// and reported; the next sync re-reads their messages from the server.
func (s *LocalStore) loadSQL() error {
	c := s.sqlc
	var bad []string
	for _, sl := range s.kvSlots() {
		var raw []byte
		err := c.db.QueryRow("SELECT value FROM kv WHERE key = ?", sl.key).Scan(&raw)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, sl.dest); err != nil {
			bad = append(bad, sl.key)
			continue
		}
		c.kv[sl.key] = raw
	}
	rows, err := c.db.Query("SELECT id, data, body, html, indexed FROM messages ORDER BY rowid")
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Messages = s.Messages[:0]
	skipped := 0
	for rows.Next() {
		var id string
		var data []byte
		var body, html string
		var indexed int64
		if err := rows.Scan(&id, &data, &body, &html, &indexed); err != nil {
			return err
		}
		var m Message
		if err := json.Unmarshal(data, &m); err != nil || m.ID != MessageID(id) {
			skipped++
			continue
		}
		m.Body, m.HTML = body, html
		c.stamps[m.ID] = messageStamp(&m) // as stored
		if indexed != 0 {
			c.text[m.ID] = indexed
		}
		// Older syncs made other clients' bookkeeping keywords ($Forwarded,
		// NonJunk, $label1…) into tags; tidied after stamping, so the next
		// save writes the change.
		normalizeTags(&m)
		s.Messages = append(s.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if skipped > 0 {
		bad = append(bad, fmt.Sprintf("%d message rows", skipped))
	}
	if len(bad) > 0 {
		s.health = fmt.Errorf("mail: skipped unreadable cache entries (%v); re-sync to refill", bad)
	}
	return nil
}

// saveSQL writes what changed since the last save, in one transaction.
func (s *LocalStore) saveSQL() error {
	c := s.sqlc
	type kvWrite struct {
		key string
		val []byte
	}
	var kvs []kvWrite
	for _, sl := range s.kvSlots() {
		raw, err := json.Marshal(sl.dest)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, c.kv[sl.key]) {
			kvs = append(kvs, kvWrite{sl.key, raw})
		}
	}
	var changed []int
	live := make(map[MessageID]bool, len(s.Messages))
	stamps := make([]uint64, len(s.Messages))
	for i := range s.Messages {
		m := &s.Messages[i]
		live[m.ID] = true
		st := messageStamp(m)
		stamps[i] = st
		if old, ok := c.stamps[m.ID]; !ok || old != st || c.text[m.ID] != textStamp(m) {
			changed = append(changed, i)
		}
	}
	var gone []MessageID
	for id := range c.stamps {
		if !live[id] {
			gone = append(gone, id)
		}
	}
	if len(kvs) == 0 && len(changed) == 0 && len(gone) == 0 {
		return nil
	}

	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, w := range kvs {
		if _, err := tx.Exec("INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", w.key, w.val); err != nil {
			return err
		}
	}
	if len(gone) > 0 {
		del, err := tx.Prepare("DELETE FROM messages WHERE id = ? RETURNING rowid")
		if err != nil {
			return err
		}
		unindex, err := tx.Prepare("DELETE FROM message_text WHERE rowid = ?")
		if err != nil {
			return err
		}
		for _, id := range gone {
			var rowid int64
			switch err := del.QueryRow(string(id)).Scan(&rowid); err {
			case nil:
			case sql.ErrNoRows:
				continue
			default:
				return err
			}
			if c.text[id] != 0 {
				if _, err := unindex.Exec(rowid); err != nil {
					return err
				}
			}
		}
		_ = del.Close()
		_ = unindex.Close()
	}
	if len(changed) > 0 {
		// ON CONFLICT … DO UPDATE keeps a row's rowid, so the table keeps
		// the order messages arrived in.
		up, err := tx.Prepare(`INSERT INTO messages(id, folder, data, body, html, indexed) VALUES(?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET folder = excluded.folder, data = excluded.data, body = excluded.body, html = excluded.html, indexed = excluded.indexed
			RETURNING rowid`)
		if err != nil {
			return err
		}
		index, err := tx.Prepare("INSERT OR REPLACE INTO message_text(rowid, text) VALUES(?, ?)")
		if err != nil {
			return err
		}
		unindex, err := tx.Prepare("DELETE FROM message_text WHERE rowid = ?")
		if err != nil {
			return err
		}
		for _, i := range changed {
			m := s.Messages[i]
			ts := textStamp(&m)
			var text string
			if ts != c.text[m.ID] && ts != 0 {
				text = DisplayBody(m)
			}
			body, html := m.Body, m.HTML
			m.Body, m.HTML = "", ""
			data, err := json.Marshal(m)
			if err != nil {
				return err
			}
			var rowid int64
			if err := up.QueryRow(string(m.ID), string(m.Folder), data, body, html, ts).Scan(&rowid); err != nil {
				return err
			}
			switch {
			case ts == c.text[m.ID]:
			case ts == 0:
				if _, err := unindex.Exec(rowid); err != nil {
					return err
				}
			default:
				if _, err := index.Exec(rowid, text); err != nil {
					return err
				}
			}
		}
		_ = up.Close()
		_ = index.Close()
		_ = unindex.Close()
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, w := range kvs {
		c.kv[w.key] = w.val
	}
	for _, id := range gone {
		delete(c.stamps, id)
		delete(c.text, id)
	}
	for _, i := range changed {
		m := &s.Messages[i]
		c.stamps[m.ID] = stamps[i]
		if ts := textStamp(m); ts != 0 {
			c.text[m.ID] = ts
		} else {
			delete(c.text, m.ID)
		}
	}
	return nil
}

// textStamp says which text of m the search index should hold: 0 for none
// (not downloaded). A message's text does not change once parsed, so the
// lengths tell one text from another.
func textStamp(m *Message) int64 {
	if !messageHasBody(*m) {
		return 0
	}
	return int64(len(m.Body))<<32 ^ int64(len(m.HTML))
}

// textHits returns the messages whose indexed text contains q, ignoring
// case (q of three or more characters: the index holds trigrams).
func (c *sqlCache) textHits(q string) (map[MessageID]bool, error) {
	rows, err := c.db.Query(`SELECT m.id FROM message_text t JOIN messages m ON m.rowid = t.rowid
		WHERE message_text MATCH ?`, `"`+strings.ReplaceAll(q, `"`, `""`)+`"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := map[MessageID]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		hits[MessageID(id)] = true
	}
	return hits, rows.Err()
}

// messageStamp fingerprints every field of m that is stored. Body and HTML
// count by length: a message's text does not change once parsed, only
// whether it has been. TestMessageStampCoversEveryField fails when a field
// is added to Message and not here.
func messageStamp(m *Message) uint64 {
	h := fnv.New64a()
	str := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	num := func(v uint64) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], v)
		_, _ = h.Write(b[:])
	}
	flag := func(v bool) {
		if v {
			num(1)
		} else {
			num(0)
		}
	}
	str(string(m.ID))
	str(string(m.Folder))
	str(m.AccountID)
	str(m.From)
	str(m.To)
	str(m.Cc)
	str(m.Bcc)
	str(m.Subject)
	num(uint64(m.Date.UnixNano()))
	num(uint64(m.Size))
	flag(m.Read)
	flag(m.Starred)
	flag(m.HasAttach)
	strs(h, m.Tags)
	num(uint64(len(m.Body)))
	num(uint64(len(m.HTML)))
	str(m.Snippet)
	num(uint64(m.UID))
	num(uint64(len(m.Parts)))
	for _, p := range m.Parts {
		str(p.ID)
		str(p.MIMEType)
		str(p.Filename)
		num(uint64(p.Size))
		str(p.Charset)
		flag(p.Inline)
	}
	str(m.IdentityID)
	str(m.RFCMessageID)
	str(m.ReplyTo)
	str(m.InReplyTo)
	str(m.References)
	str(m.ThreadID)
	str(m.Category)
	strs(h, m.Attachments)
	flag(m.SignatureInBody)
	strs(h, m.Keywords)
	flag(m.Answered)
	flag(m.Forwarded)
	keys := make([]string, 0, len(m.Headers))
	for k := range m.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		str(k)
		str(m.Headers[k])
	}
	num(uint64(len(keys)))
	return h.Sum64()
}

func strs(h hash.Hash64, list []string) {
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(list)))
	_, _ = h.Write(n[:])
	for _, s := range list {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
}
