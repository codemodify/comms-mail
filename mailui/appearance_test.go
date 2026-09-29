package mailui

import (
	"testing"

	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Appearance lists the themes in the build; picking one gives
// comms-mail that theme alone — look.json, shared with every uitoolkit
// app, is not touched, and a change to it later does not undo the pick —
// and "Use the shared theme" goes back to following it.
func TestAppearanceTabGivesCommsMailItsOwnTheme(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := style.SaveAppearance(style.Appearance{Theme: style.ThemeDark}); err != nil {
		t.Fatal(err)
	}
	shared := style.LoadAppearance()
	_, a, _, stop := openMailLookSession(t, style.PreferredLook(), true, AppOptions{})
	defer stop()
	keepOwnTheme(a)

	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 560, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(prefsAppearance(a))
	a.PumpOnce()
	var table *widgets.TableView
	var follow *widgets.Button
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TableView:
			table = v
		case *widgets.Button:
			follow = v
		}
	})
	if table == nil || follow == nil || table.RowCount != len(style.ListThemes()) || table.RowCount < 2 {
		t.Fatalf("the list: %v rows of %d themes", table, len(style.ListThemes()))
	}
	if follow.Enabled() {
		t.Fatal("\"use the shared theme\" is on while comms-mail already follows it")
	}

	light := -1
	for i, p := range style.ListThemes() { // the list's order, unsorted
		if p.Name == "light" {
			light = i
		}
	}
	if light < 0 {
		t.Fatal("no Light theme in the list")
	}
	table.Selected = light
	table.OnSelect(light)
	a.PumpOnce()
	if ownTheme() != "light" || style.LookAppearance(a.Look()).Name != "light" {
		t.Fatalf("own %q, live %q", ownTheme(), style.LookAppearance(a.Look()).Name)
	}
	if got := style.LoadAppearance(); got != shared {
		t.Fatalf("look.json changed: %+v", got)
	}
	if !follow.Enabled() {
		t.Fatal("no way back to the shared theme")
	}

	// uitoolkit's Settings changes the shared theme: comms-mail keeps its own.
	if err := style.SaveAppearance(style.Appearance{Theme: style.ThemeDark, Corners: style.CornersSquare}); err != nil {
		t.Fatal(err)
	}
	a.ReloadPreferredLook()
	a.PumpOnce()
	if got := style.LookAppearance(a.Look()).Name; got != "light" {
		t.Fatalf("a change to look.json replaced comms-mail's own theme with %q", got)
	}

	follow.OnClick()
	a.PumpOnce()
	if ownTheme() != "" || style.LookAppearance(a.Look()).Name != "dark" {
		t.Fatalf("after following again: own %q, live %q", ownTheme(), style.LookAppearance(a.Look()).Name)
	}
}
