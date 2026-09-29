package mailui

import (
	"net/mail"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// recipientField is a To / Cc / Bcc field. Every recipient is a chip
// (uitoolkit's TokenField) with a cross that takes it out; a comma, a
// semicolon or Return ends one, Backspace in the empty editor takes the
// last one back to fix it, and leaving the field keeps what was typed.
//
// The address book's suggestions for what is being typed drop down under
// it in a popup that leaves the keyboard with the field: Up, Down, Return,
// Tab and Escape drive the list while it is up, and everything else is
// typing.
//
// Text that is not an address stays in the editor rather than becoming a
// chip, where the writer can see it; Text still includes it, so sending
// says what is wrong with it as before.
type recipientField struct {
	widget.Base
	chips *widgets.TokenField
	pop   *suggestPopup
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
	rf.chips = widgets.NewTokenField(placeholder, nil)
	rf.chips.Accept = validRecipient
	ed := rf.chips.Editor()
	// The field's own split cuts at a comma inside a quoted name
	// ("Doe, Jane" <jane@example.com>, uitoolkit-gaps.md #19); this one
	// reads the address grammar.
	ed.OnInput = rf.splitTyped
	ed.OnChange = func(string) { rf.onEdit() }
	commit := ed.OnFocusLost
	ed.OnFocusLost = func() {
		// A click on a suggestion takes the focus for a moment, and accept
		// puts in the contact; committing the half-typed name first would
		// add it as well.
		if rf.open {
			return
		}
		if commit != nil {
			commit()
		}
	}
	rf.list = widgets.NewListView(0, rf.itemText, rf.accept)
	rf.list.ItemDetail = rf.itemDetail
	rf.list.Frameless = false
	rf.list.RowHeight = 24
	rf.pop = &suggestPopup{rf: rf}
	rf.pop.Init(rf.pop)
	rf.pop.Add(rf.list)
	widget.SetPopupKeysPass(rf.pop, true)
	rf.Add(rf.chips)
	return rf
}

// validRecipient is whether v is one address a chip can hold.
func validRecipient(v string) bool {
	_, err := mail.ParseAddress(v)
	return err == nil
}

// Text is every recipient, and what is typed and not yet a chip, as an
// address header's value.
func (rf *recipientField) Text() string {
	parts := rf.chips.Tokens()
	if p := strings.TrimSpace(rf.chips.Pending()); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

// SetText replaces the recipients (loading a draft, a reply's). Addresses
// become chips; anything that is not one is left in the editor.
func (rf *recipientField) SetText(s string) {
	var chips, rest []string
	for _, part := range recipientParts(s) {
		if a, err := mail.ParseAddress(part); err == nil {
			chips = append(chips, mailcore.FormatAddr(a))
		} else {
			rest = append(rest, part)
		}
	}
	rf.chips.SetTokens(chips)
	rf.chips.Editor().SetText(strings.Join(rest, ", "))
	rf.close()
}

// recipientParts cuts an address list at the commas and semicolons that
// separate addresses — not one inside a quoted name, a comment or angle
// brackets.
func recipientParts(s string) []string {
	parts, tail := splitRecipients(s)
	if t := strings.TrimSpace(tail); t != "" {
		parts = append(parts, t)
	}
	return parts
}

// splitRecipients returns the complete parts of s — each ended by a
// separator — and what follows the last separator.
func splitRecipients(s string) (parts []string, tail string) {
	quoted, escaped := false, false
	depth, angle := 0, 0
	start := 0
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && quoted:
			escaped = true
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case r == '<':
			angle++
		case r == '>' && angle > 0:
			angle--
		case (r == ',' || r == ';') && depth == 0 && angle == 0:
			if p := strings.TrimSpace(s[start:i]); p != "" {
				parts = append(parts, p)
			}
			start = i + 1
		}
	}
	return parts, s[start:]
}

// splitTyped turns the addresses the writer ended with a separator into
// chips. One that is not an address stops it: it and what follows stay in
// the editor, without the separator, so the next key does not try it
// again. One already there is dropped.
func (rf *recipientField) splitTyped(s string) {
	parts, tail := splitRecipients(s)
	ed := rf.chips.Editor()
	if len(parts) == 0 {
		// Nothing finished. Separators and spaces alone are nothing to
		// keep: the space after a comma is not the start of an address.
		if s != "" && strings.Trim(s, ",; \t") == "" {
			ed.SetText("")
		}
		return
	}
	for i, p := range parts {
		if rf.has(p) {
			continue
		}
		if !rf.chips.AddToken(p) {
			rest := strings.Join(parts[i:], ", ")
			if t := strings.TrimLeft(tail, " \t"); t != "" {
				rest += " " + t
			}
			ed.SetText(rest)
			return
		}
	}
	ed.SetText(strings.TrimLeft(tail, " \t"))
}

// contactAddr is a contact as a chip holds it, the name quoted when it
// has a comma or another character the address grammar reserves.
func contactAddr(c mailcore.Contact) string {
	return mailcore.FormatAddr(&mail.Address{Name: c.Name, Address: c.Address})
}

// has reports whether the address in v is already a chip.
func (rf *recipientField) has(v string) bool {
	a, err := mail.ParseAddress(v)
	if err != nil {
		return false
	}
	for _, t := range rf.chips.Tokens() {
		if b, err := mail.ParseAddress(t); err == nil && strings.EqualFold(a.Address, b.Address) {
			return true
		}
	}
	return false
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

// currentToken is what is being typed: the editor's text, which never
// holds a finished address.
func (rf *recipientField) currentToken() string {
	return strings.TrimSpace(rf.chips.Pending())
}

// onEdit fires as the editor's text changes: it asks for suggestions for
// what is typed, unless nothing is.
func (rf *recipientField) onEdit() {
	tok := rf.currentToken()
	if tok == "" {
		rf.close()
		return
	}
	if rf.fetch != nil {
		rf.fetch(tok)
	}
}

// setSuggestions shows cs under the field (or takes the list down when
// there are none). The owner calls it on the UI goroutine with the
// daemon's answer.
func (rf *recipientField) setSuggestions(cs []mailcore.Contact) {
	if rf.currentToken() == "" {
		cs = nil
	}
	// A contact already on the field is not suggested again.
	kept := cs[:0:0]
	for _, c := range cs {
		if !rf.has(contactAddr(c)) {
			kept = append(kept, c)
		}
	}
	cs = kept
	if len(cs) > maxSuggestRows {
		cs = cs[:maxSuggestRows]
	}
	if len(cs) == 0 {
		rf.close()
		return
	}
	rf.items = cs
	rf.list.Count = len(cs)
	rf.list.Selected = 0
	rf.list.OffsetY = 0
	rf.list.Invalidate()
	rf.open = true
	rf.showPopup()
}

// showPopup places the list under the field, as wide as it, and puts it
// up (again, when the field has grown a row of chips since).
func (rf *recipientField) showPopup() {
	if rf.Host() == nil {
		return // not in a window: the list is kept, and shown by nothing
	}
	o := widget.DeviceOrigin(rf)
	b := rf.LocalBounds()
	anchor := paintengine2d.XYWH(o.X, o.Y, b.Dx(), b.Dy())
	widget.PlacePopupForAnchor(rf, rf.pop, anchor, b.Dx(), 2)
	widget.ShowPopup(rf, rf.pop)
	rf.pop.Invalidate()
}

func (rf *recipientField) close() {
	was := rf.open
	rf.open = false
	rf.items = nil
	rf.list.Count = 0
	if was && rf.Host() != nil {
		if h, ok := rf.Host().(widget.PopupHost); ok && h.Popup() == widget.Component(rf.pop) {
			widget.DismissPopup(rf)
		}
	}
}

// accept makes contact i a chip in place of what was typed, and gives the
// keyboard back to the editor.
func (rf *recipientField) accept(i int) {
	if i < 0 || i >= len(rf.items) {
		return
	}
	c := rf.items[i]
	rf.close()
	ed := rf.chips.Editor()
	if !rf.has(contactAddr(c)) {
		rf.chips.AddToken(contactAddr(c))
	}
	ed.SetText("")
	ed.RequestFocus()
}

// listKey is a key while the suggestions are up: the ones about the list
// drive it, and the rest are the editor's.
func (rf *recipientField) listKey(e widget.KeyEvent) bool {
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

func (rf *recipientField) Measure(c layout.Constraints) paintengine2d.Point {
	return rf.chips.Measure(c)
}

func (rf *recipientField) Arrange(r paintengine2d.Rect) {
	rf.SetBounds(r)
	rf.chips.Arrange(paintengine2d.XYWH(0, 0, r.Dx(), r.Dy()))
}

// suggestPopup is the suggestion list on the window's popup layer. It
// does not take the keyboard (SetPopupKeysPass): it answers the list's
// keys and lets the rest reach the editor.
type suggestPopup struct {
	widget.Base
	rf *recipientField
}

func (p *suggestPopup) Measure(c layout.Constraints) paintengine2d.Point {
	h := p.rf.list.HeightForRows(len(p.rf.items))
	return c.Constrain(paintengine2d.Pt(p.rf.LocalBounds().Dx(), h))
}

func (p *suggestPopup) Arrange(r paintengine2d.Rect) {
	p.SetBounds(r)
	p.rf.list.Arrange(paintengine2d.XYWH(0, 0, r.Dx(), r.Dy()))
}

func (p *suggestPopup) KeyPress(e widget.KeyEvent) bool { return p.rf.listKey(e) }

// Dismissed is the popup taken down from outside — a click elsewhere.
func (p *suggestPopup) Dismissed() {
	p.rf.open = false
	p.rf.items = nil
	p.rf.list.Count = 0
}
