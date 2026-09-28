package mailui

import (
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// recipientField is a To / Cc / Bcc field that completes addresses from the
// address book as you type. It is a plain text field with a suggestion list
// that drops under it: the field keeps the keyboard (a popup-layer dropdown
// would take it, so the list is laid out inline instead), and the nav keys
// the field does not use — Up, Down, Return, Tab, Escape — reach this
// wrapper by bubbling and drive the list.
//
// Recipients are comma-separated, so completion works on the token the
// caret is in: accepting a suggestion replaces that token with the
// contact's `Name <addr>` and leaves the others alone.
type recipientField struct {
	widget.Base
	field *widgets.TextField
	list  *widgets.ListView
	items []mailcore.Contact
	open  bool
	// fetch asks the owner for suggestions for token, off the UI goroutine;
	// the owner calls setSuggestions back on the UI goroutine.
	fetch func(token string)
}

const maxSuggestRows = 6

func newRecipientField(placeholder string, fetch func(token string)) *recipientField {
	rf := &recipientField{fetch: fetch}
	rf.Init(rf)
	rf.field = widgets.NewTextField("", placeholder, func(string) { rf.onEdit() })
	rf.list = widgets.NewListView(0, rf.itemText, rf.accept)
	rf.list.ItemDetail = rf.itemDetail
	rf.list.Frameless = false
	rf.list.RowHeight = 24
	rf.list.SetVisible(false)
	rf.Add(rf.field)
	rf.Add(rf.list)
	return rf
}

// Text is the whole recipient string.
func (rf *recipientField) Text() string { return rf.field.Text }

// SetText replaces it (loading a draft, a reply's recipients).
func (rf *recipientField) SetText(s string) {
	rf.field.SetText(s)
	rf.close()
}

func (rf *recipientField) itemText(i int) string {
	if i < 0 || i >= len(rf.items) {
		return ""
	}
	c := rf.items[i]
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return c.Address
}

func (rf *recipientField) itemDetail(i int) string {
	if i < 0 || i >= len(rf.items) {
		return ""
	}
	if strings.TrimSpace(rf.items[i].Name) == "" {
		return "" // the address is already the main text
	}
	return rf.items[i].Address
}

// onEdit fires as the field's text changes: it asks for suggestions for the
// token the caret is in, unless that token is empty.
func (rf *recipientField) onEdit() {
	tok := strings.TrimSpace(rf.currentToken())
	if tok == "" {
		rf.close()
		return
	}
	if rf.fetch != nil {
		rf.fetch(tok)
	}
}

// setSuggestions shows cs under the field (or hides the list when empty).
// The owner calls it on the UI goroutine with the daemon's answer.
func (rf *recipientField) setSuggestions(cs []mailcore.Contact) {
	// A suggestion equal to what is already typed is noise.
	if tok := strings.TrimSpace(rf.currentToken()); tok == "" {
		cs = nil
	}
	if len(cs) > maxSuggestRows {
		cs = cs[:maxSuggestRows]
	}
	rf.items = cs
	rf.list.Count = len(cs)
	rf.list.Selected = 0
	rf.list.OffsetY = 0
	was := rf.open
	rf.open = len(cs) > 0
	rf.list.SetVisible(rf.open)
	rf.list.Invalidate()
	if was != rf.open {
		rf.RequestLayout()
	}
	rf.Invalidate()
}

func (rf *recipientField) close() {
	if !rf.open && !rf.list.Visible() {
		return
	}
	rf.open = false
	rf.items = nil
	rf.list.Count = 0
	rf.list.SetVisible(false)
	rf.RequestLayout()
	rf.Invalidate()
}

// currentToken is the comma-separated segment the caret sits in — the one
// being typed.
func (rf *recipientField) currentToken() string {
	text := rf.field.Text
	// The caret is a rune offset; recipients rarely contain multibyte
	// runes before the caret, but cut on the last comma before it safely.
	runes := []rune(text)
	caret := rf.field.Caret()
	if caret > len(runes) {
		caret = len(runes)
	}
	before := string(runes[:caret])
	if i := strings.LastIndex(before, ","); i >= 0 {
		return before[i+1:]
	}
	return before
}

// accept replaces the current token with contact i's address and closes the
// list. It keeps the recipients before and after it.
func (rf *recipientField) accept(i int) {
	if i < 0 || i >= len(rf.items) {
		return
	}
	runes := []rune(rf.field.Text)
	caret := rf.field.Caret()
	if caret > len(runes) {
		caret = len(runes)
	}
	before := string(runes[:caret])
	after := string(runes[caret:])
	start := 0
	if j := strings.LastIndex(before, ","); j >= 0 {
		start = j + 1
	}
	head := before[:start]
	if strings.TrimSpace(head) != "" && !strings.HasSuffix(head, " ") {
		head += " "
	}
	repl := head + rf.items[i].Display() + ", "
	rf.field.SetText(repl + strings.TrimLeft(after, " "))
	rf.field.SetSelection(runeCount(repl), runeCount(repl))
	rf.field.RequestFocus()
	rf.close()
}

func runeCount(s string) int { return len([]rune(s)) }

// KeyPress handles the list's navigation keys, reached by bubbling from the
// focused field. Everything else it leaves to the field.
func (rf *recipientField) KeyPress(e widget.KeyEvent) bool {
	if !rf.open {
		return false
	}
	switch e.Key {
	case platform.KeyDown:
		rf.moveSel(1)
		return true
	case platform.KeyUp:
		rf.moveSel(-1)
		return true
	case platform.KeyReturn, platform.KeyTab:
		if !e.Mods.Shift() {
			rf.accept(rf.list.Selected)
			return true
		}
		return false
	case platform.KeyEscape:
		rf.close()
		return true
	}
	return false
}

func (rf *recipientField) moveSel(d int) {
	n := len(rf.items)
	if n == 0 {
		return
	}
	rf.list.Selected = (rf.list.Selected + d + n) % n
	rf.list.EnsureVisible(rf.list.Selected)
	rf.list.Invalidate()
}

func (rf *recipientField) listH() float32 {
	if !rf.open || len(rf.items) == 0 {
		return 0
	}
	return rf.list.Measure(layout.Loose(rf.LocalBounds().Dx(), 1e6)).Y
}

func (rf *recipientField) Measure(c layout.Constraints) paintengine2d.Point {
	fs := rf.field.Measure(c)
	h := fs.Y + rf.listH()
	return c.Constrain(paintengine2d.Pt(fs.X, h))
}

func (rf *recipientField) Arrange(r paintengine2d.Rect) {
	rf.SetBounds(r)
	fh := rf.field.Measure(layout.Loose(r.Dx(), 1e6)).Y
	rf.field.Arrange(paintengine2d.XYWH(0, 0, r.Dx(), fh))
	if rf.open {
		rf.list.Arrange(paintengine2d.XYWH(0, fh, r.Dx(), rf.listH()))
	} else {
		rf.list.Arrange(paintengine2d.XYWH(0, fh, r.Dx(), 0))
	}
}
