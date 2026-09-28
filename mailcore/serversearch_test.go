package mailcore

import (
	"strings"
	"testing"
)

func TestSearchTerms(t *testing.T) {
	got := searchTerms(`  invoice "march 2026"  Müller` + "\r\nX-Evil")
	want := []string{"invoice", "march 2026", "Müller", "X-Evil"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("terms %q", got)
	}
	if len(searchTerms("   ")) != 0 {
		t.Fatal("blank query has terms")
	}
}

// The server's answer names messages the cache already lists; each word is
// its own TEXT criterion, and a folder or every folder can be searched.
func TestSearchServerFindsCachedMessages(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1, 2, 3}, "Archive": {7, 8}})
	st := newFolderStore(t, fs)
	fs.mu.Lock()
	fs.hits = map[string][]uint32{"INBOX": {2}, "Archive": {8, 99}} // 99: not synced yet
	fs.mu.Unlock()

	inbox, _ := folderNamed(st, "Inbox")
	got, err := st.SearchServer(inbox.ID, `invoice "march 2026"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != MessageID(inbox.ID+":2") {
		t.Fatalf("inbox hits %+v", got)
	}
	if l := fs.takeLog(); len(l) < 2 || l[len(l)-1] != `INBOX: UID SEARCH TEXT "invoice" TEXT "march 2026"` {
		t.Fatalf("server saw %q", l)
	}

	all, err := st.SearchServer("", "invoice")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("every folder: %+v", all)
	}
}

// Words outside ASCII go as a UTF-8 literal: quoted strings are 7-bit.
func TestSearchServerSendsUTF8AsALiteral(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}})
	st := newFolderStore(t, fs)
	inbox, _ := folderNamed(st, "Inbox")
	if _, err := st.SearchServer(inbox.ID, "Müller Rechnung"); err != nil {
		t.Fatal(err)
	}
	l := fs.takeLog()
	want := "INBOX: UID SEARCH CHARSET UTF-8 TEXT {" + itoa(len("Müller Rechnung")) + "} Müller Rechnung"
	if len(l) == 0 || l[len(l)-1] != want {
		t.Fatalf("server saw %q, want %q", l, want)
	}
	st.SetOnline(false)
	if _, err := st.SearchServer(inbox.ID, "x"); err == nil {
		t.Fatal("searched the server while offline")
	}
}
