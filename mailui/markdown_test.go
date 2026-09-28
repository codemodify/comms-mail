package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// The reading pane's fourth tab shows the message as Markdown text, and
// follows the selection while it is in front.
func TestReadingPaneHasAMarkdownTab(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.LightLook(), false, AppOptions{})
	defer done()
	s.selectFolder(mailcore.FolderAdaInbox)
	s.selected = []mailcore.MessageID{mailcore.DemoNewsletterID}
	s.loadPreview()
	s.waitIdle()

	var tabs *widgets.TabView
	widget.Walk(w.Content(), func(c widget.Component) {
		if tv, ok := c.(*widgets.TabView); ok && tabs == nil {
			if titles := tv.Bar().Titles; len(titles) == 4 && titles[3] == "Markdown" {
				tabs = tv
			}
		}
	})
	if tabs == nil {
		t.Fatal("no Message / Source / HTML / Markdown tabs")
	}
	tabs.Select(3)
	a.PumpOnce()
	got := s.markdown.Text
	for _, want := range []string{"# uitoolkit Weekly", "**From:** uitoolkit Weekly", "**images in richtext**", "![banner](https://images.example/uitoolkit/banner.png)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Markdown tab lacks %q:\n%s", want, got)
		}
	}
	// Another message, with the tab in front: the tab follows.
	s.selected = []mailcore.MessageID{mailcore.DemoInviteID}
	s.selectFolder(mailcore.FolderWorkInbox)
	s.selected = []mailcore.MessageID{mailcore.DemoInviteID}
	s.loadPreview()
	s.waitIdle()
	if !strings.Contains(s.markdown.Text, "# Invitation: Toolkit design review") {
		t.Fatalf("after another message:\n%s", s.markdown.Text)
	}
}
