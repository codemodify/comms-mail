package mailui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/internal/svtest"
	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/layout"
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
			radios[placeTitle(rb.Text)] = rb
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
	if keep == nil || buttons["Not now"] == nil {
		t.Fatalf("the window: buttons %v", buttons)
	}
	for _, title := range places {
		if rb := radios[title]; rb == nil || rb.Selected {
			t.Fatalf("%s is not there, or picked beforehand: %v", title, radios)
		}
	}
	if keep.Enabled() {
		t.Fatal("Keep is on with nothing picked")
	}
	if radios["Secret Vault"].Enabled() {
		t.Fatal("secretvault can be picked, and is not running here")
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

// With secretvault running, the window offers it, asks comms-mail for no
// passphrase, and moves the passwords there; Settings says what that
// means. When the vault locks, Settings offers to unlock it — secretvault
// asks, not comms-mail — and a window starting while it is locked waits
// rather than asking.
func TestChoosingSecretVaultInTheWindow(t *testing.T) {
	sv := svtest.Start(t, false)
	cli, _ := vaultDaemon(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	st, _ := cli.SecretsStatus()
	if !st.SecretVaultAvailable {
		t.Fatalf("secretvault runs, and is not offered: %+v", st)
	}
	w := newWindowFrom(t, a, func() { openStoreChooser(a, cli, accountIntro, st, nil) })
	radios, _, buttons := chooserParts(w)
	if !radios["Secret Vault"].Enabled() {
		t.Fatal("secretvault cannot be picked")
	}
	radios["Secret Vault"].SetSelected(true)
	a.PumpOnce()
	if _, fields, _ := chooserParts(w); shownFields(fields) != 0 {
		t.Fatal("picking secretvault asks comms-mail for a passphrase")
	}
	buttons["OK"].OnClick()
	a.PumpOnce()
	if !w.Closed() {
		t.Fatal("the window stayed after the secrets moved")
	}
	if st, _ := cli.SecretsStatus(); st.Store != mailcore.StoreSecretVault || !st.Ready || st.PlainSecrets {
		t.Fatalf("after: %+v", st)
	}
	if _, ok := sv.Item("comms-mail/pass/home/imap"); !ok {
		t.Fatal("the password is not in secretvault")
	}

	unlockButton := func() (*widgets.Button, string) {
		section, _ := passwordsSection(a, nil, cli)
		var btn *widgets.Button
		var texts []string
		widget.Walk(section, func(c widget.Component) {
			switch v := c.(type) {
			case *widgets.Button:
				if v.Text == "Unlock secretvault…" {
					btn = v
				}
			case *widgets.Label:
				texts = append(texts, v.Text)
			case *widgets.RadioButton:
				texts = append(texts, v.Text)
			}
		})
		texts = append(texts, "in use: "+strings.Join(inUseShown(section), ","))
		return btn, strings.Join(texts, "\n")
	}
	btn, text := unlockButton()
	// (A hidden button is not walked.)
	if !strings.Contains(text, "in use: Secret Vault") || strings.Contains(text, "Locked now") || btn != nil && btn.Visible() {
		t.Fatalf("Settings, unlocked (unlock shown %v):\n%s", btn != nil && btn.Visible(), text)
	}

	locked := func() bool {
		st, _ := cli.SecretsStatus()
		return st.Locked
	}
	sv.Lock()
	for end := time.Now().Add(2 * time.Second); !locked() && time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
	}
	if !locked() {
		t.Fatal("the lock was not noticed")
	}

	// A window starting now waits: no prompt, and secretvault is not asked.
	main, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1100, Height: 720, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(a, main, cli, AppOptions{})
	main.SetContent(s.build())
	before := len(a.Windows())
	s.checkVault()
	a.PumpOnce()
	if len(a.Windows()) != before || sv.Unlocks() != 0 {
		t.Fatal("a window starting with secretvault locked asked to unlock it")
	}

	// Settings asks secretvault to unlock, which shows its own prompt.
	btn, text = unlockButton()
	if btn == nil || !btn.Visible() || !strings.Contains(text, "Locked now") {
		t.Fatalf("Settings, locked:\n%s", text)
	}
	btn.OnClick()
	for end := time.Now().Add(2 * time.Second); sv.Unlocks() == 0 && time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		a.PumpOnce()
	}
	if sv.Unlocks() != 1 {
		t.Fatalf("Unlock asked secretvault %d times", sv.Unlocks())
	}
	for end := time.Now().Add(2 * time.Second); locked() && time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
	}
	if st, _ := cli.SecretsStatus(); !st.Ready {
		t.Fatalf("after Unlock: %+v", st)
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

// With no accounts, the window starting asks nothing, and Settings offers
// the places with none of them in use.
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

	// Settings offers the places, none of them in use.
	section, _ := passwordsSection(a, nil, cli)
	var radios []string
	widget.Walk(section, func(c widget.Component) {
		if rb, ok := c.(*widgets.RadioButton); ok && slices.Contains(places, placeTitle(rb.Text)) {
			radios = append(radios, placeTitle(rb.Text))
			if rb.Selected {
				t.Errorf("%s is picked", rb.Text)
			}
		}
	})
	if strings.Join(radios, "|") != "System Keyring|Secret Vault|Encrypted file|Plain file" {
		t.Fatalf("Settings offers %q", radios)
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

// Settings › Security › Passwords is the choice itself, where the window
// that asked was: each place with what it means, the plain and encrypted
// files' full paths first, the place in use picked, and Apply, which moves
// everything to another — the encrypted file's passphrase asked twice. The
// page then shows the new place in use, with Change passphrase… under it.
func TestPasswordsPageIsTheChoice(t *testing.T) {
	cli, dir := vaultDaemon(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 900, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	page, _ := passwordsSection(a, w, cli)
	w.SetContent(page)
	a.PumpOnce()
	type parts struct {
		radios    map[string]*widgets.RadioButton
		buttons   map[string]*widgets.Button
		fields    []*widgets.TextField // the passphrase's
		vaultName *widgets.TextField
		texts     []string
	}
	look := func() parts {
		p := parts{radios: map[string]*widgets.RadioButton{}, buttons: map[string]*widgets.Button{}}
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *widgets.RadioButton:
				p.radios[placeTitle(v.Text)] = v
			case *widgets.Button:
				p.buttons[v.Text] = v
			case *widgets.TextField:
				if v.Placeholder == "Vault name" {
					p.vaultName = v
				} else {
					p.fields = append(p.fields, v)
				}
			case *widgets.Label:
				p.texts = append(p.texts, v.Text)
			}
		})
		return p
	}
	vaultPath := filepath.Join(dir, "secrets", "vault.json")
	configPath, _ := filepath.Abs(mailcore.ConfigPath())
	p := look()
	// Each place: its name with what it is in brackets, and no more — a
	// file's path as text that can be selected but not edited, why one
	// cannot be used here (no keyring, no secretvault in tests), and
	// secretvault's vault; no text over them.
	want := []string{
		"radio System Keyring", "text (Secret Service)", "text Not available: ",
		"radio Secret Vault", "text Not available: ", "radio Default", "radio Custom",
		"radio Encrypted file", "text (Argon2id hashing + AES-256-GCM encryption)", "path " + vaultPath,
		"radio Plain file", "text (open to anyone)", "path " + configPath,
	}
	got := placesSay(page)
	if len(got) != len(want) {
		t.Fatalf("the places say:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i], w) {
			t.Fatalf("the places say:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	for _, title := range places {
		if p.radios[title] == nil {
			t.Fatalf("no %s: %v", title, p.radios)
		}
	}
	first := ""
	widget.Walk(page, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Label:
			if first == "" {
				first = "text " + v.Text
			}
		case *widgets.RadioButton:
			if first == "" {
				first = "radio"
			}
		}
	})
	if first != "radio" {
		t.Fatalf("the page starts with %q", first)
	}
	if p.buttons["Change where…"] != nil || p.buttons["Apply"] == nil || p.buttons["Apply"].Enabled() {
		t.Fatalf("buttons %v", p.buttons)
	}
	if len(p.fields) != 0 {
		t.Fatal("passphrase fields before the encrypted file is picked")
	}
	// The path is selected and copied, never changed.
	var path *widgets.TextArea
	widget.Walk(page, func(c widget.Component) {
		if v, ok := c.(*widgets.TextArea); ok && v.Text == vaultPath {
			path = v
		}
	})
	path.KeyPress(widget.KeyEvent{Key: platform.KeyA, Mods: platform.ModCtrl})
	path.TextInput('x')
	path.KeyPress(widget.KeyEvent{Key: platform.KeyBackspace})
	if path.SelectedText() != vaultPath || path.Text != vaultPath {
		t.Fatalf("the path: %q selected, %q now", path.SelectedText(), path.Text)
	}
	// The window that asks says the same, under its own words.
	st, _ := cli.SecretsStatus()
	ask := newWindowFrom(t, a, func() { openStoreChooser(a, cli, switchIntro, st, nil) })
	if got := strings.Join(placesSay(ask.Content()), "\n"); !strings.Contains(got, strings.Join(want[7:], "\n")) || !strings.Contains(labelTexts(ask), switchIntro) {
		t.Fatalf("the window says:\n%s", got)
	}
	ask.Close()

	p.radios["Encrypted file"].SetSelected(true)
	a.PumpOnce()
	p = look()
	if len(p.fields) != 2 || !p.buttons["Apply"].Enabled() {
		t.Fatalf("Encrypted file picked: %d fields, Apply %v", len(p.fields), p.buttons["Apply"].Enabled())
	}
	p.fields[0].SetText("correct horse battery")
	p.fields[1].SetText("correct horse battery")
	p.buttons["Apply"].OnClick()
	a.PumpOnce()
	if st, _ := cli.SecretsStatus(); st.Store != mailcore.StoreEncrypted || !st.Ready || st.PlainSecrets {
		t.Fatalf("after Apply: %+v", st)
	}
	p = look()
	inUse := p.radios["Encrypted file"]
	if inUse == nil || !inUse.Selected || !slices.Equal(inUseShown(page), []string{"Encrypted file"}) || len(p.fields) != 0 || p.buttons["Change passphrase…"] == nil || p.buttons["Apply"].Enabled() {
		t.Fatalf("after the move: radios %v, %d fields, buttons %v", p.radios, len(p.fields), p.buttons)
	}
	// Picking another place offers Apply; the one in use again takes it back.
	p.radios["Plain file"].SetSelected(true)
	if !p.buttons["Apply"].Enabled() || inUse.Selected {
		t.Fatal("picking the plain file")
	}
	inUse.SetSelected(true)
	if p.buttons["Apply"].Enabled() {
		t.Fatal("Apply is on with the place in use picked")
	}
}

// placesSay is what the choice of places says, in order from the first
// place: "radio <name>", "path <file>" for a read-only text, "text <label>".
func placesSay(root widget.Component) []string {
	var out []string
	widget.Walk(root, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.RadioButton:
			out = append(out, "radio "+v.Text)
		case *widgets.TextArea:
			if v.ReadOnly {
				out = append(out, "path "+v.Text)
			}
		case *widgets.Label:
			// (A mark alone is an iconLine's; its text follows.)
			if strings.TrimSpace(v.Text) != "" {
				out = append(out, "text "+v.Text)
			}
		}
	})
	// From the first place on: what comes before it is the window's own.
	for i, s := range out {
		if strings.HasPrefix(s, "radio ") {
			return out[i:]
		}
	}
	return nil
}

// A file's path shows whole: its box is as tall as the path wrapped to its
// width, nothing scrolled away, and a parent measuring it again after the
// layout (probing how narrow it can go) leaves it as it was.
func TestPathViewShowsAllOfIt(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Path", Width: 260, Height: 300, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	path := "/home/someone/.data/comms-mail/secrets/vault.json"
	pv := pathView(path)
	w.SetContent(widgets.NewColumn(pv))
	a.PumpOnce()
	var view *widgets.TextArea
	widget.Walk(pv, func(c widget.Component) {
		if v, ok := c.(*widgets.TextArea); ok {
			view = v
		}
	})
	if view == nil || !view.ReadOnly || view.Text != path {
		t.Fatal("not a read-only view of the path")
	}
	if len(view.Lines()) < 2 || view.MaxOffset() > 0 {
		t.Fatalf("%d lines, %v hidden", len(view.Lines()), view.MaxOffset())
	}
	before := view.Bounds()
	pv.Measure(layout.Constraints{MaxW: 40, MaxH: -1})
	if view.Bounds() != before {
		t.Fatalf("a probe moved the view from %v to %v", before, view.Bounds())
	}
}

// places are the places the passwords can be kept, by name.
var places = []string{"System Keyring", "Secret Vault", "Encrypted file", "Plain file"}

// placeTitle is a radio's name without what follows it: "Encrypted file"
// of "Encrypted file (Argon2id …)".
func placeTitle(text string) string {
	if i := strings.IndexAny(text, "(,"); i > 0 {
		return strings.TrimSpace(text[:i])
	}
	return text
}

// Secret Vault's vault: its default (named) or one typed under Custom.
// Custom with no name offers no Apply; a vault secretvault does not have
// is said, with how to make it, and nothing moves; once it is there, Apply
// moves the passwords into it and the page shows it in use.
func TestPasswordsInANamedVault(t *testing.T) {
	sv := svtest.Start(t, false)
	cli, _ := vaultDaemon(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 900, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	page, _ := passwordsSection(a, w, cli)
	w.SetContent(page)
	a.PumpOnce()
	type parts struct {
		radios map[string]*widgets.RadioButton
		vault  *widgets.RadioGroup // Default, Custom: a click selects through it
		name   *widgets.TextField
		apply  *widgets.Button
	}
	look := func() parts {
		p := parts{radios: map[string]*widgets.RadioButton{}}
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *widgets.RadioButton:
				p.radios[placeTitle(v.Text)] = v
			case *widgets.RadioGroup:
				p.vault = v
			case *widgets.TextField:
				if v.Placeholder == "Vault name" {
					p.name = v
				}
			case *widgets.Button:
				if v.Text == "Apply" {
					p.apply = v
				}
			}
		})
		return p
	}
	p := look()
	if r := p.radios["Default"]; r == nil || r.Text != "Default (personal)" || !r.Selected || p.radios["Custom"].Selected || p.name.Enabled() {
		t.Fatalf("the vault: %v, name on %v", p.radios, p.name.Enabled())
	}
	p.vault.Select(1)
	a.PumpOnce()
	if !p.radios["Secret Vault"].Selected || !p.name.Enabled() || p.apply.Enabled() {
		t.Fatalf("Custom: Secret Vault picked %v, name on %v, Apply on %v", p.radios["Secret Vault"].Selected, p.name.Enabled(), p.apply.Enabled())
	}
	p.name.SetText("work")
	p.name.OnInput("work")
	if !p.apply.Enabled() {
		t.Fatal("Apply is off with a vault named")
	}
	p.apply.OnClick()
	a.PumpOnce()
	said := ""
	if ov := w.Overlay(); ov != nil {
		widget.Walk(ov, func(c widget.Component) {
			if l, ok := c.(*widgets.Label); ok {
				said += l.Text + "\n"
			}
		})
	}
	if !strings.Contains(said, `no vault named "work"`) || !strings.Contains(said, "secretvault vault create --name work") {
		t.Fatalf("a vault secretvault does not have:\n%s", said)
	}
	if st, _ := cli.SecretsStatus(); st.Store != "" {
		t.Fatalf("something moved: %+v", st)
	}
	widget.DismissOverlay(w.Overlay())

	sv.AddVault("work")
	p.apply.OnClick()
	a.PumpOnce()
	if st, _ := cli.SecretsStatus(); st.Store != mailcore.StoreSecretVault || st.SecretVault != "work" {
		t.Fatalf("after Apply: %+v", st)
	}
	if _, ok := sv.ItemIn("work", "comms-mail/pass/home/imap"); !ok {
		t.Fatal("the password is not in work")
	}
	p = look()
	if r := p.radios["Secret Vault"]; !r.Selected || !slices.Equal(inUseShown(page), []string{"Secret Vault"}) ||
		!p.radios["Custom"].Selected || p.name.Text != "work" || p.apply.Enabled() {
		t.Fatalf("the page after: %v, name %q, Apply %v", p.radios, p.name.Text, p.apply.Enabled())
	}
	p.vault.Select(0)
	if !p.apply.Enabled() {
		t.Fatal("back to the default vault offers no Apply")
	}
}

// inUseShown are the places with the green check after their names: the
// place in use.
func inUseShown(root widget.Component) []string {
	var out []string
	last := ""
	widget.Walk(root, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.RadioButton:
			last = placeTitle(v.Text)
		case *widgets.Label:
			if v.Icon == style.IconCheck && v.Text == "" && v.Tone == widgets.ToneSuccess {
				out = append(out, last)
			}
		}
	})
	return out
}
