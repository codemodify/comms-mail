package mailui

import (
	"strings"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/style"
)

// The look. comms-mail follows the theme set for every uitoolkit app —
// ~/.config/uitoolkit/look.json, what uitoolkit's Settings writes — unless
// a theme of its own is chosen in Settings › Appearance. That choice is
// kept in mailui.json ("theme") and overrides look.json's theme for
// comms-mail alone; corners, icons and fonts still come from look.json,
// and no other application is touched. A theme needs its engine in the
// build (uitoolkit's engines are opt-in, docs/engines.md): comms-mail is
// built with every one (the Makefile's -tags theme_engine_all).

// PreferredLook is the look comms-mail starts with.
func PreferredLook() style.LookAndFeel { return effectiveAppearance().Look() }

// ownTheme is the theme chosen for comms-mail alone; "" follows look.json.
func ownTheme() string { return strings.TrimSpace(loadChromePrefs().Theme) }

// OwnTheme is comms-mail's own theme (Settings › Appearance); "" when it
// follows the theme every uitoolkit app shares.
func OwnTheme() string { return ownTheme() }

// effectiveAppearance is look.json's appearance, with comms-mail's own
// theme in place of its theme when one is chosen and in this build.
func effectiveAppearance() style.Appearance {
	ap := style.LoadAppearance()
	if name := ownTheme(); name != "" {
		if pack, ok := style.LoadTheme(name); ok {
			ap.Name, ap.Theme, ap.FollowDesktop = pack.Name, pack.Palette, false
		}
	}
	return ap
}

// applyEffectiveLook puts the look comms-mail should have on every
// window of a, at the density the window was set to.
func applyEffectiveLook(a *app.Application) {
	if a == nil {
		return
	}
	a.SetLook(style.WithDensity(effectiveAppearance().Look(), style.ParseDensity(loadChromePrefs().Density)))
}

// setOwnTheme keeps name as comms-mail's own theme ("" follows look.json
// again) and applies it.
func setOwnTheme(a *app.Application, name string) {
	p := loadChromePrefs()
	p.Theme = strings.TrimSpace(name)
	saveChromePrefs(p)
	applyEffectiveLook(a)
}

// keepOwnTheme puts comms-mail's own theme back after the app has applied
// look.json (it watches the file and applies it as it is, when uitoolkit's
// Settings changes it).
func keepOwnTheme(a *app.Application) {
	if a == nil {
		return
	}
	a.OnLookChange(func() {
		want := effectiveAppearance().Name
		if ownTheme() != "" && style.LookAppearance(a.Look()).Name != want {
			applyEffectiveLook(a)
		}
	})
}

// noWindowMenu leaves the window-menu button out of the caption of every
// comms-mail window the toolkit draws a frame for. The menu itself is still
// a right click on the caption. The toolkit takes the button's state from
// look.json each time it applies that file (a theme changed in uitoolkit's
// Settings), so it is dropped again after every change of look.
func noWindowMenu(a *app.Application) {
	if a == nil {
		return
	}
	a.SetHideWindowMenu(true)
	a.OnLookChange(func() { a.SetHideWindowMenu(true) })
}
