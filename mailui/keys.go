package mailui

import (
	"goframework.io/desktop-ui-widgets"
	"goframework.io/desktop-ui/layout"
	"goframework.io/desktop-ui/platform"
	"goframework.io/desktop-ui/widget"
	"goframework.io/graphics-paint2d"
)

// Thunderbird-like shortcuts (when focus is not a text field):
//
//	n / p     next / previous message
//	#         delete (Shift+3 or KeyHash)
//	r         reply
//	f         forward
//	c         compose
//	m         mark read
//	s         star / unstar
//	j         junk
//
// Ctrl+N / Ctrl+R / Del / F5 / F7 / F8 stay as toolbar / key bindings.

type shortcutRoot struct {
	widget.Base
	onKey   func(widget.KeyEvent) bool
	onReady func(widget.Component)
	ready   bool
}

func wrapShortcuts(col *widgets.FlexBox, on func(widget.KeyEvent) bool) widget.Component {
	return wrapShortcutsReady(col, on, nil)
}

func wrapShortcutsReady(col *widgets.FlexBox, on func(widget.KeyEvent) bool, ready func(widget.Component)) widget.Component {
	s := &shortcutRoot{onKey: on, onReady: ready}
	s.Init(s)
	if col != nil {
		s.Add(col)
	}
	return s
}

func (s *shortcutRoot) Measure(c layout.Constraints) paint2d.Point {
	if len(s.Children()) == 0 {
		return paint2d.Point{}
	}
	return s.Children()[0].Measure(c)
}

func (s *shortcutRoot) Arrange(r paint2d.Rect) {
	s.SetBounds(r)
	if len(s.Children()) > 0 {
		s.Children()[0].Arrange(paint2d.XYWH(0, 0, r.Dx(), r.Dy()))
	}
	if !s.ready && s.Host() != nil && s.onReady != nil {
		s.ready = true
		s.onReady(s)
	}
}

func (s *shortcutRoot) Paint(*paint2d.Context) {}

func (s *shortcutRoot) KeyPress(e widget.KeyEvent) bool {
	if s.onKey != nil && s.onKey(e) {
		return true
	}
	return false
}

func isTextFocus(c widget.Component) bool {
	switch t := c.(type) {
	case *widgets.TextField, *widgets.NumberField:
		return true
	case *widgets.TextArea:
		return !t.ReadOnly
	default:
		return false
	}
}

func isHashDelete(e widget.KeyEvent) bool {
	if e.Key == platform.KeyHash {
		return true
	}
	return e.Key == platform.Key3 && e.Mods.Shift() && !e.Mods.Ctrl()
}

// ShortcutHelp is documented in docs/mail.md.
const ShortcutHelp = `Thunderbird-like (thread list focused, not a text field)
  n / p     next / previous message
  #         delete (also Del)
  r         reply
  f         forward
  c         compose (Write)
  m         mark as read
  s         star / unstar
  j         junk (move to Junk)

Menus
  F5        Fetch
  Ctrl+N    Write
  Ctrl+R    Reply
  Ctrl+L    Forward
  Ctrl+F    Search
  Ctrl+,    Settings
  F10       the app menu
  Ctrl+Q    Quit
  Ctrl+U    Message Source (raw RFC822)
  F7 / F8   previous / next
  Esc       closes a tooltip, popup or overlay`
