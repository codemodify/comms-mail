package mailcore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func searchIDs(st *LocalStore, q string) map[MessageID]bool {
	out := map[MessageID]bool{}
	for _, m := range st.Search(SearchQuery{Filter: Filter{Query: q}}) {
		out[m.ID] = true
	}
	return out
}

// The text of a message is found wherever the words sit in it — mid-word,
// across several words, in HTML-only mail — from the index, which a restart
// keeps rather than rebuilds.
func TestSearchFindsTextFromTheIndex(t *testing.T) {
	dir := t.TempDir()
	st := newTestLocalStore(t, dir)
	seedCache(st, 3)
	st.mu.Lock()
	st.Messages[0].Body = "Please find the quarterly report attached."
	st.Messages[1].HTML = "<html><body><p>Your <b>invoice</b> for September</p></body></html>"
	st.saveLocked()
	st.mu.Unlock()

	for q, want := range map[string]MessageID{
		"quarterly repo": "a/inbox:1", // two words, the last one partial
		"RTERLY":         "a/inbox:1", // mid-word, any case
		"invoice for":    "a/inbox:2", // HTML-only: its text, not its tags
		"bob@exam":       "",          // a header: every message
	} {
		got := searchIDs(st, q)
		if want == "" {
			if len(got) != 3 {
				t.Errorf("%q found %v, want every message", q, got)
			}
			continue
		}
		if len(got) != 1 || !got[want] {
			t.Errorf("%q found %v, want %s", q, got, want)
		}
	}
	if got := searchIDs(st, "<b>"); len(got) != 0 {
		t.Errorf("markup is not text: %v", got)
	}

	// The index answered those, and holds the text across a restart.
	var n int
	if err := st.sqlc.db.QueryRow("SELECT count(*) FROM messages WHERE indexed <> 0").Scan(&n); err != nil || n != 2 {
		t.Fatalf("indexed rows = %d (%v), want 2", n, err)
	}
	again := newTestLocalStore(t, dir)
	again.mu.Lock()
	hits, err := again.sqlc.textHits("invoice")
	again.mu.Unlock()
	if err != nil || !hits["a/inbox:2"] || len(hits) != 1 {
		t.Fatalf("after restart the index gave %v, %v", hits, err)
	}

	// A message that goes leaves the index with it.
	again.mu.Lock()
	again.Messages = again.Messages[:1]
	again.saveLocked()
	hits, _ = again.sqlc.textHits("invoice")
	again.mu.Unlock()
	if len(hits) != 0 {
		t.Fatalf("a deleted message is still found: %v", hits)
	}
}

// Text not yet saved to the index is read instead, and a query too short
// for it looks in the text part.
func TestSearchReadsTextNotYetIndexed(t *testing.T) {
	st := newTestLocalStore(t, t.TempDir())
	seedCache(st, 2)
	st.mu.Lock()
	st.Messages[1].HTML = "<p>fresh from the server</p>"
	st.Messages[0].Body = "a b c"
	st.mu.Unlock()
	if got := searchIDs(st, "from the"); len(got) != 1 || !got["a/inbox:2"] {
		t.Fatalf("unsaved text: %v", got)
	}
	if got := searchIDs(st, "b"); !got["a/inbox:1"] || !got["a/inbox:2"] {
		// "b": in bob@example.org for both, and in the first one's text
		t.Fatalf("short query: %v", got)
	}
}

// A cache from before the index (schema 1) opens, keeps its messages, and
// has their text indexed.
func TestSchemaOneCacheGetsTheIndex(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE kv (key TEXT PRIMARY KEY, value BLOB NOT NULL)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, folder TEXT NOT NULL, data BLOB NOT NULL, body TEXT NOT NULL DEFAULT '', html TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE folder_meta (folder TEXT PRIMARY KEY, data BLOB NOT NULL)`,
		`INSERT INTO messages VALUES('a/inbox:1', 'a/inbox', '{"ID":"a/inbox:1","Folder":"a/inbox","Subject":"old"}', 'kept from before', '')`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	_ = db.Close()

	st := newTestLocalStore(t, dir)
	if st.sqlc == nil || len(st.Messages) != 1 {
		t.Fatalf("the upgraded cache: %v, %d messages", st.health, len(st.Messages))
	}
	st.mu.Lock()
	hits, err := st.sqlc.textHits("from before")
	st.mu.Unlock()
	if err != nil || !hits["a/inbox:1"] {
		t.Fatalf("index after the upgrade: %v, %v", hits, err)
	}
}

// A list row carries no text; the message itself does.
func TestListRowsLeaveTheTextOut(t *testing.T) {
	m := Message{ID: "x", Subject: "s", HTML: "<p>Hello   there</p>"}
	r := listRow(m)
	if r.Body != "" || r.HTML != "" || r.Snippet != "Hello there" {
		t.Fatalf("row = %+v", r)
	}
	if full := fullMessage(m); full.Body == "" || full.HTML == "" {
		t.Fatalf("full = %+v", full)
	}
}
