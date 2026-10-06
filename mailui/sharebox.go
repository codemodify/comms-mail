package mailui

import (
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// reserveBox gives its child the child's own height, but always leaves
// reserve (1x pixels) of the height its parent offers for what comes
// after it: a header that grows with what it shows (an invitation,
// attachments) without pushing the message body out of a short window. Put
// a scroll view inside it (newHeaderScroll), so what does not fit scrolls.
// (A scroll view's MaxHeight is a fixed cap; this one moves with the
// window.)
//
// It measures what the scroll view shows, never the scroll view: a
// ScrollView takes a measure's content size for what it scrolls
// (uitoolkit-gaps.md #51), and the reading pane is measured at other
// widths after every layout — the header's chips and warnings, which wrap,
// were then scrolled as they would be at that width, and the scroll
// jumped.
type reserveBox struct {
	widget.Base
	reserve float32
}

func newReserveBox(reserve float32, child widget.Component) *reserveBox {
	b := &reserveBox{reserve: reserve}
	b.Init(b)
	b.Add(child)
	return b
}

func (b *reserveBox) Measure(c layout.Constraints) paintengine2d.Point {
	kids := b.Children()
	if len(kids) == 0 {
		return c.Constrain(paintengine2d.Pt(0, 0))
	}
	cc := layout.Constraints{MinW: c.MinW, MaxW: c.MaxW, MaxH: c.MaxH}
	if c.HasMaxH() {
		cc.MaxH = max(c.MaxH-style.Dip(b.Look(), b.reserve), c.MaxH/3)
	}
	var p paintengine2d.Point
	if sv, ok := kids[0].(*widgets.ScrollView); ok && sv.ShrinkToContent && len(sv.Children()) == 1 {
		// As tall as what it shows, at the width it will have — its bar's
		// gutter is always kept.
		g := style.ScrollGutter(b.Look())
		w := c.MaxW
		if c.HasMaxW() {
			w = max(c.MaxW-g, 0)
		}
		p = sv.Children()[0].Measure(layout.Constraints{MaxW: w, MaxH: -1})
		p.X += g
		if c.HasMaxW() {
			p.X = c.MaxW
		}
	} else {
		p = kids[0].Measure(cc)
	}
	if c.HasMaxH() && p.Y > cc.MaxH {
		p.Y = cc.MaxH
	}
	return c.Constrain(p)
}

// newHeaderScroll is the scroll view a reserveBox holds: as tall as what
// is in it, not as tall as it is offered, which would make every
// message's header as tall as the cap and squeeze the body under it.
func newHeaderScroll(content widget.Component) *widgets.ScrollView {
	sv := widgets.NewScrollView(content)
	sv.ShrinkToContent = true
	return sv
}

func (b *reserveBox) Arrange(r paintengine2d.Rect) {
	b.SetBounds(r)
	for _, k := range b.Children() {
		k.Arrange(paintengine2d.XYWH(0, 0, r.Dx(), r.Dy()))
	}
}
