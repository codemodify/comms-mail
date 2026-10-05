package cms

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests check this package against the openssl command, the
// reference every S/MIME program is tested against. They are skipped
// where OpenSSL 3 or later is not installed (LibreSSL's openssl lacks
// AES-GCM and the ECDH options in cms); testdata's fixture keeps BER
// covered there.

func needOpenSSL(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl is not installed")
	}
	out, err := exec.Command("openssl", "version").Output()
	var major int
	if _, scanErr := fmt.Sscanf(string(out), "OpenSSL %d.", &major); err != nil || scanErr != nil || major < 3 {
		t.Skipf("these tests need OpenSSL 3 or later, not %q", strings.TrimSpace(string(out)))
	}
}

func openssl(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("openssl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("openssl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// opensslFiles are the files an openssl test works with, in a directory
// of its own: the content, and PEM certificates and keys by certificate.
type opensslFiles struct {
	dir, content string
	cert, key    map[*x509.Certificate]string
}

// writeOpenSSLFiles writes content and each identity's certificate and
// key.
func writeOpenSSLFiles(t *testing.T, content []byte, ids ...identity) opensslFiles {
	t.Helper()
	f := opensslFiles{
		dir:  t.TempDir(),
		cert: map[*x509.Certificate]string{},
		key:  map[*x509.Certificate]string{},
	}
	f.content = f.write(t, "content", content)
	for _, id := range ids {
		pkcs8, err := x509.MarshalPKCS8PrivateKey(id.key)
		if err != nil {
			t.Fatal(err)
		}
		name := id.cert.Subject.CommonName
		f.cert[id.cert] = f.write(t, name+".cert.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: id.cert.Raw}))
		f.key[id.cert] = f.write(t, name+".key.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	}
	return f
}

func (f opensslFiles) write(t *testing.T, name string, b []byte) string {
	t.Helper()
	path := filepath.Join(f.dir, name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f opensslFiles) read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// opensslContent is long enough that openssl -stream writes it in pieces.
var opensslContent = bytes.Repeat(testContent, 300)

// What openssl cms -sign writes verifies here: RSA PKCS #1 v1.5 and PSS,
// ECDSA and Ed25519; SHA-1 to SHA-512; attached and detached; without
// signed attributes; the signer named by key identifier; the chain
// included; and -stream's BER (openssl streams DER output with the
// content in it, detached or not, and writes a detached smime.p7s in
// DER). A message without the signer's certificate is read, and its
// signer says the certificate is missing. Changed content fails each one.
func TestOpenSSLSignaturesVerify(t *testing.T) {
	needOpenSSL(t)
	p := pki(t)
	f := writeOpenSSLFiles(t, opensslContent, p.ca, p.rsa, p.p256, p.p384, p.ed25519)
	changed := bytes.Clone(opensslContent)
	changed[len(changed)/2] ^= 1
	for _, c := range []struct {
		name     string
		signer   identity
		args     []string
		detached bool
		digest   crypto.Hash
		ber      bool
		certs    int // in the message, 0 for none
	}{
		{"RSA attached", p.rsa, []string{"-nodetach"}, false, crypto.SHA256, false, 1},
		{"RSA detached", p.rsa, nil, true, crypto.SHA256, false, 1},
		{"ECDSA P-256 attached", p.p256, []string{"-nodetach"}, false, crypto.SHA256, false, 1},
		{"ECDSA P-384 SHA-384 detached", p.p384, []string{"-md", "sha384"}, true, crypto.SHA384, false, 1},
		{"ECDSA P-256 SHA-512", p.p256, []string{"-nodetach", "-md", "sha512"}, false, crypto.SHA512, false, 1},
		{"RSA SHA-1", p.rsa, []string{"-nodetach", "-md", "sha1"}, false, crypto.SHA1, false, 1},
		{"RSA SHA-224", p.rsa, []string{"-nodetach", "-md", "sha224"}, false, crypto.SHA224, false, 1},
		{"RSA SHA-512", p.rsa, []string{"-nodetach", "-md", "sha512"}, false, crypto.SHA512, false, 1},
		{"RSASSA-PSS", p.rsa, []string{"-nodetach", "-keyopt", "rsa_padding_mode:pss"}, false, crypto.SHA256, false, 1},
		{"RSASSA-PSS SHA-384 detached", p.rsa, []string{"-md", "sha384", "-keyopt", "rsa_padding_mode:pss", "-keyopt", "rsa_pss_saltlen:20"}, true, crypto.SHA384, false, 1},
		{"Ed25519", p.ed25519, []string{"-nodetach", "-md", "sha512"}, false, crypto.SHA512, false, 1},
		{"no signed attributes", p.rsa, []string{"-nodetach", "-noattr"}, false, crypto.SHA256, false, 1},
		{"no signed attributes ECDSA detached", p.p256, []string{"-noattr"}, true, crypto.SHA256, false, 1},
		{"signer by key identifier", p.p256, []string{"-nodetach", "-keyid"}, false, crypto.SHA256, false, 1},
		{"with the chain", p.rsa, []string{"-nodetach", "-certfile", f.cert[p.ca.cert]}, false, crypto.SHA256, false, 2},
		{"streamed BER attached", p.rsa, []string{"-nodetach", "-stream"}, false, crypto.SHA256, true, 1},
		{"streamed BER ECDSA", p.p256, []string{"-nodetach", "-stream"}, false, crypto.SHA256, true, 1},
		{"without certificates", p.rsa, []string{"-nodetach", "-nocerts"}, false, crypto.SHA256, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := filepath.Join(f.dir, "signed.p7m")
			args := append([]string{"cms", "-sign", "-binary", "-in", f.content, "-outform", "DER", "-out", out,
				"-signer", f.cert[c.signer.cert], "-inkey", f.key[c.signer.cert]}, c.args...)
			openssl(t, args...)
			der := f.read(t, out)
			if c.ber && der[1] != 0x80 {
				t.Fatal("openssl did not write BER")
			}
			var given []byte
			if c.detached {
				given = opensslContent
			}
			sd, err := Verify(der, given)
			if err != nil {
				t.Fatal(err)
			}
			if c.detached && sd.Content != nil || !c.detached && !bytes.Equal(sd.Content, opensslContent) {
				t.Fatalf("content: %d bytes", len(sd.Content))
			}
			if len(sd.Certificates) != c.certs || len(sd.Signers) != 1 {
				t.Fatalf("%d certificates, %d signers", len(sd.Certificates), len(sd.Signers))
			}
			s := sd.Signers[0]
			if c.certs == 0 {
				if s.Certificate != nil || s.Err == nil {
					t.Fatalf("no certificate: %+v", s)
				}
				return
			}
			if s.Err != nil {
				t.Fatal(s.Err)
			}
			if !s.Certificate.Equal(c.signer.cert) || s.Digest != c.digest {
				t.Fatalf("signer %v, digest %v", s.Certificate.Subject, s.Digest)
			}
			if !strings.Contains(c.name, "no signed attributes") && s.SigningTime.IsZero() {
				t.Fatal("no signing time")
			}
			if c.detached {
				sd, err = Verify(der, changed)
			} else {
				i := bytes.Index(der, opensslContent[:64])
				tampered := bytes.Clone(der)
				tampered[i+32] ^= 1
				sd, err = Verify(tampered, nil)
			}
			if err != nil || sd.Signers[0].Err == nil {
				t.Fatalf("changed content: %v, %v", err, sd.Signers[0].Err)
			}
		})
	}
}

// What Sign makes, openssl cms -verify accepts: RSA and ECDSA, attached
// and detached, with the chain, and gives back the content.
func TestOurSignaturesVerifyInOpenSSL(t *testing.T) {
	needOpenSSL(t)
	p := pki(t)
	f := writeOpenSSLFiles(t, opensslContent)
	for _, c := range []struct {
		name   string
		signer identity
		opts   SignOptions
	}{
		{"RSA attached", p.rsa, SignOptions{}},
		{"RSA detached", p.rsa, SignOptions{Detached: true}},
		{"ECDSA P-256 attached", p.p256, SignOptions{}},
		{"ECDSA P-384 SHA-384 detached", p.p384, SignOptions{Detached: true, Digest: crypto.SHA384}},
		{"ECDSA P-521 SHA-512", p.p521, SignOptions{Digest: crypto.SHA512}},
		{"RSA SHA-512 with the chain", p.rsa, SignOptions{Digest: crypto.SHA512, Chain: certs(p.ca)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			der, err := Sign(opensslContent, c.signer.cert, c.signer.key, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			in := f.write(t, "signed.p7m", der)
			out := filepath.Join(f.dir, "verified")
			args := []string{"cms", "-verify", "-binary", "-noverify", "-inform", "DER", "-in", in, "-out", out}
			if c.opts.Detached {
				args = append(args, "-content", f.content)
			}
			openssl(t, args...)
			if !bytes.Equal(f.read(t, out), opensslContent) {
				t.Fatal("openssl gave other content")
			}
		})
	}
}

// What openssl cms -encrypt makes, Decrypt opens: RSA key transport with
// PKCS #1 v1.5 and OAEP (default and SHA-256 parameters); ECDH with each
// KDF digest, cofactor mode, and each AES wrap; AES-CBC of each size,
// DES-EDE3-CBC, AES-GCM of each size (AuthEnvelopedData); recipients by
// key identifier; several recipients; and -stream's BER.
func TestOpenSSLEncryptionDecrypts(t *testing.T) {
	needOpenSSL(t)
	p := pki(t)
	f := writeOpenSSLFiles(t, opensslContent, p.rsa, p.p256, p.p384, p.p521)
	keys := map[*x509.Certificate]crypto.PrivateKey{
		p.rsa.cert: p.rsaKey, p.p256.cert: p.p256Key, p.p384.cert: p.p384Key, p.p521.cert: p.p521Key,
	}
	type recipient struct {
		id      identity
		keyopts []string
	}
	for _, c := range []struct {
		name   string
		cipher []string
		to     []recipient
		kind   string
		ber    bool
	}{
		{"AES-256-CBC to RSA", []string{"-aes256"}, []recipient{{p.rsa, nil}}, "enveloped-data", false},
		{"AES-128-CBC to RSA", []string{"-aes128"}, []recipient{{p.rsa, nil}}, "enveloped-data", false},
		{"AES-192-CBC to RSA", []string{"-aes192"}, []recipient{{p.rsa, nil}}, "enveloped-data", false},
		{"DES-EDE3-CBC to RSA", []string{"-des3"}, []recipient{{p.rsa, nil}}, "enveloped-data", false},
		{"RSAES-OAEP", []string{"-aes256"}, []recipient{{p.rsa, []string{"rsa_padding_mode:oaep"}}}, "enveloped-data", false},
		{"RSAES-OAEP SHA-256", []string{"-aes256"}, []recipient{{p.rsa, []string{"rsa_padding_mode:oaep", "rsa_oaep_md:sha256", "rsa_mgf1_md:sha256"}}}, "enveloped-data", false},
		{"ECDH P-256 SHA-1 KDF, AES-256 wrap", []string{"-aes256"}, []recipient{{p.p256, nil}}, "enveloped-data", false},
		{"ECDH P-256 SHA-256 KDF, AES-128 wrap", []string{"-aes128"}, []recipient{{p.p256, []string{"ecdh_kdf_md:sha256"}}}, "enveloped-data", false},
		{"ECDH P-384 SHA-384 KDF, AES-192 wrap", []string{"-aes192"}, []recipient{{p.p384, []string{"ecdh_kdf_md:sha384"}}}, "enveloped-data", false},
		{"ECDH P-384 SHA-224 KDF", []string{"-aes256"}, []recipient{{p.p384, []string{"ecdh_kdf_md:sha224"}}}, "enveloped-data", false},
		{"ECDH P-521 SHA-512 KDF", []string{"-aes256"}, []recipient{{p.p521, []string{"ecdh_kdf_md:sha512"}}}, "enveloped-data", false},
		{"ECDH cofactor mode", []string{"-aes256"}, []recipient{{p.p256, []string{"ecdh_cofactor_mode:1", "ecdh_kdf_md:sha256"}}}, "enveloped-data", false},
		{"AES-128-GCM to RSA", []string{"-aes-128-gcm"}, []recipient{{p.rsa, nil}}, "authEnveloped-data", false},
		{"AES-192-GCM to EC", []string{"-aes-192-gcm"}, []recipient{{p.p256, nil}}, "authEnveloped-data", false},
		{"AES-256-GCM to RSA and EC", []string{"-aes-256-gcm"}, []recipient{{p.rsa, nil}, {p.p384, nil}}, "authEnveloped-data", false},
		{"recipients by key identifier", []string{"-aes256", "-keyid"}, []recipient{{p.rsa, nil}, {p.p256, nil}}, "enveloped-data", false},
		{"several recipients", []string{"-aes256"}, []recipient{{p.rsa, nil}, {p.p256, nil}, {p.p384, nil}, {p.p521, nil}}, "enveloped-data", false},
		{"streamed BER to RSA", []string{"-aes256", "-stream"}, []recipient{{p.rsa, nil}}, "enveloped-data", true},
		{"streamed BER to EC", []string{"-aes128", "-stream"}, []recipient{{p.p256, []string{"ecdh_kdf_md:sha256"}}}, "enveloped-data", true},
		{"streamed BER AES-GCM", []string{"-aes-128-gcm", "-stream"}, []recipient{{p.rsa, nil}, {p.p256, nil}}, "authEnveloped-data", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := filepath.Join(f.dir, "encrypted.p7m")
			args := append([]string{"cms", "-encrypt", "-binary", "-in", f.content, "-outform", "DER", "-out", out}, c.cipher...)
			for _, r := range c.to {
				args = append(args, "-recip", f.cert[r.id.cert])
				for _, o := range r.keyopts {
					args = append(args, "-keyopt", o)
				}
			}
			openssl(t, args...)
			der := f.read(t, out)
			if c.ber && der[1] != 0x80 {
				t.Fatal("openssl did not write BER")
			}
			if kind, err := ContentType(der); err != nil || kind != c.kind {
				t.Fatalf("ContentType = %q, %v", kind, err)
			}
			for _, r := range c.to {
				got, err := Decrypt(der, r.id.cert, keys[r.id.cert])
				if err != nil {
					t.Fatalf("%s: %v", certName(r.id.cert), err)
				}
				if !bytes.Equal(got, opensslContent) {
					t.Fatalf("%s: other content", certName(r.id.cert))
				}
			}
			if _, err := Decrypt(der, p.stranger.cert, p.strangerKey); !errors.Is(err, ErrNoRecipient) {
				t.Fatalf("stranger: %v", err)
			}
		})
	}
}

// What Encrypt makes, openssl cms -decrypt opens, for an RSA recipient,
// an EC one on each curve, and all of them in one message.
func TestOurEncryptionDecryptsInOpenSSL(t *testing.T) {
	needOpenSSL(t)
	p := pki(t)
	f := writeOpenSSLFiles(t, opensslContent, p.rsa, p.p256, p.p384, p.p521)
	for _, c := range []struct {
		name string
		to   []identity
	}{
		{"RSA", []identity{p.rsa}},
		{"ECDH P-256", []identity{p.p256}},
		{"ECDH P-384", []identity{p.p384}},
		{"ECDH P-521", []identity{p.p521}},
		{"all of them", []identity{p.rsa, p.p256, p.p384, p.p521}},
	} {
		t.Run(c.name, func(t *testing.T) {
			der, err := Encrypt(opensslContent, certs(c.to...))
			if err != nil {
				t.Fatal(err)
			}
			in := f.write(t, "encrypted.p7m", der)
			for _, r := range c.to {
				out := filepath.Join(f.dir, "decrypted")
				openssl(t, "cms", "-decrypt", "-binary", "-inform", "DER", "-in", in,
					"-recip", f.cert[r.cert], "-inkey", f.key[r.cert], "-out", out)
				if !bytes.Equal(f.read(t, out), opensslContent) {
					t.Fatalf("%s: openssl gave other content", certName(r.cert))
				}
			}
		})
	}
}
