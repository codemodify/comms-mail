package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
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

// The title bar holds the M menu, then Fetch, Write and Search as icons
// alone, then the tabs — starting where the pages do; over the folder pane
// it is empty caption. The folder pane runs from the title bar to the
// bottom. No quick filter, no All folders or On server buttons: they are
// in the search dialog.
func TestMailTitleBarChrome(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	for i := 0; i < 4; i++ {
		a.PumpOnce()
	}
	head, ok := w.TitleBar().(*widgets.HeaderBar)
	if !ok || head.Center() != widget.Component(s.tabs) {
		t.Fatal("the tabs are not in the title bar")
	}
	for _, c := range []widget.Component{s.titleGap, s.appBtn, s.fetchBtn, s.writeBtn, s.searchBtn} {
		if !widget.Contains(head, c) {
			t.Fatalf("%T is not in the title bar", c)
		}
	}
	if len(s.titleGap.Children()) != 0 || !s.titleGap.CaptionAt(paintengine2d.Pt(1, 1)) {
		t.Fatal("the title bar over the folder pane should be empty caption")
	}
	mx, bx, tx := widget.DeviceOrigin(s.appBtn).X, widget.DeviceOrigin(s.fetchBtn).X, widget.DeviceOrigin(s.tabs).X
	if !(mx < bx && bx < tx) {
		t.Fatalf("order: menu at %v, buttons at %v, tabs at %v", mx, bx, tx)
	}
	var pages *edgeWatch
	walkAll(w.Content(), func(c widget.Component) {
		if e, ok := c.(*edgeWatch); ok {
			pages = e
		}
	})
	if pages == nil {
		t.Fatal("no pages pane")
	}
	if d := mx - widget.DeviceOrigin(pages).X; d > 2 || d < -2 {
		t.Fatalf("the menu button starts at %v, the pages at %v", mx, widget.DeviceOrigin(pages).X)
	}
	// Real buttons — the look's push-button face — with an icon alone
	// and a name for the tip and the screen reader.
	for _, b := range []*widgets.Button{s.appBtn, s.fetchBtn, s.writeBtn, s.searchBtn} {
		if b.Text != "" || b.Content == nil || b.Tip == "" || b.AccessibleName() == "" {
			t.Fatalf("button %q: text %q, tip %q — want an icon alone with a name", b.AccessibleName(), b.Text, b.Tip)
		}
		if sz := b.Bounds(); sz.Dx() < 20 || sz.Dy() < 20 {
			t.Fatalf("button %q is %v", b.AccessibleName(), sz)
		}
	}
	for _, b := range []*widgets.Button{s.appBtn, s.fetchBtn, s.writeBtn, s.searchBtn} {
		img := paintengine2d.NewImage(int(b.Bounds().Dx()), int(b.Bounds().Dy()))
		b.Paint(paintengine2d.NewContext(img))
		if ink := cellInk(img, 0, img.Width); ink < 20 {
			t.Fatalf("button %q draws nothing (%d)", b.AccessibleName(), ink)
		}
	}
	// The menu drops from its button.
	pop := openAppMenu(t, a, w)
	var labels []string
	for _, it := range pop.Items {
		if it != nil && !it.Separator {
			l, _, _ := widgets.ParseMnemonic(it.Text)
			labels = append(labels, l)
		}
	}
	if strings.Join(labels, "|") != "View|Notify|Settings|Quit" {
		t.Fatalf("app menu %q", labels)
	}
	w.DismissPopup()
	a.PumpOnce()
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
		case *widgets.MenuBar:
			t.Fatal("a menu bar in the window: the app menu is a button")
		}
	})

	// The folder pane: from the top of the content to its bottom.
	tree := widget.DeviceBounds(s.tree)
	outbox := widget.DeviceBounds(s.outboxTree)
	content := widget.DeviceBounds(w.Content())
	if tree.Min.Y > content.Min.Y+12 || outbox.Max.Y < content.Max.Y-12 {
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
	searching := func() bool { return strings.TrimSpace(s.filter.Query) != "" }
	if searching() {
		t.Fatal("a search with none asked for")
	}
	runSearch(t, a, w, "lunch", true)
	if s.filter.Query != "lunch" || !s.searchAll || !searching() {
		t.Fatalf("query %q all %v", s.filter.Query, s.searchAll)
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
	if s.filter.Query != "" || s.searchAll || len(s.rows) != before || w.Overlay() != nil {
		t.Fatalf("after Clear: query %q all %v rows %d", s.filter.Query, s.searchAll, len(s.rows))
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

// appMenuButton is the app menu's button in the title bar.
func appMenuButton(t *testing.T, w *app.Window) *widgets.Button {
	t.Helper()
	var btn *widgets.Button
	walkAll(mailTree(w), func(c widget.Component) {
		if b, ok := c.(*widgets.Button); ok && b.AccessibleName() == "Menu" {
			btn = b
		}
	})
	if btn == nil {
		t.Fatal("no app menu button in the title bar")
	}
	return btn
}

// openAppMenu presses the app menu's button and returns the menu it
// dropped, which stays open.
func openAppMenu(t *testing.T, a *app.Application, w *app.Window) *widgets.PopupMenu {
	t.Helper()
	appMenuButton(t, w).OnClick()
	a.PumpOnce()
	pop, ok := w.Popup().(*widgets.PopupMenu)
	if !ok || pop == nil {
		t.Fatal("the app menu did not open")
	}
	return pop
}

// appMenuRows is the app menu's rows, the menu closed again.
func appMenuRows(t *testing.T, a *app.Application, w *app.Window) []*widgets.MenuItem {
	t.Helper()
	pop := openAppMenu(t, a, w)
	items := pop.Items
	w.DismissPopup()
	a.PumpOnce()
	return items
}
