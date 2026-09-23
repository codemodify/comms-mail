package mail

import (
	"strings"
	"testing"
)

func TestDefaultTagsIncludeLockedSystemPins(t *testing.T) {
	tags := DefaultTags()
	want := []string{TagUnread, TagStarred, TagAttachment, "Important", "Work", "Personal", "To Do", "Later"}
	if len(tags) != len(want) {
		t.Fatalf("DefaultTags %v", tagNames(tags))
	}
	for i, name := range want {
		if tags[i].Name != name {
			t.Fatalf("tag %d %q want %q", i, tags[i].Name, name)
		}
		if IsSystemTag(name) != tags[i].System {
			t.Fatalf("%s system=%v", name, tags[i].System)
		}
	}
	if !IsSystemTag("unread") || !IsSystemTag("Starred") || !IsSystemTag("ATTACHMENT") {
		t.Fatal("system names")
	}
	if IsSystemTag("Important") || IsSystemTag("Work") {
		t.Fatal("user tags must stay removable")
	}
}

func TestMergeTagStoreMigratesKeywordOnlyList(t *testing.T) {
	old := []Tag{
		{Name: "Important", Color: "#c0392b"},
		{Name: "Work", Color: "#d35400"},
		{Name: "Later", Color: "#111111"},
	}
	got := mergeTagStore(old)
	names := tagNames(got)
	if names[0] != TagUnread || names[1] != TagStarred || names[2] != TagAttachment {
		t.Fatalf("system prefix %v", names)
	}
	later, ok := tagByName(got, "Later")
	if !ok || later.Color != "#111111" {
		t.Fatalf("preserved Later %+v", later)
	}
	if !got[0].System || got[0].Color == "" {
		t.Fatalf("Unread %+v", got[0])
	}
}

func TestLockedTagsNotDeletable(t *testing.T) {
	s := NewDemoStore()
	for _, name := range []string{TagUnread, TagStarred, TagAttachment} {
		if err := s.DeleteTag(name); err == nil {
			t.Fatalf("deleted locked %s", name)
		}
	}
	if _, err := s.PutTag(Tag{Name: "Project X", Color: "#123456"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTag("Project X"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tagByName(s.ListTags(), "Project X"); ok {
		t.Fatal("user tag still present")
	}
}

func TestSidebarAndPrefsTagListsAgree(t *testing.T) {
	s := NewDemoStore()
	prefs := tagNames(s.ListTags())
	sidebar := tagNames(mergeTagStore(s.ListTags()))
	if len(prefs) != len(sidebar) {
		t.Fatalf("prefs %v sidebar %v", prefs, sidebar)
	}
	for i := range prefs {
		if prefs[i] != sidebar[i] {
			t.Fatalf("prefs %v sidebar %v", prefs, sidebar)
		}
	}
	for _, name := range []string{"Work", "Personal", "Later", "Important", "To Do"} {
		if !containsLabel(prefs, name) {
			t.Fatalf("unified list missing %q: %v", name, prefs)
		}
	}
}

func TestNewMessageGetsUnreadTag(t *testing.T) {
	s := NewMemoryStore(DemoNow)
	s.addAccount(Account{ID: AcctAda, Name: "Ada", Address: "ada@example.com"})
	s.addFolder(Folder{ID: FolderAdaInbox, AccountID: AcctAda, Name: "Inbox", Kind: FolderInbox})
	id, err := s.Append(FolderAdaInbox, Message{
		From: "bot@example.com", To: "ada@example.com", Subject: "Hello",
		Body: "new", Read: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := s.GetMessage(id)
	if !ok {
		t.Fatal("missing")
	}
	if m.Read {
		t.Fatal("new mail should be unread")
	}
	if !hasTag(m.Tags, TagUnread) {
		t.Fatalf("new mail tags %v want Unread", m.Tags)
	}
}

func TestAttachmentPartsGetAttachmentTag(t *testing.T) {
	s := NewMemoryStore(DemoNow)
	s.addAccount(Account{ID: AcctAda, Name: "Ada", Address: "ada@example.com"})
	s.addFolder(Folder{ID: FolderAdaInbox, AccountID: AcctAda, Name: "Inbox", Kind: FolderInbox})
	id, err := s.Append(FolderAdaInbox, Message{
		From: "bot@example.com", Subject: "Files", Body: "see attached",
		Parts: []Part{{Filename: "shot.png", MIMEType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := s.GetMessage(id)
	if !ok || !m.HasAttach || !hasTag(m.Tags, TagAttachment) {
		t.Fatalf("attachment %+v", m)
	}
	if !hasTag(m.Tags, TagUnread) {
		t.Fatalf("unread+attach tags %v", m.Tags)
	}
}

func TestRenameUserTagUpdatesMessages(t *testing.T) {
	s := NewDemoStore()
	id := s.ListMessages(FolderAdaInbox)[0].ID
	tags := []string{"Work"}
	if err := s.SetFlags(id, FlagPatch{Tags: &tags}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutTag(Tag{Name: "Office", Color: "#d35400", Previous: "Work"}); err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMessage(id)
	if hasTag(m.Tags, "Work") || !hasTag(m.Tags, "Office") {
		t.Fatalf("renamed tags %v", m.Tags)
	}
	if _, ok := tagByName(s.ListTags(), "Work"); ok {
		t.Fatal("old Work definition remains")
	}
}

func TestPutTagCannotRenameSystem(t *testing.T) {
	s := NewDemoStore()
	if _, err := s.PutTag(Tag{Name: "Inbox pin", Previous: TagUnread}); err == nil {
		t.Fatal("renamed Unread")
	}
}

func TestApplyAutomaticTags(t *testing.T) {
	m := Message{Read: false, Starred: true, HasAttach: true, Tags: []string{"Important"}}
	applyAutomaticTags(&m)
	for _, name := range []string{TagUnread, TagStarred, TagAttachment, "Important"} {
		if !hasTag(m.Tags, name) {
			t.Fatalf("missing %s in %v", name, m.Tags)
		}
	}
	m.Read = true
	m.Starred = false
	applyAutomaticTags(&m)
	if hasTag(m.Tags, TagUnread) || hasTag(m.Tags, TagStarred) {
		t.Fatalf("stale system tags %v", m.Tags)
	}
	if !hasTag(m.Tags, "Important") {
		t.Fatal("lost user tag")
	}
}

func TestDemoWelcomeHasSystemTags(t *testing.T) {
	s := NewDemoStore()
	all := s.ListMessages(FolderAdaInbox)
	if len(all) == 0 {
		t.Fatal("empty")
	}
	var welcome Message
	for _, m := range all {
		if strings.HasPrefix(m.Subject, "Welcome") {
			welcome = m
			break
		}
	}
	if welcome.ID == "" {
		t.Fatal("welcome")
	}
	if !hasTag(welcome.Tags, TagUnread) || !hasTag(welcome.Tags, TagStarred) || !hasTag(welcome.Tags, TagAttachment) {
		t.Fatalf("welcome tags %v", welcome.Tags)
	}
}
