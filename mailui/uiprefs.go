package mailui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/codemodify/comms-mail/mailcore"
	"goframework.io/desktop-ui/style"
)

// ChromePrefs is UI-only (card vs table, density, layout, filter). Lives
// in mailui.json. Theme packs stay in look.json (Settings); Light is a
// menu-checkmark mirror of LoadAppearance, not a second skin owner.
type ChromePrefs struct {
	CardView bool   `json:"cardView"`
	Density  string `json:"density"`
	Layout   string `json:"layout,omitempty"`
	Light    bool   `json:"light,omitempty"`
	Threaded bool   `json:"threaded,omitempty"`
	HideMute bool   `json:"hideMuted,omitempty"`
	// InviteLess folds invitation cards (Less / More).
	InviteLess bool `json:"inviteLess,omitempty"`
	// Theme is comms-mail's own theme (Settings › Appearance), over the
	// one every desktop-ui app shares in look.json; "" follows look.json.
	Theme string `json:"theme,omitempty"`
	// Tabs are the messages open in tabs, in order, and TabShowing the
	// one showing ("" the Mail tab): the next start reopens them.
	Tabs       []mailcore.MessageID `json:"tabs,omitempty"`
	TabShowing mailcore.MessageID   `json:"tabShowing,omitempty"`
}

// chromePrefsPath is mailui.json, beside mail.json in ConfigDir, so the
// two agree on where configuration lives.
func chromePrefsPath() string {
	return filepath.Join(mailcore.ConfigDir(), "mailui.json")
}

func loadChromePrefs() ChromePrefs {
	var p ChromePrefs
	b, err := os.ReadFile(chromePrefsPath())
	if err != nil {
		return p
	}
	_ = json.Unmarshal(b, &p)
	return p
}

func saveChromePrefs(p ChromePrefs) {
	path := chromePrefsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o600)
}

func (p ChromePrefs) density() style.Density {
	return style.ParseDensity(p.Density)
}
