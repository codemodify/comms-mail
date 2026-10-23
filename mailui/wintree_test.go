package mailui

import (
	"goframework.io/desktop-ui"
	"goframework.io/desktop-ui/widget"
	"goframework.io/graphics-paint2d"
)

// windowTree is the whole Mail window for tests that look widgets up: its
// title bar (the chrome row: M menu, Fetch / Write, the quick filter) and its
// content, as children of one node — without re-parenting either.
type windowTree struct {
	widget.Base
	kids []widget.Component
}

func (t *windowTree) Children() []widget.Component { return t.kids }

func mailTree(w *ui.Window) widget.Component {
	t := &windowTree{}
	t.Init(t)
	ww, hh := w.SurfaceSize()
	t.SetBounds(paint2d.XYWH(0, 0, float32(ww), float32(hh)))
	if tb := w.TitleBar(); tb != nil {
		t.kids = append(t.kids, tb)
	}
	if c := w.Content(); c != nil {
		t.kids = append(t.kids, c)
	}
	return t
}
