package mailcore

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// ownPGPStore is an account whose OpenPGP is comms-mail's own, its keys
// in the encrypted file, with a key made for ada@example.com. It sends
// through f, the stand-in secretvault running (and not asked).
func ownPGPStore(t *testing.T, f *fakeSMTP) *LocalStore {
	t.Helper()
	st, _ := sendingStore(t, f)
	if err := st.UseKeys(FormatOpenPGP, StoreEncrypted, "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MakePGPKey("ada@example.com"); err != nil {
		t.Fatal(err)
	}
	return st
}

// armoredPublic is e's public key, armoured.
func armoredPublic(t *testing.T, e *openpgp.Entity) []byte {
	t.Helper()
	var b bytes.Buffer
	w, _ := armor.Encode(&b, openpgp.PublicKeyType, nil)
	if err := e.Serialize(w); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	return b.Bytes()
}

// The key comms-mail makes is yours: listed for its address, its private
// half where the keys are kept, never in the index. Signed mail goes as
// PGP/MIME, offering the key (Autocrypt), and checks as yours.
func TestOwnOpenPGPSigns(t *testing.T) {
	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	v := st.KeysView(FormatOpenPGP)
	if v.Place.Engine != EngineOwn || v.Place.Store != StoreEncrypted || !v.Place.Ready {
		t.Fatalf("place %+v", v.Place)
	}
	var key *OwnPGPKey
	for _, a := range v.Addresses {
		if a.Address == "ada@example.com" {
			key = a.PGP
		}
	}
	if key == nil || !strings.Contains(key.PublicKey, "PUBLIC KEY") {
		t.Fatalf("no key for ada: %+v", v.Addresses)
	}
	idx, _ := os.ReadFile(filepath.Join(st.dir, "keys", "index.json"))
	if bytes.Contains(idx, []byte("PRIVATE")) {
		t.Fatal("the index holds a private key")
	}
	if names, _ := st.keyStore(FormatOpenPGP).Names(); len(names) != 1 || !strings.HasPrefix(names[0], "keys/openpgp/") {
		t.Fatalf("kept: %v", names)
	}

	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true}), nil); err != nil {
		t.Fatal(err)
	}
	got := smtp.delivered()
	if len(got) != 1 || !strings.Contains(got[0], "multipart/signed") || !strings.Contains(got[0], "application/pgp-signature") ||
		!strings.Contains(got[0], "Autocrypt: addr=ada@example.com") || !strings.Contains(got[0], "blue door") {
		t.Fatalf("delivered:\n%s", got)
	}
	sec := st.ownInspect([]byte(got[0]), true, Message{From: "Ada <ada@example.com>"})
	if sec.Signed != "full" || len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" || sec.Signatures[0].Trust != "verified" || sec.Signatures[0].Level != "own" {
		t.Fatalf("checked: %+v", sec)
	}
	// Changed on the way, it no longer holds.
	bad := strings.Replace(got[0], "blue door", "red door", 1)
	if sec := st.ownInspect([]byte(bad), true, Message{From: "ada@example.com"}); len(sec.Signatures) != 1 || sec.Signatures[0].Status != "bad" {
		t.Fatalf("changed: %+v", sec.Signatures)
	}
}

// Encrypted, the message goes to the recipient's key and the writer's,
// the real subject inside and "..." outside; without a key for every
// recipient it does not go at all.
func TestOwnOpenPGPEncrypts(t *testing.T) {
	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	bob, err := openpgp.NewEntity("Bob", "", "bob@example.org", pgpConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err == nil || !strings.Contains(err.Error(), "bob@example.org") {
		t.Fatalf("without Bob's key: %v", err)
	}
	if n, err := st.ImportKeys(FormatOpenPGP, armoredPublic(t, bob), nil); err != nil || n != 1 {
		t.Fatalf("import: %d %v", n, err)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	got := smtp.delivered()
	if len(got) != 1 || !strings.Contains(got[0], "multipart/encrypted") || strings.Contains(got[0], "blue door") ||
		!strings.Contains(got[0], "Subject: ...") || strings.Contains(got[0], "The plan") {
		t.Fatalf("delivered:\n%s", got)
	}
	// Bob opens it with his key.
	_, entity := splitEntity(crlf([]byte(got[0])))
	fields, body := splitEntity(crlf([]byte(got[0])))
	_, params := contentType(fields)
	parts, _ := multipartParts(body, params["boundary"])
	_ = entity
	f2, b2 := splitEntity(parts[1])
	data, _ := decodedBody(f2, b2)
	block, _ := armor.Decode(bytes.NewReader(data))
	md, err := openpgp.ReadMessage(block.Body, openpgp.EntityList{bob}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var inner bytes.Buffer
	_, _ = inner.ReadFrom(md.UnverifiedBody)
	if !strings.Contains(inner.String(), "blue door") || !strings.Contains(inner.String(), "Subject: The plan") || !md.IsSigned {
		t.Fatalf("Bob reads:\n%s", inner.String())
	}
	// The writer's own copy opens too, signed by the writer.
	sec := st.ownInspect([]byte(got[0]), true, Message{From: "Ada <ada@example.com>"})
	if !sec.Decrypted || sec.Subject != "The plan" || sec.Content == nil || !strings.Contains(sec.Content.Body, "blue door") ||
		len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" {
		t.Fatalf("Ada reads: %+v", sec)
	}
}

// A key that comes in an Autocrypt header is recorded as first seen in
// mail, and a signature by it holds but is not called verified; a
// message from someone else signed with it is the wrong person's.
func TestOwnOpenPGPLearnsKeysFromMail(t *testing.T) {
	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	carol, _ := openpgp.NewEntity("Carol", "", "carol@example.net", pgpConfig())
	// Carol's message, signed by her and offering her key.
	entity := []byte("Content-Type: text/plain; charset=utf-8\r\n\r\nSee you at noon.\r\n")
	var sig bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&sig, carol, bytes.NewReader(entity), nil); err != nil {
		t.Fatal(err)
	}
	var key bytes.Buffer
	_ = carol.Serialize(&key)
	ctype, body := signedMultipart(entity, "application/pgp-signature", "pgp-sha256",
		append([]byte("Content-Type: application/pgp-signature\r\n\r\n"), sig.Bytes()...))
	head := []mimeField{{name: "From", raw: "From: Carol <carol@example.net>\r\n"}, {name: "Subject", raw: "Subject: Lunch\r\n"}}
	ac := "Autocrypt: addr=carol@example.net; keydata=" + b64(key.Bytes())
	raw := wrapMessage(head, false, ctype, []string{ac}, body)

	sec := st.ownInspect(raw, true, Message{From: "Carol <carol@example.net>"})
	if len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" || sec.Signatures[0].Trust != "unverified" || sec.Signatures[0].Level != "tofu" {
		t.Fatalf("Carol's: %+v", sec.Signatures)
	}
	if l := st.keys().contacts(FormatOpenPGP, "carol@example.net"); len(l) != 1 || l[0].Source != "autocrypt" {
		t.Fatalf("recorded %+v", l)
	}
	// The same signature on mail from someone else.
	sec = st.ownInspect(raw, true, Message{From: "mallory@example.com"})
	if len(sec.Signatures) != 1 || sec.Signatures[0].Trust != "suspicious" {
		t.Fatalf("not hers: %+v", sec.Signatures)
	}
}

// With the keys' place locked, encrypted mail says so rather than failing
// quietly, and a message to sign waits in the Outbox.
func TestOwnOpenPGPLocked(t *testing.T) {
	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err == nil {
		t.Fatal("sent to Bob without his key")
	}
	if _, err := st.ImportKeys(FormatOpenPGP, armoredPublic(t, mustEntity(t, "bob@example.org")), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	sent := smtp.delivered()[0]
	st.vaultOf().Lock()
	if p := st.keyPlace(FormatOpenPGP); !p.Locked {
		t.Fatalf("place %+v", p)
	}
	if sec := st.ownInspect([]byte(sent), true, Message{From: "ada@example.com"}); sec.KeysLocked != FormatOpenPGP || sec.Decrypted {
		t.Fatalf("locked: %+v", sec)
	}
	_, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true}), nil)
	var q *QueuedError
	if !errors.As(err, &q) {
		t.Fatalf("locked send: %v", err)
	}
	if err := st.UnlockKeys(FormatOpenPGP, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if sec := st.ownInspect([]byte(sent), true, Message{From: "ada@example.com"}); !sec.Decrypted {
		t.Fatalf("unlocked: %+v", sec)
	}
}

// Keys and passwords share the encrypted file without taking each other
// along: the passwords moving out leave the keys, and the file stays.
func TestKeysAndPasswordsShareAPlace(t *testing.T) {
	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(st.encryptedUsers(), []string{"passwords", FormatOpenPGP}) {
		t.Fatalf("users %v", st.encryptedUsers())
	}
	if err := st.UseStore(StorePlain, ""); err != nil {
		t.Fatal(err)
	}
	if !st.vaultOf().Exists() {
		t.Fatal("the encrypted file went with the passwords")
	}
	if names, _ := st.keyStore(FormatOpenPGP).Names(); len(names) != 1 {
		t.Fatalf("the keys went: %v", names)
	}
	// The keys moving to the plain file leave the encrypted file empty:
	// it goes.
	if err := st.UseKeys(FormatOpenPGP, StorePlain, "", ""); err != nil {
		t.Fatal(err)
	}
	if st.vaultOf().Exists() {
		t.Fatal("an empty encrypted file stayed")
	}
	if names, _ := st.keyStore(FormatOpenPGP).Names(); len(names) != 1 {
		t.Fatalf("the keys did not move: %v", names)
	}
}

// GnuPG reads what comms-mail's own OpenPGP writes, and comms-mail reads
// what GnuPG writes: signatures each way, and a message encrypted to a
// GnuPG key and back.
func TestOwnOpenPGPWithGnuPG(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
	home := t.TempDir()
	gpg := func(stdin []byte, args ...string) []byte {
		t.Helper()
		cmd := exec.Command("gpg", append([]string{"--homedir", home, "--batch", "--yes", "--pinentry-mode", "loopback", "--passphrase", ""}, args...)...)
		cmd.Stdin = bytes.NewReader(stdin)
		out, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				t.Fatalf("gpg %v: %v\n%s", args, err, ee.Stderr)
			}
			t.Fatalf("gpg %v: %v", args, err)
		}
		return out
	}
	t.Cleanup(func() { _ = exec.Command("gpgconf", "--homedir", home, "--kill", "gpg-agent").Run() })
	gpg(nil, "--quick-gen-key", "Bob <bob@example.org>", "default", "default", "never")
	bobPub := gpg(nil, "--armor", "--export", "bob@example.org")

	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	adaPub := []byte(st.keys().own(FormatOpenPGP, "ada@example.com")[0].Public)
	gpg(adaPub, "--import")
	gpg(nil, "--quick-lsign-key", st.keys().own(FormatOpenPGP, "ada@example.com")[0].ID)
	if _, err := st.ImportKeys(FormatOpenPGP, bobPub, nil); err != nil {
		t.Fatal(err)
	}

	// comms-mail signs, GnuPG checks.
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true}), nil); err != nil {
		t.Fatal(err)
	}
	signed := crlf([]byte(smtp.delivered()[0]))
	fields, body := splitEntity(signed)
	_, params := contentType(fields)
	parts, _ := multipartParts(body, params["boundary"])
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "part"), parts[0], 0o600)
	sf, sb := splitEntity(parts[1])
	sig, _ := decodedBody(sf, sb)
	_ = os.WriteFile(filepath.Join(dir, "part.asc"), sig, 0o600)
	gpg(nil, "--verify", filepath.Join(dir, "part.asc"), filepath.Join(dir, "part"))

	// comms-mail encrypts to Bob's GnuPG key, GnuPG opens it.
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	enc := crlf([]byte(smtp.delivered()[1]))
	fields, body = splitEntity(enc)
	_, params = contentType(fields)
	parts, _ = multipartParts(body, params["boundary"])
	ef, eb := splitEntity(parts[1])
	data, _ := decodedBody(ef, eb)
	if inner := gpg(data, "--decrypt"); !bytes.Contains(inner, []byte("blue door")) {
		t.Fatalf("GnuPG reads:\n%s", inner)
	}

	// GnuPG signs and encrypts to Ada as Bob; comms-mail opens and checks.
	inner := []byte("Content-Type: text/plain; charset=utf-8\r\n\r\nThe key is under the mat.\r\n")
	armored := gpg(inner, "--armor", "--sign", "--local-user", "bob@example.org", "--encrypt", "--recipient", st.keys().own(FormatOpenPGP, "ada@example.com")[0].ID, "--trust-model", "always")
	bd := "gpg-boundary"
	msg := "From: Bob <bob@example.org>\r\nTo: ada@example.com\r\nSubject: Mat\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\"" + bd + "\"\r\n\r\n" +
		"--" + bd + "\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n\r\n" +
		"--" + bd + "\r\nContent-Type: application/octet-stream\r\n\r\n" + string(crlf(armored)) + "\r\n--" + bd + "--\r\n"
	sec := st.ownInspect([]byte(msg), true, Message{From: "Bob <bob@example.org>"})
	if !sec.Decrypted || sec.Content == nil || !strings.Contains(sec.Content.Body, "under the mat") ||
		len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" || sec.Signatures[0].Trust != "verified" {
		t.Fatalf("from GnuPG: %+v", sec)
	}
}

func mustEntity(t *testing.T, addr string) *openpgp.Entity {
	t.Helper()
	e, err := openpgp.NewEntity("", "", addr, pgpConfig())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func slicesEqual(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func b64(b []byte) string {
	var out bytes.Buffer
	writeBase64Lines(&out, b)
	return strings.ReplaceAll(strings.TrimSpace(out.String()), "\r\n", "\r\n ")
}

// Text that is not 7-bit is made so before it is signed — a signature
// must hold however the message travels — and still reads the same.
func TestOwnOpenPGPSignsSevenBit(t *testing.T) {
	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	m := outgoing(&Protection{Sign: true})
	m.Body = "Rendez-vous au café, près de la porte bleue.\n"
	if _, err := st.SendViaSMTP("w", "", m, nil); err != nil {
		t.Fatal(err)
	}
	got := smtp.delivered()[0]
	if !isSevenBit([]byte(got)) || !strings.Contains(got, "Content-Transfer-Encoding: quoted-printable") {
		t.Fatalf("not 7-bit:\n%s", got)
	}
	sec := st.ownInspect([]byte(got), true, Message{From: "ada@example.com"})
	if len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" {
		t.Fatalf("checked: %+v", sec.Signatures)
	}
	if parsed, _ := ParseRFC822([]byte(got), "", ""); !strings.Contains(parsed.Body, "café") {
		t.Fatalf("reads %q", parsed.Body)
	}
}

// An Autocrypt header gives only the sender's own key: a header naming
// someone else, on mail from another address, is not taken, whatever
// addresses the key carries.
func TestAutocryptOnlyForTheSender(t *testing.T) {
	smtp := startFakeSMTP(t)
	st := ownPGPStore(t, smtp)
	both, _ := openpgp.NewEntity("Carol", "", "carol@example.net", pgpConfig())
	_ = both.AddUserId("Mallory", "", "mallory@example.com", pgpConfig())
	var key bytes.Buffer
	_ = both.Serialize(&key)
	head := []mimeField{{name: "From", raw: "From: mallory@example.com\r\n"}}
	raw := wrapMessage(head, false, "text/plain", []string{"Autocrypt: addr=carol@example.net; keydata=" + b64(key.Bytes())}, []byte("hi\r\n"))
	st.ownInspect(raw, true, Message{From: "mallory@example.com"})
	if l := st.keys().contacts(FormatOpenPGP, ""); len(l) != 0 {
		t.Fatalf("taken: %+v", l)
	}
}

// Keys kept in a vault of secretvault's that is not there yet: it is
// asked for, and secretvault does the work with the keys it keeps there —
// a key made for an address is made in that vault. comms-mail's own keys
// stay where they were, for when one of its places is chosen again.
func TestKeysInARequestedSecretVaultVault(t *testing.T) {
	smtp := startFakeSMTP(t)
	st, sv := sendingStore(t, smtp)
	if err := st.UseKeys(FormatOpenPGP, StoreEncrypted, "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MakePGPKey("ada@example.com"); err != nil {
		t.Fatal(err)
	}
	own := st.keys().own(FormatOpenPGP, "ada@example.com")[0].ID
	if err := st.UseKeys(FormatOpenPGP, StoreSecretVault, "", "keys"); err != nil {
		t.Fatal(err)
	}
	if r := sv.Requests(); len(r) != 1 || r[0] != "keys" {
		t.Fatalf("asked for %v", r)
	}
	if p := st.keyPlace(FormatOpenPGP); p.Engine != EngineSecretVault || p.Vault != "keys" || !p.Ready {
		t.Fatalf("place %+v", p)
	}
	if _, err := st.MakePGPKey("ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if g := sv.Generates(); len(g) != 1 || g[0].Vault != "keys" {
		t.Fatalf("made %+v", g)
	}
	// Back to the encrypted file: comms-mail's own key is there still.
	if err := st.UseKeys(FormatOpenPGP, StoreEncrypted, "", ""); err != nil {
		t.Fatal(err)
	}
	if v := st.KeysView(FormatOpenPGP); v.Place.Engine != EngineOwn || len(v.Addresses) == 0 || v.Addresses[0].PGP == nil || v.Addresses[0].PGP.Fingerprint != own {
		t.Fatalf("back: %+v", v)
	}
	// Its private half too: it signs.
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true}), nil); err != nil {
		t.Fatal(err)
	}
	if got := smtp.delivered(); len(got) != 1 || !strings.Contains(got[0], "application/pgp-signature") {
		t.Fatalf("signed with comms-mail's key: %v", got)
	}
}
