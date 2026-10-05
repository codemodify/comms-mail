package mailui

import (
	"bytes"
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
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

const (
	uiSignedPGP = `From: Alice <alice@example.org>
To: Bob <bob@example.com>
Subject: Lunch
Date: Mon, 28 Sep 2026 10:00:00 +0000
Message-ID: <signed@example.org>
MIME-Version: 1.0
Content-Type: multipart/signed; micalg=pgp-sha256; protocol="application/pgp-signature"; boundary="b1"

--b1
Content-Type: text/plain; charset=utf-8

Shall we meet at noon?
--b1
Content-Type: application/pgp-signature; name="signature.asc"

-----BEGIN PGP SIGNATURE-----
iQ==
-----END PGP SIGNATURE-----
--b1--
`
	uiEncryptedPGP = `From: Alice <alice@example.org>
To: Bob <bob@example.com>
Subject: ...
Date: Mon, 28 Sep 2026 11:00:00 +0000
Message-ID: <encrypted@example.org>
MIME-Version: 1.0
Content-Type: multipart/encrypted; protocol="application/pgp-encrypted"; boundary="b2"

--b2
Content-Type: application/pgp-encrypted

Version: 1
--b2
Content-Type: application/octet-stream; name="encrypted.asc"

-----BEGIN PGP MESSAGE-----
hQEMA9xX0Lq2eW3YAQfzyzzyx
-----END PGP MESSAGE-----
--b2--
`
)

// securityDaemon is comms-maild with its secrets in a stand-in
// secretvault, holding a signed and an encrypted message, and the window
// showing their folder.
func securityDaemon(t *testing.T) (*session, *app.Application, *svtest.Vault, map[string]mailcore.MessageID) {
	t.Helper()
	sv := svtest.Start(t, false)
	sv.InspectWith(func(raw []byte, decrypt bool) any {
		if bytes.Contains(raw, []byte("multipart/signed")) {
			return map[string]any{
				"report": map[string]any{"signed": "full", "encrypted": "none",
					"layers": []any{map[string]any{"id": 1, "kind": "signature", "format": "pgp-mime", "covers": "message",
						"signatures": []any{map[string]any{"status": "valid", "fingerprint": "AAAA1111"}}}}},
				"verdicts": []any{map[string]any{"layer": 1, "index": 0, "verdict": map[string]any{
					"contact": "Alice", "level": "in-person", "status": "verified"}}},
			}
		}
		report := map[string]any{"signed": "none", "encrypted": "full",
			"layers": []any{map[string]any{"id": 1, "kind": "encryption", "format": "pgp-mime", "covers": "message",
				"encryption": map[string]any{"decrypted": decrypt}}}}
		if decrypt {
			report["content"] = map[string]any{"layer": 1, "raw": []byte("Content-Type: text/plain; charset=utf-8\r\n\r\nThe plan is in the blue folder.\r\n")}
			report["protected_headers"] = map[string]any{"headers": map[string]any{"subject": "The real subject"}}
		}
		return map[string]any{"report": report}
	})
	dir := t.TempDir()
	t.Setenv(mailcore.EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := mailcore.NewLocalStoreDir(mailcore.MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	mails := t.TempDir()
	for i, raw := range []string{uiSignedPGP, uiEncryptedPGP} {
		crlf := strings.ReplaceAll(raw, "\n", "\r\n")
		if err := os.WriteFile(filepath.Join(mails, string(rune('a'+i))+".eml"), []byte(crlf), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ImportLocalMail([]mailcore.LocalMailStore{{Source: "Folder", Name: "Inbox", Path: mails, Kind: mailcore.StoreEML}}); err != nil {
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
	if err := cli.UseStore(mailcore.StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	main, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(a, main, cli, AppOptions{})
	main.SetContent(s.build())
	folders, err := cli.ListFolders(mailcore.LocalAccountID)
	if err != nil || len(folders) == 0 {
		t.Fatalf("folders %v %v", folders, err)
	}
	s.selectFolder(folders[0].ID)
	s.waitIdle()
	ids := map[string]mailcore.MessageID{}
	for _, m := range s.rows {
		ids[m.Subject] = m.ID
	}
	if len(ids) != 2 {
		t.Fatalf("rows %v", ids)
	}
	return s, a, sv, ids
}

// open shows message id in the reading pane and waits for secretvault's
// say on it.
func open(s *session, a *app.Application, id mailcore.MessageID) {
	s.selected = []mailcore.MessageID{id}
	s.loadPreview()
	for i := 0; i < 3; i++ {
		s.waitIdle()
		a.PumpOnce()
	}
}

// securityLines are the reading pane's security lines: their icons and
// texts.
func securityLines(r *reader) (icons []style.ToolIcon, texts []string) {
	return iconLines(r.sec.lines)
}

// iconLines are root's lines of text, each with the mark before it (an
// iconLine: a mark alone, then its text), IconNone for a line with none.
func iconLines(root widget.Component) (icons []style.ToolIcon, texts []string) {
	mark := style.IconNone
	widget.Walk(root, func(c widget.Component) {
		l, ok := c.(*widgets.Label)
		if !ok {
			return
		}
		if l.Text == "" && l.Icon != style.IconNone {
			mark = l.Icon
			return
		}
		icons, texts = append(icons, mark), append(texts, l.Text)
		mark = style.IconNone
	})
	return
}

// A signed message says who signed it and how far that is verified; an
// encrypted one shows what secretvault opened, under its real subject;
// while secretvault is locked, the pane offers to unlock it rather than
// asking by itself.
func TestReadingSignedAndEncryptedMail(t *testing.T) {
	s, a, sv, ids := securityDaemon(t)

	open(s, a, ids["Lunch"])
	icons, texts := securityLines(s.rd)
	if len(texts) != 1 || icons[0] != style.IconCheck || !strings.Contains(texts[0], "Signed by Alice, verified in person") {
		t.Fatalf("signed: %v %q", icons, texts)
	}
	if !strings.Contains(s.rd.text.Text, "Shall we meet at noon?") {
		t.Fatalf("signed body %q", s.rd.text.Text)
	}

	open(s, a, ids["..."])
	icons, texts = securityLines(s.rd)
	if len(texts) != 1 || icons[0] != style.IconLock || !strings.Contains(texts[0], "opened with your key") {
		t.Fatalf("encrypted: %v %q", icons, texts)
	}
	if !strings.Contains(s.rd.text.Text, "blue folder") || s.rd.subj.Text != "The real subject" {
		t.Fatalf("decrypted: subject %q, text %q", s.rd.subj.Text, s.rd.text.Text)
	}
	// The window's copy of the message is the one received.
	if s.shown.Body != "" || strings.Contains(s.shown.Subject, "real") {
		t.Fatalf("the message acted on is the decrypted one: %+v", s.shown)
	}

	// Locked: not checked, and secretvault is not asked to unlock until
	// the button is pressed.
	sv.Lock()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if st, _ := s.cli.SecretsStatus(); st.Locked {
			break
		}
	}
	open(s, a, ids["Lunch"])
	_, texts = securityLines(s.rd)
	if len(texts) == 0 || !strings.Contains(texts[0], "secretvault is locked") || !s.rd.sec.unlock.Visible() || sv.Unlocks() != 0 {
		t.Fatalf("locked: %q (unlock shown %v, asked %d)", texts, s.rd.sec.unlock.Visible(), sv.Unlocks())
	}
	s.rd.sec.unlock.OnClick()
	for i := 0; i < 4; i++ {
		s.waitIdle()
		a.PumpOnce()
	}
	icons, texts = securityLines(s.rd)
	if sv.Unlocks() != 1 || len(texts) != 1 || icons[0] != style.IconCheck || s.rd.sec.unlock.Visible() {
		t.Fatalf("after Unlock: %v %q (asked %d)", icons, texts, sv.Unlocks())
	}
}

// writeParts are a Write window's Sign and Encrypt boxes, the note beside
// them and its body.
func writeParts(w *app.Window) (sign, encrypt *widgets.Checkbox, note string, body *widgets.TextArea) {
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Checkbox:
			switch v.Text {
			case "Sign":
				sign = v
			case "Encrypt":
				encrypt = v
			}
		case *widgets.Label:
			if strings.Contains(v.Text, "secretvault") {
				note = v.Text
			}
		case *widgets.TextArea:
			body = v
		}
	})
	return
}

// Replying to an encrypted message quotes what secretvault decrypted,
// under the real subject, and starts encrypted; Sign is on when
// secretvault holds a key for the From address, and cannot be when it
// holds none.
func TestWritingSignedAndEncrypted(t *testing.T) {
	s, a, sv, ids := securityDaemon(t)
	if _, err := s.cli.PutAccount(mailcore.AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: mailcore.ServerConfig{Host: "127.0.0.1:1", TLSMode: "ssl"}}); err != nil {
		t.Fatal(err)
	}
	sv.OwnKeys("ada@example.com")
	open(s, a, ids["..."])
	w := newWindowFrom(t, a, s.reply)
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	sign, encrypt, note, body := writeParts(w)
	if sign == nil || encrypt == nil || body == nil {
		t.Fatal("the Write window has no Sign or Encrypt")
	}
	if !encrypt.Checked || !sign.Checked || !sign.Enabled() {
		t.Fatalf("reply to an encrypted message: sign %v (enabled %v), encrypt %v", sign.Checked, sign.Enabled(), encrypt.Checked)
	}
	if !strings.Contains(body.Text, "> The plan is in the blue folder.") || !strings.Contains(w.Title(), "The real subject") {
		t.Fatalf("quoted %q, title %q", body.Text, w.Title())
	}
	w.Close()

	// No key for the address: Sign is off and says why.
	sv.OwnKeys()
	w = newWindowFrom(t, a, s.write)
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	sign, encrypt, note, _ = writeParts(w)
	if sign.Checked || sign.Enabled() || encrypt.Checked || !strings.Contains(note, "holds no key") {
		t.Fatalf("no key: sign %v (enabled %v), encrypt %v, note %q", sign.Checked, sign.Enabled(), encrypt.Checked, note)
	}
}

// sectionParts are a component's labels and buttons by text.
func sectionParts(c widget.Component) (texts []string, buttons map[string]*widgets.Button) {
	buttons = map[string]*widgets.Button{}
	widget.Walk(c, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Label:
			texts = append(texts, v.Text)
		case *widgets.Button:
			buttons[v.Text] = v
		}
	})
	return
}

// Settings › Security › Keys shows your keys for each address you send
// from, a format at a time, and makes an OpenPGP key with secretvault; the
// .p12 password is asked in a field that holds bytes, and handed over
// once.
func TestYourKeysInSettings(t *testing.T) {
	s, a, sv, _ := securityDaemon(t)
	if _, err := s.cli.PutAccount(mailcore.AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: mailcore.ServerConfig{Host: "127.0.0.1:1", TLSMode: "ssl"}}); err != nil {
		t.Fatal(err)
	}
	section, refresh := keysSection(a, s.cli)
	prefs, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 560, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	prefs.SetContent(section)
	a.PumpOnce()
	texts, buttons := sectionParts(section)
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "ada@example.com") || !strings.Contains(joined, "No OpenPGP key") || strings.Contains(joined, "S/MIME certificate") {
		t.Fatalf("Your keys says:\n%s", joined)
	}
	mk := buttons["Make an OpenPGP key"]
	if mk == nil {
		t.Fatalf("buttons %v", buttons)
	}
	// S/MIME, a page of its own.
	format := func(i int) {
		widget.Walk(section, func(c widget.Component) {
			if sg, ok := c.(*widgets.Segmented); ok {
				sg.Selected = i
				sg.OnChange(i)
			}
		})
		a.PumpOnce()
	}
	format(1)
	texts, buttons = sectionParts(section)
	if joined = strings.Join(texts, "\n"); !strings.Contains(joined, "No S/MIME certificate") || buttons["Import S/MIME…"] == nil || strings.Contains(joined, "OpenPGP key") {
		t.Fatalf("S/MIME says:\n%s", joined)
	}
	format(0)
	mk.OnClick()
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	texts, buttons = sectionParts(section)
	if joined = strings.Join(texts, "\n"); !strings.Contains(joined, "OpenPGP key 0WNK EY01 2345") || buttons["Copy public key"] == nil || len(sv.Generates()) != 1 {
		t.Fatalf("after making one:\n%s\n%v", joined, buttons)
	}

	// The password dialog.
	var got []byte
	cancelled := false
	askP12Password(section, "ada.p12", func(pw []byte) { got = pw }, func() { cancelled = true })
	a.PumpOnce()
	var field *widgets.SecretField
	var ok *widgets.Button
	widget.Walk(prefs.Overlay(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.SecretField:
			field = v
		case *widgets.Button:
			if v.Text == "Import" {
				ok = v
			}
		}
	})
	if field == nil || ok == nil {
		t.Fatal("no secret field or Import in the password dialog")
	}
	field.SetBytes([]byte("open sesame"))
	ok.OnClick()
	if string(got) != "open sesame" || cancelled || prefs.Overlay() != nil {
		t.Fatalf("got %q, cancelled %v", got, cancelled)
	}
	if b := field.Bytes(); len(b) != 0 {
		t.Fatal("the field kept the password")
	}

	// The passwords kept elsewhere: secretvault still keeps the keys.
	if err := s.cli.UseStore(mailcore.StorePlain, ""); err != nil {
		t.Fatal(err)
	}
	refresh()
	a.PumpOnce()
	if texts, _ = sectionParts(section); !strings.Contains(strings.Join(texts, "\n"), "OpenPGP key 0WNK EY01 2345") {
		t.Fatalf("passwords elsewhere:\n%s", strings.Join(texts, "\n"))
	}
	// secretvault gone: the page says so.
	sv.Stop()
	refresh()
	a.PumpOnce()
	texts, buttons = sectionParts(section)
	if joined = strings.Join(texts, "\n"); !strings.Contains(joined, "not running") || buttons["Make an OpenPGP key"] != nil {
		t.Fatalf("without secretvault:\n%s", joined)
	}
}

// A forged sender is said under From: the server's failed check, and the
// name that shows another address. Nothing is said of a sender with
// nothing to say.
func TestSenderWarningsInTheReadingPane(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(mailcore.EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := mailcore.NewLocalStoreDir(mailcore.MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	mails := t.TempDir()
	for name, raw := range map[string]string{
		"a.eml": "Authentication-Results: mail.local; dmarc=fail header.from=paypal.com\nFrom: \"service@paypal.com\" <x@evil.biz>\nTo: ada@example.com\nSubject: Your account\n\nLog in now.\n",
		"b.eml": "Authentication-Results: mail.local; dmarc=pass header.from=example.org\nFrom: Ann <ann@example.org>\nTo: ada@example.com\nSubject: Lunch\n\nNoon?\n",
	} {
		if err := os.WriteFile(filepath.Join(mails, name), []byte(strings.ReplaceAll(raw, "\n", "\r\n")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ImportLocalMail([]mailcore.LocalMailStore{{Source: "Folder", Name: "Inbox", Path: mails, Kind: mailcore.StoreEML}}); err != nil {
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
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	main, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(a, main, cli, AppOptions{})
	main.SetContent(s.build())
	folders, _ := cli.ListFolders(mailcore.LocalAccountID)
	s.selectFolder(folders[0].ID)
	s.waitIdle()
	ids := map[string]mailcore.MessageID{}
	for _, m := range s.rows {
		ids[m.Subject] = m.ID
	}
	lines := func() (icons []style.ToolIcon, texts []string) { return iconLines(s.rd.sender.view) }
	open(s, a, ids["Your account"])
	icons, texts := lines()
	joined := strings.Join(texts, " | ")
	if len(texts) != 2 || icons[0] != style.IconWarning || !strings.Contains(joined, "DMARC") || !strings.Contains(joined, "The name shows service@paypal.com") {
		t.Fatalf("forged: %v %q", icons, texts)
	}
	open(s, a, ids["Lunch"])
	if _, texts := lines(); len(texts) != 0 || s.rd.sender.view.Visible() {
		t.Fatalf("a sender with nothing to say: %q", texts)
	}
}

// Where the keys are is the only choice on the Keys page: the four places,
// Secret Vault in use by default and saying it does the work too. The
// encrypted file asks a passphrase twice; Apply makes it so, marked in use,
// and comms-mail does the work: a key made then is its own, with a backup
// to save and a remove, and other people's keys get a list of their own.
func TestKeysPlaces(t *testing.T) {
	cli, _ := vaultDaemon(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	section, _ := keysSection(a, cli)
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 1400, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(section)
	a.PumpOnce()
	radios := func() map[string]*widgets.RadioButton {
		out := map[string]*widgets.RadioButton{}
		widget.Walk(section, func(c widget.Component) {
			if rb, ok := c.(*widgets.RadioButton); ok {
				out[placeTitle(rb.Text)] = rb
			}
		})
		return out
	}
	fields := func() []*widgets.TextField {
		var out []*widgets.TextField
		widget.Walk(section, func(c widget.Component) {
			if f, ok := c.(*widgets.TextField); ok && f.Password {
				out = append(out, f)
			}
		})
		return out
	}
	r := radios()
	for _, name := range places {
		if r[name] == nil {
			t.Fatalf("no %s: %v", name, r)
		}
	}
	if r["Built into comms-mail"] != nil {
		t.Fatal("who does the work is still a choice of its own")
	}
	texts, buttons := sectionParts(section)
	if !r["Secret Vault"].Selected || !slices.Contains(inUseShown(section), "Secret Vault") ||
		!strings.Contains(strings.Join(texts, "\n"), "(keeps the keys and does the work)") || buttons["Apply"].Enabled() {
		t.Fatalf("by default: %v, in use %v, Apply %v", r, inUseShown(section), buttons["Apply"].Enabled())
	}
	r["Encrypted file"].SetSelected(true)
	a.PumpOnce()
	f := fields()
	if len(f) != 2 {
		t.Fatalf("%d passphrase fields", len(f))
	}
	f[0].SetText("correct horse battery")
	f[1].SetText("correct horse battery")
	if !buttons["Apply"].Enabled() {
		t.Fatal("no Apply")
	}
	buttons["Apply"].OnClick()
	a.PumpOnce()
	v, _ := cli.KeysView(mailcore.FormatOpenPGP)
	if v.Place.Engine != mailcore.EngineOwn || v.Place.Store != mailcore.StoreEncrypted || !v.Place.Ready {
		t.Fatalf("after Apply: %+v", v.Place)
	}
	if !slices.Equal(inUseShown(section), []string{"Encrypted file"}) {
		t.Fatalf("in use: %v", inUseShown(section))
	}
	texts, buttons = sectionParts(section)
	if buttons["Make an OpenPGP key"] == nil || !strings.Contains(strings.Join(texts, "\n"), "Other people's keys") {
		t.Fatalf("comms-mail's own page:\n%s\n%v", strings.Join(texts, "\n"), buttons)
	}
	buttons["Make an OpenPGP key"].OnClick()
	a.PumpOnce()
	texts, buttons = sectionParts(section)
	if !strings.Contains(strings.Join(texts, "\n"), "OpenPGP key ") || buttons["Save a backup…"] == nil || buttons["Remove…"] == nil || buttons["Import a key…"] == nil {
		t.Fatalf("after making a key:\n%s\n%v", strings.Join(texts, "\n"), buttons)
	}
	// Back to Secret Vault offers Apply.
	radios()["Secret Vault"].SetSelected(true)
	if _, buttons = sectionParts(section); !buttons["Apply"].Enabled() {
		t.Fatal("Secret Vault again offers no Apply")
	}
}
