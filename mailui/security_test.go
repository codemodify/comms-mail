package mailui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/internal/svtest"
	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/a11y"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/layout"
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
						"signatures": []any{map[string]any{"status": "valid", "fingerprint": "AAAA1111",
							"signed_at": "2026-10-05T10:00:00Z", "hash": "SHA256", "algorithm": "EdDSA"}}}}},
				"verdicts": []any{map[string]any{"layer": 1, "index": 0, "verdict": map[string]any{
					"contact": "Alice", "level": "in-person", "status": "verified"}}},
			}
		}
		report := map[string]any{"signed": "none", "encrypted": "full",
			"layers": []any{map[string]any{"id": 1, "kind": "encryption", "format": "pgp-mime", "covers": "message",
				"encryption": map[string]any{"decrypted": decrypt, "decrypted_with": "1234567890ABCDEFAAAA1111BBBB2222",
					"cipher": "AES256 OCB", "integrity": "aead", "recipients": []any{
						map[string]any{"key_id": "AAAA1111BBBB2222", "algorithm": "X25519"},
						map[string]any{"key_id": "CCCC3333DDDD4444", "algorithm": "RSA"}}}}}}
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

// chipsOf are the reading pane's chips under From: their tones and words.
func chipsOf(r *reader) (tones []secTone, texts []string) {
	widget.Walk(r.secView.chips, func(c widget.Component) {
		if ch, ok := c.(*secChip); ok {
			tones, texts = append(tones, ch.Tone), append(texts, ch.Text)
		}
	})
	return
}

// securityTab is the Security tab's words: its section titles and lines.
func securityTab(r *reader) string {
	var out []string
	widget.Walk(r.secView.body, func(c widget.Component) {
		if l, ok := c.(*widgets.Label); ok && l.Text != "" {
			out = append(out, l.Text)
		}
	})
	return strings.Join(out, "\n")
}

// iconLines are root's lines of text, each with its mark (an iconLine's
// own, or a mark alone before the text), IconNone for a line with none.
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
		if l.Icon != style.IconNone {
			mark = l.Icon
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
	// At a glance under From, all of it in the Security tab: the lines,
	// the key, and what checked it.
	tones, chips := chipsOf(s.rd)
	if !slices.Contains(chips, "Signed by Alice") || tones[slices.Index(chips, "Signed by Alice")] != secGood || !slices.Contains(chips, "Not encrypted") {
		t.Fatalf("chips %q %v", chips, tones)
	}
	if tab := securityTab(s.rd); !strings.Contains(tab, "Signature and encryption") || !strings.Contains(tab, "Signed by Alice, verified in person") ||
		!strings.Contains(tab, "Checked by secretvault") || !strings.Contains(tab, "Signed Mon 05 Oct 2026") || !strings.Contains(tab, "with SHA256 and EdDSA") {
		t.Fatalf("security tab:\n%s", tab)
	}
	if !strings.Contains(s.rd.text.Text, "Shall we meet at noon?") {
		t.Fatalf("signed body %q", s.rd.text.Text)
	}

	open(s, a, ids["..."])
	icons, texts = securityLines(s.rd)
	if len(texts) != 1 || icons[0] != style.IconLock || !strings.Contains(texts[0], "opened with your key") {
		t.Fatalf("encrypted: %v %q", icons, texts)
	}
	if _, chips := chipsOf(s.rd); !slices.Contains(chips, "Encrypted") {
		t.Fatalf("encrypted chips %q", chips)
	}
	// Whom it is encrypted to, which key opened it, and with what.
	if tab := securityTab(s.rd); !strings.Contains(tab, "Encrypted to 2 keys") || !strings.Contains(tab, "AAAA 1111 BBBB 2222 (X25519), which opened it") ||
		!strings.Contains(tab, "CCCC 3333 DDDD 4444 (RSA)") || !strings.Contains(tab, "AES256 OCB, with authenticated encryption") {
		t.Fatalf("encrypted tab:\n%s", tab)
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
	// The chips say it at a glance: not confirmed, warnings; the tab says
	// who checked and what each check found.
	tones, chips := chipsOf(s.rd)
	if len(chips) != 2 || chips[0] != "2 warnings about the sender" || tones[0] != secBad ||
		chips[1] != "Not signed or encrypted" || tones[1] != secNeutral {
		t.Fatalf("forged chips %q %v", chips, tones)
	}
	tab := securityTab(s.rd)
	for _, want := range []string{"Who sent it", "Checked by mail.local", "DMARC fail: paypal.com's policy is not met", "Domain signatures (DKIM)", "No domain signed it", "Not signed: nothing proves who wrote it"} {
		if !strings.Contains(tab, want) {
			t.Fatalf("security tab lacks %q:\n%s", want, tab)
		}
	}
	// A chip opens the tab.
	var chip *secChip
	widget.Walk(s.rd.secView.chips, func(c widget.Component) {
		if ch, ok := c.(*secChip); ok && chip == nil {
			chip = ch
		}
	})
	chip.KeyPress(widget.KeyEvent{Key: platform.KeyReturn})
	a.PumpOnce()
	if s.rd.tabs.Selected() != readerTabSecurity {
		t.Fatalf("the chip did not open the Security tab: %d", s.rd.tabs.Selected())
	}
	s.rd.tabs.Select(readerTabText)

	open(s, a, ids["Lunch"])
	if _, texts := lines(); len(texts) != 0 || s.rd.sender.view.Visible() {
		t.Fatalf("a sender with nothing to say: %q", texts)
	}
	if tones, chips := chipsOf(s.rd); len(chips) == 0 || chips[0] != "example.org confirmed" || tones[0] != secGood {
		t.Fatalf("confirmed chips %q %v", chips, tones)
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

// importedSession is a window on a store holding mails (file name → raw
// message, with \n line ends), imported from files; and the messages'
// ids by subject.
func importedSession(t *testing.T, mails map[string]string) (*session, *app.Application, map[string]mailcore.MessageID) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(mailcore.EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := mailcore.NewLocalStoreDir(mailcore.MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	for name, raw := range mails {
		if err := os.WriteFile(filepath.Join(src, name), []byte(strings.ReplaceAll(raw, "\n", "\r\n")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ImportLocalMail([]mailcore.LocalMailStore{{Source: "Folder", Name: "Inbox", Path: src, Kind: mailcore.StoreEML}}); err != nil {
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
	return s, a, ids
}

// The way a message came, from its Received headers: a chip says how
// many hops were in the clear, and the Security tab draws each hop — the
// sender's app signing in, a relay in the clear, TLS 1.3 to the provider,
// and inside it — with the protocol, TLS and cipher, and when.
func TestTheWayItCame(t *testing.T) {
	s, a, ids := importedSession(t, map[string]string{"a.eml": "" +
		"Received: by 2002:a05:7300:d311:b0:1c5:f6de:a6c with SMTP id k4csp1; Mon, 5 Oct 2026 10:00:05 +0000\n" +
		"Received: from mail.example.com (mail.example.com [203.0.113.5]) by mx.google.com with ESMTPS id x (version=TLS1_3 cipher=TLS_AES_256_GCM_SHA384); Mon, 5 Oct 2026 10:00:03 +0000\n" +
		"Received: from old.relay.example (old.relay.example [192.0.2.1]) by mail.example.com with SMTP id 9; Mon, 5 Oct 2026 10:00:01 +0000\n" +
		"Received: from [10.0.0.2] (unknown [198.51.100.9]) (Authenticated sender: ann@example.com) by old.relay.example with ESMTPSA id 1; Mon, 5 Oct 2026 10:00:00 +0000\n" +
		"From: Ann <ann@example.com>\nTo: me@example.org\nSubject: Numbers\n\nHello.\n"})
	open(s, a, ids["Numbers"])
	tones, chips := chipsOf(s.rd)
	i := slices.Index(chips, "1 hop in the clear")
	if i < 0 || tones[i] != secWarn {
		t.Fatalf("chips %q %v", chips, tones)
	}
	var rv *routeView
	widget.Walk(s.rd.secView.body, func(c widget.Component) {
		if v, ok := c.(*routeView); ok {
			rv = v
		}
	})
	if rv == nil || len(rv.hops) != 4 || len(rv.nodes()) != 5 || rv.nodes()[0].name != "the sender's app" || rv.nodes()[4].note != "your mailbox" {
		t.Fatalf("route %+v", rv)
	}
	var n a11y.Node
	rv.Describe(&n)
	for _, want := range []string{"ESMTPSA · TLS · signed in", "SMTP · in the clear", "ESMTPS · TLS 1.3 TLS_AES_256_GCM_SHA384", "inside one organisation"} {
		if !strings.Contains(n.Name, want) {
			t.Errorf("the route says %q, not %q", n.Name, want)
		}
	}
	if how, when := hopWords(rv.hops[2], rv.hops[1].At); !strings.Contains(how, "TLS 1.3") || !strings.HasSuffix(when, "2 s later") {
		t.Errorf("hop 3: %q, %q", how, when)
	}
	if tab := securityTab(s.rd); !strings.Contains(tab, "The way it came") || !strings.Contains(tab, "TLS hides a hop from the network") {
		t.Fatalf("security tab:\n%s", tab)
	}
	if hopInk(s.rd.view.Look(), rv.hops[1]) != inksOf(s.rd.view.Look()).bad || hopInk(s.rd.view.Look(), rv.hops[2]) != inksOf(s.rd.view.Look()).good {
		t.Error("the hop in the clear is not red, or the one with TLS not green")
	}
}

// What a message's content does: a link that shows one domain and goes to
// another, a tracking pixel, a form asking for a password, an attachment
// that hides its real ending, a read receipt asked for — each a chip, and
// each said in the Security tab.
func TestWhatItsContentDoes(t *testing.T) {
	s, a, ids := importedSession(t, map[string]string{"a.eml": "" +
		"From: \"PayPal\" <service@paypa1.com>\nTo: me@example.org\nSubject: Verify\n" +
		"Disposition-Notification-To: x@paypa1.com\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"b\"\n\n" +
		"--b\nContent-Type: text/html\n\n<p><a href=\"https://evil.example/login\">www.paypal.com</a>" +
		"<img src=\"https://t.tracker.example/o.gif\" width=\"1\" height=\"1\">" +
		"<form action=\"https://collect.example/\"><input type=\"password\"></form></p>\n" +
		"--b\nContent-Type: application/octet-stream; name=\"invoice.pdf.exe\"\nContent-Disposition: attachment; filename=\"invoice.pdf.exe\"\n" +
		"Content-Transfer-Encoding: base64\n\nTVo=\n--b--\n"})
	open(s, a, ids["Verify"])
	tones, chips := chipsOf(s.rd)
	for want, tone := range map[string]secTone{"1 link to look at": secWarn, "1 tracking image": secNeutral, "Asks for a password": secBad, "1 risky attachment": secBad} {
		i := slices.Index(chips, want)
		if i < 0 || tones[i] != tone {
			t.Errorf("no %q chip of tone %v: %q %v", want, tone, chips, tones)
		}
	}
	tab := securityTab(s.rd)
	for _, want := range []string{"Links", "“www.paypal.com” shows paypal.com but goes to evil.example",
		"1 tracking image, from tracker.example", "It asks for a password", "Attachments",
		"invoice.pdf.exe hides its real ending, which is a program's: .exe", "Other headers", "It asks for a read receipt, to x@paypa1.com"} {
		if !strings.Contains(tab, want) {
			t.Errorf("the tab lacks %q:\n%s", want, tab)
		}
	}
}

// The sender's domain is looked up only when asked: the button says
// what it will look up, and the tab then says what was found — or, here,
// with no resolver to answer, that nothing was.
func TestLookingUpTheSendersDomain(t *testing.T) {
	t.Setenv(mailcore.EnvDNS, "127.0.0.1:1")
	s, a, ids := importedSession(t, map[string]string{"a.eml": "From: Ann <ann@example.com>\nTo: me@example.org\nSubject: Hi\n\nHello.\n"})
	open(s, a, ids["Hi"])
	var btn *widgets.Button
	widget.Walk(s.rd.secView.body, func(c widget.Component) {
		if b, ok := c.(*widgets.Button); ok && b.Text == "Look up example.com in DNS" {
			btn = b
		}
	})
	if btn == nil || strings.Contains(securityTab(s.rd), "Looked up") {
		t.Fatalf("no button, or looked up by itself:\n%s", securityTab(s.rd))
	}
	btn.OnClick()
	for i := 0; i < 3; i++ {
		s.waitIdle()
		a.PumpOnce()
	}
	if tab := securityTab(s.rd); !strings.Contains(tab, "Looked up") || !strings.Contains(tab, "Not answered — MX") {
		t.Fatalf("after the lookup:\n%s", tab)
	}
}

// What a domain publishes, in words: its mail servers, SPF's end, DMARC's
// policy and for how much, MTA-STS, DANE that counts only signed, BIMI,
// DNSSEC.
func TestWhatADomainPublishes(t *testing.T) {
	var texts []string
	rows := domainRows("example.com", &mailcore.DomainReport{Domain: "example.com", MX: []string{"mx1.example.com"}, SPF: "v=spf1 -all", SPFAll: "-all",
		DMARC: "v=DMARC1; p=quarantine; pct=50", Policy: "quarantine", Pct: 50, MTASTS: "1",
		DANE: []mailcore.DANEHost{{Host: "mx1.example.com", Records: 2}}, BIMI: "https://example.com/l.svg", Checked: true}, false, nil)
	for _, r := range rows {
		_, tx := iconLines(r)
		texts = append(texts, tx...)
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{"Its mail servers: mx1.example.com", "SPF ends with -all", "DMARC: mail that fails its checks is to go to spam (for 50% of it)",
		"MTA-STS: it publishes a policy", "DANE for mx1.example.com: 2 TLSA records, but not confirmed signed", "BIMI: a logo, with no mark certificate",
		"DNSSEC not confirmed"} {
		if !strings.Contains(all, want) {
			t.Errorf("lacks %q:\n%s", want, all)
		}
	}
}

// The way it came ends with the last hop, from your mailbox to you, as
// comms-mail fetches it: green with TLS, its version and cipher, and who
// certified the server; red in the clear.
func TestTheLastHopIsYours(t *testing.T) {
	hops := []mailcore.RouteHop{{From: "mail.example.com", By: "mx.example.org", With: "ESMTPS", TLS: mailcore.HopTLS}}
	v := newRouteView(hops, &mailcore.ConnectionInfo{Protocol: "IMAP", Server: "imap.example.org:993", Mode: "ssl", Live: true,
		Version: "TLS 1.3", Cipher: "TLS_AES_128_GCM_SHA256", Issuer: "Let's Encrypt R10"})
	nodes, edges := v.nodes(), v.edges()
	if len(nodes) != 3 || nodes[2].name != "you, in comms-mail" || len(edges) != 2 || edges[1].TLS != mailcore.HopTLS {
		t.Fatalf("nodes %+v edges %+v", nodes, edges)
	}
	if how, _ := hopWords(edges[1], time.Time{}); how != "IMAP · TLS 1.3 TLS_AES_128_GCM_SHA256" || !strings.Contains(v.fetchWhen(), "certified by Let's Encrypt R10") {
		t.Fatalf("last hop %q, %q", how, v.fetchWhen())
	}
	plain := newRouteView(hops, &mailcore.ConnectionInfo{Protocol: "IMAP", Server: "imap.example.org:143", Mode: "plain"})
	if e := plain.edges(); e[1].TLS != mailcore.HopClear {
		t.Fatalf("a plain fetch is not in the clear: %+v", e[1])
	}
	if tone, text, _ := routeSummary(plain.edges()); tone != secWarn || text != "1 hop in the clear" {
		t.Fatalf("summary %v %q", tone, text)
	}
}

// The Security tab stays quick to switch through: a newsletter's many
// click-counting links are one line, and the tab measures its sections
// once — not on every layout — until they change.
func TestSecurityTabStaysQuick(t *testing.T) {
	var c mailcore.ContentReport
	c.LinkCount = 200
	for i := 0; i < 200; i++ {
		c.Links = append(c.Links, mailcore.LinkIssue{Text: "shop.example", Href: "https://click.esp.example/r", Host: "click.esp.example",
			Kind: mailcore.LinkElsewhere, Shown: "shop.example"})
	}
	rows := linkRows(c)
	if len(rows) != 3 {
		t.Fatalf("200 alike links in %d lines", len(rows))
	}
	if _, tx := iconLines(rows[1]); len(tx) != 1 || tx[0] != "“shop.example” and 199 more show shop.example but go to click.esp.example" {
		t.Fatalf("grouped: %q", tx)
	}

	s, a, ids := importedSession(t, map[string]string{"a.eml": "From: Ann <ann@example.com>\nSubject: Hi\n\nHello.\n"})
	open(s, a, ids["Hi"])
	s.rd.tabs.Select(readerTabSecurity)
	a.PumpOnce()
	box := s.rd.secView.box
	if box.MinWidth() <= 0 || len(box.measured) == 0 {
		t.Fatalf("min width %v, measured %d", box.MinWidth(), len(box.measured))
	}
	n := len(box.measured)
	s.rd.view.RequestLayout()
	a.PumpOnce()
	if len(box.measured) != n {
		t.Fatalf("measured anew on a layout with nothing changed: %d, then %d", n, len(box.measured))
	}
	s.rd.secView.show(s.rd.msg)
	if len(box.measured) != 0 {
		t.Fatal("new sections kept the old measures")
	}
}

// The tabs start at the top of the reading pane, the header — subject,
// From, the chips — in the Message tab; and measuring the pane, as its
// splitter does on every layout, does not change how far the Security
// tab scrolls (uitoolkit-gaps.md #51).
func TestTabsOnTopAndTheSecurityTabScrollsItsContent(t *testing.T) {
	links := strings.Repeat(`<p><a href="https://click.example/x">shop.example</a> <img src="https://cdn.example/a.png"></p>`, 12)
	s, a, ids := importedSession(t, map[string]string{"a.eml": "From: Shop <news@shop.example>\nTo: me@example.org\nSubject: Sale\nContent-Type: text/html\n\n" + links + "\n"})
	open(s, a, ids["Sale"])
	r := s.rd
	if top, pane := widget.DeviceOrigin(r.tabs).Y, widget.DeviceOrigin(r.view).Y; top != pane {
		t.Fatalf("the tabs start at %v, the pane at %v", top, pane)
	}
	if !widget.Contains(r.tabs, r.subj) || !widget.Contains(r.tabs, r.secView.chips) || widget.DeviceOrigin(r.subj).Y <= widget.DeviceOrigin(r.tabs).Y {
		t.Fatal("the header is not in the Message tab")
	}
	r.tabs.Select(readerTabSecurity)
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	want := r.secView.scroll.MaxOffset()
	widget.MinWidthOf(r.view)
	r.view.Measure(layout.Unbounded())
	r.tabs.Measure(layout.Constraints{MaxW: 120, MaxH: -1})
	if got := r.secView.scroll.MaxOffset(); got != want || want <= 0 {
		t.Fatalf("measuring the pane changed how far the tab scrolls: %v, then %v", want, got)
	}
}

// The Message tab's header — tall with its attachments, so it scrolls —
// scrolls its own content however the pane is measured (uitoolkit-gaps.md
// #51).
func TestTheHeaderScrollsItsContent(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.LightLook(), false, AppOptions{})
	defer done()
	w.SetSize(1280, 520)
	var names []string
	for i := 0; i < 12; i++ {
		names = append(names, "file-"+strconv.Itoa(i)+".pdf")
	}
	m := mailcore.Message{ID: "tall", Subject: "A long one", From: "a@example.com", To: "b@example.com",
		Cc: "c@example.com, d@example.com", HasAttach: true, Attachments: names, Body: "the text"}
	s.showHeaders(m)
	s.showBody(m)
	// What wraps, and so is as tall as the pane is narrow: the chips, and
	// the warnings about the sender.
	s.rd.sender.show(mailcore.SenderCheck{Warnings: []string{
		"Your mail server could not confirm this is from example.com: it failed example.com's own sender policy (DMARC).",
		"The name shows service@paypal.com, but it is from a@example.com."}})
	s.rd.secView.report = &mailcore.SecurityReport{Sender: mailcore.SenderCheck{Auth: mailcore.AuthFail, Warnings: []string{"x", "y"}},
		Content: mailcore.ContentReport{Trackers: []string{"t.example"}, Links: []mailcore.LinkIssue{{Kind: mailcore.LinkElsewhere, Host: "a.example", Shown: "b.example"}}}}
	s.rd.secView.show(m)
	for i := 0; i < 3; i++ {
		a.PumpOnce()
	}
	var head *widgets.ScrollView
	widget.Walk(s.rd.view, func(c widget.Component) {
		if sv, ok := c.(*widgets.ScrollView); ok && head == nil && widget.Contains(sv, s.rd.subj) {
			head = sv
		}
	})
	if head == nil {
		t.Fatal("no header scroll")
	}
	want := head.MaxOffset()
	widget.MinWidthOf(s.rd.view)
	s.rd.view.Measure(layout.Unbounded())
	s.rd.tabs.Measure(layout.Constraints{MaxW: 120, MaxH: -1})
	if got := head.MaxOffset(); got != want || want <= 0 {
		t.Fatalf("measuring the pane changed how far the header scrolls: %v, then %v", want, got)
	}
}
