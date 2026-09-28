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

func importChecks(t *testing.T, a *app.Application) []*widgets.Checkbox {
	t.Helper()
	var out []*widgets.Checkbox
	for _, w := range a.Windows() {
		if w.Title() != "Import" {
			continue
		}
		widget.Walk(w.Content(), func(c widget.Component) {
			if cb, ok := c.(*widgets.Checkbox); ok {
				out = append(out, cb)
			}
		})
		w.Close()
	}
	return out
}

// The import window offers a checkbox per account and one for local mail,
// each shown only when there is something of that kind.
func TestImportWindowOffersAccountsAndMail(t *testing.T) {
	cli := demoClient(t)
	accs := []mailcore.ImportedAccount{
		{Source: "Thunderbird", Account: mailcore.AccountConfig{Address: "a@x.com", Protocol: "imap", IMAP: mailcore.ServerConfig{Host: "imap.x.com:993"}}},
		{Source: "KMail", Account: mailcore.AccountConfig{Address: "b@y.org", Protocol: "pop3", POP: mailcore.ServerConfig{Host: "pop.y.org:995"}}},
	}
	mail := []mailcore.LocalMailStore{
		{Source: "Thunderbird", Name: "Inbox", Kind: "mbox"},
		{Source: "KMail", Name: "old", Kind: "maildir"},
	}

	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	openImportWindow(a, cli, accs, mail, nil)
	a.PumpOnce()
	checks := importChecks(t, a)
	if len(checks) != 3 {
		t.Fatalf("both kinds: %d checkboxes, want 3 (2 accounts + local mail)", len(checks))
	}
	var mailBox *widgets.Checkbox
	for _, c := range checks {
		if !c.Checked {
			t.Errorf("%q should start ticked", c.Text)
		}
		if strings.HasPrefix(c.Text, "Import local mail") {
			mailBox = c
		}
	}
	if mailBox == nil || !strings.Contains(mailBox.Text, "2 folder(s)") {
		t.Fatalf("no local-mail checkbox summarising 2 folders: %v", mailBox)
	}

	a2 := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	openImportWindow(a2, cli, accs, nil, nil)
	a2.PumpOnce()
	if n := len(importChecks(t, a2)); n != 2 {
		t.Fatalf("accounts only: %d checkboxes, want 2", n)
	}

	a3 := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	openImportWindow(a3, cli, nil, mail, nil)
	a3.PumpOnce()
	if n := len(importChecks(t, a3)); n != 1 {
		t.Fatalf("mail only: %d checkboxes, want 1", n)
	}
}
