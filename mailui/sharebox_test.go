package mailui

import (
	"testing"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// fixedBox is content of a known height.
type fixedBox struct {
	widget.Base
	h float32
}

func newFixedBox(h float32) *fixedBox {
	b := &fixedBox{h: h}
	b.Init(b)
	return b
}

func (b *fixedBox) Measure(c layout.Constraints) paintengine2d.Point {
	return c.Constrain(paintengine2d.Pt(300, b.h))
}

func (b *fixedBox) Arrange(r paintengine2d.Rect) { b.SetBounds(r) }

// The reading pane's header is as tall as what is in it, and never takes
// the room the body needs.
func TestHeaderBoxIsItsContentsHeight(t *testing.T) {
	short := newReserveBox(200, newHeaderScroll(newFixedBox(90)))
	short.SetLook(style.DarkLook())
	if got := short.Measure(layout.Loose(400, 700)).Y; got != 90 {
		t.Fatalf("a short header asked for %v, want its 90", got)
	}
	tall := newReserveBox(200, newHeaderScroll(newFixedBox(1000)))
	tall.SetLook(style.DarkLook())
	if got := tall.Measure(layout.Loose(400, 700)).Y; got != 500 {
		t.Fatalf("a tall header asked for %v, want 700 less the 200 kept for the body", got)
	}
}
