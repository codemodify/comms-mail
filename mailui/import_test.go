package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

func windowChecks(w *app.Window) []*widgets.Checkbox {
	var out []*widgets.Checkbox
	widget.Walk(w.Content(), func(c widget.Component) {
		if cb, ok := c.(*widgets.Checkbox); ok {
			out = append(out, cb)
		}
	})
	return out
}

// Each client gets a Config checkbox (over one per account) and an Emails
// checkbox; ticking Config off unticks its accounts; accounts already set up
// here are not offered.
func TestImportWindowPerClient(t *testing.T) {
	cli := demoClient(t)
	sources := []mailcore.ImportSource{
		{
			Source: "Thunderbird",
			Accounts: []mailcore.ImportedAccount{
				{Source: "Thunderbird", Account: mailcore.AccountConfig{Address: "a@x.com", Protocol: "imap", IMAP: mailcore.ServerConfig{Host: "imap.x.com:993"}}},
				{Source: "Thunderbird", Account: mailcore.AccountConfig{Address: "b@y.org", Protocol: "pop3", POP: mailcore.ServerConfig{Host: "pop.y.org:995"}}},
			},
			Mail: []mailcore.LocalMailStore{{Source: "Thunderbird", Name: "Inbox", Path: "/tmp/Inbox", Kind: mailcore.StoreMbox}},
		},
		{
			Source:   "Evolution",
			Accounts: []mailcore.ImportedAccount{{Source: "Evolution", Account: mailcore.AccountConfig{Address: "already@here.com", Protocol: "imap"}}},
			Mail:     []mailcore.LocalMailStore{{Source: "Evolution", Name: "Inbox", Path: "/tmp/e", Kind: mailcore.StoreMaildir}},
		},
		{Source: "Geary", Accounts: []mailcore.ImportedAccount{{Source: "Geary", Account: mailcore.AccountConfig{Address: "g@gmail.com", Protocol: "imap", Provider: "google"}}}},
	}
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w := openImportWindow(a, cli, sources, map[string]bool{"already@here.com": true}, nil)
	if w == nil {
		t.Fatal("no window")
	}
	defer w.Close()
	a.PumpOnce()

	checks := windowChecks(w)
	// Thunderbird: Config + 2 accounts + Emails; Evolution: Emails only
	// (its account is already here); Geary: Config + 1 account.
	if len(checks) != 7 {
		var names []string
		for _, c := range checks {
			names = append(names, c.Text)
		}
		t.Fatalf("%d checkboxes, want 7: %q", len(checks), names)
	}
	var tbConfig *widgets.Checkbox
	emails, google := 0, false
	for _, c := range checks {
		if !c.Checked {
			t.Errorf("%q should start ticked", c.Text)
		}
		switch {
		case strings.HasPrefix(c.Text, "Config — 2 account"):
			tbConfig = c
		case strings.HasPrefix(c.Text, "Emails — "):
			emails++
		case strings.Contains(c.Text, "already@here.com"):
			t.Error("an account already set up was offered")
		case strings.Contains(c.Text, "sign in with google"):
			google = true
		}
	}
	if emails != 2 {
		t.Errorf("%d Emails checkboxes, want 2 (Thunderbird, Evolution)", emails)
	}
	if !google {
		t.Error("a Gmail account should say it signs in with Google")
	}
	if tbConfig == nil {
		t.Fatal("no Thunderbird Config checkbox")
	}
	tbConfig.SetChecked(false)
	for _, c := range checks {
		if (strings.HasPrefix(c.Text, "a@x.com") || strings.HasPrefix(c.Text, "b@y.org")) && c.Checked {
			t.Errorf("unticking Config left %q ticked", c.Text)
		}
	}
}

// With nothing found, the window still opens, to add a folder by hand.
func TestImportWindowEmptyOffersFolder(t *testing.T) {
	cli := demoClient(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w := openImportWindow(a, cli, nil, nil, nil)
	defer w.Close()
	a.PumpOnce()
	found := false
	widget.Walk(w.Content(), func(c widget.Component) {
		if b, ok := c.(*widgets.Button); ok && strings.HasPrefix(b.Text, "Add a folder") {
			found = true
		}
	})
	if !found {
		t.Fatal("no Add a folder button")
	}
	if len(windowChecks(w)) != 0 {
		t.Fatal("an empty scan should offer no checkboxes")
	}
}
