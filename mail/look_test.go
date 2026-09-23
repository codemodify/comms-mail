package mail

import (
	"context"
	"testing"
	"time"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
)

func TestMailRebuildDoesNotClobberLookJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := style.Appearance{Theme: style.ThemeLight, Corners: style.CornersSquare, Icons: style.IconSetSharp}
	if err := style.SaveAppearance(want); err != nil {
		t.Fatal(err)
	}
	s, a, w, stop := openMailLookSession(t, style.PreferredLook(), true, AppOptions{ShowFilter: true, Light: false})
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
	s, a, _, stop := openMailLookSession(t, style.PreferredLook(), true, AppOptions{ShowFilter: true, Light: false})
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

func TestMailViewLightWritesPaletteOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	start := style.Appearance{Theme: style.ThemeDark, Corners: style.CornersSquare, Icons: style.IconSetSharp}
	if err := style.SaveAppearance(start); err != nil {
		t.Fatal(err)
	}
	s, a, _, stop := openMailLookSession(t, style.PreferredLook(), true, AppOptions{ShowFilter: true})
	defer stop()

	s.setPalette(true)
	a.PumpOnce()
	got := style.LoadAppearance()
	if got.Name != "light" || got.Theme != style.ThemeLight || got.Corners != style.CornersSquare || got.Icons != style.IconSetSharp {
		t.Fatalf("View Light should SaveAppearance(WithPalette): %+v", got)
	}
	live := style.LookAppearance(a.Look())
	if live != got {
		t.Fatalf("live %+v prefs %+v", live, got)
	}
}

func openMailLookSession(t *testing.T, look style.LookAndFeel, watch bool, opts AppOptions) (*session, *app.Application, *app.Window, func()) {
	t.Helper()
	sock, stopDemo, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		stopDemo()
		t.Fatal(err)
	}
	a := uitoolkit.New(uitoolkit.Options{Look: look, Headless: true, Scale: 1, WatchLook: watch})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
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
