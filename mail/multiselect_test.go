package mail

import (
	"testing"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// e2e finding: Ctrl+click, Shift+click and Ctrl+A only ever selected one
// message, so bulk actions (read, star, delete, archive) saw one id.
func TestMailMessageListMultiSelects(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	tv := s.table
	if tv == nil || tv.RowCount < 4 {
		t.Fatalf("thread table with rows: %v", tv)
	}
	click := func(row int, mods platform.Modifiers) {
		r := tv.RowBounds(row)
		p := paintengine2d.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
		tv.MousePress(widget.MouseEvent{Pos: p, Mods: mods, Button: platform.ButtonLeft})
		tv.MouseRelease(widget.MouseEvent{Pos: p, Button: platform.ButtonLeft})
		a.PumpOnce()
	}
	click(0, 0)
	if got := len(s.ids()); got != 1 {
		t.Fatalf("click: %d selected, want 1", got)
	}
	click(2, platform.ModCtrl)
	if got := len(s.ids()); got != 2 {
		t.Fatalf("ctrl+click: %d selected, want 2", got)
	}
	if prim, ok := s.primary(); !ok || prim.ID != s.rows[2].ID {
		t.Fatal("the ctrl+clicked message should be the primary (previewed) one")
	}
	click(3, platform.ModShift)
	if got := len(s.ids()); got != 2 {
		t.Fatalf("shift+click from row 2 to 3: %d selected, want 2", got)
	}
	tv.KeyPress(widget.KeyEvent{Key: platform.KeyA, Mods: platform.ModCtrl})
	if got := len(s.ids()); got != len(s.rows) {
		t.Fatalf("ctrl+a: %d selected, want all %d", got, len(s.rows))
	}
	// A refresh keeps the selection on screen.
	s.refreshList()
	if got := len(tv.SelectedRows()); got != len(s.rows) {
		t.Fatalf("after refresh the table shows %d selected, want %d", got, len(s.rows))
	}
	click(1, 0)
	if got := len(s.ids()); got != 1 || s.ids()[0] != s.rows[1].ID {
		t.Fatalf("plain click after a multi-selection: %v", s.ids())
	}
}
