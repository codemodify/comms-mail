package mail

import (
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreGetRawRFC822(t *testing.T) {
	s := NewDemoStore()
	all := s.ListMessages(FolderAdaInbox)
	if len(all) == 0 {
		t.Fatal("empty")
	}
	raw, err := s.GetRaw(all[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "MIME-Version:") || !strings.Contains(text, "Content-Type:") {
		t.Fatalf("stored raw is not RFC822:\n%s", text)
	}
	if strings.Contains(text, "X-Flag:") {
		t.Fatal("GetRaw must not invent X-Flag headers")
	}
}

func TestInboxNewestIsWelcome(t *testing.T) {
	s := NewDemoStore()
	all := s.ListMessages(FolderAdaInbox)
	sortMessages(all, 4, false, FolderInbox)
	if len(all) == 0 {
		t.Fatal("empty")
	}
	if all[0].Subject != "Welcome to Mail on uitoolkit" {
		t.Fatalf("first %q date %v", all[0].Subject, all[0].Date)
	}
}

func TestDemoStoreSize(t *testing.T) {
	s := NewDemoStore()
	n := s.Count()
	if n < 50 || n > 200 {
		t.Fatalf("demo message count %d; want 50–200", n)
	}
	accts := s.Accounts()
	if len(accts) != 2 {
		t.Fatalf("accounts %d", len(accts))
	}
	inbox, ok := s.GetFolder(FolderAdaInbox)
	if !ok || inbox.Kind != FolderInbox {
		t.Fatal("ada inbox")
	}
	if s.Unread(FolderAdaInbox) < 1 {
		t.Fatal("expected unread in inbox")
	}
}

func TestMemoryStoreFlagsMoveDelete(t *testing.T) {
	s := NewDemoStore()
	list := s.ListMessages(FolderAdaInbox)
	if len(list) < 2 {
		t.Fatal("inbox empty")
	}
	id := list[0].ID
	if err := s.SetFlags(id, FlagPatch{Read: boolPtr(true), Starred: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	m, ok := s.GetMessage(id)
	if !ok || !m.Read || !m.Starred {
		t.Fatalf("flags %+v", m)
	}
	tags := []string{"Work"}
	if err := s.SetFlags(id, FlagPatch{Tags: &tags}); err != nil {
		t.Fatal(err)
	}
	if err := s.Move([]MessageID{id}, FolderAdaProjects); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetMessage(id); !ok {
		t.Fatal("missing after move")
	}
	found := false
	for _, m := range s.ListMessages(FolderAdaProjects) {
		if m.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("not in projects")
	}
	if err := s.Delete([]MessageID{id}); err != nil {
		t.Fatal(err)
	}
	m, ok = s.GetMessage(id)
	if !ok || m.Folder != FolderAdaTrash {
		t.Fatalf("delete should trash, got %+v ok=%v", m, ok)
	}
	if err := s.Delete([]MessageID{id}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetMessage(id); ok {
		t.Fatal("expunge from trash")
	}
}

func TestMemoryStoreAppendSendDraftFetch(t *testing.T) {
	s := NewDemoStore()
	id, err := s.Append(FolderAdaDrafts, Message{
		To: "kai@paintengine.example", Subject: "hello draft", Body: "n",
	})
	if err != nil || id == "" {
		t.Fatal(err, id)
	}
	if err := s.Update(id, Message{To: "kai@paintengine.example", Subject: "hello draft", Body: "edited"}); err != nil {
		t.Fatal(err)
	}
	m, ok := s.GetMessage(id)
	if !ok || m.Body != "edited" {
		t.Fatalf("update %+v", m)
	}
	sent, err := s.Append(FolderAdaSent, Message{
		To: "kai@paintengine.example", Subject: "hello", Body: "sent",
	})
	if err != nil || sent == "" {
		t.Fatal(err)
	}
	n, err := s.Fetch(AcctAda)
	if err != nil || n != 1 {
		t.Fatalf("fetch1 %d %v", n, err)
	}
	n, err = s.Fetch(AcctAda)
	if err != nil || n != 1 {
		t.Fatalf("fetch2 %d %v", n, err)
	}
	n, err = s.Fetch(AcctAda)
	if err != nil || n != 1 {
		t.Fatalf("fetch3 %d %v", n, err)
	}
	n, err = s.Fetch(AcctAda)
	if err != nil || n != 0 {
		t.Fatalf("fetch cap %d %v", n, err)
	}
}

func TestQuickFilterAndSort(t *testing.T) {
	s := NewDemoStore()
	all := s.ListMessages(FolderAdaInbox)
	f := Filter{Unread: true}
	unread := 0
	for _, m := range all {
		if f.Match(m) {
			unread++
		}
	}
	if unread == 0 || unread == len(all) {
		t.Fatalf("unread pin %d / %d", unread, len(all))
	}
	f = Filter{Query: "Welcome to Mail"}
	hit := 0
	for _, m := range all {
		if f.Match(m) {
			hit++
		}
	}
	if hit != 1 {
		t.Fatalf("welcome query hits %d", hit)
	}
	f = Filter{Query: "retained scene", Body: true}
	bodyHits := 0
	for _, m := range all {
		if f.Match(m) {
			bodyHits++
		}
	}
	if bodyHits < 1 {
		t.Fatal("body scope")
	}
	cp := append([]Message(nil), all...)
	sortMessages(cp, 4, false, FolderInbox)
	if !cp[0].Date.After(cp[len(cp)-1].Date) && !cp[0].Date.Equal(cp[len(cp)-1].Date) {
		t.Fatal("date desc")
	}
	sortMessages(cp, 2, true, FolderInbox)
	if strings.ToLower(cp[0].Subject) > strings.ToLower(cp[len(cp)-1].Subject) {
		t.Fatal("subject asc")
	}
}

func TestFormatters(t *testing.T) {
	now := time.Date(2026, 9, 11, 17, 0, 0, 0, time.UTC)
	if got := formatDate(now.Add(-10*time.Minute), now); got != "4:50 PM" {
		t.Fatalf("today got %q", got)
	}
	if got := formatDate(now.Add(-24*time.Hour), now); got != "Yesterday" {
		t.Fatalf("yesterday got %q", got)
	}
	if formatSize(800) != "800 B" || formatSize(3500) != "3 KB" {
		t.Fatalf("size %s %s", formatSize(800), formatSize(3500))
	}
	if DisplayName("Ada Lovelace <ada@example.com>") != "Ada Lovelace" {
		t.Fatal("display name")
	}
}

func TestToggleTag(t *testing.T) {
	got := toggleTag(nil, "Work")
	if !hasTag(got, "Work") {
		t.Fatal(got)
	}
	got = toggleTag(got, "Work")
	if hasTag(got, "Work") {
		t.Fatal(got)
	}
}
