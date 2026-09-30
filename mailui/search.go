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

// Search is a dialog, opened by the Search button after Write or by
// Ctrl+F: what to look for, in this folder or all of them, and whether to
// ask the mail server too. Search narrows the list to what matches; the
// button stays pressed while it does, and Clear (in the same dialog) takes
// the list back.

// openSearch shows the search dialog over the window.
func (s *session) openSearch() {
	if s.win == nil {
		return
	}
	field := widgets.NewTextField(s.filter.Query, "Subject, people or text", nil)
	all := widgets.NewCheckbox("All folders", s.searchAll, nil)
	server := widgets.NewCheckbox("On the server too", s.srv.on, nil)
	serverNote := wrapLabel("Finds words in mail not downloaded yet; the mail server is asked as well.")

	var ov *widgets.Overlay
	done := func() { widget.DismissOverlay(ov) }
	apply := func() {
		s.setSearch(field.Text, all.Checked, server.Checked)
		done()
	}
	clear := newButton("Clear", func() {
		s.setSearch("", false, server.Checked)
		done()
	})
	clear.Tip = "Show the whole folder again"
	cancel := newButton("Cancel", done)
	search := newButton("Search", apply)
	search.Icon = style.IconSearch
	search.Primary = true
	field.OnSubmit = func(string) { apply() }

	buttons := widgets.NewButtonBox().
		AddButton(clear, widgets.RoleAction).
		AddButton(cancel, widgets.RoleReject).
		AddButton(search, widgets.RoleAccept)
	col := widgets.NewColumn(field, all, server, serverNote, buttons).WithGap(8).WithPad(4)
	card := widgets.NewPanel("Search", col)
	card.Window = true
	card.Raised = true
	card.OnClose = done
	ov = widgets.NewOverlay(card)
	ov.Modal = true
	ov.MinCardW = 420
	ov.InitialFocus = field
	widget.ShowOverlay(s.win.Content(), ov)
	field.SelectAll()
}

// setSearch narrows the list to what matches query — in this folder or in
// all of them, and on the server too when server is set — or shows the
// whole folder again for an empty query.
func (s *session) setSearch(query string, all, server bool) {
	query = strings.TrimSpace(query)
	s.filter.Query = query
	s.searchAll = all && query != ""
	if server != s.srv.on {
		s.srv.on = server
		s.srv.asked, s.srv.key, s.srv.hits = "", "", nil
	}
	s.syncSearchBtn()
	s.refreshList()
	switch {
	case query == "":
		s.mark("Search cleared")
	case s.searchAll:
		s.mark("Searching all folders for “" + query + "”")
	default:
		s.mark("Searching for “" + query + "”")
	}
}

// syncSearchBtn presses the Search button while a search narrows the list,
// and says what it is in its tip.
func (s *session) syncSearchBtn() {
	b := s.searchBtn
	if b == nil {
		return
	}
	q := strings.TrimSpace(s.filter.Query)
	b.Down = q != ""
	switch {
	case q == "":
		b.Tip = "Search (Ctrl+F)"
	case s.searchAll:
		b.Tip = "Searching all folders for “" + q + "” — click to change or clear"
	default:
		b.Tip = "Searching for “" + q + "” — click to change or clear"
	}
	if s.mainBar != nil {
		s.mainBar.Invalidate()
	}
}

// sideHead is the title bar's left end: the menu, Fetch, Write and Search,
// over the folder pane and as wide as it, so the tabs start where the
// pages do and the folder pane reads as running from the top of the window
// to the bottom. want is that width, from the panes' last layout
// (alignSideHead); the row inside keeps its own width at the left.
type sideHead struct {
	widget.Base
	want  float32 // device pixels
	tries int     // adjustments in a row that have not settled
}

func newSideHead(row widget.Component) *sideHead {
	h := &sideHead{}
	h.Init(h)
	h.Add(row)
	return h
}

func (h *sideHead) Measure(c layout.Constraints) paintengine2d.Point {
	kid := h.Children()[0].Measure(layout.Constraints{MaxW: -1, MaxH: c.MaxH})
	return c.Constrain(paintengine2d.Pt(max(kid.X, h.want), kid.Y))
}

func (h *sideHead) Arrange(r paintengine2d.Rect) {
	h.SetBounds(r)
	h.Children()[0].Arrange(paintengine2d.XYWH(0, 0, r.Dx(), r.Dy()))
}

// alignSideHead sizes the title bar's left end so that the tabs after it
// start where the pages do: pages is the right-hand side of the window,
// just laid out. It measures where the tabs are rather than adding up the
// header bar's gaps and padding. The title bar is laid out on its own, so
// a change is taken up on the next layout.
func (s *session) alignSideHead(pages widget.Component) {
	h := s.side
	if h == nil || s.tabs == nil || h.Host() == nil || pages.Host() == nil || s.tabs.Host() == nil {
		return
	}
	off := widget.DeviceOrigin(pages).X - widget.DeviceOrigin(s.tabs).X
	if math.Abs(float64(off)) < 1 {
		h.tries = 0
		return
	}
	// A layout that cannot settle (a window too narrow for the buttons)
	// is left as it is rather than laid out again and again.
	if h.tries++; h.tries > 4 {
		return
	}
	want := h.Bounds().Dx() + off
	natural := h.Children()[0].Measure(layout.Constraints{MaxW: -1, MaxH: -1}).X
	if sp := s.sideSplit; want < natural && sp != nil && sp.A != nil && sp.Ratio > 0 {
		// The folder pane is narrower than the buttons over it: it is
		// widened to them, which makes their width its narrowest
		// (uitoolkit's Splitter has no minimum of its own,
		// uitoolkit-gaps.md #36). Half a pixel over, for the pane's
		// rounding.
		if aw := sp.A.Bounds().Dx(); aw > 0 {
			sp.Ratio = (aw + natural - want + 0.5) * sp.Ratio / aw
		}
		want = natural
	}
	h.want = max(want, 0)
	h.Invalidate()
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
