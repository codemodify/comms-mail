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
// a scroll view inside it, so what does not fit scrolls. (uitoolkit's
// HeightBox is a fixed height with a cap; this is the child's height with
// a cap.)
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
	// A scroll view asks for all the height it is offered; the box wants
	// what is in it. (Taking the scroll view's word made the reading
	// pane's header as tall as the cap for every message, the body squeezed
	// under it — uitoolkit-gaps.md #14.)
	if sv, ok := kids[0].(*widgets.ScrollView); ok && sv.ContentHeight() < p.Y {
		p.Y = sv.ContentHeight()
	}
	if c.HasMaxH() && p.Y > cc.MaxH {
		p.Y = cc.MaxH
	}
	return c.Constrain(p)
}

func (b *reserveBox) Arrange(r paintengine2d.Rect) {
	b.SetBounds(r)
	for _, k := range b.Children() {
		k.Arrange(r)
	}
}
