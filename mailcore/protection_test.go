package mailcore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/internal/svtest"
)

// Messages as they arrive, one for each structure.
const (
	msgSignedPGP = `From: Alice <alice@example.org>
To: Bob <bob@example.com>
Subject: Lunch
Date: Mon, 28 Sep 2026 10:00:00 +0000
Message-ID: <signed@example.org>
Autocrypt: addr=alice@example.org; keydata=QUJD
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
	msgEncryptedPGP = `From: Alice <alice@example.org>
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
Content-Disposition: inline; filename="encrypted.asc"

-----BEGIN PGP MESSAGE-----
hQEMA9xX0Lq2eW3YAQfzyzzyx
-----END PGP MESSAGE-----
--b2--
`
	msgSMIMEEnveloped = `From: Carol <carol@example.net>
To: Bob <bob@example.com>
Subject: Contract
Date: Mon, 28 Sep 2026 12:00:00 +0000
Message-ID: <enveloped@example.net>
MIME-Version: 1.0
Content-Type: application/pkcs7-mime; smime-type=enveloped-data; name="smime.p7m"
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="smime.p7m"

MIIBugYJKoZIhvcNAQcDoIIBqzCCAacCAQAxggEcMIIBGAIBADCBgDB4MQswCQYDVQQGEwJVUzE=
`
	msgSMIMEOpaque = `From: Carol <carol@example.net>
To: Bob <bob@example.com>
Subject: Signed, opaque
Date: Mon, 28 Sep 2026 12:30:00 +0000
MIME-Version: 1.0
Content-Type: application/pkcs7-mime; smime-type=signed-data; name="smime.p7m"
Content-Transfer-Encoding: base64

MIIBugYJKoZIhvcNAQcCoIIBqzCCAacCAQExDzANBglghkgBZQMEAgEFADA=
`
	msgSMIMECerts = `From: Carol <carol@example.net>
Subject: My certificate
MIME-Version: 1.0
Content-Type: application/pkcs7-mime; smime-type=certs-only; name="smime.p7c"
Content-Transfer-Encoding: base64

MIIBugYJKoZIhvcNAQcCoA==
`
	msgInlineSigned = `From: Dan <dan@example.com>
Subject: Inline signed
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

See you there.
-----BEGIN PGP SIGNATURE-----
iQ==
-----END PGP SIGNATURE-----
`
	msgInlineEncrypted = `From: Dan <dan@example.com>
Subject: Inline encrypted
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

-----BEGIN PGP MESSAGE-----
hQEMA9xX0Lq2eW3YAQfzyzzyx
-----END PGP MESSAGE-----
`
	msgPlain = `From: Eve <eve@example.com>
To: Bob <bob@example.com>
Subject: Hello
Date: Mon, 28 Sep 2026 13:00:00 +0000
Message-ID: <plain@example.com>
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

Just text.
`
)

func lf2crlf(s string) []byte { return bytes.ReplaceAll([]byte(s), []byte("\n"), []byte("\r\n")) }

// comms-mail tells signed and encrypted mail apart by its structure alone,
// and keeps none of an encrypted message's ciphertext as text.
func TestRecogniseProtection(t *testing.T) {
	for _, c := range []struct {
		name                string
		raw                 string
		signed, encrypted   bool
		body                string // in the body, or "" for none
		autocrypt, attached bool
	}{
		{"PGP/MIME signed", msgSignedPGP, true, false, "Shall we meet at noon?", true, false},
		{"PGP/MIME encrypted", msgEncryptedPGP, false, true, "", false, false},
		{"S/MIME enveloped", msgSMIMEEnveloped, false, true, "", false, false},
		{"S/MIME opaque signed", msgSMIMEOpaque, true, false, "", false, false},
		{"S/MIME certificates only", msgSMIMECerts, false, false, "", false, false},
		{"inline signed", msgInlineSigned, true, false, "See you there.", false, false},
		{"inline encrypted", msgInlineEncrypted, false, true, "", false, false},
		{"plain", msgPlain, false, false, "Just text.", false, false},
	} {
		m, err := ParseRFC822(lf2crlf(c.raw), "f", "a")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if m.Signed != c.signed || m.Encrypted != c.encrypted || m.Autocrypt != c.autocrypt {
			t.Errorf("%s: signed %v encrypted %v autocrypt %v", c.name, m.Signed, m.Encrypted, m.Autocrypt)
		}
		switch {
		case c.body == "" && (m.Body != "" || m.HTML != "" || m.Snippet != ""):
			t.Errorf("%s: kept text %q / %q", c.name, m.Body, m.Snippet)
		case c.body != "" && !strings.Contains(m.Body, c.body):
			t.Errorf("%s: body %q", c.name, m.Body)
		}
		if c.encrypted && (m.HasAttach || len(m.Attachments) > 0) {
			t.Errorf("%s: its container shows as an attachment %v", c.name, m.Attachments)
		}
		if !c.attached && (m.HasAttach || len(m.Attachments) > 0) {
			t.Errorf("%s: attachments %v", c.name, m.Attachments)
		}
	}
}

// protectedStore is a store whose secrets are in a stand-in secretvault,
// holding a signed, an encrypted and a plain message; it returns them by
// subject.
func protectedStore(t *testing.T) (*LocalStore, map[string]MessageID, *svtest.Vault) {
	t.Helper()
	sv := startFakeSecretVault(t, false)
	st, _ := vaultFixture(t)
	if err := st.UseStore(StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for i, raw := range []string{msgSignedPGP, msgEncryptedPGP, msgPlain} {
		if err := os.WriteFile(filepath.Join(dir, string(rune('a'+i))+".eml"), lf2crlf(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ImportLocalMail([]LocalMailStore{{Source: "Folder", Name: "Inbox", Path: dir, Kind: StoreEML}}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]MessageID{}
	for _, f := range st.ListFolders(LocalAccountID) {
		for _, m := range st.ListMessages(f.ID) {
			ids[m.Subject] = m.ID
		}
	}
	if len(ids) != 3 {
		t.Fatalf("imported %v", ids)
	}
	return st, ids, sv
}

// What secretvault answers, as its mail.inspect does: a valid signature
// by a contact verified in person, with the key the message carries; an
// encryption opened, with its protected subject and text.
func inspectAnswer(raw []byte, decrypt bool) any {
	switch {
	case bytes.Contains(raw, []byte("multipart/signed")):
		return map[string]any{
			"report": map[string]any{
				"signed": "full", "encrypted": "none",
				"layers": []any{map[string]any{"id": 1, "kind": "signature", "format": "pgp-mime", "covers": "message",
					"signatures": []any{map[string]any{"status": "valid", "verified": true, "fingerprint": "AAAA1111",
						"user_ids": []string{"Alice <alice@example.org>"}}}}},
				"keys": []any{map[string]any{"source": "autocrypt", "type": "openpgp", "address": "alice@example.org", "data": []byte("ABC")}},
			},
			"verdicts": []any{map[string]any{"layer": 1, "index": 0, "verdict": map[string]any{
				"contact": "Alice", "level": "in-person", "status": "verified", "reasons": []string{"fingerprint compared in person"}}}},
		}
	case bytes.Contains(raw, []byte("multipart/encrypted")):
		layer := map[string]any{"id": 1, "kind": "encryption", "format": "pgp-mime", "covers": "message",
			"encryption": map[string]any{"decrypted": decrypt}}
		report := map[string]any{"signed": "none", "encrypted": "full", "layers": []any{layer}}
		if decrypt {
			report["content"] = map[string]any{"layer": 1, "raw": lf2crlf("Content-Type: text/plain; charset=utf-8\n\nThe plan is in the blue folder.\n")}
			report["protected_headers"] = map[string]any{"form": "rfc9788", "headers": map[string]any{"subject": "The real subject"}}
		}
		return map[string]any{"report": report}
	}
	return map[string]any{"report": map[string]any{"signed": "none", "encrypted": "none"}}
}

// secretvault checks a signed message and opens an encrypted one; the
// keys a message carries go to its contacts; what it decrypts is the
// reading pane's only — neither cached nor indexed.
func TestMessageSecurityThroughSecretVault(t *testing.T) {
	st, ids, sv := protectedStore(t)
	sv.InspectWith(inspectAnswer)

	// Plain mail is not handed over.
	if sec, err := st.MessageSecurity(ids["Hello"], true); err != nil || sec.Signed != "none" || sec.Encrypted != "none" || len(sv.Inspects()) != 0 {
		t.Fatalf("plain: %+v %v (inspected %d)", sec, err, len(sv.Inspects()))
	}

	sec, err := st.MessageSecurity(ids["Lunch"], false)
	if err != nil {
		t.Fatal(err)
	}
	if !sec.Checked || sec.Signed != "full" || len(sec.Signatures) != 1 {
		t.Fatalf("signed: %+v", sec)
	}
	if s := sec.Signatures[0]; s.Status != "valid" || s.Trust != "verified" || s.Level != "in-person" || s.Signer != "Alice" || s.Format != "openpgp" {
		t.Fatalf("signature %+v", s)
	}
	seen := sv.Seens()
	if len(seen) != 1 || seen[0].Address != "alice@example.org" || string(seen[0].Key) != "ABC" || seen[0].Source != "autocrypt" || seen[0].Name != "Alice" {
		t.Fatalf("keys passed on: %+v", seen)
	}

	sec, err = st.MessageSecurity(ids["..."], true)
	if err != nil {
		t.Fatal(err)
	}
	if !sec.Decrypted || sec.Content == nil || !strings.Contains(sec.Content.Body, "blue folder") || sec.Subject != "The real subject" {
		t.Fatalf("encrypted: %+v", sec)
	}
	if got := sv.Inspects(); !got[len(got)-1].Decrypt {
		t.Fatal("decrypting was not asked")
	}
	// The cache and the index hold neither the ciphertext nor the text.
	m, ok := st.GetMessage(ids["..."])
	if !ok || m.Body != "" || !m.Encrypted {
		t.Fatalf("cached: %+v (found %v)", m, ok)
	}
	for _, q := range []string{"blue folder", "hQEMA9xX0Lq2eW3YAQfzyzzyx"} {
		if hits := st.Search(SearchQuery{Filter: Filter{Query: q}}); len(hits) != 0 {
			t.Fatalf("searching %q finds %d", q, len(hits))
		}
	}

	// Locked: nothing is checked, and nobody is asked to unlock.
	sv.Lock()
	waitFor(t, "the lock was not noticed", func() bool { return st.SecretsStatus().Locked })
	before := len(sv.Inspects())
	if sec, _ := st.MessageSecurity(ids["Lunch"], false); !sec.Locked || sec.Checked || len(sv.Inspects()) != before || sv.Unlocks() != 0 {
		t.Fatalf("locked: %+v", sec)
	}
	sv.Unlock()
	waitFor(t, "the unlock was not noticed", func() bool { return st.SecretsStatus().Ready })

	// Another store in use: protected mail says secretvault reads it.
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if sec, _ := st.MessageSecurity(ids["..."], true); sec.Checked || sec.Encrypted != "full" || !strings.Contains(sec.Why, "choose it in Settings") {
		t.Fatalf("another store: %+v", sec)
	}
}

// The demo store keeps no secrets: it says what the structure says.
func TestMessageSecurityWithoutSecretVault(t *testing.T) {
	if sec := MessageSecurityOf(lf2crlf(msgSMIMEEnveloped)); sec.Checked || sec.Encrypted != "full" || sec.Why == "" {
		t.Fatalf("%+v", sec)
	}
	if sec := MessageSecurityOf(lf2crlf(msgPlain)); sec.Why != "" || sec.Signed != "none" {
		t.Fatalf("plain %+v", sec)
	}
}

// A protected message cached by an older comms-mail — no flags, its
// S/MIME ciphertext kept as text — is read again from its raw message
// when it is opened.
func TestOldCachedProtectedMailIsReadAgain(t *testing.T) {
	st := newImportStore(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.eml"), lf2crlf(msgSMIMEEnveloped), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ImportLocalMail([]LocalMailStore{{Source: "Folder", Name: "Inbox", Path: dir, Kind: StoreEML}}); err != nil {
		t.Fatal(err)
	}
	var id MessageID
	for _, f := range st.ListFolders(LocalAccountID) {
		for _, m := range st.ListMessages(f.ID) {
			id = m.ID
		}
	}
	st.mu.Lock()
	i, _ := st.indexLocked(id)
	st.Messages[i].Encrypted = false
	st.Messages[i].Body = "0\x82\x01\xba garbage"
	st.Messages[i].Attachments, st.Messages[i].HasAttach = []string{"smime.p7m"}, true
	st.mu.Unlock()

	m, ok := st.GetMessage(id)
	if !ok || !m.Encrypted || m.Body != "" || m.HasAttach {
		t.Fatalf("opened: %+v", m)
	}
}
