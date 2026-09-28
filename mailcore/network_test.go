package mailcore

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A reply that stops halfway must not leave its remainder in the session
// for the next command to read as its own: the session is dropped, and the
// next use reconnects and gets a whole answer.
func TestIMAPSessionReconnectsAfterMidReplyDrop(t *testing.T) {
	const body = "Subject: hi\r\n\r\nhello there\r\n"
	var mu sync.Mutex
	fetches := 0
	srv := newScriptIMAP(t, "IMAP4rev1", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.send("* 1 EXISTS")
			s.send("* OK [UIDVALIDITY 1]")
			s.send("%s OK selected", tag)
			return true
		case strings.Contains(up, "BODY.PEEK[]"):
			mu.Lock()
			fetches++
			n := fetches
			mu.Unlock()
			if n == 1 {
				s.raw("* 1 FETCH (UID 5 BODY[] {" + itoa(len(body)) + "}\r\nSubject: h")
				s.hangUp = true
				return true
			}
			s.raw("* 1 FETCH (UID 5 BODY[] {" + itoa(len(body)) + "}\r\n" + body + ")\r\n")
			s.send("%s OK fetch", tag)
			return true
		}
		return false
	})

	c := newIMAPClient(plainIMAPConfig(srv.addr()), "ada@example.com")
	if err := c.connect(); err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if _, err := c.selectBox("INBOX", true); err != nil {
		t.Fatal(err)
	}
	_, err := c.uidFetchRFC822(5)
	var lost *imapConnError
	if !errors.As(err, &lost) {
		t.Fatalf("a reply cut off halfway gave %v, want a lost connection", err)
	}
	c.mu.Lock()
	dropped := c.conn == nil
	c.mu.Unlock()
	if !dropped {
		t.Fatal("the session was kept after the connection was lost mid-reply")
	}
	if _, err := c.selectBox("INBOX", true); err != nil {
		t.Fatalf("reconnecting: %v", err)
	}
	raw, err := c.uidFetchRFC822(5)
	if err != nil {
		t.Fatalf("fetch after reconnect: %v", err)
	}
	if string(raw) != body {
		t.Fatalf("fetch after reconnect got %q, want %q", raw, body)
	}
}

// Once the server has failed at the network level, the next fetch a person
// is waiting on says so at once instead of waiting out another connect
// timeout.
func TestForegroundFetchFailsFastWhenUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens there now: every dial is refused

	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: addr, User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	inbox := Folder{ID: "home/inbox", AccountID: "home", Name: "INBOX", Kind: FolderInbox, Remote: "INBOX"}
	st.mu.Lock()
	st.Folders = append(st.Folders, inbox)
	st.Messages = append(st.Messages, Message{ID: "home/inbox:5", Folder: inbox.ID, AccountID: "home", UID: 5, Subject: "s"})
	st.mu.Unlock()

	if _, err := st.GetRaw("home/inbox:5"); err == nil {
		t.Fatal("fetching from a server that refuses connections succeeded")
	}
	start := time.Now()
	_, err = st.GetRaw("home/inbox:5")
	took := time.Since(start)
	var down *UnreachableError
	if !errors.As(err, &down) {
		t.Fatalf("second fetch gave %v, want UnreachableError", err)
	}
	if took > 100*time.Millisecond {
		t.Fatalf("second fetch took %v; it should not have dialled", took)
	}
	if !strings.Contains(err.Error(), "can't reach the mail server") {
		t.Fatalf("error %q does not say what happened", err)
	}
}

// A mark-read that cannot reach the server is kept and sent later, not lost.
func TestFailedFlagPushIsQueuedAndRetried(t *testing.T) {
	var mu sync.Mutex
	dropStore := true
	stores := 0
	srv := newScriptIMAP(t, "IMAP4rev1 UIDPLUS", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case cmd == "LIST":
			s.send(`* LIST (\HasNoChildren) "/" "INBOX"`)
			s.send("%s OK list", tag)
			return true
		case cmd == "LSUB":
			s.send("%s OK lsub", tag)
			return true
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.send("* 1 EXISTS")
			s.send("* OK [UIDVALIDITY 1]")
			s.send("* OK [UIDNEXT 99]")
			s.send("%s OK selected", tag)
			return true
		case strings.Contains(up, "UID STORE"):
			mu.Lock()
			drop := dropStore
			if !drop {
				stores++
			}
			mu.Unlock()
			if drop {
				s.hangUp = true
				return true
			}
			s.send("%s OK store", tag)
			return true
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "(UID)"):
			s.send("* 1 FETCH (UID 7)")
			s.send("%s OK fetch", tag)
			return true
		case strings.Contains(up, "UID FETCH"):
			s.send(`* 1 FETCH (UID 7 FLAGS () RFC822.SIZE 10 ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" "m7" (("B" NIL "b" "ex.com")) NIL NIL (("A" NIL "a" "ex.com")) NIL NIL NIL "<m7@ex>"))`)
			s.send("%s OK fetch", tag)
			return true
		}
		return false
	})

	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: srv.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs := st.ListMessages("home/inbox")
	if len(msgs) != 1 {
		t.Fatalf("cached %d messages, want 1", len(msgs))
	}
	id := msgs[0].ID

	if err := st.SetFlags(id, FlagPatch{Read: BoolPtr(true)}); err != nil {
		t.Fatalf("SetFlags: %v", err)
	}
	if m, _ := st.CachedMessage(id); !m.Read {
		t.Fatal("the cache lost the change when the push failed")
	}
	var queued []OutboxOp
	for _, op := range st.ListOutbox() {
		if op.Kind == "flag" && op.MessageID == id {
			queued = append(queued, op)
		}
	}
	if len(queued) != 1 {
		t.Fatalf("%d flag ops queued after a failed push, want 1", len(queued))
	}

	mu.Lock()
	dropStore = false
	mu.Unlock()
	if _, err := st.flushOutbox(func(op OutboxOp) bool { return op.Kind == "flag" }); err != nil {
		t.Fatalf("retry: %v", err)
	}
	mu.Lock()
	got := stores
	mu.Unlock()
	if got != 1 {
		t.Fatalf("server saw %d UID STOREs on retry, want 1", got)
	}
	if n := len(st.ListOutbox()); n != 0 {
		t.Fatalf("%d ops still queued after a successful retry", n)
	}
}

// After a sync, recent small messages outside Junk and Trash are downloaded
// in the background, so opening one needs no round trip; old, large and
// junk messages wait for a click.
func TestSyncPrefetchesRecentBodies(t *testing.T) {
	now := time.Now()
	type msg struct {
		uid  uint32
		date time.Time
		size int
	}
	boxes := map[string][]msg{
		"INBOX": {
			{1, now.Add(-time.Hour), 400},           // fetched
			{2, now.Add(-60 * 24 * time.Hour), 400}, // too old
			{3, now.Add(-time.Hour), 5 << 20},       // too large
		},
		"Junk": {{4, now.Add(-time.Hour), 400}}, // junk
	}
	var mu sync.Mutex
	fetched := map[uint32]int{}
	srv := newScriptIMAP(t, "IMAP4rev1 UIDPLUS", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case cmd == "LIST":
			s.send(`* LIST (\HasNoChildren) "/" "INBOX"`)
			s.send(`* LIST (\HasNoChildren \Junk) "/" "Junk"`)
			s.send("%s OK list", tag)
			return true
		case cmd == "LSUB":
			s.send("%s OK lsub", tag)
			return true
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.box = strings.Trim(strings.Fields(line)[2], `"`)
			s.send("* %d EXISTS", len(boxes[s.box]))
			s.send("* OK [UIDVALIDITY 1]")
			s.send("* OK [UIDNEXT 99]")
			s.send("%s OK selected", tag)
			return true
		case strings.Contains(up, "BODY.PEEK[]"):
			var uid uint32
			for _, f := range strings.Fields(line) {
				if n := atoi(f); n > 0 {
					uid = uint32(n)
					break
				}
			}
			mu.Lock()
			fetched[uid]++
			mu.Unlock()
			body := "Subject: m\r\n\r\nbody of " + itoa(int(uid)) + "\r\n"
			s.raw("* 1 FETCH (UID " + itoa(int(uid)) + " BODY[] {" + itoa(len(body)) + "}\r\n" + body + ")\r\n")
			s.send("%s OK fetch", tag)
			return true
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "(UID)"):
			for i, m := range boxes[s.box] {
				s.send("* %d FETCH (UID %d)", i+1, m.uid)
			}
			s.send("%s OK fetch", tag)
			return true
		case strings.Contains(up, "UID FETCH"):
			for i, m := range boxes[s.box] {
				s.send(`* %d FETCH (UID %d FLAGS () RFC822.SIZE %d ENVELOPE ("%s" "m%d" (("B" NIL "b" "ex.com")) NIL NIL (("A" NIL "a" "ex.com")) NIL NIL NIL "<m%d@ex>"))`,
					i+1, m.uid, m.size, m.date.Format(time.RFC1123Z), m.uid, m.uid)
			}
			s.send("%s OK fetch", tag)
			return true
		}
		return false
	})

	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: srv.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for st.prefetching.Load() || func() bool { mu.Lock(); defer mu.Unlock(); return fetched[1] == 0 }() {
		if time.Now().After(deadline) {
			t.Fatal("the recent message was never prefetched")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	got := map[uint32]int{}
	for k, v := range fetched {
		got[k] = v
	}
	mu.Unlock()
	if got[1] != 1 || len(got) != 1 {
		t.Fatalf("prefetched %v, want only UID 1 once", got)
	}

	m, ok := st.GetMessage("home/inbox:1")
	if !ok || !strings.Contains(m.Body, "body of 1") {
		t.Fatalf("prefetched message has body %q", m.Body)
	}
	mu.Lock()
	again := fetched[1]
	mu.Unlock()
	if again != 1 {
		t.Fatalf("opening a prefetched message fetched it again (%d fetches)", again)
	}
}
