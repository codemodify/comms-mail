package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/richtext"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

func press(s *session, k platform.Key, mods platform.Modifiers) bool {
	return s.handleKey(widget.KeyEvent{Key: k, Mods: mods})
}

// E opens the selected message in a tab of its own in the title bar; E on a
// message that already has one brings it forward instead of opening another.
func TestEOpensTheMessageInATab(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	if s.tabs == nil || s.tabs.Len() != 1 || s.tabs.Tab(0).Title != "Mail" || !s.tabs.Tab(0).NoClose {
		t.Fatal("the window should open with one Mail tab that cannot be closed")
	}
	want := s.rows[1]
	s.selected = []mailcore.MessageID{want.ID}
	press(s, platform.KeyE, 0)
	if s.tabs.Len() != 2 || s.tabs.Selected() != 1 {
		t.Fatalf("after E: %d tabs, selected %d", s.tabs.Len(), s.tabs.Selected())
	}
	mt, ok := s.activeTab()
	if !ok || mt.msg.ID != want.ID || s.tabs.Tab(1).Title != strings.TrimSpace(want.Subject) {
		t.Fatalf("the tab shows %v, want %s %q", mt, want.ID, want.Subject)
	}
	if mt.rd.text.Text == "" || !mt.view.Visible() || s.mainPage.Visible() {
		t.Fatal("the tab's message should show, with its body, and the three panes hidden")
	}

	s.tabs.Select(0)
	if !s.mainPage.Visible() || mt.view.Visible() {
		t.Fatal("selecting Mail should bring the three panes back")
	}
	press(s, platform.KeyE, 0)
	if s.tabs.Len() != 2 || s.tabs.Selected() != 1 {
		t.Fatalf("E on a message already open: %d tabs, selected %d", s.tabs.Len(), s.tabs.Selected())
	}

	// A double click (the table's activate) on another row opens that one.
	s.tabs.Select(0)
	s.table.OnActivate(2)
	if s.tabs.Len() != 3 {
		t.Fatalf("double click did not open a tab: %d tabs", s.tabs.Len())
	}
	s.closeTab(2)
	s.tabs.Select(1)

	// A layout change rebuilds the window; the tab survives it.
	s.opts.Layout = LayoutClassic
	s.rebuild()
	if s.tabs.Len() != 2 {
		t.Fatalf("the rebuild dropped the message tab: %d tabs", s.tabs.Len())
	}
	if mt, ok := s.activeTab(); !ok || mt.msg.ID != want.ID {
		t.Fatal("the rebuild did not keep the message tab in front")
	}
}

// T tags, D deletes. On a message tab both act on the tab's message, and a
// deleted message's tab closes.
func TestTagAndDeleteKeys(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	first, second := s.rows[0].ID, s.rows[1].ID

	s.selected = []mailcore.MessageID{first}
	press(s, platform.KeyD, 0)
	if m, _, _ := s.cli.GetMessage(first); !strings.HasSuffix(string(m.Folder), "/trash") {
		t.Fatalf("D left the message in %q", m.Folder)
	}

	s.selected = []mailcore.MessageID{second}
	press(s, platform.KeyE, 0)
	mt, ok := s.activeTab()
	if !ok || mt.msg.ID != second {
		t.Fatal("E did not open the message")
	}
	items := s.tagMenuItems()
	var work func()
	for _, it := range items {
		if it.Text == "Work" {
			work = it.OnClick
		}
		if mailcore.IsSystemTag(it.Text) {
			t.Errorf("the tag menu offers the %q pin", it.Text)
		}
	}
	if work == nil {
		t.Fatalf("no Work in the tag menu: %v", items)
	}
	press(s, platform.KeyT, 0) // opens the menu over the tab; must not act by itself
	work()
	if m, _, _ := s.cli.GetMessage(second); !containsTag(m.Tags, "Work") {
		t.Fatalf("tagging from the tab did not reach the daemon: %v", m.Tags)
	}
	if !containsTag(mt.msg.Tags, "Work") || !strings.Contains(mt.rd.extra.Text, "Work") {
		t.Fatal("the tab does not show the tag it was given")
	}

	press(s, platform.KeyD, 0)
	if m, _, _ := s.cli.GetMessage(second); !strings.HasSuffix(string(m.Folder), "/trash") {
		t.Fatalf("D on the tab left its message in %q", m.Folder)
	}
	if s.tabs.Len() != 1 || !s.mainPage.Visible() {
		t.Fatal("deleting the tab's message should close its tab and show Mail")
	}
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// A message opened in a tab has its views there too: Open HTML, and the
// Markdown rendering with the inline images drawn and the remote ones
// offered.
func TestMessageTabShowsHTMLAndMarkdown(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	rows, _ := s.cli.ListMessages(mailcore.FolderAdaInbox, mailcore.Filter{})
	var row mailcore.Message
	for _, m := range rows {
		if m.ID == mailcore.DemoNewsletterID {
			row = m
		}
	}
	if row.ID == "" || row.HTML != "" {
		t.Fatalf("list row %q html %d", row.ID, len(row.HTML))
	}
	i := s.addMessageTab(row)
	s.waitIdle()
	mt := s.tabs.Tab(i).Data.(*messageTab)
	if !mt.rd.htmlBtn.Visible() {
		t.Fatal("the tab has no Open HTML")
	}
	r := mt.rd.md
	if got := r.rich.PlainText(); !strings.Contains(got, "images in richtext") {
		t.Fatalf("tab Markdown %q", got)
	}
	if !r.bar.Visible() || r.rich.ResolveImageKind("cid:logo@news.example", richtext.ImageInline) == nil {
		t.Fatal("the tab should draw the inline logo and offer the remote banner")
	}
}
