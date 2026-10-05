package mailcore

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/internal/cms"
	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// testCA is a throwaway certificate authority, trusted for the test.
type testCA struct {
	cert *x509.Certificate
	key  crypto.Signer
}

var serial int64 = 100

func newTestCA(t *testing.T, trusted bool) testCA {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	serial++
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "Example Test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(2, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	if trusted {
		prev := smimeRoots
		smimeRoots = func() *x509.CertPool {
			p := x509.NewCertPool()
			p.AddCert(cert)
			return p
		}
		t.Cleanup(func() { smimeRoots = prev })
	}
	return testCA{cert, key}
}

// issue is a certificate for mail from addr, with key (RSA or ECDSA).
func (ca testCA) issue(t *testing.T, name, addr string, rsaKey bool) (*x509.Certificate, crypto.Signer) {
	t.Helper()
	var key crypto.Signer
	if rsaKey {
		key, _ = rsa.GenerateKey(rand.Reader, 2048)
	} else {
		key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	serial++
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name},
		EmailAddresses: []string{addr}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageKeyAgreement,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, key.Public(), ca.key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return cert, key
}

// ownSMIMEStore is an account whose S/MIME is comms-mail's own, its keys
// in the encrypted file, with Ada's .p12 brought in. Its OpenPGP is
// comms-mail's own too, with no keys, so S/MIME is the format mail goes
// in.
func ownSMIMEStore(t *testing.T, f *fakeSMTP, ca testCA) *LocalStore {
	t.Helper()
	st, _ := sendingStore(t, f)
	if err := st.UseKeys(FormatSMIME, StoreEncrypted, "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.UseKeys(FormatOpenPGP, StoreEncrypted, "", ""); err != nil {
		t.Fatal(err)
	}
	cert, key := ca.issue(t, "Ada", "ada@example.com", true)
	p12, err := pkcs12.Modern.Encode(key, cert, []*x509.Certificate{ca.cert}, "p12 secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ImportKeys(FormatSMIME, p12, nil); !errors.Is(err, errPassphraseNeeded) {
		t.Fatalf("without the password: %v", err)
	}
	if n, err := st.ImportKeys(FormatSMIME, p12, []byte("p12 secret")); err != nil || n != 1 {
		t.Fatalf("import: %d %v", n, err)
	}
	return st
}

// Your certificate comes from a .p12, opened with its password: listed
// for its address, its key where the keys are kept. Signed mail goes as
// multipart/signed and checks as yours; changed, it does not.
func TestOwnSMIMESigns(t *testing.T) {
	ca := newTestCA(t, true)
	smtp := startFakeSMTP(t)
	st := ownSMIMEStore(t, smtp, ca)
	v := st.KeysView(FormatSMIME)
	found := false
	for _, a := range v.Addresses {
		if a.Address == "ada@example.com" && len(a.SMIME) == 1 && strings.Contains(a.SMIME[0].Issuer, "Example Test CA") {
			found = true
		}
	}
	if !found {
		t.Fatalf("keys page: %+v", v.Addresses)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true}), nil); err != nil {
		t.Fatal(err)
	}
	got := smtp.delivered()
	if len(got) != 1 || !strings.Contains(got[0], "application/pkcs7-signature") || !strings.Contains(got[0], "blue door") {
		t.Fatalf("delivered:\n%s", got)
	}
	sec := st.ownInspect([]byte(got[0]), true, Message{From: "Ada <ada@example.com>"})
	if len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" || sec.Signatures[0].Trust != "verified" || sec.Signatures[0].Format != FormatSMIME {
		t.Fatalf("checked: %+v", sec)
	}
	bad := strings.Replace(got[0], "blue door", "red door", 1)
	if sec := st.ownInspect([]byte(bad), true, Message{From: "ada@example.com"}); sec.Signatures[0].Status != "bad" {
		t.Fatalf("changed: %+v", sec.Signatures)
	}
	// The backup opens with its passphrase, and is the same certificate.
	data, _, err := st.KeyBackup(FormatSMIME, v.Addresses[0].SMIME[0].SHA256, []byte("a backup passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if _, cert, _, err := pkcs12.DecodeChain(data, "a backup passphrase"); err != nil || certID(cert) != v.Addresses[0].SMIME[0].SHA256 {
		t.Fatalf("backup: %v", err)
	}
}

// Someone's signed mail, from an authority this computer trusts, is
// verified, and their certificate kept: mail to them can then be
// encrypted, signed inside, the real subject inside and "..." outside.
// They open it with their key; the writer's copy opens too.
func TestOwnSMIMEEncryptsToWhoSigned(t *testing.T) {
	ca := newTestCA(t, true)
	smtp := startFakeSMTP(t)
	st := ownSMIMEStore(t, smtp, ca)
	bobCert, bobKey := ca.issue(t, "Bob", "bob@example.org", false)
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Encrypt: true}), nil); err == nil || !strings.Contains(err.Error(), "bob@example.org") {
		t.Fatalf("without Bob's certificate: %v", err)
	}
	// Bob's signed message arrives.
	entity := []byte("Content-Type: text/plain; charset=utf-8\r\n\r\nNoon works.\r\n")
	der, err := cms.Sign(entity, bobCert, bobKey, cms.SignOptions{Detached: true, Chain: []*x509.Certificate{ca.cert}})
	if err != nil {
		t.Fatal(err)
	}
	var part bytes.Buffer
	part.WriteString("Content-Type: application/pkcs7-signature\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	writeBase64Lines(&part, der)
	ctype, body := signedMultipart(entity, "application/pkcs7-signature", "sha-256", part.Bytes())
	raw := wrapMessage([]mimeField{{name: "From", raw: "From: Bob <bob@example.org>\r\n"}}, false, ctype, nil, body)
	sec := st.ownInspect(raw, true, Message{From: "Bob <bob@example.org>"})
	if len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" || sec.Signatures[0].Trust != "verified" || sec.Signatures[0].Level != "certificate" {
		t.Fatalf("Bob's: %+v", sec.Signatures)
	}
	if l := st.keys().contacts(FormatSMIME, "bob@example.org"); len(l) != 1 || l[0].Source != "signed" {
		t.Fatalf("kept %+v", l)
	}
	// The same, claimed by someone else.
	if sec := st.ownInspect(raw, true, Message{From: "mallory@example.com"}); sec.Signatures[0].Trust != "suspicious" {
		t.Fatalf("not his: %+v", sec.Signatures)
	}

	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	got := crlf([]byte(smtp.delivered()[0]))
	if !bytes.Contains(got, []byte("smime-type=enveloped-data")) || bytes.Contains(got, []byte("blue door")) || !bytes.Contains(got, []byte("Subject: ...")) {
		t.Fatalf("delivered:\n%s", got)
	}
	fields, b := splitEntity(got)
	enc, _ := decodedBody(fields, b)
	inner, err := cms.Decrypt(enc, bobCert, bobKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(inner, []byte("multipart/signed")) || !bytes.Contains(inner, []byte("blue door")) || !bytes.Contains(inner, []byte("Subject: The plan")) {
		t.Fatalf("Bob reads:\n%s", inner)
	}
	sec = st.ownInspect(got, true, Message{From: "Ada <ada@example.com>"})
	if !sec.Decrypted || sec.Subject != "The plan" || sec.Content == nil || !strings.Contains(sec.Content.Body, "blue door") ||
		len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" {
		t.Fatalf("Ada reads: %+v", sec)
	}
}

// A certificate from an authority this computer does not trust is said
// so, and not kept.
func TestOwnSMIMEUntrustedAuthority(t *testing.T) {
	trusted := newTestCA(t, true)
	other := newTestCA(t, false)
	smtp := startFakeSMTP(t)
	st := ownSMIMEStore(t, smtp, trusted)
	cert, key := other.issue(t, "Eve", "eve@example.net", false)
	entity := []byte("Content-Type: text/plain\r\n\r\nTrust me.\r\n")
	der, _ := cms.Sign(entity, cert, key, cms.SignOptions{Detached: true})
	var part bytes.Buffer
	part.WriteString("Content-Type: application/pkcs7-signature\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	writeBase64Lines(&part, der)
	ctype, body := signedMultipart(entity, "application/pkcs7-signature", "sha-256", part.Bytes())
	raw := wrapMessage([]mimeField{{name: "From", raw: "From: eve@example.net\r\n"}}, false, ctype, nil, body)
	sec := st.ownInspect(raw, true, Message{From: "eve@example.net"})
	if len(sec.Signatures) != 1 || sec.Signatures[0].Status != "untrusted-root" {
		t.Fatalf("Eve's: %+v", sec.Signatures)
	}
	if l := st.keys().contacts(FormatSMIME, "eve@example.net"); len(l) != 0 {
		t.Fatalf("kept %+v", l)
	}
}

// OpenSSL reads what comms-mail's own S/MIME writes, and comms-mail reads
// what OpenSSL writes: a signature each way, and encryption each way.
func TestOwnSMIMEWithOpenSSL(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl is not installed")
	}
	ca := newTestCA(t, true)
	smtp := startFakeSMTP(t)
	st := ownSMIMEStore(t, smtp, ca)
	bobCert, bobKey := ca.issue(t, "Bob", "bob@example.org", true)
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(bobKey)
	bobKeyPath := write("bob.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	bobCertPath := write("bob.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bobCert.Raw}))
	caPath := write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw}))
	adaCert := parseCerts([]byte(st.keys().own(FormatSMIME, "ada@example.com")[0].Public))[0]
	adaCertPath := write("ada.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: adaCert.Raw}))
	openssl := func(stdin []byte, args ...string) []byte {
		t.Helper()
		cmd := exec.Command("openssl", args...)
		cmd.Stdin = bytes.NewReader(stdin)
		out, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				t.Fatalf("openssl %v: %v\n%s", args, err, ee.Stderr)
			}
			t.Fatal(err)
		}
		return out
	}
	if _, err := st.ImportKeys(FormatSMIME, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bobCert.Raw}), nil); err != nil {
		t.Fatal(err)
	}

	// comms-mail signs, OpenSSL checks against the CA.
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true}), nil); err != nil {
		t.Fatal(err)
	}
	openssl([]byte(smtp.delivered()[0]), "smime", "-verify", "-CAfile", caPath, "-purpose", "any")

	// comms-mail encrypts to Bob, OpenSSL opens with his key.
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	if inner := openssl([]byte(smtp.delivered()[1]), "smime", "-decrypt", "-recip", bobCertPath, "-inkey", bobKeyPath); !bytes.Contains(inner, []byte("blue door")) {
		t.Fatalf("OpenSSL reads:\n%s", inner)
	}

	// OpenSSL signs as Bob, then encrypts to Ada; comms-mail opens and checks.
	content := []byte("Content-Type: text/plain; charset=utf-8\r\n\r\nThe key is under the mat.\r\n")
	signed := openssl(content, "smime", "-sign", "-signer", bobCertPath, "-inkey", bobKeyPath, "-certfile", caPath)
	enveloped := openssl(signed, "smime", "-encrypt", "-aes256", "-from", "bob@example.org", "-to", "ada@example.com", "-subject", "Mat", adaCertPath)
	sec := st.ownInspect(enveloped, true, Message{From: "bob@example.org"})
	if !sec.Decrypted || sec.Content == nil || !strings.Contains(sec.Content.Body, "under the mat") ||
		len(sec.Signatures) != 1 || sec.Signatures[0].Status != "valid" || sec.Signatures[0].Trust != "verified" {
		t.Fatalf("from OpenSSL: %+v", sec)
	}
}

// With both formats comms-mail's own, mail goes in the first the
// recipients can read: OpenPGP to someone with an OpenPGP key, S/MIME to
// someone with only a certificate.
func TestOwnEnginesPickTheFormat(t *testing.T) {
	ca := newTestCA(t, true)
	smtp := startFakeSMTP(t)
	st := ownSMIMEStore(t, smtp, ca)
	if _, err := st.MakePGPKey("ada@example.com"); err != nil {
		t.Fatal(err)
	}
	bobCert, _ := ca.issue(t, "Bob", "bob@example.org", false)
	if _, err := st.ImportKeys(FormatSMIME, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bobCert.Raw}), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	if got := smtp.delivered()[0]; !strings.Contains(got, "application/pkcs7-mime") {
		t.Fatalf("to Bob's certificate:\n%s", got)
	}
	if _, err := st.ImportKeys(FormatOpenPGP, armoredPublic(t, mustEntity(t, "bob@example.org")), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	if got := smtp.delivered()[1]; !strings.Contains(got, "multipart/encrypted") {
		t.Fatalf("to Bob's OpenPGP key:\n%s", got)
	}
	// Signing alone goes in the first format with a key for From.
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{SignIfKey: true}), nil); err != nil {
		t.Fatal(err)
	}
	if got := smtp.delivered()[2]; !strings.Contains(got, "application/pgp-signature") {
		t.Fatalf("signed:\n%s", got)
	}
}
