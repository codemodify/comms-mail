package mailui

import (
	"slices"
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
// a right click on the caption. The choice is comms-mail's own: look.json's
// "hideWindowMenu", applied again whenever the file changes, does not undo
// it.
func noWindowMenu(a *app.Application) {
	if a == nil {
		return
	}
	a.SetHideWindowMenu(true)
}

// scriptFaces draw the scripts uitoolkit's default fallback list names no
// face for (uitoolkit-gaps.md #63): mail comes in every script, and a
// shaping build (font_complex, font_emoji) borrows Arabic, Hebrew and the
// Indic scripts only from a family it is told of. Families that are not
// installed are skipped.
var scriptFaces = []string{
	"Noto Sans Arabic", "Noto Naskh Arabic", "Noto Sans Hebrew",
	"Noto Sans Devanagari", "Noto Sans Bengali", "Noto Sans Gurmukhi",
	"Noto Sans Gujarati", "Noto Sans Tamil", "Noto Sans Telugu",
	"Noto Sans Kannada", "Noto Sans Malayalam", "Noto Sans Sinhala",
	"Noto Sans Thai", "Noto Sans Lao", "Noto Sans Khmer", "Noto Sans Myanmar",
	"Noto Sans Georgian", "Noto Sans Armenian", "Noto Sans Ethiopic",
}

// useScriptFaces adds scriptFaces after the fallback families in force.
func useScriptFaces() {
	have := style.FallbackFamilies()
	list := append([]string(nil), have...)
	for _, f := range scriptFaces {
		if !slices.Contains(have, f) {
			list = append(list, f)
		}
	}
	if len(list) != len(have) {
		style.SetFallbackFamilies(list...)
	}
}
