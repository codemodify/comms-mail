package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/diag"
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
	for _, c := range []widget.Component{s.appBtn, s.fetchBtn, s.writeBtn, s.searchBtn} {
		if !widget.Contains(head, c) {
			t.Fatalf("%T is not in the title bar", c)
		}
	}
	if head.StartWidth <= 0 {
		t.Fatal("the title bar over the folder pane should be left empty")
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
	// It follows the divider, in the layout that moves it.
	var split *widgets.Splitter
	walkAll(w.Content(), func(c widget.Component) {
		if sp, ok := c.(*widgets.Splitter); ok && widget.Contains(sp, pages) && split == nil {
			split = sp
		}
	})
	if split == nil {
		t.Fatal("no splitter beside the folder pane")
	}
	split.SetRatio(0.3)
	a.PumpOnce()
	if px, bx := widget.DeviceOrigin(pages).X, widget.DeviceOrigin(s.appBtn).X; px <= mx+50 || bx-px > 2 || bx-px < -2 {
		t.Fatalf("after moving the divider: the pages at %v (were %v), the menu button at %v", px, mx, bx)
	}
	split.SetRatio(0.17)
	a.PumpOnce()
	mx = widget.DeviceOrigin(s.appBtn).X
	// Real buttons — the look's push-button face — with an icon alone
	// and a name for the tip and the screen reader.
	buttons := []*widgets.Button{&s.appBtn.Button, &s.fetchBtn.Button, &s.writeBtn.Button, &s.searchBtn.Button}
	if s.appBtn.Icon != style.IconMenu {
		t.Fatal("the app menu's button should show the menu icon")
	}
	for _, b := range buttons {
		if b.Text != "" || b.Content == nil || b.Tip == "" || b.AccessibleName() == "" {
			t.Fatalf("button %q: text %q, tip %q — want an icon alone with a name", b.AccessibleName(), b.Text, b.Tip)
		}
		if sz := b.Bounds(); sz.Dx() < 20 || sz.Dy() < 20 {
			t.Fatalf("button %q is %v", b.AccessibleName(), sz)
		}
	}
	for _, b := range buttons {
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

// Under a pack whose era draws its own title strip (KDE 1 here), with the
// toolkit drawing the frame, the title bar is still the caption — the
// tabs in it, the menu and buttons over the pages — not a row under the
// era's strip that runs over the folder pane. The toolkit says so when it
// overrules a title bar, so no such finding is the test.
func TestTitleBarIsTheCaptionUnderAStackedPack(t *testing.T) {
	if !style.ThemePackAvailable("kde1") {
		t.Skip("kde1 is not in this build (-tags theme_engine_all)")
	}
	var look style.LookAndFeel
	for _, p := range style.ListBuiltinThemes() {
		if p.Name == "kde1" {
			look = p.Look()
		}
	}
	if style.DecorationOf(look, style.DecorationState{Active: true}).Stacked == false {
		t.Skip("kde1 no longer draws a stacked frame")
	}
	diag.Reset()
	t.Cleanup(diag.Reset)
	s, a, w, done := openMailFramedSession(t, look, false, AppOptions{}, platform.DecorationsClient)
	defer done()
	w.Inject(platform.Event{Kind: platform.EventResize, Width: 1280, Height: 800})
	a.PumpOnce()
	if !s.head.Framed() || s.head.Stacked() {
		t.Fatalf("framed %v, stacked %v: want the title bar to be the caption", s.head.Framed(), s.head.Stacked())
	}
	for _, f := range a.Diagnostics() {
		if f.Area == "caption" {
			t.Fatalf("the toolkit overruled the title bar: %v", f)
		}
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
	mx, px := widget.DeviceOrigin(s.appBtn).X, widget.DeviceOrigin(pages).X
	if d := mx - px; d > 2 || d < -2 {
		t.Fatalf("the menu button starts at %v, the pages at %v", mx, px)
	}
	if widget.DeviceBounds(s.appBtn).Max.Y > widget.DeviceBounds(s.tree).Min.Y {
		t.Fatal("the title bar's buttons are not above the folder pane")
	}
}

// Under BeOS, whose caption is a tab only as wide as its title, the title
// bar is still the caption, and a whole one: the tab's fitted width and
// the window's silhouette are dropped for it, where before uitoolkit
// 0.23.3 they cut the bar away above the window's body
// (uitoolkit-gaps.md #45).
func TestTitleBarUnderACaptionTab(t *testing.T) {
	var look style.LookAndFeel
	for _, p := range style.ListBuiltinThemes() {
		if p.Name == "beos" {
			look = p.Look()
		}
	}
	if look == nil {
		t.Skip("beos is not in this build (-tags theme_engine_all)")
	}
	if !style.DecorationOf(look, style.DecorationState{Active: true}).CaptionFits {
		t.Skip("beos no longer fits its caption to its title")
	}
	s, a, w, done := openMailFramedSession(t, look, false, AppOptions{}, platform.DecorationsClient)
	defer done()
	w.Inject(platform.Event{Kind: platform.EventResize, Width: 1280, Height: 800})
	a.PumpOnce()
	if s.head.Stacked() {
		t.Fatal("the title bar is a row under BeOS's tab, not the caption")
	}
	st := s.head.DecorationState()
	if !st.Merged || style.DecorationOf(a.Look(), st).CaptionFits {
		t.Fatalf("merged %v: the caption is still fitted to a title it does not draw", st.Merged)
	}
	var pages *edgeWatch
	walkAll(w.Content(), func(c widget.Component) {
		if e, ok := c.(*edgeWatch); ok {
			pages = e
		}
	})
	if d := widget.DeviceOrigin(s.appBtn).X - widget.DeviceOrigin(pages).X; d > 2 || d < -2 {
		t.Fatalf("the menu button at %v, the pages at %v", widget.DeviceOrigin(s.appBtn).X, widget.DeviceOrigin(pages).X)
	}
}

// The marks on the title bar's buttons are readable on a pack with short
// controls at Compact density, where uitoolkit before 0.23.2 drew them a
// few pixels across (uitoolkit-gaps.md #43): at most 10 in this face, and
// 12 to 19 since. What is measured is the mark's own ink: the button
// painted with its icon and without.
func TestTitleBarMarksAtCompact(t *testing.T) {
	var look style.LookAndFeel
	for _, p := range style.ListBuiltinThemes() {
		if p.Name == "metal-ocean" {
			look = style.WithDensity(p.Look(), style.DensityCompact)
		}
	}
	if look == nil {
		t.Skip("metal-ocean is not in this build (-tags theme_engine_all)")
	}
	s, a, _, done := openMailLookSession(t, look, false, AppOptions{})
	defer done()
	lk := a.Look()
	for _, b := range []*widgets.IconButton{&s.appBtn.IconButton, s.fetchBtn, s.writeBtn, s.searchBtn} {
		w, h := int(b.Bounds().Dx()), int(b.Bounds().Dy())
		with := paintengine2d.NewImage(w, h)
		b.Paint(paintengine2d.NewContext(with))
		icon := b.Icon
		b.Icon = style.IconNone
		without := paintengine2d.NewImage(w, h)
		b.Paint(paintengine2d.NewContext(without))
		b.Icon = icon
		x0, x1, y0, y1 := w, -1, h, -1
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r1, g1, b1, _ := with.PremulAt(x, y)
				r2, g2, b2, _ := without.PremulAt(x, y)
				if r1 != r2 || g1 != g2 || b1 != b2 {
					x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
				}
			}
		}
		if span := float32(max(x1-x0, y1-y0) + 1); x1 < 0 || span < style.Dip(lk, 11) {
			t.Fatalf("%q in a %dx%d button draws its mark %v px across", b.Action, w, h, span)
		}
	}
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
func appMenuButton(t *testing.T, w *app.Window) *widgets.MenuButton {
	t.Helper()
	var btn *widgets.MenuButton
	walkAll(mailTree(w), func(c widget.Component) {
		if b, ok := c.(*widgets.MenuButton); ok && b.AccessibleName() == "Menu" {
			btn = b
		}
	})
	if btn == nil {
		t.Fatal("no app menu button in the title bar")
	}
	return btn
}

// openAppMenu opens the app menu from its button and returns it; it stays
// open.
func openAppMenu(t *testing.T, a *app.Application, w *app.Window) *widgets.PopupMenu {
	t.Helper()
	if !appMenuButton(t, w).Open() {
		t.Fatal("the app menu did not open")
	}
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
