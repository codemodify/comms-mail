package mailcore

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

// Signed and encrypted mail. comms-mail tells them apart by their
// structure, as every mail program does, and that is all it does itself:
// checking a signature, judging whose key made it, and decrypting are
// secretvault's (secretvault.go), which keeps the person's keys and
// contacts and does that work in its own daemon. comms-mail has PGP and
// S/MIME only while secretvault is the store in use.

// recogniseProtection reads how a message is protected from its outermost
// structure, without checking or opening anything.
func recogniseProtection(media string, params map[string]string, body string) (signed, encrypted bool) {
	switch low := strings.ToLower(media); {
	case low == "multipart/encrypted":
		return false, true
	case low == "multipart/signed":
		return true, false
	case isOpaqueSMIME(low):
		switch strings.ToLower(params["smime-type"]) {
		case "signed-data":
			return true, false
		case "certs-only": // a certificate sent on its own
			return false, false
		}
		return false, true // enveloped-data, authEnveloped-data, or unnamed
	}
	t := strings.TrimLeft(body, " \t\r\n")
	switch {
	case strings.HasPrefix(t, "-----BEGIN PGP MESSAGE-----"):
		return false, true
	case strings.HasPrefix(t, "-----BEGIN PGP SIGNED MESSAGE-----"):
		return true, false
	}
	return false, false
}

// looksProtected says a message cached without the Signed and Encrypted
// flags has a part only signed or encrypted mail has, or an armoured
// OpenPGP block for its text: worth reading again from its raw message.
func looksProtected(m Message) bool {
	for _, p := range m.Parts {
		switch strings.ToLower(p.MIMEType) {
		case "application/pkcs7-mime", "application/x-pkcs7-mime", "application/pgp-encrypted",
			"application/pgp-signature", "application/pkcs7-signature", "application/x-pkcs7-signature":
			return true
		}
	}
	return strings.HasPrefix(strings.TrimLeft(m.Body, " \t\r\n"), "-----BEGIN PGP")
}

// applyProtection takes into dst what parsing its raw message again says
// of its protection: the flags, and for an encrypted message the text
// and attachments that are not its own.
func applyProtection(dst *Message, parsed Message) {
	dst.Signed, dst.Encrypted, dst.Autocrypt = parsed.Signed, parsed.Encrypted, parsed.Autocrypt
	if parsed.Signed || parsed.Encrypted {
		dst.Body, dst.HTML, dst.Snippet = parsed.Body, parsed.HTML, parsed.Snippet
		dst.Attachments, dst.HasAttach = parsed.Attachments, parsed.HasAttach
	}
}

// dropSignatureParts takes a signed message's detached signature out of
// its attachments (signature.asc, smime.p7s): the reading pane says the
// message is signed instead.
func dropSignatureParts(m *Message) {
	for _, p := range m.Parts {
		switch strings.ToLower(p.MIMEType) {
		case "application/pgp-signature", "application/pkcs7-signature", "application/x-pkcs7-signature":
			for i, name := range m.Attachments {
				if name == p.Filename {
					m.Attachments = append(m.Attachments[:i], m.Attachments[i+1:]...)
					break
				}
			}
		}
	}
	m.HasAttach = len(m.Attachments) > 0
}

func isOpaqueSMIME(media string) bool {
	low := strings.ToLower(media)
	return low == "application/pkcs7-mime" || low == "application/x-pkcs7-mime"
}

// MessageSecurity is what secretvault found in a message, for the reading
// pane. Content, a decrypted message, is held in memory only: it is never
// written to the cache or indexed.
type MessageSecurity struct {
	// Signed and Encrypted are how much of the message is covered:
	// "none", "partial" or "full".
	Signed    string `json:"signed"`
	Encrypted string `json:"encrypted"`
	// Checked says secretvault looked at the message; otherwise Why says
	// why not, and Locked that secretvault is locked.
	Checked bool   `json:"checked"`
	Locked  bool   `json:"locked,omitempty"`
	Why     string `json:"why,omitempty"`
	// Signatures are the message's signatures, outermost first.
	Signatures []SignatureCheck `json:"signatures,omitempty"`
	// Decrypted says the encryption was opened, with the person's key;
	// DecryptError why it was not.
	Decrypted    bool   `json:"decrypted,omitempty"`
	DecryptError string `json:"decryptError,omitempty"`
	// Content is the message inside the encryption (or an opaque S/MIME
	// signature): its text, HTML and parts.
	Content *Message `json:"content,omitempty"`
	// Subject is the protected subject, the one to show, when the
	// message carries one (RFC 9788; an encrypted message's outer subject
	// is often "...").
	Subject  string   `json:"subject,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// SignatureCheck is one signature: whether it holds, and whose it is.
type SignatureCheck struct {
	// Format is "openpgp" or "smime".
	Format string `json:"format"`
	// Status is secretvault's: "valid", "bad" (the message was changed),
	// "unknown-key", "expired-key", "revoked-key", "untrusted-root", …;
	// Problem says it in words when it is not valid.
	Status  string `json:"status"`
	Problem string `json:"problem,omitempty"`
	// Signer is who made it — the contact's name, or what the key or
	// certificate says — and Fingerprint the key's.
	Signer      string `json:"signer,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// Trust is the contacts' verdict on the signer for the From address:
	// "verified", "unverified", "suspicious" (the address belongs to
	// someone else's key) or "unknown"; Level how the key was verified
	// (tofu, published, organisation, in-person); Reasons in words.
	Trust   string   `json:"trust,omitempty"`
	Level   string   `json:"level,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
	// Part says the signature covers only part of the message.
	Part bool `json:"part,omitempty"`
}

// noSecurity is a message with nothing to check.
var noSecurity = MessageSecurity{Signed: "none", Encrypted: "none"}

// recognised is what comms-mail can say of raw without secretvault.
func recognised(raw []byte) (MessageSecurity, Message) {
	m, err := ParseRFC822(raw, "", "")
	if err != nil {
		return noSecurity, Message{}
	}
	out := noSecurity
	if m.Signed {
		out.Signed = "full"
	}
	if m.Encrypted {
		out.Encrypted = "full"
	}
	return out, m
}

// whyNoSecretVault is what the reading pane says of protected mail while
// another store is in use.
const whyNoSecretVault = "secretvault checks signed and encrypted mail: choose it in Settings › Privacy to read this one."

// MessageSecurityOf is MessageSecurity for a store that keeps no secrets
// (the demo): what the structure says, checked by nobody.
func MessageSecurityOf(raw []byte) MessageSecurity {
	sec, m := recognised(raw)
	if m.Signed || m.Encrypted {
		sec.Why = whyNoSecretVault
	}
	return sec
}

// MessageSecurity has secretvault check message id, and, with decrypt,
// open what is encrypted to the person's keys (secretvault asks about
// decrypting the first time). It asks nobody to unlock anything: while
// secretvault is locked the message is reported unchecked. Keys the
// message carries (Autocrypt, an S/MIME signer's certificate) are passed
// on to secretvault's contacts.
func (s *LocalStore) MessageSecurity(id MessageID, decrypt bool) (MessageSecurity, error) {
	raw, err := s.GetRaw(id)
	if err != nil {
		return MessageSecurity{}, err
	}
	sec, m := recognised(raw)
	if !m.Signed && !m.Encrypted && !m.Autocrypt {
		return sec, nil
	}
	if s.secretKind() != StoreSecretVault {
		if m.Signed || m.Encrypted {
			sec.Why = whyNoSecretVault
		}
		return sec, nil
	}
	sv := secretVaultStore{theSecretVault}
	switch err := sv.Ready(); {
	case errors.Is(err, ErrLocked):
		sec.Locked, sec.Why = true, "secretvault is locked"
		return sec, nil
	case err != nil:
		sec.Why = err.Error()
		return sec, nil
	}
	var res svInspectResult
	err = theSecretVault.call("mail.inspect", map[string]any{"message": raw, "decrypt": decrypt && m.Encrypted}, &res)
	if err != nil {
		switch svCode(err) {
		case svCodeLocked:
			sec.Locked, sec.Why = true, "secretvault is locked"
		case svCodeDenied, svCodeCanceled:
			sec.Why = "secretvault did not let comms-mail check this message"
		default:
			sec.Why = err.Error()
		}
		return sec, nil
	}
	out := securityFromReport(res, m)
	s.passOnKeys(res.Report.Keys, m)
	return out, nil
}

// passOnKeys tells secretvault's contacts about the keys a message
// carries. They are offered, not trusted: secretvault records how each
// was seen. A refusal is not the reading pane's business.
func (s *LocalStore) passOnKeys(keys []svDiscoveredKey, m Message) {
	if len(keys) == 0 {
		return
	}
	name, from := "", ""
	if a, err := mail.ParseAddress(m.From); err == nil {
		name, from = a.Name, a.Address
	}
	when := m.Date
	if when.IsZero() {
		when = time.Now()
	}
	for _, k := range keys {
		if len(k.Data) == 0 || k.Error != "" {
			continue
		}
		addr := k.Address
		if addr == "" {
			addr = from
		}
		p := map[string]any{"key": k.Data, "address": addr, "time": when, "source": k.Source}
		if strings.EqualFold(addr, from) && name != "" {
			p["name"] = name
		}
		if err := theSecretVault.call("trust.seen", p, nil); err != nil {
			Logf("secretvault: a key %s carried for %s was not recorded: %v", k.Source, addr, err)
		}
	}
}

// What comms-maild reads of mail.inspect's answer (secretvault's
// svrpc.MailInspectResult and svmail.Report).

type svInspectResult struct {
	Report   svReport    `json:"report"`
	Verdicts []svVerdict `json:"verdicts"`
}

type svReport struct {
	Protected *struct {
		Headers struct {
			Subject string `json:"subject"`
		} `json:"headers"`
	} `json:"protected_headers"`
	Signed    string            `json:"signed"`
	Encrypted string            `json:"encrypted"`
	Layers    []svLayer         `json:"layers"`
	Content   *svContent        `json:"content"`
	Keys      []svDiscoveredKey `json:"keys"`
	Warnings  []struct {
		Message string `json:"message"`
	} `json:"warnings"`
}

type svLayer struct {
	ID         int           `json:"id"`
	Kind       string        `json:"kind"`
	Format     string        `json:"format"`
	Covers     string        `json:"covers"`
	Signatures []svSignature `json:"signatures"`
	Encryption *struct {
		Decrypted bool   `json:"decrypted"`
		Error     string `json:"error"`
	} `json:"encryption"`
	Error string `json:"error"`
}

type svSignature struct {
	Status      string   `json:"status"`
	Error       string   `json:"error"`
	Fingerprint string   `json:"fingerprint"`
	UserIDs     []string `json:"user_ids"`
	Certificate *struct {
		Subject string   `json:"subject"`
		Emails  []string `json:"emails"`
		SHA256  string   `json:"sha256"`
	} `json:"certificate"`
}

type svContent struct {
	Layer int    `json:"layer"`
	Raw   []byte `json:"raw"`
}

type svDiscoveredKey struct {
	Source  string `json:"source"`
	Address string `json:"address"`
	Data    []byte `json:"data"`
	Error   string `json:"error"`
}

type svVerdict struct {
	Layer   int `json:"layer"`
	Index   int `json:"index"`
	Verdict struct {
		Contact string   `json:"contact"`
		Level   string   `json:"level"`
		Status  string   `json:"status"`
		Reasons []string `json:"reasons"`
	} `json:"verdict"`
}

// securityFromReport is secretvault's report as the reading pane shows it.
func securityFromReport(res svInspectResult, outer Message) MessageSecurity {
	r := res.Report
	out := MessageSecurity{Signed: r.Signed, Encrypted: r.Encrypted, Checked: true}
	if out.Signed == "" {
		out.Signed = "none"
	}
	if out.Encrypted == "" {
		out.Encrypted = "none"
	}
	verdicts := map[[2]int]svVerdict{}
	for _, v := range res.Verdicts {
		verdicts[[2]int{v.Layer, v.Index}] = v
	}
	inline := false
	for _, l := range r.Layers {
		format := "openpgp"
		if strings.HasPrefix(l.Format, "smime") {
			format = "smime"
		}
		if l.Format == "pgp-inline" {
			inline = true
		}
		switch l.Kind {
		case "signature":
			for i, sig := range l.Signatures {
				c := SignatureCheck{Format: format, Status: sig.Status, Fingerprint: sig.Fingerprint, Part: l.Covers == "part"}
				if sig.Status != "valid" {
					c.Problem = sig.Error
				}
				switch {
				case len(sig.UserIDs) > 0:
					c.Signer = sig.UserIDs[0]
				case sig.Certificate != nil:
					c.Signer = sig.Certificate.Subject
					if len(sig.Certificate.Emails) > 0 {
						c.Signer = sig.Certificate.Emails[0]
					}
					if c.Fingerprint == "" {
						c.Fingerprint = sig.Certificate.SHA256
					}
				}
				if v, ok := verdicts[[2]int{l.ID, i}]; ok {
					c.Trust, c.Level, c.Reasons = v.Verdict.Status, v.Verdict.Level, v.Verdict.Reasons
					if v.Verdict.Contact != "" {
						c.Signer = v.Verdict.Contact
					}
				}
				out.Signatures = append(out.Signatures, c)
			}
		case "encryption":
			if l.Encryption != nil {
				if l.Encryption.Decrypted {
					out.Decrypted = true
				} else if out.DecryptError == "" {
					out.DecryptError = firstNonEmpty(l.Encryption.Error, l.Error)
				}
			}
		}
	}
	if r.Protected != nil {
		out.Subject = r.Protected.Headers.Subject
	}
	if r.Content != nil && len(r.Content.Raw) > 0 {
		out.Content = contentMessage(r.Content.Raw, inline, outer)
	}
	for _, w := range r.Warnings {
		out.Warnings = append(out.Warnings, w.Message)
	}
	return out
}

// contentMessage is the message inside a layer: a MIME entity to read
// like any message, or, for inline OpenPGP, plain text.
func contentMessage(raw []byte, inline bool, outer Message) *Message {
	m := Message{ID: outer.ID, Folder: outer.Folder, AccountID: outer.AccountID}
	if !inline {
		if in, err := ParseRFC822(raw, outer.Folder, outer.AccountID); err == nil {
			m = in
			m.ID = outer.ID
		} else {
			m.Body = string(raw)
		}
	} else {
		m.Body = string(raw)
	}
	// What the outer message says of itself stays: the inner part of a
	// PGP/MIME or S/MIME message usually has no From, To or Date.
	m.From, m.To, m.Cc, m.Date = firstNonEmpty(m.From, outer.From), firstNonEmpty(m.To, outer.To), firstNonEmpty(m.Cc, outer.Cc), outer.Date
	if m.Subject == "" {
		m.Subject = outer.Subject
	}
	return &m
}
