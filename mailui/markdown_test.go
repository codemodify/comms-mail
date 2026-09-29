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
			if titles := tv.Bar().Titles; len(titles) == 3 && titles[2] == "Markdown" {
				tabs = tv
			}
		}
	})
	if tabs == nil {
		t.Fatal("no Message / Source / Markdown tabs")
	}
	tabs.Select(readerTabMarkdown)
	a.PumpOnce()
	// The tab is the rendering itself: no Markdown text beside it.
	got := s.rd.md.rich.PlainText()
	if !strings.Contains(got, "images in richtext") || strings.Contains(got, "**") {
		t.Fatalf("Markdown tab:\n%s", got)
	}
	// Another message, with the tab in front: the tab follows.
	s.selected = []mailcore.MessageID{mailcore.DemoInviteID}
	s.selectFolder(mailcore.FolderWorkInbox)
	s.selected = []mailcore.MessageID{mailcore.DemoInviteID}
	s.loadPreview()
	s.waitIdle()
	if after := s.rd.md.rich.PlainText(); after == "" || after == got {
		t.Fatalf("after another message:\n%s", after)
	}
}
