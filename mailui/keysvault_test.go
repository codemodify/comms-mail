package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Keys asks before a change of place leaves your
// keys in a vault of secretvault's behind — it cannot move them from one
// of its vaults to another, and they never leave it — saying where they
// stay; No changes nothing, Yes changes it. Naming the same vault, or a
// format with no keys there, asks nothing.
func TestKeysAskBeforeLeavingThemInAVault(t *testing.T) {
	s, a, sv, _ := securityDaemon(t)
	if _, err := s.cli.PutAccount(mailcore.AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: mailcore.ServerConfig{Host: "127.0.0.1:1", TLSMode: "ssl"}}); err != nil {
		t.Fatal(err)
	}
	sv.OwnKeys("ada@example.com")
	sv.AddVault("work")
	section, _ := keysSection(a, s.cli)
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 900, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(section)
	a.PumpOnce()

	vaultOf := func(format string) string {
		t.Helper()
		v, err := s.cli.KeysView(format)
		if err != nil {
			t.Fatal(err)
		}
		return v.Place.Vault
	}
	// pick names the vault (Custom), or secretvault's default (""), and
	// presses Apply.
	pick := func(vault string) {
		t.Helper()
		var group *widgets.RadioGroup
		var name *widgets.TextField
		var apply *widgets.Button
		widget.Walk(section, func(c widget.Component) {
			switch v := c.(type) {
			case *widgets.RadioGroup:
				group = v
			case *widgets.TextField:
				if v.Placeholder == "Vault name" {
					name = v
				}
			case *widgets.Button:
				if v.Text == "Apply" {
					apply = v
				}
			}
		})
		if vault == "" {
			group.Select(0)
		} else {
			group.Select(1)
			name.SetText(vault)
			name.OnInput(vault)
		}
		a.PumpOnce()
		if !apply.Enabled() {
			t.Fatalf("Apply is off for %q", vault)
		}
		apply.OnClick()
		a.PumpOnce()
	}
	asks := func() string {
		var texts []string
		if ov := w.Overlay(); ov != nil {
			widget.Walk(ov, func(c widget.Component) {
				if l, ok := c.(*widgets.Label); ok {
					texts = append(texts, l.Text)
				}
			})
		}
		return strings.Join(texts, "\n")
	}

	// The default's own name is the same vault: nothing to leave behind.
	pick("personal")
	if q := asks(); q != "" {
		t.Fatalf("naming the same vault asks:\n%s", q)
	}
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	if got := vaultOf(mailcore.FormatOpenPGP); got != "personal" {
		t.Fatalf("the keys are in %q", got)
	}

	pick("work")
	q := asks()
	if !strings.Contains(q, "Your OpenPGP key stays in secretvault’s vault “personal”") ||
		!strings.Contains(q, "cannot move keys from one of its vaults to another") || !strings.Contains(q, "Use “work” anyway?") {
		t.Fatalf("leaving the key behind asks:\n%s", q)
	}
	answerOverlay(t, a, w, "No")
	if got := vaultOf(mailcore.FormatOpenPGP); got != "personal" || w.Overlay() != nil {
		t.Fatalf("after No the keys are in %q", got)
	}
	pick("work")
	answerOverlay(t, a, w, "Yes")
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	if got := vaultOf(mailcore.FormatOpenPGP); got != "work" {
		t.Fatalf("after Yes the keys are in %q", got)
	}

	// S/MIME has no certificate here: nothing is asked.
	widget.Walk(section, func(c widget.Component) {
		if sg, ok := c.(*widgets.Segmented); ok {
			sg.Selected = 1
			sg.OnChange(1)
		}
	})
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	pick("work")
	if q := asks(); q != "" {
		t.Fatalf("S/MIME with nothing to leave asks:\n%s", q)
	}
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	if got := vaultOf(mailcore.FormatSMIME); got != "work" {
		t.Fatalf("the S/MIME keys are in %q", got)
	}
}
