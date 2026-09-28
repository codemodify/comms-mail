package mailui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// vaultDaemon is comms-maild over a disk store holding one account whose
// password is in mail.json: an install from before the passphrase.
func vaultDaemon(t *testing.T) (*mailcore.Client, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(mailcore.EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := mailcore.NewLocalStoreDir(mailcore.MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	sock, stop, err := mailcore.StartStore(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	if _, err := cli.PutAccount(mailcore.AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: mailcore.ServerConfig{Host: "127.0.0.1:1", User: "ada", Pass: "imap-secret", TLSMode: "ssl"}}); err != nil {
		t.Fatal(err)
	}
	return cli, dir
}

// passFields are a passphrase window's fields, in order, and its buttons.
func passFields(w *app.Window) (fields []*widgets.TextField, buttons map[string]*widgets.Button) {
	buttons = map[string]*widgets.Button{}
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			if v.Password {
				fields = append(fields, v)
			}
		case *widgets.Button:
			buttons[v.Text] = v
		}
	})
	return fields, buttons
}

// The window starting on passwords in plain text offers a passphrase;
// once set, mail.json no longer holds the password.
func TestWindowOffersAPassphraseForPlainPasswords(t *testing.T) {
	cli, _ := vaultDaemon(t)
	if st, _ := cli.VaultStatus(); !st.Supported || st.Exists || !st.PlainSecrets {
		t.Fatalf("before: %+v", st)
	}
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	main, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1100, Height: 720, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	var s *session
	w := newWindowFrom(t, a, func() {
		s = newSession(a, main, cli, AppOptions{})
		main.SetContent(s.build())
		s.checkVault()
	})
	fields, buttons := passFields(w)
	if len(fields) != 2 || buttons["Protect"] == nil || buttons["Not now"] == nil {
		t.Fatalf("the window: %d fields, buttons %v", len(fields), buttons)
	}
	// Too short, then not the same: refused, the window stays.
	fields[0].SetText("short")
	fields[1].SetText("short")
	buttons["Protect"].OnClick()
	answerOverlay(t, a, w, "OK")
	fields[0].SetText("correct horse battery")
	fields[1].SetText("correct horse batterie")
	buttons["Protect"].OnClick()
	answerOverlay(t, a, w, "OK")
	if st, _ := cli.VaultStatus(); st.Exists {
		t.Fatal("a vault was made from a bad passphrase")
	}
	fields[1].SetText("correct horse battery")
	buttons["Protect"].OnClick()
	a.PumpOnce()
	if !w.Closed() {
		t.Fatal("the window stayed after the passphrase was set")
	}
	if st, _ := cli.VaultStatus(); !st.Exists || !st.Unlocked || st.PlainSecrets {
		t.Fatalf("after: %+v", st)
	}
	raw, _ := os.ReadFile(mailcore.ConfigPath())
	if strings.Contains(string(raw), "imap-secret") {
		t.Fatalf("mail.json still holds the password:\n%s", raw)
	}
	// A second start asks nothing: the daemon is unlocked.
	s.vaultAsked = false
	before := len(a.Windows())
	s.checkVault()
	a.PumpOnce()
	if len(a.Windows()) != before {
		t.Fatal("an unlocked daemon was asked for its passphrase")
	}
}

// A locked daemon: the unlock window refuses a wrong passphrase and
// takes the right one; Settings changes it.
func TestUnlockAndChangePassphrase(t *testing.T) {
	cli, dir := vaultDaemon(t)
	if err := cli.CreateVault("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	mailcore.OpenVault(filepath.Join(dir, "secrets", "vault.json")).Lock() // the next run
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})

	var unlocked bool
	w := newWindowFrom(t, a, func() { openPassphrase(a, cli, passUnlock, "", func() { unlocked = true }) })
	fields, buttons := passFields(w)
	if len(fields) != 1 || buttons["Unlock"] == nil || buttons["Forgot it…"] == nil {
		t.Fatalf("unlock window: %d fields, %v", len(fields), buttons)
	}
	fields[0].SetText("not the passphrase")
	buttons["Unlock"].OnClick()
	answerOverlay(t, a, w, "OK")
	if st, _ := cli.VaultStatus(); st.Unlocked || unlocked {
		t.Fatal("a wrong passphrase unlocked it")
	}
	fields[0].SetText("correct horse battery")
	buttons["Unlock"].OnClick()
	a.PumpOnce()
	if st, _ := cli.VaultStatus(); !st.Unlocked || !unlocked || !w.Closed() {
		t.Fatalf("after unlock: %+v, done %v, closed %v", st, unlocked, w.Closed())
	}

	w = newWindowFrom(t, a, func() { openPassphrase(a, cli, passChange, "", nil) })
	fields, buttons = passFields(w)
	if len(fields) != 3 {
		t.Fatalf("change window: %d fields", len(fields))
	}
	fields[0].SetText("correct horse battery")
	fields[1].SetText("a brand new passphrase")
	fields[2].SetText("a brand new passphrase")
	buttons["Change"].OnClick()
	a.PumpOnce()
	if !w.Closed() {
		t.Fatal("the change window stayed")
	}
	v := mailcore.OpenVault(filepath.Join(dir, "secrets", "vault.json"))
	v.Lock()
	if err := cli.UnlockVault("a brand new passphrase"); err != nil {
		t.Fatalf("the new passphrase: %v", err)
	}
}
