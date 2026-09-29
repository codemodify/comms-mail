package mailui

import (
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// flowRow lays its children out left to right at their own size and
// starts a new line when the next one would not fit — a row of buttons
// that stays whole in a narrow pane instead of running off its edge.
// uitoolkit has rows and grids but nothing that wraps (uitoolkit-gaps.md
// #28).
type flowRow struct {
	widget.Base
	gap float32 // 1x pixels, between buttons and between lines
}

func newFlowRow(gap float32, kids ...widget.Component) *flowRow {
	f := &flowRow{gap: gap}
	f.Init(f)
	for _, k := range kids {
		f.Add(k)
	}
	return f
}

// place is where each visible child goes within maxW (unbounded when
// negative), and the size of the whole.
func (f *flowRow) place(maxW float32) ([]paintengine2d.Rect, paintengine2d.Point) {
	gap := style.Dip(f.Look(), f.gap)
	kids := f.Children()
	rects := make([]paintengine2d.Rect, len(kids))
	var x, y, lineH, width float32
	for i, k := range kids {
		if !k.Visible() {
			continue
		}
		sz := k.Measure(layout.Unbounded())
		if maxW >= 0 && x > 0 && x+sz.X > maxW {
			y += lineH + gap
			x, lineH = 0, 0
		}
		rects[i] = paintengine2d.XYWH(x, y, sz.X, sz.Y)
		x += sz.X + gap
		lineH = max(lineH, sz.Y)
		width = max(width, x-gap)
	}
	return rects, paintengine2d.Pt(width, y+lineH)
}

func (f *flowRow) Measure(c layout.Constraints) paintengine2d.Point {
	maxW := float32(-1)
	if c.HasMaxW() {
		maxW = c.MaxW
	}
	_, size := f.place(maxW)
	return c.Constrain(size)
}

func (f *flowRow) Arrange(r paintengine2d.Rect) {
	f.SetBounds(r)
	rects, _ := f.place(r.Dx())
	for i, k := range f.Children() {
		k.Arrange(rects[i])
	}
}
