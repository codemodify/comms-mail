package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

// The list's marks are icons: star, paperclip, status and the muted bell.
func TestListMarksAreIcons(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	s.rows = []mailcore.Message{
		{Subject: "new", Starred: true, HasAttach: true},
		{Subject: "answered", Read: true, Answered: true},
		{Subject: "passed on", Read: true, Answered: true, Forwarded: true},
		{Subject: "quiet", Read: true, ThreadID: "t1"},
	}
	s.muted = map[string]bool{"t1": true}
	icon := func(row, col int) style.ToolIcon {
		i, _ := s.cellIcon(row, col)
		return i
	}
	for _, c := range []struct {
		row, col int
		want     style.ToolIcon
	}{
		{0, colStar, style.IconStarFilled},
		{0, colAttach, style.IconAttach},
		{0, colStatus, style.IconDot},
		{0, colTopic, style.IconNone},
		{1, colStar, style.IconNone},
		{1, colStatus, style.IconReply},
		{2, colStatus, style.IconForward},
		{3, colStatus, style.IconNone},
		{3, colTopic, style.IconMute},
	} {
		if got := icon(c.row, c.col); got != c.want {
			t.Errorf("row %d col %d: icon %v, want %v", c.row, c.col, got, c.want)
		}
	}
	for row := range s.rows {
		for _, col := range []int{colStar, colAttach, colStatus} {
			if txt := s.cellText(row, col); txt != "" {
				t.Errorf("row %d col %d is text %q", row, col, txt)
			}
		}
		if txt := s.cellText(row, colTopic); txt != s.rows[row].Subject {
			t.Errorf("row %d topic %q", row, txt)
		}
	}
	// Sorting by the status column puts unread first.
	msgs := append([]mailcore.Message(nil), s.rows...)
	mailcore.SortMessages(msgs, colSort[colStatus], false, mailcore.FolderInbox)
	if msgs[0].Subject != "new" || msgs[1].Subject != "passed on" || msgs[2].Subject != "answered" {
		t.Fatalf("status order %q %q %q", msgs[0].Subject, msgs[1].Subject, msgs[2].Subject)
	}
}

// A filter pin that is on carries a check icon, not bold or a glyph.
func TestFilterPinIcon(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	pinNode := func() *widgets.TreeNode {
		var found *widgets.TreeNode
		var walk func([]*widgets.TreeNode)
		walk = func(ns []*widgets.TreeNode) {
			for _, n := range ns {
				if p, ok := n.Data.(filterPin); ok && p == pinUnread {
					found = n
				}
				walk(n.Children)
			}
		}
		walk(s.tree.Roots)
		return found
	}
	n := pinNode()
	if n == nil {
		t.Fatal("no Unread pin in the tree")
	}
	if n.Icon != style.IconNone || n.Bold {
		t.Fatalf("an off pin shows icon %v bold %v", n.Icon, n.Bold)
	}
	s.toggleFilterPin(pinUnread)
	if n = pinNode(); n == nil || n.Icon != style.IconCheck || n.Bold || n.Label != "Unread" {
		t.Fatalf("an on pin: %+v", n)
	}
}
