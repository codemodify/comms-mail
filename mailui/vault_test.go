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

// shownFields counts the fields on screen (a hidden one's parent is).
func shownFields(fields []*widgets.TextField) int {
	n := 0
	for _, f := range fields {
		if on, _ := shownOnScreen(f); on {
			n++
		}
	}
	return n
}

// chooserParts are the store chooser's radios by title, its fields and
// its buttons.
func chooserParts(w *app.Window) (radios map[string]*widgets.RadioButton, fields []*widgets.TextField, buttons map[string]*widgets.Button) {
	radios = map[string]*widgets.RadioButton{}
	fields, buttons = passFields(w)
	widget.Walk(w.Content(), func(c widget.Component) {
		if rb, ok := c.(*widgets.RadioButton); ok {
			radios[strings.TrimSuffix(rb.Text, " (in use now)")] = rb
		}
	})
	return
}

// The window starting on passwords in plain text asks where to keep them,
// with nothing picked; the encrypted file takes a passphrase, and then
// mail.json no longer holds the password.
func TestWindowAsksWhereToKeepPlainPasswords(t *testing.T) {
	cli, _ := vaultDaemon(t)
	if st, _ := cli.SecretsStatus(); !st.Supported || st.Store != "" || !st.PlainSecrets {
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
	radios, fields, buttons := chooserParts(w)
	keep := buttons["OK"]
	if !strings.Contains(labelTexts(w), laterNote) {
		t.Fatal("the window does not say this can be changed later in Settings")
	}
	if len(radios) != 4 || keep == nil || buttons["Not now"] == nil {
		t.Fatalf("the window: radios %v, buttons %v", radios, buttons)
	}
	for title, rb := range radios {
		if rb.Selected {
			t.Fatalf("%s is picked beforehand", title)
		}
	}
	if keep.Enabled() {
		t.Fatal("Keep is on with nothing picked")
	}
	if radios["secretvault"].Enabled() {
		t.Fatal("secretvault can be picked, and is not there yet")
	}
	if shownFields(fields) != 0 {
		t.Fatal("passphrase fields show before the encrypted file is picked")
	}

	radios["Encrypted file"].SetSelected(true)
	a.PumpOnce()
	_, fields, _ = chooserParts(w)
	if !keep.Enabled() || shownFields(fields) != 2 {
		t.Fatal("picking the encrypted file did not ask for a passphrase")
	}
	// Too short, then not the same: refused, the window stays.
	fields[0].SetText("short")
	fields[1].SetText("short")
	keep.OnClick()
	answerOverlay(t, a, w, "OK")
	fields[0].SetText("correct horse battery")
	fields[1].SetText("correct horse batterie")
	keep.OnClick()
	answerOverlay(t, a, w, "OK")
	if st, _ := cli.SecretsStatus(); st.Store != "" {
		t.Fatal("the secrets moved on a bad passphrase")
	}
	// Picking another hides the fields again.
	radios["Plain file"].SetSelected(true)
	if _, now, _ := chooserParts(w); shownFields(now) != 0 || radios["Encrypted file"].Selected {
		t.Fatal("the choice did not move")
	}
	radios["Encrypted file"].SetSelected(true)
	fields[1].SetText("correct horse battery")
	keep.OnClick()
	a.PumpOnce()
	if !w.Closed() {
		t.Fatal("the window stayed after the secrets moved")
	}
	if st, _ := cli.SecretsStatus(); st.Store != mailcore.StoreEncrypted || !st.Ready || st.PlainSecrets {
		t.Fatalf("after: %+v", st)
	}
	raw, _ := os.ReadFile(mailcore.ConfigPath())
	if strings.Contains(string(raw), "imap-secret") {
		t.Fatalf("mail.json still holds the password:\n%s", raw)
	}
	// A second start asks nothing: the store is unlocked.
	s.vaultAsked = false
	before := len(a.Windows())
	s.checkVault()
	a.PumpOnce()
	if len(a.Windows()) != before {
		t.Fatal("an unlocked store was asked about again")
	}
}

// Choosing the plain file keeps the password where it was.
func TestChoosingThePlainFileKeepsMailJSON(t *testing.T) {
	cli, _ := vaultDaemon(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	st, _ := cli.SecretsStatus()
	w := newWindowFrom(t, a, func() { openStoreChooser(a, cli, accountIntro, st, nil) })
	radios, _, buttons := chooserParts(w)
	radios["Plain file"].SetSelected(true)
	buttons["OK"].OnClick()
	a.PumpOnce()
	if st, _ := cli.SecretsStatus(); st.Store != mailcore.StorePlain {
		t.Fatalf("store %+v", st)
	}
	if raw, _ := os.ReadFile(mailcore.ConfigPath()); !strings.Contains(string(raw), "imap-secret") {
		t.Fatal("the plain file lost the password")
	}
}

// A locked encrypted file: the unlock window refuses a wrong passphrase
// and takes the right one; the change window replaces it.
func TestUnlockAndChangePassphrase(t *testing.T) {
	cli, dir := vaultDaemon(t)
	if err := cli.UseStore(mailcore.StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	mailcore.OpenVault(filepath.Join(dir, "secrets", "vault.json")).Lock() // the next run
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})

	var unlocked bool
	w := newWindowFrom(t, a, func() { openPassphrase(a, cli, passUnlock, func() { unlocked = true }) })
	fields, buttons := passFields(w)
	if len(fields) != 1 || buttons["Unlock"] == nil || buttons["Forgot it…"] == nil {
		t.Fatalf("unlock window: %d fields, %v", len(fields), buttons)
	}
	fields[0].SetText("not the passphrase")
	buttons["Unlock"].OnClick()
	answerOverlay(t, a, w, "OK")
	if st, _ := cli.SecretsStatus(); st.Ready || unlocked {
		t.Fatal("a wrong passphrase unlocked it")
	}
	fields[0].SetText("correct horse battery")
	buttons["Unlock"].OnClick()
	a.PumpOnce()
	if st, _ := cli.SecretsStatus(); !st.Ready || !unlocked || !w.Closed() {
		t.Fatalf("after unlock: %+v, done %v, closed %v", st, unlocked, w.Closed())
	}

	w = newWindowFrom(t, a, func() { openPassphrase(a, cli, passChange, nil) })
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
	mailcore.OpenVault(filepath.Join(dir, "secrets", "vault.json")).Lock()
	if err := cli.UnlockSecrets("a brand new passphrase"); err != nil {
		t.Fatalf("the new passphrase: %v", err)
	}
}

// The chooser's text says what is actually saved: named accounts, only
// old sign-ins, or nothing yet — and always opens with the motto.
func TestChooserTextFitsWhatIsSaved(t *testing.T) {
	withAccounts := plainIntro(mailcore.SecretsStatus{PlainAccounts: []string{"ada@example.com", "bob@example.org"}, PlainTokens: true})
	tokensOnly := plainIntro(mailcore.SecretsStatus{PlainSecrets: true, PlainTokens: true})
	nothing := plainIntro(mailcore.SecretsStatus{})
	for name, text := range map[string]string{"accounts": withAccounts, "tokens": tokensOnly, "nothing": nothing} {
		if !strings.HasPrefix(text, "Secrets. Secrets. Secrets. Keep'em safe.\n\n") {
			t.Errorf("%s: does not open with the motto:\n%s", name, text)
		}
	}
	if !strings.Contains(withAccounts, "ada@example.com, bob@example.org") || !strings.Contains(withAccounts, "sign-ins") {
		t.Errorf("accounts:\n%s", withAccounts)
	}
	if strings.Contains(tokensOnly, "passwords for") || !strings.Contains(tokensOnly, "sign-ins") {
		t.Errorf("tokens only:\n%s", tokensOnly)
	}
	if !strings.Contains(nothing, "No passwords are saved yet.") || strings.Contains(nothing, "mail.json") || strings.Contains(nothing, "passwords for") {
		t.Errorf("nothing saved:\n%s", nothing)
	}
}

// With no accounts, the window starting asks nothing, and Settings says
// no passwords are saved yet — and offers the choice with that text.
func TestNoAccountsNoPasswordsToProtect(t *testing.T) {
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
	defer stop()
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	status, _ := cli.SecretsStatus()
	if status.Store != "" || status.PlainSecrets || len(status.PlainAccounts) != 0 {
		t.Fatalf("status %+v", status)
	}

	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	main, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1100, Height: 720, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(a, main, cli, AppOptions{})
	main.SetContent(s.build())
	before := len(a.Windows())
	s.checkVault()
	a.PumpOnce()
	if len(a.Windows()) != before {
		t.Fatal("the window asked where to keep passwords when none are saved")
	}

	section := passphraseSection(a, cli)
	var texts []string
	widget.Walk(section, func(c widget.Component) {
		if l, ok := c.(*widgets.Label); ok {
			texts = append(texts, l.Text)
		}
	})
	if joined := strings.Join(texts, "\n"); !strings.Contains(joined, "No passwords are saved yet.") {
		t.Fatalf("Settings says:\n%s", joined)
	}
}

// labelTexts are the texts of w's labels, one a line.
func labelTexts(w *app.Window) string {
	var texts []string
	widget.Walk(w.Content(), func(c widget.Component) {
		if l, ok := c.(*widgets.Label); ok {
			texts = append(texts, l.Text)
		}
	})
	return strings.Join(texts, "\n")
}
