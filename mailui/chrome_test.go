package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// runSearch opens the search dialog with Ctrl+F, types query, sets All
// folders, and presses Search.
func runSearch(t *testing.T, a *app.Application, w *app.Window, query string, all bool) {
	t.Helper()
	w.Inject(platform.Event{Kind: platform.EventKeyDown, Key: platform.KeyF, Mods: platform.ModCtrl})
	a.PumpOnce()
	o := w.Overlay()
	if o == nil {
		t.Fatal("Ctrl+F opened no search dialog")
	}
	var field *widgets.TextField
	var search *widgets.Button
	widget.Walk(o, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			field = v
		case *widgets.Checkbox:
			if v.Text == "All folders" {
				v.Checked = all
			}
		case *widgets.Button:
			if v.Text == "Search" {
				search = v
			}
		}
	})
	if field == nil || search == nil {
		t.Fatal("the search dialog has no field or no Search button")
	}
	field.SetText(query)
	search.OnClick()
	a.PumpOnce()
	if w.Overlay() != nil {
		t.Fatal("Search did not close the dialog")
	}
}

// The title bar: M, then Fetch, Write and Search as icons alone, over the
// folder pane and as wide as it; the tabs start where the pages do. No
// quick filter, no All folders or On server buttons: they are in the
// search dialog. The folder pane runs from the title bar to the bottom.
func TestMailTitleBarChrome(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	head, ok := w.TitleBar().(*widgets.HeaderBar)
	if !ok || head.Center() != widget.Component(s.tabs) {
		t.Fatal("the title bar is not a header bar with the tabs in the middle")
	}
	if !widget.Contains(head, s.side) || !widget.Contains(s.side, s.mainBar) {
		t.Fatal("the menu and tool bar are not at the title bar's left end")
	}
	var menu *widgets.MenuBar
	widget.Walk(s.side, func(c widget.Component) {
		if m, ok := c.(*widgets.MenuBar); ok {
			menu = m
		}
	})
	if menu == nil || len(menu.Menus()) != 1 || menu.Menus()[0].Title != "M" {
		t.Fatal("the M menu is not at the left end")
	}
	want := []style.ToolIcon{style.IconDownload, style.IconPen, style.IconSearch}
	items := s.mainBar.Items()
	if len(items) != len(want) {
		t.Fatalf("tool bar has %d items", len(items))
	}
	for i, it := range items {
		if it.Icon != want[i] || it.Text != "" || it.Tip == "" {
			t.Fatalf("item %d: icon %v text %q tip %q — want icon alone, with a tip", i, it.Icon, it.Text, it.Tip)
		}
	}
	assertFetchWriteIconsPaint(t, s.mainBar)
	walkAll(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			if strings.Contains(v.Placeholder, "Quick Filter") {
				t.Fatal("the quick filter is still in the window")
			}
		case *widgets.ToolBar:
			for _, it := range v.Items() {
				if it.Text == "All folders" || it.Text == "On server" {
					t.Fatalf("%q is still on a tool bar", it.Text)
				}
			}
		}
	})
	walkAll(head, func(c widget.Component) {
		if tb, ok := c.(*widgets.ToolBar); ok && tb != s.mainBar {
			t.Fatal("a second tool bar in the title bar")
		}
	})

	// The tabs begin where the pages do: the left end is as wide as the
	// folder pane.
	var pages *edgeWatch
	walkAll(w.Content(), func(c widget.Component) {
		if e, ok := c.(*edgeWatch); ok {
			pages = e
		}
	})
	if pages == nil {
		t.Fatal("no pages pane")
	}
	tabsX := widget.DeviceOrigin(s.tabs).X
	pagesX := widget.DeviceOrigin(pages).X
	if d := tabsX - pagesX; d > 2 || d < -2 {
		t.Fatalf("tabs start at %v, the pages at %v", tabsX, pagesX)
	}
	// The folder pane runs to the bottom of the window.
	tree := widget.DeviceBounds(s.tree)
	outbox := widget.DeviceBounds(s.outboxTree)
	content := widget.DeviceBounds(w.Content())
	if outbox.Max.Y < content.Max.Y-12 || tree.Min.Y > content.Min.Y+12 {
		t.Fatalf("folder pane %v…%v inside %v: not the full height", tree, outbox, content)
	}
	assertTagsTree(t, s.tree)
}

// The search dialog narrows the list, presses the Search button while it
// does, and Clear takes the list back.
func TestSearchDialog(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	before := len(s.rows)
	if s.searchBtn.Down {
		t.Fatal("Search is pressed with no search")
	}
	runSearch(t, a, w, "lunch", true)
	if s.filter.Query != "lunch" || !s.searchAll || !s.searchBtn.Down {
		t.Fatalf("query %q all %v pressed %v", s.filter.Query, s.searchAll, s.searchBtn.Down)
	}
	if len(s.rows) == 0 || len(s.rows) >= before {
		t.Fatalf("rows %d, before %d", len(s.rows), before)
	}
	if !strings.Contains(s.searchBtn.Tip, "lunch") {
		t.Fatalf("tip %q", s.searchBtn.Tip)
	}

	// The dialog opens with the search as it is; Clear ends it.
	s.openSearch()
	a.PumpOnce()
	var field *widgets.TextField
	var all *widgets.Checkbox
	var clear *widgets.Button
	widget.Walk(w.Overlay(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			field = v
		case *widgets.Checkbox:
			if v.Text == "All folders" {
				all = v
			}
		case *widgets.Button:
			if v.Text == "Clear" {
				clear = v
			}
		}
	})
	if field == nil || field.Text != "lunch" || all == nil || !all.Checked || clear == nil {
		t.Fatal("the dialog does not show the search it would change")
	}
	clear.OnClick()
	a.PumpOnce()
	if s.filter.Query != "" || s.searchAll || s.searchBtn.Down || len(s.rows) != before || w.Overlay() != nil {
		t.Fatalf("after Clear: query %q all %v pressed %v rows %d", s.filter.Query, s.searchAll, s.searchBtn.Down, len(s.rows))
	}

	// "On the server too" is the server search's setting.
	s.setSearch("x", false, true)
	if !s.srv.on {
		t.Fatal("the server search did not turn on")
	}
	s.setSearch("", false, false)
	if s.srv.on {
		t.Fatal("the server search did not turn off")
	}
}

// A message tab takes the place of the list and the reading pane, and
// leaves the folder pane where it is.
func TestMessageTabKeepsFolderPane(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	s.selected = []mailcore.MessageID{s.rows[0].ID}
	s.openInTab()
	s.waitIdle()
	a.PumpOnce()
	mt, ok := s.activeTab()
	if !ok || !mt.view.Visible() {
		t.Fatal("no message tab showing")
	}
	if !s.tree.Visible() || s.tree.Bounds().Dx() <= 0 {
		t.Fatal("the folder pane went with the tab")
	}
	if widget.DeviceOrigin(mt.view).X < widget.DeviceBounds(s.tree).Max.X {
		t.Fatal("the tab's page is not to the right of the folder pane")
	}
}

var _ = uitoolkit.Version
