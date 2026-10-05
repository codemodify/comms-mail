package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/internal/svtest"
	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Keys: another of secretvault's vaults takes the
// keys along — secretvault moves them, and asks the person itself, so
// comms-mail asks nothing first; when the move does not happen it says
// why, and nothing changes. Leaving Secret Vault for a place of
// comms-mail's leaves the keys in secretvault, which they never leave:
// that is asked first, saying where they stay — No changes nothing, Yes
// changes it — and asks nothing when there are no keys to leave.
func TestKeysGoAlongToAnotherVault(t *testing.T) {
	s, a, sv, _ := securityDaemon(t)
	if _, err := s.cli.PutAccount(mailcore.AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: mailcore.ServerConfig{Host: "127.0.0.1:1", TLSMode: "ssl"}}); err != nil {
		t.Fatal(err)
	}
	sv.OwnKeys("ada@example.com")
	sv.PutIn("", svtest.Item{Kind: "key", Name: "pgp/ada@example.com"})
	sv.AddVault("work")
	section, _ := keysSection(a, s.cli)
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 900, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(section)
	a.PumpOnce()

	pump := func() {
		for i := 0; i < 3; i++ {
			a.PumpOnce()
		}
	}
	placeOf := func(format string) mailcore.KeyPlace {
		t.Helper()
		v, err := s.cli.KeysView(format)
		if err != nil {
			t.Fatal(err)
		}
		return v.Place
	}
	// apply picks the place (title), with secretvault's vault named
	// (Custom) or its default (""), and presses Apply.
	apply := func(title, vault string) {
		t.Helper()
		var group *widgets.RadioGroup
		var name *widgets.TextField
		var btn *widgets.Button
		radios := map[string]*widgets.RadioButton{}
		widget.Walk(section, func(c widget.Component) {
			switch v := c.(type) {
			case *widgets.RadioGroup:
				group = v
			case *widgets.RadioButton:
				radios[placeTitle(v.Text)] = v
			case *widgets.TextField:
				if v.Placeholder == "Vault name" {
					name = v
				}
			case *widgets.Button:
				if v.Text == "Apply" {
					btn = v
				}
			}
		})
		radios[title].SetSelected(true)
		if title == "Secret Vault" {
			if vault == "" {
				group.Select(0)
			} else {
				group.Select(1)
				name.SetText(vault)
				name.OnInput(vault)
			}
		}
		a.PumpOnce()
		if !btn.Enabled() {
			t.Fatalf("Apply is off for %s %q", title, vault)
		}
		btn.OnClick()
		pump()
	}
	says := func() string {
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
	in := func(vault string) bool { _, ok := sv.ItemIn(vault, "pgp/ada@example.com"); return ok }

	// The person says no in secretvault: why, and nothing changes.
	sv.DenyMove(true)
	apply("Secret Vault", "work")
	if q := says(); !strings.Contains(q, "You said no in secretvault") {
		t.Fatalf("a refused move says:\n%s", q)
	}
	answerOverlay(t, a, w, "OK")
	pump()
	if p := placeOf(mailcore.FormatOpenPGP); p.Vault != "" || !in("personal") {
		t.Fatalf("after no: %+v", p)
	}
	sv.DenyMove(false)

	apply("Secret Vault", "work")
	if q := says(); q != "" {
		t.Fatalf("moving to another vault asks first:\n%s", q)
	}
	if p := placeOf(mailcore.FormatOpenPGP); p.Vault != "work" || !in("work") || in("personal") {
		t.Fatalf("after the move: %+v", p)
	}

	// Out of secretvault: the key stays in "work", asked first.
	apply("Plain file", "")
	q := says()
	if !strings.Contains(q, "Your OpenPGP key stays in secretvault’s vault “work”") ||
		!strings.Contains(q, "never leave it") || !strings.Contains(q, "Choosing Secret Vault and “work” again finds it") {
		t.Fatalf("leaving the key behind asks:\n%s", q)
	}
	answerOverlay(t, a, w, "No")
	if p := placeOf(mailcore.FormatOpenPGP); p.Store != mailcore.StoreSecretVault || p.Vault != "work" {
		t.Fatalf("after No: %+v", p)
	}
	apply("Plain file", "")
	answerOverlay(t, a, w, "Yes")
	pump()
	if p := placeOf(mailcore.FormatOpenPGP); p.Store != mailcore.StorePlain || !in("work") {
		t.Fatalf("after Yes: %+v", p)
	}

	// S/MIME has no certificate in secretvault: nothing is asked.
	widget.Walk(section, func(c widget.Component) {
		if sg, ok := c.(*widgets.Segmented); ok {
			sg.Selected = 1
			sg.OnChange(1)
		}
	})
	pump()
	apply("Plain file", "")
	if q := says(); q != "" {
		t.Fatalf("S/MIME with nothing to leave asks:\n%s", q)
	}
	if p := placeOf(mailcore.FormatSMIME); p.Store != mailcore.StorePlain {
		t.Fatalf("S/MIME: %+v", p)
	}
}
