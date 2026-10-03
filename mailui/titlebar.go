package mailui

import (
	"math"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// The title bar holds the menu, Fetch / Write / Search and the tabs, and
// nothing over the folder pane: its start is left empty (HeaderBar's
// StartWidth) as wide as the pane, so the menu starts where the list does.
// Every layout that moves the pages — the first, a window resize, a drag of
// the divider — sets it from where they landed (alignTitle).

// titleButtons is the app menu's button, then Fetch, Write and Search:
// push buttons with the look's own face and an icon alone on it, named
// for the tip and the screen reader.
func (s *session) titleButtons() widget.Component {
	s.appBtn = widgets.NewMenuButton(style.IconMenu, "Menu")
	s.appBtn.Build = s.appMenuItems
	s.fetchBtn = widgets.NewIconButton(style.IconDownload, "Fetch new messages for this account", s.getMessages)
	s.writeBtn = widgets.NewIconButton(style.IconPen, "Write a new message", s.write)
	s.searchBtn = widgets.NewIconButton(style.IconSearch, "Search", s.openSearch)
	s.searchBtn.Toggle = true // down while a search narrows the list
	s.syncSearchBtn()
	return widgets.NewRow(s.appBtn, s.fetchBtn, s.writeBtn, s.searchBtn).WithGap(4).WithAlign(layout.AlignCenter)
}

// openAppMenu drops the app menu under its button (F10).
func (s *session) openAppMenu() {
	if s.appBtn != nil {
		s.appBtn.Open()
	}
}

// alignTitle starts the title bar's items where pages — the right-hand
// side, just laid out — landed. The window lays the title bar out before
// its content, so a change re-arranges the bar in the same pass rather
// than a frame later (uitoolkit-gaps.md #37).
func (s *session) alignTitle(pages widget.Component) {
	h := s.head
	if h == nil || h.Host() == nil || pages.Host() == nil {
		return
	}
	want := max(widget.DeviceOrigin(pages).X-widget.DeviceOrigin(h).X, 0)
	if math.Abs(float64(want-h.StartWidth)) < 1 {
		return
	}
	h.StartWidth = want
	h.Arrange(h.Bounds())
	h.Invalidate()
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
