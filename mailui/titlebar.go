package mailui

import (
	"math"
	"strings"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// The title bar holds the menu, Fetch / Write / Search and the tabs, and
// nothing over the folder pane: its left end is empty caption as wide as
// the pane, so the menu starts where the list does. The title bar is one
// row across the window (uitoolkit has no title bar split beside a pane,
// uitoolkit-gaps.md #37), so the empty part is sized after each layout of
// the panes, to where they put the pages.

// titleButtons is the app menu's button, then Fetch, Write and Search:
// push buttons with the look's own face and an icon alone on it — their
// names are their tips and what a screen reader says. (uitoolkit's
// icon-only button is the flat tool face, uitoolkit-gaps.md #39.)
func (s *session) titleButtons() widget.Component {
	lk := s.app.Look()
	if s.win != nil {
		lk = s.win.Look()
	}
	s.appBtn = newIconButton(appMenuIcon(lk), "Menu", s.openAppMenu)
	s.appBtn.Tip = "Menu (F10)"
	s.fetchBtn = newIconButton(style.IconDownload, "Fetch new messages for this account", s.getMessages)
	s.writeBtn = newIconButton(style.IconPen, "Write a new message", s.write)
	s.searchBtn = newIconButton(style.IconSearch, "Search", s.openSearch)
	s.searchBtn.SetAccessibleName("Search")
	// A search narrowing the list shows as a dot on the button: a push
	// button has no pressed state to keep.
	draw := s.searchBtn.Content
	s.searchBtn.Content = func(ctx *paintengine2d.Context, r paintengine2d.Rect, st style.ControlState) {
		draw(ctx, r, st)
		if strings.TrimSpace(s.filter.Query) == "" {
			return
		}
		lk := s.searchBtn.Look()
		d := style.Dip(lk, 7)
		dot := paintengine2d.XYWH(r.Max.X-d-style.Dip(lk, 4), r.Min.Y+style.Dip(lk, 4), d, d)
		ctx.DrawRoundRect(dot, d/2, d/2, paintengine2d.Fill(lk.Palette().Accent))
	}
	s.syncSearchBtn()
	return widgets.NewRow(s.appBtn, s.fetchBtn, s.writeBtn, s.searchBtn).WithGap(4).WithAlign(layout.AlignCenter)
}

// newIconButton is a push button that is its icon: the look's button face
// with the icon drawn on it, in the label's colour. what is its tip and its
// accessible name.
func newIconButton(icon style.ToolIcon, what string, on func()) *widgets.Button {
	b := widgets.NewButton("", on)
	b.Tip = what
	b.SetAccessibleName(what)
	b.Content = func(ctx *paintengine2d.Context, r paintengine2d.Rect, st style.ControlState) {
		lk := b.Look()
		sz := min(r.Dy()-style.Dip(lk, 10), style.Dip(lk, 20))
		if sz <= 0 {
			return
		}
		col := lk.Palette().Text
		if st.Disabled() {
			col = lk.Palette().TextMuted
		}
		box := paintengine2d.XYWH(r.Min.X+(r.Dx()-sz)/2, r.Min.Y+(r.Dy()-sz)/2, sz, sz)
		style.DrawToolIcon(ctx, box, icon, col, style.IconSetOf(lk))
	}
	return b
}

// appMenuIcon is the app menu button's icon: "more" (the dots every
// program's overflow menu has) where the look draws from an icon pack,
// and the cog where it draws its own set, which has no "more" — a stem it
// has no vector for would be the no-icon mark (uitoolkit-gaps.md #38).
func appMenuIcon(lk style.LookAndFeel) style.ToolIcon {
	if lk != nil && style.IsFileIconSet(style.IconSetOf(lk)) {
		if icon, ok := style.IconByStem("more"); ok {
			return icon
		}
	}
	return style.IconSettings
}

// openAppMenu drops the app menu under its button.
func (s *session) openAppMenu() {
	if s.appBtn == nil {
		return
	}
	r := widget.LocalToWindow(s.appBtn, s.appBtn.LocalBounds())
	widgets.ShowContextMenu(s.appBtn, paintengine2d.Pt(r.Min.X, r.Max.Y), s.appMenuItems()...)
}

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
	if g == nil || s.appBtn == nil || g.Host() == nil || s.appBtn.Host() == nil || pages.Host() == nil {
		return
	}
	off := widget.DeviceOrigin(pages).X - widget.DeviceOrigin(s.appBtn).X
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
