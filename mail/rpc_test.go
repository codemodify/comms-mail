package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRPCRoundTrip(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	if err := cli.Ping(); err != nil {
		t.Fatal(err)
	}
	st, err := cli.Status()
	if err != nil || st.Backend != "memory" {
		t.Fatalf("status %+v %v", st, err)
	}
	accts, err := cli.Accounts()
	if err != nil || len(accts) != 2 {
		t.Fatalf("accounts %v %v", accts, err)
	}
	folders, err := cli.ListFolders(AcctAda)
	if err != nil || len(folders) < 5 {
		t.Fatalf("folders %d %v", len(folders), err)
	}
	inbox, ok := specialFolderClient(cli, AcctAda, FolderInbox)
	if !ok {
		t.Fatal("inbox")
	}
	all, err := cli.ListMessages(inbox.ID, Filter{})
	if err != nil || len(all) < 10 {
		t.Fatalf("list %d %v", len(all), err)
	}
	unread, err := cli.ListMessages(inbox.ID, Filter{Unread: true})
	if err != nil || len(unread) == 0 || len(unread) == len(all) {
		t.Fatalf("quick filter unread %d / %d %v", len(unread), len(all), err)
	}
	q, err := cli.ListMessages(inbox.ID, Filter{Query: "Welcome to Mail"})
	if err != nil || len(q) != 1 {
		t.Fatalf("welcome filter %d %v", len(q), err)
	}
	m, ok, err := cli.GetMessage(q[0].ID)
	if err != nil || !ok || len(m.Attachments) < 1 {
		t.Fatalf("get welcome %+v ok=%v %v", m, ok, err)
	}
	n, err := cli.Unread(inbox.ID)
	if err != nil || n < 1 {
		t.Fatalf("unread %d %v", n, err)
	}
	hits, err := cli.Search(SearchQuery{AccountID: AcctAda, Filter: Filter{Query: "Welcome to Mail"}})
	if err != nil || len(hits) != 1 {
		t.Fatalf("search %d %v", len(hits), err)
	}
}

func TestRPCMutations(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	inbox, ok := specialFolderClient(cli, AcctAda, FolderInbox)
	if !ok {
		t.Fatal("inbox")
	}
	list, err := cli.ListMessages(inbox.ID, Filter{})
	if err != nil || len(list) < 1 {
		t.Fatal(err)
	}
	id := list[0].ID
	if err := cli.SetFlags(id, FlagPatch{Read: boolPtr(true), Starred: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	m, ok, err := cli.GetMessage(id)
	if err != nil || !ok || !m.Read || !m.Starred {
		t.Fatalf("flags %+v %v", m, err)
	}
	draft, err := cli.SaveDraft(AcctAda, Message{To: "kai@example.com", Subject: "rpc draft", Body: "hi"}, "")
	if err != nil || draft == "" {
		t.Fatal(err, draft)
	}
	sent, err := cli.Send(AcctAda, Message{To: "kai@example.com", Subject: "rpc send", Body: "sent"}, "")
	if err != nil || sent == "" {
		t.Fatal(err, sent)
	}
	f, err := cli.CreateFolder(AcctAda, "RPC Folder", "")
	if err != nil || f.ID == "" {
		t.Fatal(err, f)
	}
	n, err := cli.Fetch(AcctAda)
	if err != nil || n != 1 {
		t.Fatalf("fetch %d %v", n, err)
	}
}

func TestRPCGetSourceReturnsRFC822(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	inbox, ok := specialFolderClient(cli, AcctAda, FolderInbox)
	if !ok {
		t.Fatal("inbox")
	}
	list, err := cli.ListMessages(inbox.ID, Filter{})
	if err != nil || len(list) < 1 {
		t.Fatal(err)
	}
	raw, err := cli.GetSource(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "MIME-Version:") || !strings.Contains(raw, "From:") {
		t.Fatalf("not RFC822:\n%s", raw)
	}
	if strings.Contains(raw, "X-Flag:") {
		t.Fatal("source must be stored RFC822, not a reconstructed header dump")
	}
}

func TestIMAPStoreHealthWithoutEnv(t *testing.T) {
	s := NewIMAPStoreFromEnv()
	if s.Backend() != "imap" {
		t.Fatal(s.Backend())
	}
	if err := s.Health(); err == nil {
		t.Fatal("expected clear env error")
	}
	if s.ListFolders("imap") != nil && s.Health() == nil {
		t.Fatal("disconnected list should not clear health")
	}
}
