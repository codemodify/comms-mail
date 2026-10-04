package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Accounts: Add, Edit, Remove and Import, each with its icon.
// Edit — or a double click — opens the account's settings with the
// password left empty, and saving a change keeps the saved password and
// what the form does not show.
func TestAccountsTabEditsAnAccount(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	cli, _ := vaultDaemon(t)
	if err := cli.UseStore(mailcore.StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	a, _, _, tv := openSettingsOn(t, cli)
	page := selectTab(a, tv, "Accounts")
	var buttons []*widgets.Button
	var table *widgets.TableView
	widget.Walk(page, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Button:
			buttons = append(buttons, v)
		case *widgets.TableView:
			table = v
		}
	})
	want := []struct {
		text string
		icon style.ToolIcon
	}{{"Add", style.IconPlus}, {"Edit", style.IconPen}, {"Remove", style.IconTrash}, {"Import", style.IconDownload}}
	if len(buttons) != len(want) {
		t.Fatalf("%d buttons", len(buttons))
	}
	for i, w := range want {
		if buttons[i].Text != w.text || buttons[i].Icon != w.icon {
			t.Errorf("button %d is %q with icon %v, want %q with %v", i, buttons[i].Text, buttons[i].Icon, w.text, w.icon)
		}
	}
	if table == nil || table.OnActivate == nil {
		t.Fatal("a double click does not edit")
	}

	ew := newWindowFrom(t, a, buttons[1].OnClick)
	if ew.Title() != "Edit account" {
		t.Fatalf("Edit opened %q", ew.Title())
	}
	fields := map[string]*widgets.TextField{}
	var save *widgets.Button
	widget.Walk(ew.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			fields[v.Placeholder] = v
		case *widgets.Button:
			if v.Text == "Save account" {
				save = v
			}
		}
	})
	name, addr, user := fields["Display name"], fields["you@example.com"], fields["Username (defaults to address)"]
	pass := fields["Leave empty to keep the saved password"]
	host := fields["imap.host:993 or pop.host:995"]
	if name == nil || addr == nil || user == nil || pass == nil || host == nil || save == nil {
		t.Fatalf("the form: %v", fields)
	}
	if addr.Text != "ada@example.com" || user.Text != "ada" || host.Text != "127.0.0.1:1" || pass.Text != "" {
		t.Fatalf("not the account's settings: %q %q %q, password %d long", addr.Text, user.Text, host.Text, len(pass.Text))
	}
	name.SetText("Ada Lovelace")
	save.OnClick()
	a.PumpOnce()
	cfg, err := cli.AccountConfig("home")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Ada Lovelace" || cfg.IMAP.User != "ada" || cfg.IMAP.TLSMode != "ssl" {
		t.Fatalf("saved %+v", cfg)
	}
	if got := table.CellText(0, 0); got != "Ada Lovelace" {
		t.Fatalf("the list says %q", got)
	}
	if st, _ := cli.SecretsStatus(); !st.Ready {
		t.Fatalf("secrets %+v", st)
	}
}

// What the form does not show stays as it was; the name and address carry
// to the identity that had them; a new host's security follows its port;
// sending with a login of its own keeps it.
func TestEditedAccount(t *testing.T) {
	base := mailcore.AccountConfig{ID: "w", Name: "Ada", Address: "ada@example.com", Protocol: mailcore.ProtoIMAP,
		IMAP: mailcore.ServerConfig{Host: "imap.example.com:993", User: "ada", TLSMode: "ssl"},
		SMTP: mailcore.ServerConfig{Host: "smtp.example.com:587", User: "ada", TLSMode: "starttls"},
		Identities: []mailcore.Identity{
			{ID: "w-default", Name: "Ada", Address: "ada@example.com", Default: true},
			{ID: "w-help", Name: "Help desk", Address: "help@example.com"},
		}}
	same := accountEdits{Name: "Ada L", Address: "ada@example.org", Protocol: mailcore.ProtoIMAP,
		Incoming: base.IMAP.Host, SMTP: base.SMTP.Host, User: "ada"}
	got := editedAccount(base, same)
	if got.IMAP.Pass != "" || got.SMTP.Pass != "" || got.IMAP.TLSMode != "ssl" || got.SMTP.TLSMode != "starttls" {
		t.Fatalf("unchanged servers: %+v / %+v", got.IMAP, got.SMTP)
	}
	if id := got.Identities[0]; id.Name != "Ada L" || id.Address != "ada@example.org" {
		t.Fatalf("the account's identity %+v", id)
	}
	if got.Identities[1] != base.Identities[1] || base.Identities[0].Name != "Ada" {
		t.Fatal("another identity, or the settings edited from, changed")
	}

	moved := same
	moved.Incoming, moved.Password = "mail.example.org:143", "new secret"
	got = editedAccount(base, moved)
	if got.IMAP.TLSMode != "" || got.IMAP.Host != "mail.example.org:143" || got.IMAP.Pass != "new secret" {
		t.Fatalf("a new host: %+v", got.IMAP)
	}
	if got.SMTP.Pass != "new secret" || got.SMTP.TLSMode != "starttls" {
		t.Fatalf("sending with the same login: %+v", got.SMTP)
	}

	relay := base
	relay.SMTP.User = "relay"
	other := same
	other.User, other.Password = "ada2", "new secret"
	got = editedAccount(relay, other)
	if got.SMTP.User != "relay" || got.SMTP.Pass != "" || got.IMAP.User != "ada2" {
		t.Fatalf("a login of its own: %+v", got.SMTP)
	}
}
