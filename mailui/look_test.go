package mailui

import (
	"context"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"goframework.io/desktop-ui"
	"goframework.io/desktop-ui/platform"
	"goframework.io/desktop-ui/style"
)

func TestMailRebuildDoesNotClobberLookJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := style.Appearance{Theme: style.ThemeLight, Corners: style.CornersSquare, Icons: style.IconSetSharp}
	if err := style.SaveAppearance(want); err != nil {
		t.Fatal(err)
	}
	s, a, w, stop := openMailLookSession(t, style.PreferredLook(), true, AppOptions{Light: false})
	defer stop()

	s.density = style.DensityCompact
	s.opts.Layout = LayoutClassic
	s.rebuild()
	a.PumpOnce()

	got := style.LoadAppearance()
	if got != want.Normalize() {
		t.Fatalf("rebuild clobbered look.json: %+v", got)
	}
	live := style.LookAppearance(a.Look())
	if live.Theme != style.ThemeLight || live.Corners != style.CornersSquare || live.Icons != style.IconSetSharp || live.Name != "light" {
		t.Fatalf("applyLook after rebuild %+v", live)
	}
	if a.Look().Metrics().Radius != 0 {
		t.Fatalf("square radius %v", a.Look().Metrics().Radius)
	}
	if !s.opts.Light {
		t.Fatal("opts.Light should follow look.json, not the stale AppOptions flag")
	}
	_ = w
}

func TestMailWatchLookAppliesSettingsPack(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := style.SaveAppearance(style.DefaultAppearance().WithPalette(style.ThemeDark)); err != nil {
		t.Fatal(err)
	}
	s, a, _, stop := openMailLookSession(t, style.PreferredLook(), true, AppOptions{Light: false})
	defer stop()
	if style.LookAppearance(a.Look()).Theme != style.ThemeDark {
		t.Fatalf("start %+v", style.LookAppearance(a.Look()))
	}

	next := style.Appearance{Theme: style.ThemeLight, Corners: style.CornersSquare, Icons: style.IconSetSharp}
	if err := style.SaveAppearance(next); err != nil {
		t.Fatal(err)
	}
	a.PumpOnce()
	got := style.LookAppearance(a.Look())
	if got.Theme != style.ThemeLight || got.Corners != style.CornersSquare || got.Icons != style.IconSetSharp || got.Name != "light" {
		t.Fatalf("WatchLook after Settings Apply %+v", got)
	}
	if a.Look().Metrics().Radius != 0 {
		t.Fatalf("square radius %v", a.Look().Metrics().Radius)
	}
	if style.LoadAppearance() != next.Normalize() {
		t.Fatalf("Mail must not rewrite look.json on pump: %+v", style.LoadAppearance())
	}

	s.persistChrome()
	if style.LoadAppearance() != next.Normalize() {
		t.Fatalf("persistChrome rewrote look.json: %+v", style.LoadAppearance())
	}
}

// View Light picks comms-mail's own theme; the theme every desktop-ui app
// shares (look.json) is not touched.
func TestMailViewLightKeepsItsOwnTheme(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	start := style.Appearance{Theme: style.ThemeDark, Corners: style.CornersSquare, Icons: style.IconSetSharp}
	if err := style.SaveAppearance(start); err != nil {
		t.Fatal(err)
	}
	before := style.LoadAppearance()
	s, a, _, stop := openMailLookSession(t, style.PreferredLook(), true, AppOptions{})
	defer stop()

	s.setPalette(true)
	a.PumpOnce()
	if got := style.LoadAppearance(); got != before {
		t.Fatalf("look.json changed: %+v, was %+v", got, before)
	}
	if ownTheme() != "light" {
		t.Fatalf("own theme %q", ownTheme())
	}
	live := style.LookAppearance(a.Look())
	if live.Name != "light" || live.Theme != style.ThemeLight || live.Corners != style.CornersSquare || live.Icons != style.IconSetSharp {
		t.Fatalf("live %+v", live)
	}
}

func openMailLookSession(t *testing.T, look style.LookAndFeel, watch bool, opts AppOptions) (*session, *ui.Application, *ui.Window, func()) {
	t.Helper()
	return openMailFramedSession(t, look, watch, opts, platform.DecorationsAuto)
}

// openMailFramedSession is openMailLookSession with the window's frame
// drawn by deco: platform.DecorationsClient for the toolkit's own caption.
func openMailFramedSession(t *testing.T, look style.LookAndFeel, watch bool, opts AppOptions, deco platform.Decorations) (*session, *ui.Application, *ui.Window, func()) {
	t.Helper()
	sock, stopDemo, err := mailcore.StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		stopDemo()
		t.Fatal(err)
	}
	a := ui.New(ui.Options{Look: look, Headless: true, Scale: 1, WatchLook: watch})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true, Decorations: deco})
	if err != nil {
		cli.Close()
		stopDemo()
		t.Fatal(err)
	}
	s := newSession(a, w, cli, opts)
	w.SetContent(s.build())
	a.PumpOnce()
	return s, a, w, func() {
		w.Close()
		cli.Close()
		stopDemo()
	}
}

// comms-mail's captions have no window-menu button, whatever look.json's
// "hideWindowMenu" says each time the file is applied again.
func TestNoWindowMenuOutlastsLookJSON(t *testing.T) {
	a := ui.New(ui.Options{Look: style.DarkLook(), Headless: true})
	noWindowMenu(a)
	ap := style.Appearance{Theme: style.ThemeDark}
	ap.HideWindowMenu = false
	a.ApplyAppearance(ap)
	if !a.HideWindowMenu() || !a.HideWindowMenuPinned() {
		t.Fatalf("after look.json: hidden %v, pinned %v", a.HideWindowMenu(), a.HideWindowMenuPinned())
	}
}

// The desktop's reduced-motion setting applies to comms-mail: desktop-ui
// only records it unless the application asks.
func TestFollowsTheDesktopsReducedMotion(t *testing.T) {
	t.Setenv(style.AnimationsEnv, "")
	defer style.SetFollowDesktopMotion(style.FollowsDesktopMotion())
	defer style.SetDesktopReduceMotion(style.DesktopReducesMotion())
	style.SetFollowDesktopMotion(false)
	cli := demoClient(t)
	a := ui.New(ui.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(Open(a, w, cli, AppOptions{}))
	style.SetDesktopReduceMotion(true)
	if !style.FollowsDesktopMotion() || style.Animations() {
		t.Fatalf("follows %v, animates %v with the desktop reducing motion", style.FollowsDesktopMotion(), style.Animations())
	}
}

// Chips and tags are square where the look's corners are, and slightly
// rounded where it rounds them — never a capsule.
func TestChipsAreBoxesNotCapsules(t *testing.T) {
	sq := style.WithCorners(style.LightLook(), style.CornersSquare)
	if r := chipRadius(sq); r != 0 {
		t.Fatalf("square corners: radius %v", r)
	}
	lk := style.LightLook()
	h := lk.Font().Height() + style.Dip(lk, 8)
	if r := chipRadius(lk); r <= 0 || r > style.Dip(lk, 3) || r >= h/4 {
		t.Fatalf("rounded corners: radius %v for a chip %v tall", r, h)
	}
}
