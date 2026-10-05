package cms

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// BER becomes DER: indefinite lengths are given, lengths take their
// shortest form, and an OCTET STRING in pieces, nested pieces and empty
// ones too, is joined; a high tag number and everything else stay as they
// were.
func TestBERBecomesDER(t *testing.T) {
	ber := unhex(t, `
		30 80
		   06 03 2a 03 04
		   a0 80
		      24 80
		         04 02 01 02
		         24 06
		            04 01 03
		            04 01 04
		         04 00
		      00 00
		   00 00
		   31 81 03 02 01 05
		   9f 1f 01 aa
		00 00`)
	want := unhex(t, `
		30 16
		   06 03 2a 03 04
		   a0 06
		      04 04 01 02 03 04
		   31 03 02 01 05
		   9f 1f 01 aa`)
	got, err := toDER(ber)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got  %x\nwant %x", got, want)
	}

	p := pki(t)
	der, err := Sign(testContent, p.rsa.cert, p.rsa.key, SignOptions{Chain: certs(p.ca)})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := toDER(der); err != nil || !bytes.Equal(got, der) {
		t.Fatalf("DER changed: %v", err)
	}
}

// An OCTET STRING in half a million empty pieces, as a hostile message
// could send, is joined with one allocation, the output: rewriting keeps
// nothing per element.
func TestManyPiecesCostNoMemory(t *testing.T) {
	ber := append([]byte{0x24, 0x80}, bytes.Repeat([]byte{0x04, 0x00}, 500_000)...)
	ber = append(ber, 0x00, 0x00)
	var der []byte
	allocs := testing.AllocsPerRun(1, func() {
		var err error
		if der, err = toDER(ber); err != nil {
			t.Fatal(err)
		}
	})
	if !bytes.Equal(der, []byte{0x04, 0x00}) || allocs > 1 {
		t.Fatalf("%x, %v allocations", der, allocs)
	}
}

// Malformed BER is an error, never a panic or a guess.
func TestMalformedBERIsAnError(t *testing.T) {
	deep := bytes.Repeat([]byte{0x30, 0x80}, maxDepth+2)
	deep = append(deep, make([]byte, 2*(maxDepth+2))...)
	for name, ber := range map[string][]byte{
		"empty":                       nil,
		"no end-of-contents":          unhex(t, "30 80 02 01 01"),
		"primitive without a length":  unhex(t, "04 80 01 00 00"),
		"a piece that is no OCTET":    unhex(t, "24 03 02 01 01"),
		"a length beyond the end":     unhex(t, "30 05 02 01 01"),
		"a long length beyond it":     unhex(t, "30 84 ff ff ff ff 00"),
		"a length too large":          unhex(t, "30 85 01 00 00 00 00"),
		"a reserved length":           unhex(t, "30 ff"),
		"data after the end":          unhex(t, "30 03 02 01 01 ff"),
		"a stray end-of-contents":     unhex(t, "30 02 00 00"),
		"a truncated high tag":        unhex(t, "1f 81"),
		"a high tag number too large": unhex(t, "1f 81 81 81 81 81 01 00"),
		"nesting too deep":            deep,
	} {
		if got, err := toDER(ber); err == nil {
			t.Errorf("%s: %x, no error", name, got)
		}
	}
}

// fixtureContent is what testdata/test-only-streamed.p7m holds.
var fixtureContent = []byte(strings.Repeat("Test-only content of the streamed BER fixture of internal/cms.\r\n", 80))

// The streamed fixture, BER as openssl -stream writes it, as Outlook does,
// decrypts and verifies without openssl: its content is in pieces in both
// layers. It is a message signed and then encrypted, each with -stream,
// for a throwaway self-signed RSA certificate:
//
//	openssl req -x509 -newkey rsa:2048 -nodes -keyout key.pem -out cert.pem -days 36500 \
//	  -subj "/O=comms-mail tests/CN=internal-cms test only" \
//	  -addext "subjectAltName=email:test-only@example.org" \
//	  -addext "extendedKeyUsage=emailProtection" -addext "keyUsage=digitalSignature,keyEncipherment"
//	for i in $(seq 80); do printf 'Test-only content of the streamed BER fixture of internal/cms.\r\n'; done > content
//	openssl cms -sign -binary -nodetach -stream -in content -signer cert.pem -inkey key.pem -outform DER -out signed.ber
//	openssl cms -encrypt -binary -stream -aes256 -in signed.ber -outform DER -out test-only-streamed.p7m cert.pem
func TestStreamedFixtureOpensWithoutOpenSSL(t *testing.T) {
	cert, key := readFixtureIdentity(t)
	raw, err := os.ReadFile("testdata/test-only-streamed.p7m")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 2 || raw[1] != 0x80 {
		t.Fatal("the fixture is not BER with indefinite lengths")
	}
	if ct, err := ContentType(raw); err != nil || ct != "enveloped-data" {
		t.Fatalf("ContentType = %q, %v", ct, err)
	}
	signed, err := Decrypt(raw, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(signed) < 2 || signed[1] != 0x80 {
		t.Fatal("the signed message inside is not BER with indefinite lengths")
	}
	sd, err := Verify(signed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sd.Content, fixtureContent) {
		t.Fatalf("content: %d bytes", len(sd.Content))
	}
	if len(sd.Signers) != 1 || sd.Signers[0].Err != nil || !sd.Signers[0].Certificate.Equal(cert) {
		t.Fatalf("signers: %+v", sd.Signers)
	}
	if sd.Signers[0].SigningTime.IsZero() {
		t.Fatal("no signing time")
	}
}

func readFixtureIdentity(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(b)
		if block == nil {
			t.Fatalf("%s: no PEM block", name)
		}
		return block.Bytes
	}
	cert, err := x509.ParseCertificate(read("test-only-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	k, err := x509.ParsePKCS8PrivateKey(read("test-only-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	key, ok := k.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("the fixture key is a %T", k)
	}
	return cert, key
}
