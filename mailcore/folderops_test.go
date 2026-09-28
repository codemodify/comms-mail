package mailcore

import (
	"path/filepath"
	"strings"
	"testing"
)

func newFolderOpsServer(t *testing.T, uids map[string][]uint32) *replayServer {
	// Reuse the replay fake but with an extra CREATE/DELETE handler.
	rs := &replayServer{uids: uids, validity: map[string]int{}}
	for b := range uids {
		rs.validity[b] = 1
	}
	rs.scriptIMAP = newScriptIMAP(t, "IMAP4rev1 UIDPLUS", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		rs.mu.Lock()
		defer rs.mu.Unlock()
		switch {
		case cmd == "LIST":
			s.send(`* LIST (\HasNoChildren) "/" "INBOX"`)
			s.send(`* LIST (\HasNoChildren) "/" "Projects"`)
			s.send("%s OK list", tag)
		case cmd == "LSUB":
			s.send("%s OK lsub", tag)
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.box = strings.Trim(strings.Fields(line)[2], `"`)
			s.send("* %d EXISTS", len(rs.uids[s.box]))
			s.send("* OK [UIDVALIDITY %d]", rs.validity[s.box])
			s.send("* OK [UIDNEXT 99]")
			s.send("%s OK selected", tag)
		case cmd == "CLOSE":
			s.send("%s OK closed", tag)
		case cmd == "DELETE", strings.Contains(up, "UID STORE"):
			rs.log = append(rs.log, s.box+": "+strings.TrimPrefix(line, tag+" "))
			s.send("%s OK done", tag)
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "(UID)"):
			for i, u := range rs.uids[s.box] {
				s.send("* %d FETCH (UID %d)", i+1, u)
			}
			s.send("%s OK fetch", tag)
		case strings.Contains(up, "UID FETCH"):
			for i, u := range rs.uids[s.box] {
				s.send(`* %d FETCH (UID %d FLAGS () RFC822.SIZE 10 ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" "m%d" (("B" NIL "b" "ex.com")) NIL NIL (("A" NIL "a" "ex.com")) NIL NIL NIL "<%s%d@ex>"))`, i+1, u, u, s.box, u)
			}
			s.send("%s OK fetch", tag)
		default:
			return false
		}
		return true
	})
	return rs
}

func TestDeleteFolderRemovesItEverywhere(t *testing.T) {
	rs := newFolderOpsServer(t, map[string][]uint32{"INBOX": {1}, "Projects": {5, 6}})
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: rs.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	proj, ok := st.GetFolder("home/projects")
	if !ok {
		t.Fatalf("no Projects folder; folders=%v", st.ListFolders("home"))
	}
	if proj.Kind != FolderCustom {
		t.Fatalf("Projects kind = %v, want custom", proj.Kind)
	}
	if n := len(st.ListMessages(proj.ID)); n != 2 {
		t.Fatalf("Projects has %d messages, want 2", n)
	}

	// A system folder is refused.
	if err := st.DeleteFolder("home/inbox"); err == nil {
		t.Fatal("deleting the inbox should be refused")
	}

	rs.takeLog()
	if err := st.DeleteFolder(proj.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(strings.Join(rs.takeLog(), "\n"), `DELETE "Projects"`) {
		t.Fatal("the server was not told to DELETE the mailbox")
	}
	if _, ok := st.GetFolder(proj.ID); ok {
		t.Fatal("the folder is still in the cache")
	}
	if n := len(st.ListMessages(proj.ID)); n != 0 {
		t.Fatalf("Projects still has %d messages", n)
	}
}

func TestMarkFolderReadFlagsWhatWasUnread(t *testing.T) {
	rs := newFolderOpsServer(t, map[string][]uint32{"INBOX": {1, 2}})
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: rs.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	for _, m := range st.ListMessages("home/inbox") {
		if m.Read {
			t.Fatal("demo inbox should start unread")
		}
	}
	rs.takeLog()
	if err := st.MarkFolderRead("home/inbox"); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	for _, m := range st.ListMessages("home/inbox") {
		if !m.Read {
			t.Fatalf("message %s still unread", m.ID)
		}
	}
	// The messages that were unread, by UID — not "1:*", which also marked
	// mail that arrived after the last sync.
	if log := strings.Join(rs.takeLog(), "\n"); !strings.Contains(log, `UID STORE 1:2 +FLAGS.SILENT (\Seen)`) {
		t.Fatalf("the server was not told to flag the messages: %v", log)
	}
}
