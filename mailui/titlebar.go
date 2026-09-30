package mailui

import (
	"math"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/widget"
)

// The title bar holds the menu, Fetch / Write / Search and the tabs, and
// nothing over the folder pane: its left end is empty caption as wide as
// the pane, so the menu starts where the list does. The title bar is one
// row across the window (uitoolkit has no title bar split beside a pane,
// uitoolkit-gaps.md #37), so the empty part is sized after each layout of
// the panes, to where they put the pages.

// titleGap is the empty caption at the title bar's left end. It is caption
// space: pressing it moves the window, as the rest of the free space does.
type titleGap struct {
	widget.Base
	want  float32 // device pixels
	tries int     // adjustments in a row that have not settled
}

func newTitleGap() *titleGap {
	g := &titleGap{}
	g.Init(g)
	return g
}

func (g *titleGap) Measure(c layout.Constraints) paintengine2d.Point {
	return c.Constrain(paintengine2d.Pt(g.want, 0))
}

func (g *titleGap) Arrange(r paintengine2d.Rect) { g.SetBounds(r) }

// CaptionAt: all of it moves the window (widget.CaptionHitTester).
func (g *titleGap) CaptionAt(paintengine2d.Point) bool { return true }

// alignTitle sizes the empty part so that the menu starts where the pages
// do: pages is the right-hand side of the window, just laid out. It
// measures where the menu landed rather than adding up the header bar's
// gaps and padding, and the next layout takes the change up.
func (s *session) alignTitle(pages widget.Component) {
	g := s.titleGap
	if g == nil || s.menu == nil || g.Host() == nil || s.menu.Host() == nil || pages.Host() == nil {
		return
	}
	off := widget.DeviceOrigin(pages).X - widget.DeviceOrigin(s.menu).X
	if math.Abs(float64(off)) < 1 {
		g.tries = 0
		return
	}
	// A layout that cannot settle is left as it is rather than laid out
	// again and again.
	if g.tries++; g.tries > 4 {
		return
	}
	g.want = max(g.Bounds().Dx()+off, 0)
	g.Invalidate()
	// The layout this runs in is over; the next one takes it up.
	s.post(func() {
		if s.win != nil {
			s.win.RequestLayout()
		}
	})
}

// edgeWatch lays its one child out in its own box and then calls
// arranged: a way to learn where a pane landed.
type edgeWatch struct {
	widget.Base
	arranged func(widget.Component)
}

func newEdgeWatch(child widget.Component, arranged func(widget.Component)) *edgeWatch {
	e := &edgeWatch{arranged: arranged}
	e.Init(e)
	e.Add(child)
	return e
}

func (e *edgeWatch) Measure(c layout.Constraints) paintengine2d.Point {
	return e.Children()[0].Measure(c)
}

func (e *edgeWatch) Arrange(r paintengine2d.Rect) {
	e.SetBounds(r)
	e.Children()[0].Arrange(paintengine2d.XYWH(0, 0, r.Dx(), r.Dy()))
	if e.arranged != nil {
		e.arranged(e)
	}
}
