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
	p := kids[0].Measure(cc)
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
		k.Arrange(r)
	}
}
