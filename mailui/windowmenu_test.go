package mailui

import (
	"testing"

	"goframework.io/desktop-ui"
	"goframework.io/desktop-ui-widgets"
	"goframework.io/desktop-ui/platform"
	"goframework.io/desktop-ui/style"
	"goframework.io/desktop-ui/widget"
)

// captionButtons are the buttons w's toolkit-drawn caption shows.
func captionButtons(w *ui.Window) []platform.CaptionButton {
	var out []platform.CaptionButton
	walkAll(mailTree(w), func(c widget.Component) {
		if wc, ok := c.(*widgets.WindowControls); ok {
			out = append(out, wc.Shown()...)
		}
	})
	return out
}

func hasWindowMenu(bs []platform.CaptionButton) bool {
	for _, b := range bs {
		if b == platform.CaptionMenu {
			return true
		}
	}
	return false
}

// comms-mail's windows have no window-menu button in the caption the
// toolkit draws, also after the shared look is applied again (look.json
// changing): the button is left out for the whole app, so a window opened
// before the mail window loses it too.
func TestNoWindowMenuButton(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	cli := demoClient(t)
	a := ui.New(ui.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	// A desktop whose captions have the button (KWin's default does).
	tb := a.TitleBarPrefs()
	tb.Layout = platform.ParseButtonLayout("menu:minimize,maximize,close")
	a.SetTitleBarPrefs(tb)
	opts := platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true, Decorations: platform.DecorationsClient}
	open := func(content func(*ui.Window) widget.Component) *ui.Window {
		w, err := a.NewWindow(opts)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(w.Close)
		w.SetContent(content(w))
		a.PumpOnce()
		return w
	}
	before := open(func(w *ui.Window) widget.Component { return newSession(a, w, cli, AppOptions{}).build() })
	if bs := captionButtons(before); !hasWindowMenu(bs) {
		t.Fatalf("no window-menu button to leave out: %v", bs)
	}
	main := open(func(w *ui.Window) widget.Component { return Open(a, w, cli, AppOptions{}) })
	check := func(when string) {
		t.Helper()
		for _, w := range []*ui.Window{main, before} {
			if bs := captionButtons(w); len(bs) == 0 || hasWindowMenu(bs) {
				t.Errorf("%s, a caption has %v", when, bs)
			}
		}
	}
	check("opened")
	// What desktop-ui's settings application changing look.json does.
	a.ApplyAppearance(style.DefaultAppearance())
	a.PumpOnce()
	check("after look.json")
}
