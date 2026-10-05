package mailcore

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/internal/cms"
	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// comms-mail's own S/MIME (RFC 8551), on its own CMS (internal/cms):
// bringing in your certificate and its key from a .p12, checking and
// opening mail, signing and encrypting it. A signature is trusted as far
// as its certificate is: issued, through the certificates the message
// carries, by an authority this computer trusts, for the address the mail
// is from — or one you brought in yourself. Other people's certificates
// come with their signed mail, or from a file you bring in, and are what
// mail to them is encrypted to.

// certID is a certificate's SHA-256, as the key index names it.
func certID(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// oidEmailAddress is the subject's emailAddress attribute, where older
// certificates put the address.
var oidEmailAddress = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}

// certEmails are the addresses a certificate is for.
func certEmails(c *x509.Certificate) []string {
	var out []string
	add := func(a string) {
		if a = normAddr(a); a != "" && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	for _, e := range c.EmailAddresses {
		add(e)
	}
	for _, n := range c.Subject.Names {
		if n.Type.Equal(oidEmailAddress) {
			if s, ok := n.Value.(string); ok {
				add(s)
			}
		}
	}
	return out
}

func certsPEM(chain []*x509.Certificate) string {
	var b bytes.Buffer
	for _, c := range chain {
		_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return b.String()
}

// parseCerts is every certificate in data, PEM or DER.
func parseCerts(data []byte) []*x509.Certificate {
	var out []*x509.Certificate
	if bytes.Contains(data, []byte("-----BEGIN")) {
		for {
			var block *pem.Block
			block, data = pem.Decode(data)
			if block == nil {
				break
			}
			if block.Type == "CERTIFICATE" {
				if c, err := x509.ParseCertificate(block.Bytes); err == nil {
					out = append(out, c)
				}
			}
		}
		return out
	}
	if cs, err := x509.ParseCertificates(data); err == nil {
		return cs
	}
	return nil
}

// smimeEntry is what the key index keeps of a certificate and its issuers.
func smimeEntry(chain []*x509.Certificate) KeyEntry {
	leaf := chain[0]
	name := leaf.Subject.CommonName
	if name == "" {
		name = leaf.Subject.String()
	}
	return KeyEntry{Format: FormatSMIME, ID: certID(leaf), Addresses: certEmails(leaf), Name: name,
		Public: certsPEM(chain), Created: leaf.NotBefore, Expires: leaf.NotAfter, Issuer: leaf.Issuer.String()}
}

// ownCert is one of your certificates, with its key and issuers.
type ownCert struct {
	cert  *x509.Certificate
	key   crypto.PrivateKey
	chain []*x509.Certificate // the issuers, after cert
}

// storeSMIME keeps your certificate: its key and certificates where the
// keys are kept, its certificates in the index.
func (s *LocalStore) storeSMIME(key crypto.PrivateKey, chain []*x509.Certificate) (KeyEntry, error) {
	st := s.keyStore(FormatSMIME)
	if st == nil {
		return KeyEntry{}, errors.New("choose where comms-mail keeps its own keys first")
	}
	if err := st.Ready(); err != nil {
		if errors.Is(err, ErrLocked) {
			return KeyEntry{}, errors.New("where comms-mail keeps its keys is locked: unlock it first")
		}
		return KeyEntry{}, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return KeyEntry{}, err
	}
	var b bytes.Buffer
	_ = pem.Encode(&b, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
	clear(der)
	b.WriteString(certsPEM(chain))
	entry := smimeEntry(chain)
	if err := st.Update(map[string]string{keyName(FormatSMIME, entry.ID): b.String()}); err != nil {
		return KeyEntry{}, err
	}
	return entry, s.keys().addOwn(entry)
}

// loadOwnCert is your certificate id, its key from where the keys are kept.
func (s *LocalStore) loadOwnCert(st secretStore, id string) (ownCert, error) {
	v, ok, err := st.Get(keyName(FormatSMIME, id))
	if err != nil {
		return ownCert{}, err
	}
	if !ok {
		return ownCert{}, errors.New("the key of your certificate is not where comms-mail keeps its keys")
	}
	data := []byte(v)
	var oc ownCert
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY":
			oc.key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return ownCert{}, err
			}
		case "CERTIFICATE":
			if c, err := x509.ParseCertificate(block.Bytes); err == nil {
				certs = append(certs, c)
			}
		}
	}
	if oc.key == nil || len(certs) == 0 {
		return ownCert{}, errors.New("your certificate is kept incomplete")
	}
	oc.cert, oc.chain = certs[0], certs[1:]
	return oc, nil
}

// smimeOwnFor is your certificate for addr — the one valid longest — and
// its key.
func (s *LocalStore) smimeOwnFor(addr string) (ownCert, error) {
	var best *KeyEntry
	for _, e := range s.keys().own(FormatSMIME, addr) {
		if e.Expires.Before(time.Now()) {
			continue
		}
		if best == nil || e.Expires.After(best.Expires) {
			best = &e
		}
	}
	if best == nil {
		return ownCert{}, fmt.Errorf("you have no valid S/MIME certificate for %s", addr)
	}
	st := s.keyStore(FormatSMIME)
	if st == nil {
		return ownCert{}, errors.New("choose where comms-mail keeps its own keys in Settings › Security › Keys")
	}
	if err := st.Ready(); err != nil {
		return ownCert{}, err
	}
	return s.loadOwnCert(st, best.ID)
}

// smimeCanSign: you have a valid certificate for addr.
func (s *LocalStore) smimeCanSign(addr string) bool {
	for _, e := range s.keys().own(FormatSMIME, addr) {
		if e.Expires.After(time.Now()) {
			return true
		}
	}
	return false
}

// smimeRecipients are the certificates to encrypt to, one for each
// address, and the addresses there is none for.
func (s *LocalStore) smimeRecipients(addrs []string) (certs []*x509.Certificate, missing []string) {
	k := s.keys()
	for _, a := range addrs {
		var found *x509.Certificate
		for _, e := range append(k.own(FormatSMIME, a), k.contacts(FormatSMIME, a)...) {
			cs := parseCerts([]byte(e.Public))
			if len(cs) > 0 && cs[0].NotAfter.After(time.Now()) {
				found = cs[0]
				break
			}
		}
		if found == nil {
			missing = append(missing, a)
			continue
		}
		certs = append(certs, found)
	}
	return certs, missing
}

// importSMIMEOwnOrCerts brings in a .p12 — your certificate and key,
// opened with password — or other people's certificates.
func (s *LocalStore) importSMIMEOwnOrCerts(data, password []byte) (int, error) {
	defer clear(password)
	if certs := parseCerts(data); len(certs) > 0 {
		n := 0
		for i, c := range certs {
			if len(certEmails(c)) == 0 {
				continue // an issuer, carried with its certificate
			}
			e := smimeEntry(append([]*x509.Certificate{c}, certs[i+1:]...))
			e.Source = "imported"
			if err := s.keys().seen(e, time.Now()); err != nil {
				return n, err
			}
			n++
		}
		return n, nil
	}
	key, cert, cas, err := pkcs12.DecodeChain(data, string(password))
	if err != nil {
		if errors.Is(err, pkcs12.ErrIncorrectPassword) {
			if len(password) == 0 {
				return 0, errPassphraseNeeded
			}
			return 0, errors.New("that password does not open the file")
		}
		return 0, fmt.Errorf("not a certificate or a .p12 file: %w", err)
	}
	if len(certEmails(cert)) == 0 {
		return 0, errors.New("the certificate in the file names no address to send from")
	}
	if _, err := s.storeSMIME(key, append([]*x509.Certificate{cert}, cas...)); err != nil {
		return 0, err
	}
	Logf("keys: brought in an S/MIME certificate for %s", strings.Join(certEmails(cert), ", "))
	return 1, nil
}

// exportSMIMEBackup is your certificate id with its key and issuers, as a
// .p12 locked with passphrase.
func (s *LocalStore) exportSMIMEBackup(id string, passphrase []byte) ([]byte, error) {
	defer clear(passphrase)
	if len(passphrase) < MinPassphrase {
		return nil, fmt.Errorf("choose a passphrase of at least %d characters", MinPassphrase)
	}
	st := s.keyStore(FormatSMIME)
	if st == nil {
		return nil, errors.New("comms-mail keeps no keys yet")
	}
	if err := st.Ready(); err != nil {
		return nil, err
	}
	oc, err := s.loadOwnCert(st, strings.ToUpper(id))
	if err != nil {
		return nil, err
	}
	return pkcs12.Modern.Encode(oc.key, oc.cert, oc.chain, string(passphrase))
}

// ---- reading ----

func smimeIsSigned(der []byte) bool {
	t, err := cms.ContentType(der)
	return err == nil && t == "signed-data"
}

// smimeVerifyDetached checks a multipart/signed message's signature over
// its first part.
func (s *LocalStore) smimeVerifyDetached(entity, sig []byte, from string) SignatureCheck {
	sd, err := cms.Verify(sig, entity)
	if err != nil {
		return SignatureCheck{Format: FormatSMIME, Status: "bad", Problem: "the signature cannot be read"}
	}
	return s.smimeCheck(sd, from)
}

// smimeVerifyOpaque checks an opaque S/MIME signature, and is what it
// signed.
func (s *LocalStore) smimeVerifyOpaque(der []byte, from string) ([]byte, SignatureCheck) {
	sd, err := cms.Verify(der, nil)
	if err != nil {
		return nil, SignatureCheck{Format: FormatSMIME, Status: "bad", Problem: "the signed message cannot be read"}
	}
	return sd.Content, s.smimeCheck(sd, from)
}

// smimeCheck is the first signer's check as the reading pane shows it;
// a signer whose certificate holds is remembered, so mail to them can be
// encrypted.
func (s *LocalStore) smimeCheck(sd *cms.SignedData, from string) SignatureCheck {
	c := SignatureCheck{Format: FormatSMIME}
	if len(sd.Signers) == 0 {
		c.Status, c.Problem = "bad", "it has no signature"
		return c
	}
	sig := sd.Signers[0]
	if sig.Certificate == nil {
		c.Status = "unknown-key"
		return c
	}
	cert := sig.Certificate
	c.Fingerprint = certID(cert)
	emails := certEmails(cert)
	c.Signer = cert.Subject.CommonName
	if len(emails) > 0 {
		c.Signer = strings.TrimSpace(c.Signer + " <" + emails[0] + ">")
	}
	if sig.Err != nil {
		c.Status, c.Problem = "bad", sig.Err.Error()
		return c
	}
	_, own, _ := s.keys().byID(FormatSMIME, c.Fingerprint)
	imported := false
	if e, isOwn, ok := s.keys().byID(FormatSMIME, c.Fingerprint); ok && !isOwn && e.Source == "imported" {
		imported = true
	}
	chain, chainErr := verifyCertChain(cert, sd.Certificates, sig.SigningTime)
	c.Status = "valid"
	switch {
	case own:
		c.Trust, c.Level = "verified", "own"
	case chainErr != nil && !imported:
		c.Status, c.Problem = "untrusted-root", "its certificate is not from an authority this computer trusts ("+chainErr.Error()+")"
		return c
	case from != "" && !slices.Contains(emails, normAddr(from)):
		c.Trust = "suspicious"
		c.Reasons = []string{"the certificate is " + strings.Join(emails, ", ") + "'s"}
		return c
	case imported:
		c.Trust, c.Level = "verified", "imported"
	default:
		c.Trust, c.Level = "verified", "certificate"
		c.Reasons = []string{"certificate from " + firstNonEmpty(cert.Issuer.CommonName, cert.Issuer.String())}
	}
	if !own && chainErr == nil && len(emails) > 0 {
		e := smimeEntry(chain)
		e.Source = "signed"
		if err := s.keys().seen(e, time.Now()); err != nil {
			Logf("keys: %v", err)
		}
	}
	return c
}

// verifyCertChain checks cert was issued for mail, through the
// certificates carried, by an authority this computer trusts — at
// signing time when the message says it, else now — and is the chain.
func verifyCertChain(cert *x509.Certificate, carried []*x509.Certificate, at time.Time) ([]*x509.Certificate, error) {
	roots := smimeRoots()
	inter := x509.NewCertPool()
	for _, c := range carried {
		if !c.Equal(cert) {
			inter.AddCert(c)
		}
	}
	if at.IsZero() || at.After(time.Now()) {
		at = time.Now()
	}
	chains, err := cert.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: at,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}})
	if err != nil {
		return []*x509.Certificate{cert}, err
	}
	chain := chains[0]
	if len(chain) > 1 {
		chain = chain[:len(chain)-1] // the root is the computer's own
	}
	return chain, nil
}

// smimeRoots are the authorities S/MIME certificates are trusted through:
// this computer's (a variable for tests).
var smimeRoots = func() *x509.CertPool {
	if pool, err := x509.SystemCertPool(); err == nil {
		return pool
	}
	return x509.NewCertPool()
}

// smimeDecrypt opens an S/MIME encrypted message with your certificates.
func (s *LocalStore) smimeDecrypt(der []byte) ([]byte, error) {
	own := s.keys().own(FormatSMIME, "")
	if len(own) == 0 {
		return nil, errors.New("you have no S/MIME certificate to open it with")
	}
	st := s.keyStore(FormatSMIME)
	if st == nil {
		return nil, errors.New("choose where comms-mail keeps its own keys in Settings › Security › Keys")
	}
	if err := st.Ready(); err != nil {
		return nil, err
	}
	var last error
	for _, e := range own {
		oc, err := s.loadOwnCert(st, e.ID)
		if err != nil {
			last = err
			continue
		}
		out, err := cms.Decrypt(der, oc.cert, oc.key)
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, cms.ErrNoRecipient) {
			last = err
		}
	}
	if last != nil {
		return nil, last
	}
	return nil, errors.New("it is not encrypted to any of your certificates")
}

// ---- sending ----

// smimeCompose signs and encrypts raw, a message comms-mail built, as
// S/MIME: signed alone, multipart/signed; encrypted, enveloped-data —
// signed first, inside, when signing too — with the real subject inside
// and "..." outside, to each recipient's certificate and your own.
func (s *LocalStore) smimeCompose(raw []byte, from string, rcpts []string, sign, encrypt bool) ([]byte, error) {
	outer, entity := splitMessage(raw)
	inner := entity
	if sign {
		oc, err := s.smimeOwnFor(from)
		if err != nil {
			return nil, err
		}
		signer, ok := oc.key.(crypto.Signer)
		if !ok {
			return nil, errors.New("your certificate's key cannot sign")
		}
		der, err := cms.Sign(entity, oc.cert, signer, cms.SignOptions{Detached: true, Chain: oc.chain})
		if err != nil {
			return nil, err
		}
		var part bytes.Buffer
		part.WriteString("Content-Type: application/pkcs7-signature; name=\"smime.p7s\"\r\n")
		part.WriteString("Content-Transfer-Encoding: base64\r\n")
		part.WriteString("Content-Disposition: attachment; filename=\"smime.p7s\"\r\n")
		part.WriteString("Content-Description: S/MIME Cryptographic Signature\r\n\r\n")
		writeBase64Lines(&part, der)
		ctype, body := signedMultipart(entity, "application/pkcs7-signature", "sha-256", part.Bytes())
		if !encrypt {
			return wrapMessage(outer, false, ctype, nil, body), nil
		}
		inner = append([]byte("Content-Type: "+ctype+"\r\n\r\n"), body...)
	}
	certs, missing := s.smimeRecipients(rcpts)
	if len(missing) > 0 {
		return nil, fmt.Errorf("no S/MIME certificate for %s, so the message cannot be encrypted to them", strings.Join(missing, ", "))
	}
	if self, _ := s.smimeRecipients([]string{from}); len(self) > 0 {
		certs = append(certs, self...)
	}
	der, err := cms.Encrypt(withProtectedHeaders(outer, inner), certs)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writeBase64Lines(&body, der)
	return wrapMessage(outer, true, `application/pkcs7-mime; smime-type=enveloped-data; name="smime.p7m"`,
		[]string{"Content-Transfer-Encoding: base64", `Content-Disposition: attachment; filename="smime.p7m"`}, body.Bytes()), nil
}
