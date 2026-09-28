package mailcore

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSharedSessionKeepsItsMailbox drives a sync of Junk and a flag push to
// an Inbox message through the one shared IMAP session at the same time.
// The server holds the Junk envelope fetch long enough for the push to be
// waiting on the session; without the mailbox lock, the push's SELECT INBOX
// lands between the Junk commands, the Junk UID list comes back as Inbox's,
// and reconciliation deletes Junk's messages from the cache.
func TestSharedSessionKeepsItsMailbox(t *testing.T) {
	uids := map[string][]uint32{"INBOX": {1, 2}, "Junk": {10, 11}}
	var slowMu sync.Mutex
	slowJunk := false
	junkFetching := make(chan struct{}, 1)

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
			s.send("* %d EXISTS", len(uids[s.box]))
			s.send("* OK [UIDVALIDITY 1]")
			s.send("* OK [UIDNEXT 99]")
			s.send("%s OK selected", tag)
			return true
		case strings.Contains(up, "UID STORE"):
			s.send("%s OK store", tag)
			return true
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "(UID)"):
			for i, u := range uids[s.box] {
				s.send("* %d FETCH (UID %d)", i+1, u)
			}
			s.send("%s OK fetch", tag)
			return true
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "ENVELOPE"):
			slowMu.Lock()
			slow := slowJunk && s.box == "Junk"
			slowMu.Unlock()
			if slow {
				junkFetching <- struct{}{}
				time.Sleep(150 * time.Millisecond)
			}
			fallthrough
		case strings.Contains(up, "UID FETCH"):
			for i, u := range uids[s.box] {
				s.send(`* %d FETCH (UID %d FLAGS () RFC822.SIZE 10 ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" "%s%d" (("B" NIL "b" "ex.com")) NIL NIL (("A" NIL "a" "ex.com")) NIL NIL NIL "<%s%d@ex>"))`, i+1, u, s.box, u, s.box, u)
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
		t.Fatalf("first sync: %v", err)
	}
	junk, _ := st.GetFolder("home/junk")
	inbox, _ := st.GetFolder("home/inbox")
	if n := len(st.ListMessages(junk.ID)); n != 2 {
		t.Fatalf("junk cached %d messages, want 2", n)
	}
	target := st.ListMessages(inbox.ID)[0]

	cli, err := st.client("home")
	if err != nil {
		t.Fatal(err)
	}
	slowMu.Lock()
	slowJunk = true
	slowMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := st.syncFolder(cli, junk); err != nil {
			t.Errorf("junk sync: %v", err)
		}
	}()
	<-junkFetching
	go func() {
		defer wg.Done()
		st.pushFlags(target, inbox, []string{`\Seen`}, nil)
	}()
	wg.Wait()

	got := st.ListMessages(junk.ID)
	if len(got) != 2 {
		t.Fatalf("junk holds %d messages after a concurrent Inbox flag push, want 2", len(got))
	}
	for _, m := range got {
		if m.UID != 10 && m.UID != 11 {
			t.Fatalf("junk holds UID %d, which is an Inbox message", m.UID)
		}
	}
}
