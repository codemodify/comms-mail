package mailui

import (
	"strings"

	"goframework.io/desktop-ui"
	"goframework.io/desktop-ui/style"
)

// The look. comms-mail follows the theme set for every desktop-ui app —
// ~/.config/goframework-desktop-ui/look.json (style.AppearancePath), what
// desktop-ui's settings application writes — unless a theme of its own is
// chosen in Settings › Appearance. That choice is kept in mailui.json
// ("theme") and overrides look.json's theme for comms-mail alone; corners,
// icons and fonts still come from look.json, and no other application is
// touched. A theme needs its engine in the build (desktop-ui's engines are
// chosen by import): comms-mail imports every one (desktop.go).

// PreferredLook is the look comms-mail starts with.
func PreferredLook() style.LookAndFeel { return effectiveAppearance().Look() }

// ownTheme is the theme chosen for comms-mail alone; "" follows look.json.
func ownTheme() string { return strings.TrimSpace(loadChromePrefs().Theme) }

// OwnTheme is comms-mail's own theme (Settings › Appearance); "" when it
// follows the theme every desktop-ui app shares.
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
func applyEffectiveLook(a *ui.Application) {
	if a == nil {
		return
	}
	a.SetLook(style.WithDensity(effectiveAppearance().Look(), style.ParseDensity(loadChromePrefs().Density)))
}

// setOwnTheme keeps name as comms-mail's own theme ("" follows look.json
// again) and applies it.
func setOwnTheme(a *ui.Application, name string) {
	p := loadChromePrefs()
	p.Theme = strings.TrimSpace(name)
	saveChromePrefs(p)
	applyEffectiveLook(a)
}

// keepOwnTheme puts comms-mail's own theme back after the app has applied
// look.json (it watches the file and applies it as it is, when desktop-ui's
// settings application changes it).
func keepOwnTheme(a *ui.Application) {
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
// a right click on the caption. The choice is comms-mail's own: look.json's
// "hideWindowMenu", applied again whenever the file changes, does not undo
// it.
func noWindowMenu(a *ui.Application) {
	if a == nil {
		return
	}
	a.SetHideWindowMenu(true)
}
