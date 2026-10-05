package mailcore

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// comms-mail's own OpenPGP, built in (ProtonMail's go-crypto): making and
// bringing in your keys, checking and opening mail, signing and encrypting
// it — PGP/MIME (RFC 3156), and inline OpenPGP when reading. Your private
// keys are kept where Settings › Security › Keys says (keychoice.go);
// other people's keys come with their mail — an Autocrypt header, an
// attached key — or are brought in by you, and are trusted that far:
// first seen in mail, or imported (keyindex.go).

// pgpConfig is how keys are made and messages signed: Ed25519 to sign,
// X25519 to encrypt, SHA-256.
func pgpConfig() *packet.Config {
	return &packet.Config{DefaultHash: crypto.SHA256, Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}
}

func pgpFingerprint(e *openpgp.Entity) string {
	return strings.ToUpper(hex.EncodeToString(e.PrimaryKey.Fingerprint))
}

// pgpEntry is what the key index keeps of e: its public half.
func pgpEntry(e *openpgp.Entity) (KeyEntry, error) {
	var pub bytes.Buffer
	w, err := armor.Encode(&pub, openpgp.PublicKeyType, nil)
	if err != nil {
		return KeyEntry{}, err
	}
	if err := e.Serialize(w); err != nil {
		return KeyEntry{}, err
	}
	if err := w.Close(); err != nil {
		return KeyEntry{}, err
	}
	out := KeyEntry{Format: FormatOpenPGP, ID: pgpFingerprint(e), Public: pub.String(), Created: e.PrimaryKey.CreationTime}
	for _, id := range e.Identities {
		if id.UserId == nil {
			continue
		}
		if a := normAddr(id.UserId.Email); a != "" && !out.has(a) {
			out.Addresses = append(out.Addresses, a)
		}
		if out.Name == "" {
			out.Name = id.UserId.Name
		}
		if sig := id.SelfSignature; sig != nil && sig.KeyLifetimeSecs != nil && *sig.KeyLifetimeSecs > 0 {
			out.Expires = e.PrimaryKey.CreationTime.Add(time.Duration(*sig.KeyLifetimeSecs) * time.Second)
		}
	}
	return out, nil
}

// pgpReadKeys is every key in data, armoured or not.
func pgpReadKeys(data []byte) (openpgp.EntityList, error) {
	if bytes.Contains(data, []byte("-----BEGIN PGP")) {
		return openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	}
	return openpgp.ReadKeyRing(bytes.NewReader(data))
}

// pgpPublicRing is everyone's keys comms-mail knows — yours and your
// contacts' — to check signatures with and encrypt to.
func (s *LocalStore) pgpPublicRing() openpgp.EntityList {
	k := s.keys()
	var ring openpgp.EntityList
	for _, e := range append(k.own(FormatOpenPGP, ""), k.contacts(FormatOpenPGP, "")...) {
		if el, err := pgpReadKeys([]byte(e.Public)); err == nil {
			ring = append(ring, el...)
		}
	}
	return ring
}

// pgpSecretRing is your private keys, from where they are kept. ErrLocked
// when that place is locked.
func (s *LocalStore) pgpSecretRing() (openpgp.EntityList, error) {
	st := s.keyStore(FormatOpenPGP)
	if st == nil {
		return nil, errors.New("choose where comms-mail keeps its own keys in Settings › Security › Keys")
	}
	if err := st.Ready(); err != nil {
		return nil, err
	}
	var ring openpgp.EntityList
	for _, e := range s.keys().own(FormatOpenPGP, "") {
		v, ok, err := st.Get(keyName(FormatOpenPGP, e.ID))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if el, err := pgpReadKeys([]byte(v)); err == nil {
			ring = append(ring, el...)
		}
	}
	return ring, nil
}

// pgpOwnFor is your key for addr, with its private half.
func (s *LocalStore) pgpOwnFor(addr string) (*openpgp.Entity, error) {
	own := s.keys().own(FormatOpenPGP, addr)
	if len(own) == 0 {
		return nil, fmt.Errorf("you have no OpenPGP key for %s", addr)
	}
	ring, err := s.pgpSecretRing()
	if err != nil {
		return nil, err
	}
	for _, e := range ring {
		if pgpFingerprint(e) == own[0].ID && e.PrivateKey != nil {
			return e, nil
		}
	}
	return nil, fmt.Errorf("the private half of your OpenPGP key for %s is not where comms-mail keeps its keys", addr)
}

// storePGPKey keeps e — its private half where the keys are kept, its
// public half in the index.
func (s *LocalStore) storePGPKey(e *openpgp.Entity) (KeyEntry, error) {
	st := s.keyStore(FormatOpenPGP)
	if st == nil {
		return KeyEntry{}, errors.New("choose where comms-mail keeps its own keys first")
	}
	if err := st.Ready(); err != nil {
		if errors.Is(err, ErrLocked) {
			return KeyEntry{}, errors.New("where comms-mail keeps its keys is locked: unlock it first")
		}
		return KeyEntry{}, err
	}
	var priv bytes.Buffer
	w, err := armor.Encode(&priv, openpgp.PrivateKeyType, nil)
	if err != nil {
		return KeyEntry{}, err
	}
	if err := e.SerializePrivateWithoutSigning(w, nil); err != nil {
		return KeyEntry{}, err
	}
	if err := w.Close(); err != nil {
		return KeyEntry{}, err
	}
	entry, err := pgpEntry(e)
	if err != nil {
		return KeyEntry{}, err
	}
	if err := st.Update(map[string]string{keyName(FormatOpenPGP, entry.ID): priv.String()}); err != nil {
		return KeyEntry{}, err
	}
	return entry, s.keys().addOwn(entry)
}

// makePGPKeyOwn makes an OpenPGP key for address, under the name you send
// as from it, and keeps it.
func (s *LocalStore) makePGPKeyOwn(address, name string) error {
	e, err := openpgp.NewEntity(name, "", address, pgpConfig())
	if err != nil {
		return err
	}
	if _, err := s.storePGPKey(e); err != nil {
		return err
	}
	Logf("keys: made an OpenPGP key for %s", address)
	return nil
}

// ImportPGP brings in OpenPGP keys: a private key becomes yours (opened
// with passphrase when it is protected by one), a public key a contact's.
func (s *LocalStore) ImportPGP(data []byte, passphrase []byte) (int, error) {
	defer clear(passphrase)
	el, err := pgpReadKeys(data)
	if err != nil {
		return 0, fmt.Errorf("not an OpenPGP key: %w", err)
	}
	n := 0
	for _, e := range el {
		if e.PrivateKey != nil {
			if e.PrivateKey.Encrypted {
				if len(passphrase) == 0 {
					return n, errPassphraseNeeded
				}
				if err := e.DecryptPrivateKeys(passphrase); err != nil {
					return n, errors.New("that passphrase does not open the key")
				}
			}
			if _, err := s.storePGPKey(e); err != nil {
				return n, err
			}
			n++
			continue
		}
		entry, err := pgpEntry(e)
		if err != nil {
			return n, err
		}
		entry.Source = "imported"
		if err := s.keys().seen(entry, time.Now()); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// errPassphraseNeeded: the key is protected by a passphrase, and none was
// given.
var errPassphraseNeeded = errors.New("passphrase needed")

// ErrPassphraseNeeded is errPassphraseNeeded, for the window to ask.
var ErrPassphraseNeeded = errPassphraseNeeded

// ExportPGPBackup is your OpenPGP key for address, private half and all,
// locked with passphrase: a backup to keep somewhere safe.
func (s *LocalStore) ExportPGPBackup(id string, passphrase []byte) ([]byte, error) {
	defer clear(passphrase)
	if len(passphrase) < MinPassphrase {
		return nil, fmt.Errorf("choose a passphrase of at least %d characters", MinPassphrase)
	}
	ring, err := s.pgpSecretRing()
	if err != nil {
		return nil, err
	}
	for _, e := range ring {
		if pgpFingerprint(e) != strings.ToUpper(id) {
			continue
		}
		if err := e.EncryptPrivateKeys(passphrase, nil); err != nil {
			return nil, err
		}
		var out bytes.Buffer
		w, err := armor.Encode(&out, openpgp.PrivateKeyType, nil)
		if err != nil {
			return nil, err
		}
		if err := e.SerializePrivateWithoutSigning(w, nil); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	return nil, errors.New("no such key")
}

// ---- reading ----

// pgpVerifyDetached checks an OpenPGP signature (armoured or not) over
// signed, made by from's key or another's.
func (s *LocalStore) pgpVerifyDetached(signed, sig []byte, from string) SignatureCheck {
	c := SignatureCheck{Format: FormatOpenPGP}
	sigBody := io.Reader(bytes.NewReader(sig))
	if bytes.Contains(sig, []byte("-----BEGIN PGP")) {
		block, err := armor.Decode(bytes.NewReader(sig))
		if err != nil {
			c.Status, c.Problem = "bad", "the signature cannot be read"
			return c
		}
		raw, _ := io.ReadAll(block.Body)
		sigBody = bytes.NewReader(raw)
		sig = raw
	}
	p, sigErr, signer := s.pgpCheck(signed, sigBody)
	return s.pgpSignatureCheck(c, p, sigErr, signer, sig, from)
}

func (s *LocalStore) pgpCheck(signed []byte, sig io.Reader) (*packet.Signature, error, *openpgp.Entity) {
	p, signer, err := openpgp.VerifyDetachedSignature(s.pgpPublicRing(), bytes.NewReader(signed), sig, nil)
	return p, err, signer
}

// pgpSignatureCheck is a signature's check as the reading pane shows it.
func (s *LocalStore) pgpSignatureCheck(c SignatureCheck, p *packet.Signature, err error, signer *openpgp.Entity, rawSig []byte, from string) SignatureCheck {
	if signer != nil {
		c.Fingerprint = pgpFingerprint(signer)
		if id := signer.PrimaryIdentity(); id != nil {
			c.Signer = id.Name
			if c.Signer == "" || id.UserId != nil && id.UserId.Email != "" {
				c.Signer = strings.TrimSpace(id.UserId.Name + " <" + id.UserId.Email + ">")
			}
		}
	}
	switch {
	case err == nil:
		c.Status = "valid"
	case errors.Is(err, pgperrors.ErrUnknownIssuer):
		c.Status = "unknown-key"
		if issuer := pgpIssuer(rawSig); issuer != "" {
			c.Fingerprint = issuer
		}
		return c
	case errors.Is(err, pgperrors.ErrKeyExpired):
		c.Status, c.Problem = "expired-key", "the key that made it has expired"
	case errors.Is(err, pgperrors.ErrKeyRevoked):
		c.Status, c.Problem = "revoked-key", "the key that made it was revoked"
	case errors.Is(err, pgperrors.ErrSignatureExpired):
		c.Status, c.Problem = "expired", "the signature has expired"
	default:
		c.Status, c.Problem = "bad", err.Error()
	}
	_ = p
	if c.Status != "valid" {
		return c
	}
	s.trustOwn(&c, FormatOpenPGP, from)
	return c
}

// pgpIssuer is the key id (or fingerprint) a binary signature names.
func pgpIssuer(sig []byte) string {
	pk, err := packet.Read(bytes.NewReader(sig))
	if err != nil {
		return ""
	}
	if p, ok := pk.(*packet.Signature); ok {
		if len(p.IssuerFingerprint) > 0 {
			return strings.ToUpper(hex.EncodeToString(p.IssuerFingerprint))
		}
		if p.IssuerKeyId != nil {
			return fmt.Sprintf("%016X", *p.IssuerKeyId)
		}
	}
	return ""
}

// trustOwn says how far the key behind a holding signature c is trusted
// for from: yours, imported by you, first seen in mail, or someone
// else's.
func (s *LocalStore) trustOwn(c *SignatureCheck, format, from string) {
	e, own, ok := s.keys().byID(format, c.Fingerprint)
	switch {
	case !ok:
	case own:
		c.Trust, c.Level = "verified", "own"
	case from != "" && !e.has(from):
		c.Trust = "suspicious"
		c.Reasons = []string{"it is " + strings.Join(e.Addresses, ", ") + "'s"}
	case e.Source == "imported":
		c.Trust, c.Level = "verified", "imported"
	default:
		c.Trust, c.Level = "unverified", "tofu"
		c.Reasons = []string{"first seen " + e.FirstSeen.Format("2 Jan 2006") + ", " + sourceWords(e.Source)}
	}
}

func sourceWords(src string) string {
	switch src {
	case "autocrypt":
		return "in a message's Autocrypt header"
	case "attached":
		return "attached to a message"
	case "signed":
		return "on a signed message"
	}
	return "in mail"
}

// pgpRecordCarried records the keys a message from from carries: its
// Autocrypt header, and keys attached to it. Only a key for the sender's
// own address is taken from the header (Autocrypt's rule).
func (s *LocalStore) pgpRecordCarried(fields []mimeField, attached [][]byte, from string, when time.Time) {
	from = normAddr(from)
	if f, ok := field(fields, "Autocrypt"); ok && from != "" {
		attrs := map[string]string{}
		for _, part := range strings.Split(f.value(), ";") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
			attrs[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
		if normAddr(attrs["addr"]) == from {
			if data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(attrs["keydata"]), "")); err == nil {
				s.pgpRecord(data, "autocrypt", from, when)
			}
		}
	}
	for _, data := range attached {
		s.pgpRecord(data, "attached", from, when)
	}
}

// pgpRecord records the public keys in data that have from among their
// addresses, as seen in mail.
func (s *LocalStore) pgpRecord(data []byte, source, from string, when time.Time) {
	el, err := pgpReadKeys(data)
	if err != nil {
		return
	}
	for _, e := range el {
		entry, err := pgpEntry(e)
		if err != nil || !entry.has(from) {
			continue
		}
		if _, own, known := s.keys().byID(FormatOpenPGP, entry.ID); known && own {
			continue
		}
		entry.Source = source
		if err := s.keys().seen(entry, when); err != nil {
			Logf("keys: %v", err)
		}
	}
}

// pgpDecrypt opens an OpenPGP message (armoured or not) with your keys,
// checking the signature it may carry inside. locked: where the keys are
// kept is locked.
func (s *LocalStore) pgpDecrypt(data []byte, from string) (content []byte, sig *SignatureCheck, err error) {
	secret, err := s.pgpSecretRing()
	if err != nil {
		return nil, nil, err
	}
	ring := append(openpgp.EntityList{}, secret...)
	have := map[string]bool{}
	for _, e := range secret {
		have[pgpFingerprint(e)] = true
	}
	for _, e := range s.pgpPublicRing() {
		if !have[pgpFingerprint(e)] {
			ring = append(ring, e)
		}
	}
	in := io.Reader(bytes.NewReader(data))
	if bytes.Contains(data, []byte("-----BEGIN PGP")) {
		block, err := armor.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, nil, fmt.Errorf("the encrypted message cannot be read: %w", err)
		}
		in = block.Body
	}
	md, err := openpgp.ReadMessage(in, ring, nil, nil)
	if err != nil {
		if errors.Is(err, pgperrors.ErrKeyIncorrect) {
			return nil, nil, errors.New("it is not encrypted to any of your keys")
		}
		return nil, nil, err
	}
	if !md.IsEncrypted {
		return nil, nil, errors.New("it is not encrypted")
	}
	content, err = io.ReadAll(md.UnverifiedBody)
	if err != nil {
		return nil, nil, err
	}
	if md.IsSigned {
		c := SignatureCheck{Format: FormatOpenPGP}
		var signer *openpgp.Entity
		if md.SignedBy != nil {
			signer = md.SignedBy.Entity
		}
		check := md.SignatureError
		if signer == nil && check == nil {
			check = pgperrors.ErrUnknownIssuer
		}
		var raw []byte
		if md.Signature != nil {
			var b bytes.Buffer
			_ = md.Signature.Serialize(&b)
			raw = b.Bytes()
		}
		r := s.pgpSignatureCheck(c, md.Signature, check, signer, raw, from)
		if r.Status == "unknown-key" && r.Fingerprint == "" && md.SignedByKeyId != 0 {
			r.Fingerprint = fmt.Sprintf("%016X", md.SignedByKeyId)
		}
		sig = &r
	}
	return content, sig, nil
}

// pgpClearsigned checks an inline cleartext-signed text, and is its text.
func (s *LocalStore) pgpClearsigned(text []byte, from string) ([]byte, SignatureCheck) {
	c := SignatureCheck{Format: FormatOpenPGP}
	block, _ := clearsign.Decode(text)
	if block == nil {
		c.Status, c.Problem = "bad", "the signed text cannot be read"
		return text, c
	}
	raw, _ := io.ReadAll(block.ArmoredSignature.Body)
	p, err, signer := s.pgpCheck(block.Bytes, bytes.NewReader(raw))
	return block.Plaintext, s.pgpSignatureCheck(c, p, err, signer, raw, from)
}

// ---- sending ----

// pgpCanSign: you have an OpenPGP key for addr.
func (s *LocalStore) pgpCanSign(addr string) bool { return len(s.keys().own(FormatOpenPGP, addr)) > 0 }

// pgpRecipients are the keys to encrypt to, one for each address, and the
// addresses there is none for.
func (s *LocalStore) pgpRecipients(addrs []string) (to openpgp.EntityList, missing []string) {
	k := s.keys()
	for _, a := range addrs {
		l := k.own(FormatOpenPGP, a)
		if len(l) == 0 {
			l = k.contacts(FormatOpenPGP, a)
		}
		var found *openpgp.Entity
		for _, e := range l {
			el, err := pgpReadKeys([]byte(e.Public))
			if err != nil || len(el) == 0 {
				continue
			}
			if _, ok := el[0].EncryptionKey(time.Now()); ok {
				found = el[0]
				break
			}
		}
		if found == nil {
			missing = append(missing, a)
			continue
		}
		to = append(to, found)
	}
	return to, missing
}

// pgpCompose signs and encrypts raw, a message comms-mail built, as
// PGP/MIME: signed alone, multipart/signed; encrypted (and signed inside),
// multipart/encrypted with the real subject inside and "..." outside.
// Encrypted, it is encrypted to each recipient and to your own key too,
// so Sent stays readable.
func (s *LocalStore) pgpCompose(raw []byte, from string, rcpts []string, sign, encrypt bool) ([]byte, error) {
	outer, entity := splitMessage(raw)
	var signer *openpgp.Entity
	if sign {
		e, err := s.pgpOwnFor(from)
		if err != nil {
			return nil, err
		}
		signer = e
	}
	if !encrypt {
		var sig bytes.Buffer
		if err := openpgp.ArmoredDetachSign(&sig, signer, bytes.NewReader(entity), pgpConfig()); err != nil {
			return nil, err
		}
		part := []byte("Content-Type: application/pgp-signature; name=\"signature.asc\"\r\n" +
			"Content-Description: OpenPGP digital signature\r\n" +
			"Content-Disposition: attachment; filename=\"signature.asc\"\r\n\r\n")
		part = append(part, crlf(sig.Bytes())...)
		ctype, body := signedMultipart(entity, "application/pgp-signature", "pgp-sha256", part)
		return wrapMessage(outer, false, ctype, s.autocryptFields(from), body), nil
	}
	to, missing := s.pgpRecipients(rcpts)
	if len(missing) > 0 {
		return nil, fmt.Errorf("no OpenPGP key for %s, so the message cannot be encrypted to them", strings.Join(missing, ", "))
	}
	if self, _ := s.pgpRecipients([]string{from}); len(self) > 0 {
		to = append(to, self...)
	}
	var enc bytes.Buffer
	w, err := armor.Encode(&enc, "PGP MESSAGE", nil)
	if err != nil {
		return nil, err
	}
	pt, err := openpgp.Encrypt(w, to, signer, &openpgp.FileHints{IsBinary: true}, pgpConfig())
	if err != nil {
		return nil, err
	}
	if _, err := pt.Write(withProtectedHeaders(outer, entity)); err != nil {
		return nil, err
	}
	if err := pt.Close(); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	armored := crlf(enc.Bytes())
	bd := newBoundary(armored)
	var body bytes.Buffer
	body.WriteString("This is an OpenPGP/MIME encrypted message (RFC 4880 and 3156)\r\n")
	body.WriteString("--" + bd + "\r\n")
	body.WriteString("Content-Type: application/pgp-encrypted\r\nContent-Description: PGP/MIME version identification\r\n\r\nVersion: 1\r\n\r\n")
	body.WriteString("--" + bd + "\r\n")
	body.WriteString("Content-Type: application/octet-stream; name=\"encrypted.asc\"\r\n")
	body.WriteString("Content-Description: OpenPGP encrypted message\r\n")
	body.WriteString("Content-Disposition: inline; filename=\"encrypted.asc\"\r\n\r\n")
	body.Write(armored)
	if !bytes.HasSuffix(armored, []byte("\r\n")) {
		body.WriteString("\r\n")
	}
	body.WriteString("--" + bd + "--\r\n")
	ctype := `multipart/encrypted; protocol="application/pgp-encrypted"; boundary="` + bd + `"`
	return wrapMessage(outer, true, ctype, s.autocryptFields(from), body.Bytes()), nil
}

// autocryptFields is the Autocrypt header that offers your OpenPGP key
// for from to whoever reads the message, when comms-mail's own OpenPGP has
// one (Autocrypt Level 1).
func (s *LocalStore) autocryptFields(from string) []string {
	own := s.keys().own(FormatOpenPGP, from)
	if len(own) == 0 {
		return nil
	}
	el, err := pgpReadKeys([]byte(own[0].Public))
	if err != nil || len(el) == 0 {
		return nil
	}
	var key bytes.Buffer
	if err := el[0].Serialize(&key); err != nil {
		return nil
	}
	data := base64.StdEncoding.EncodeToString(key.Bytes())
	var b strings.Builder
	b.WriteString("Autocrypt: addr=" + normAddr(from) + "; keydata=")
	for len(data) > 0 {
		n := min(len(data), 72)
		b.WriteString("\r\n " + data[:n])
		data = data[n:]
	}
	return []string{b.String()}
}

// withAutocrypt is raw with the Autocrypt header for from, when there is
// one to give and raw has none.
func (s *LocalStore) withAutocrypt(raw []byte, from string) []byte {
	if s.engineOf(FormatOpenPGP) != EngineOwn {
		return raw
	}
	fields := s.autocryptFields(from)
	if len(fields) == 0 {
		return raw
	}
	raw = crlf(raw)
	head, _ := splitEntity(raw)
	if _, ok := field(head, "Autocrypt"); ok {
		return raw
	}
	return append([]byte(fields[0]+"\r\n"), raw...)
}

// fromAddress is the address in a From field.
func fromAddress(v string) string {
	if a, err := mail.ParseAddress(v); err == nil {
		return a.Address
	}
	return ExtractAddr(v)
}
