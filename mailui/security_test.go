package mailui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
	widget.Walk(r.sec.lines, func(c widget.Component) {
		if l, ok := c.(*widgets.Label); ok {
			icons = append(icons, l.Icon)
			texts = append(texts, l.Text)
		}
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
