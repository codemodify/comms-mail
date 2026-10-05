package cms

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"
)

// identity is a test certificate and its key.
type identity struct {
	cert *x509.Certificate
	key  crypto.Signer
}

// testPKI is a throwaway CA and the people it certifies, made in memory
// once per test run.
type testPKI struct {
	ca                        identity
	rsa, p256, p384, p521     identity
	ed25519                   identity
	stranger                  identity // no message is for them
	rsaKey, strangerKey       *rsa.PrivateKey
	p256Key, p384Key, p521Key *ecdsa.PrivateKey
}

var makePKI = sync.OnceValues(func() (*testPKI, error) {
	p := &testPKI{}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if p.ca, err = issue(identity{}, caKey, "Test CA"); err != nil {
		return nil, err
	}
	if p.rsaKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		return nil, err
	}
	if p.strangerKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		return nil, err
	}
	for _, k := range []struct {
		key   **ecdsa.PrivateKey
		curve elliptic.Curve
	}{{&p.p256Key, elliptic.P256()}, {&p.p384Key, elliptic.P384()}, {&p.p521Key, elliptic.P521()}} {
		if *k.key, err = ecdsa.GenerateKey(k.curve, rand.Reader); err != nil {
			return nil, err
		}
	}
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	for _, l := range []struct {
		id   *identity
		key  crypto.Signer
		name string
	}{
		{&p.rsa, p.rsaKey, "rsa"},
		{&p.p256, p.p256Key, "p256"},
		{&p.p384, p.p384Key, "p384"},
		{&p.p521, p.p521Key, "p521"},
		{&p.ed25519, edKey, "ed25519"},
		{&p.stranger, p.strangerKey, "stranger"},
	} {
		if *l.id, err = issue(p.ca, l.key, l.name); err != nil {
			return nil, err
		}
	}
	return p, nil
})

func pki(t *testing.T) *testPKI {
	t.Helper()
	p, err := makePKI()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// issue makes a certificate for key, issued by ca, or self-signed as a CA
// when ca is empty: an email address, emailProtection, and a subject key
// identifier.
func issue(ca identity, key crypto.Signer, name string) (identity, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return identity{}, err
	}
	spki, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return identity{}, err
	}
	var info struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(spki, &info); err != nil {
		return identity{}, err
	}
	ski := sha1.Sum(info.PublicKey.Bytes)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name, Organization: []string{"comms-mail tests"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		SubjectKeyId: ski[:],
	}
	parent, signer := tmpl, key
	if ca.cert == nil {
		tmpl.IsCA, tmpl.BasicConstraintsValid = true, true
		tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
	} else {
		parent, signer = ca.cert, ca.key
		tmpl.EmailAddresses = []string{name + "@example.org"}
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature
		switch key.Public().(type) {
		case *rsa.PublicKey:
			tmpl.KeyUsage |= x509.KeyUsageKeyEncipherment
		case *ecdsa.PublicKey:
			tmpl.KeyUsage |= x509.KeyUsageKeyAgreement
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, key.Public(), signer)
	if err != nil {
		return identity{}, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return identity{}, err
	}
	return identity{cert, key}, nil
}

func certs(ids ...identity) []*x509.Certificate {
	var out []*x509.Certificate
	for _, id := range ids {
		out = append(out, id.cert)
	}
	return out
}

// testContent is a MIME entity with what line-ending conversion would
// change, and more: CRLF and bare LF, trailing spaces, a NUL, non-ASCII.
var testContent = []byte("Content-Type: text/plain; charset=utf-8\r\n\r\nHello, Grüße \r\nbare LF\nNUL \x00 end\r\n")

// A message signed by RSA or ECDSA keys, with each digest, verifies: its
// content, its certificates (the signer's and the chain), and the
// signer's certificate, signing time and digest are read back.
func TestSignedMessageVerifies(t *testing.T) {
	p := pki(t)
	when := time.Date(2026, 10, 4, 12, 30, 15, 0, time.UTC)
	for _, c := range []struct {
		name   string
		id     identity
		digest crypto.Hash
		want   crypto.Hash
	}{
		{"RSA", p.rsa, 0, crypto.SHA256},
		{"RSA SHA-512", p.rsa, crypto.SHA512, crypto.SHA512},
		{"ECDSA P-256", p.p256, 0, crypto.SHA256},
		{"ECDSA P-384 SHA-384", p.p384, crypto.SHA384, crypto.SHA384},
		{"ECDSA P-521 SHA-512", p.p521, crypto.SHA512, crypto.SHA512},
	} {
		t.Run(c.name, func(t *testing.T) {
			der, err := Sign(testContent, c.id.cert, c.id.key, SignOptions{
				Chain: certs(p.ca), SigningTime: when, Digest: c.digest,
			})
			if err != nil {
				t.Fatal(err)
			}
			if ct, err := ContentType(der); err != nil || ct != "signed-data" {
				t.Fatalf("ContentType = %q, %v", ct, err)
			}
			sd, err := Verify(der, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sd.Content, testContent) {
				t.Fatalf("content %q", sd.Content)
			}
			if len(sd.Certificates) != 2 || !sd.Certificates[0].Equal(c.id.cert) || !sd.Certificates[1].Equal(p.ca.cert) {
				t.Fatalf("certificates: %d", len(sd.Certificates))
			}
			if len(sd.Signers) != 1 {
				t.Fatalf("%d signers", len(sd.Signers))
			}
			s := sd.Signers[0]
			if s.Err != nil {
				t.Fatal(s.Err)
			}
			if s.Certificate == nil || !s.Certificate.Equal(c.id.cert) {
				t.Fatal("the signer's certificate was not found")
			}
			if !s.SigningTime.Equal(when) || s.Digest != c.want {
				t.Fatalf("signing time %v, digest %v", s.SigningTime, s.Digest)
			}
		})
	}
}

// A detached signature (multipart/signed) carries no content and verifies
// against the content it is given; given none, the signer says so.
func TestDetachedSignatureVerifiesAgainstTheContentGiven(t *testing.T) {
	p := pki(t)
	for _, id := range []identity{p.rsa, p.p256} {
		der, err := Sign(testContent, id.cert, id.key, SignOptions{Detached: true})
		if err != nil {
			t.Fatal(err)
		}
		sd, err := Verify(der, testContent)
		if err != nil {
			t.Fatal(err)
		}
		if sd.Content != nil || len(sd.Signers) != 1 || sd.Signers[0].Err != nil {
			t.Fatalf("content %q, signers %+v", sd.Content, sd.Signers)
		}
		if time.Since(sd.Signers[0].SigningTime).Abs() > time.Minute {
			t.Fatalf("signing time %v, not now", sd.Signers[0].SigningTime)
		}
		sd, err = Verify(der, nil)
		if err != nil || !errors.Is(sd.Signers[0].Err, errNoContent) {
			t.Fatalf("no content: %v, %v", err, sd.Signers[0].Err)
		}
	}
}

// Content changed after signing, or a changed signature, makes that
// signer's Err say so; the message itself is still read without error.
func TestChangedContentIsABadSignatureNotABadMessage(t *testing.T) {
	p := pki(t)
	changed := bytes.Replace(testContent, []byte("Hello"), []byte("Jello"), 1)

	detached, err := Sign(testContent, p.p256.cert, p.p256.key, SignOptions{Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	sd, err := Verify(detached, changed)
	if err != nil || !errors.Is(sd.Signers[0].Err, errContentChanged) {
		t.Fatalf("detached: %v, %v", err, sd.Signers[0].Err)
	}

	attached, err := Sign(testContent, p.rsa.cert, p.rsa.key, SignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(attached, []byte("Hello"))
	tampered := bytes.Clone(attached)
	tampered[i] = 'J'
	sd, err = Verify(tampered, nil)
	if err != nil || !errors.Is(sd.Signers[0].Err, errContentChanged) || !bytes.Equal(sd.Content, changed) {
		t.Fatalf("attached: %v, %v", err, sd.Signers[0].Err)
	}

	tampered = bytes.Clone(attached)
	tampered[len(tampered)-1] ^= 1 // the signature is the last thing in the message
	sd, err = Verify(tampered, nil)
	if err != nil || !errors.Is(sd.Signers[0].Err, errBadSignature) {
		t.Fatalf("signature: %v, %v", err, sd.Signers[0].Err)
	}
}

// Sign refuses a key that is not the certificate's, SHA-1, and keys it
// does not sign with.
func TestSignRefusesWhatItCannotSign(t *testing.T) {
	p := pki(t)
	if _, err := Sign(testContent, p.rsa.cert, p.p256.key, SignOptions{}); err == nil {
		t.Fatal("signed with another certificate's key")
	}
	if _, err := Sign(testContent, p.rsa.cert, p.rsa.key, SignOptions{Digest: crypto.SHA1}); err == nil {
		t.Fatal("signed with SHA-1")
	}
	if _, err := Sign(testContent, p.ed25519.cert, p.ed25519.key, SignOptions{}); err == nil {
		t.Fatal("signed with Ed25519")
	}
}

// A message encrypted to RSA and EC recipients at once opens for each of
// them, with its content exactly as it was; so does one to each alone,
// and empty content.
func TestEncryptedMessageOpensForEveryRecipient(t *testing.T) {
	p := pki(t)
	keys := map[*x509.Certificate]crypto.PrivateKey{
		p.rsa.cert: p.rsaKey, p.p256.cert: p.p256Key, p.p384.cert: p.p384Key, p.p521.cert: p.p521Key,
	}
	all := certs(p.rsa, p.p256, p.p384, p.p521)
	messages := [][]*x509.Certificate{all}
	for _, c := range all {
		messages = append(messages, []*x509.Certificate{c})
	}
	for _, content := range [][]byte{testContent, {}} {
		for _, to := range messages {
			der, err := Encrypt(content, to)
			if err != nil {
				t.Fatal(err)
			}
			if ct, err := ContentType(der); err != nil || ct != "enveloped-data" {
				t.Fatalf("ContentType = %q, %v", ct, err)
			}
			for _, c := range to {
				got, err := Decrypt(der, c, keys[c])
				if err != nil {
					t.Fatalf("%s: %v", certName(c), err)
				}
				if !bytes.Equal(got, content) {
					t.Fatalf("%s: %q", certName(c), got)
				}
			}
		}
	}
}

// A message encrypted to others is ErrNoRecipient for a certificate that
// is not among them, whatever its key.
func TestMessageForSomeoneElseIsErrNoRecipient(t *testing.T) {
	p := pki(t)
	der, err := Encrypt(testContent, certs(p.rsa, p.p256))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(der, p.stranger.cert, p.strangerKey); !errors.Is(err, ErrNoRecipient) {
		t.Fatalf("stranger: %v", err)
	}
	if _, err := Decrypt(der, p.p384.cert, p.p384Key); !errors.Is(err, ErrNoRecipient) {
		t.Fatalf("p384: %v", err)
	}
	if _, err := Decrypt(der, p.rsa.cert, p.strangerKey); err == nil || errors.Is(err, ErrNoRecipient) {
		t.Fatalf("the recipient's certificate with another key: %v", err)
	}
}

// Encrypt refuses a recipient whose key it cannot encrypt to, and no
// recipients at all.
func TestEncryptRefusesKeysItCannotEncryptTo(t *testing.T) {
	p := pki(t)
	if _, err := Encrypt(testContent, certs(p.rsa, p.ed25519)); err == nil {
		t.Fatal("encrypted to Ed25519")
	}
	if _, err := Encrypt(testContent, nil); err == nil {
		t.Fatal("encrypted to nobody")
	}
}

// ContentType names each kind of message and says "other" of the rest;
// what is not a message is an error.
func TestContentTypeNamesTheMessage(t *testing.T) {
	for _, c := range []struct {
		der  []byte
		want string
	}{
		{tlv(0x30, oidDER(oidSignedData), tlv(0xa0, tlv(0x30))), "signed-data"},
		{tlv(0x30, oidDER(oidEnvelopedData), tlv(0xa0, tlv(0x30))), "enveloped-data"},
		{tlv(0x30, oidDER(oidAuthEnvelopedData), tlv(0xa0, tlv(0x30))), "authEnveloped-data"},
		{tlv(0x30, oidDER(oidData), tlv(0xa0, tlv(0x04, []byte("x")))), "other"},
		// BER: indefinite lengths
		{append(append([]byte{0x30, 0x80}, oidDER(oidEnvelopedData)...), 0xa0, 0x80, 0x30, 0x00, 0, 0, 0, 0), "enveloped-data"},
	} {
		if got, err := ContentType(c.der); err != nil || got != c.want {
			t.Errorf("%x: %q, %v; want %q", c.der, got, err, c.want)
		}
	}
	for _, bad := range [][]byte{nil, []byte("not a message"), {0x30, 0x03, 0x06, 0x01}} {
		if got, err := ContentType(bad); err == nil {
			t.Errorf("%x: %q, no error", bad, got)
		}
	}
}

// Verify of an encrypted message, or Decrypt of a signed one, is an
// error, not a signer or recipient that fails.
func TestTheWrongKindOfMessageIsAnError(t *testing.T) {
	p := pki(t)
	signed, err := Sign(testContent, p.rsa.cert, p.rsa.key, SignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	enveloped, err := Encrypt(testContent, certs(p.rsa))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(enveloped, nil); err == nil {
		t.Fatal("verified an encrypted message")
	}
	if _, err := Decrypt(signed, p.rsa.cert, p.rsaKey); err == nil {
		t.Fatal("decrypted a signed message")
	}
}

// Every truncation of a signed and an encrypted message is an error from
// each entry point, and none panics.
func TestTruncatedMessagesAreErrors(t *testing.T) {
	p := pki(t)
	signed, err := Sign(testContent, p.p256.cert, p.p256.key, SignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	enveloped, err := Encrypt(testContent, certs(p.rsa, p.p256))
	if err != nil {
		t.Fatal(err)
	}
	for _, der := range [][]byte{signed, enveloped} {
		for n := range len(der) {
			if _, err := ContentType(der[:n]); err == nil {
				t.Fatalf("ContentType of %d of %d bytes", n, len(der))
			}
			if _, err := Verify(der[:n], testContent); err == nil {
				t.Fatalf("Verify of %d of %d bytes", n, len(der))
			}
			if _, err := Decrypt(der[:n], p.p256.cert, p.p256Key); err == nil {
				t.Fatalf("Decrypt of %d of %d bytes", n, len(der))
			}
		}
	}
}
