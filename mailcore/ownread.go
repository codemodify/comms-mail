package mailcore

import (
	"bytes"
	"errors"
	"strings"
	"time"
)

// Reading signed and encrypted mail with comms-mail's own engines: the
// message is peeled layer by layer — a signature over a part, encryption
// around one — each checked or opened by the format's own engine, until
// what is inside is an ordinary message to show. What was opened is held
// in memory only, like secretvault's.

// maxLayers is as deep as layers are peeled: a message signed, then
// encrypted, then signed again is already unusual.
const maxLayers = 6

// messageFormat is the format a message's outermost protection is in:
// openpgp, smime, or "" for none.
func messageFormat(raw []byte) string {
	fields, body := splitEntity(crlf(raw))
	media, params := contentType(fields)
	switch {
	case media == "multipart/encrypted":
		return FormatOpenPGP
	case media == "multipart/signed":
		if strings.Contains(strings.ToLower(params["protocol"]), "pkcs7") {
			return FormatSMIME
		}
		return FormatOpenPGP
	case isOpaqueSMIME(media):
		return FormatSMIME
	}
	t := strings.TrimLeft(string(firstTextBody(fields, body)), " \t\r\n")
	if strings.HasPrefix(t, "-----BEGIN PGP") {
		return FormatOpenPGP
	}
	return ""
}

// firstTextBody is a single-part text message's text.
func firstTextBody(fields []mimeField, body []byte) []byte {
	media, _ := contentType(fields)
	if !strings.HasPrefix(media, "text/") {
		return nil
	}
	b, err := decodedBody(fields, body)
	if err != nil {
		return nil
	}
	return b
}

// ownInspect checks and opens raw with comms-mail's own engines; decrypt
// opens what is encrypted.
func (s *LocalStore) ownInspect(raw []byte, decrypt bool, m Message) MessageSecurity {
	raw = crlf(raw)
	from := fromAddress(m.From)
	when := m.Date
	if when.IsZero() {
		when = time.Now()
	}
	if s.engineOf(FormatOpenPGP) == EngineOwn {
		fields, _ := splitEntity(raw)
		s.pgpRecordCarried(fields, partsOfType(raw, "application/pgp-keys", 0), from, when)
	}
	out := MessageSecurity{Signed: "none", Encrypted: "none", Checked: true}
	content, inline, opened := s.peel(&out, raw, decrypt, from, 0)
	if opened && content != nil {
		out.Content = contentMessage(content, inline, m)
		if out.Subject != "" {
			out.Content.Subject = out.Subject
		}
	}
	return out
}

// peel checks or opens entity's outer layer, then what is inside it. It
// is what is inside at the last layer peeled, inline when that is plain
// text; opened says it is not what the message already shows (something
// was decrypted, or unwrapped).
func (s *LocalStore) peel(out *MessageSecurity, entity []byte, decrypt bool, from string, depth int) (content []byte, inline, opened bool) {
	if depth >= maxLayers {
		return nil, false, false
	}
	fields, body := splitEntity(entity)
	media, params := contentType(fields)
	switch {
	case media == "multipart/signed":
		parts, err := multipartParts(body, params["boundary"])
		if err != nil || len(parts) < 2 {
			return nil, false, false
		}
		sigFields, sigBody := splitEntity(parts[1])
		sig, err := decodedBody(sigFields, sigBody)
		var c SignatureCheck
		proto := strings.ToLower(params["protocol"])
		switch {
		case err != nil:
			c = SignatureCheck{Status: "bad", Problem: "the signature cannot be read"}
		case strings.Contains(proto, "pkcs7"):
			c = s.smimeVerifyDetached(parts[0], sig, from)
		default:
			c = s.pgpVerifyDetached(parts[0], sig, from)
		}
		out.Signed = "full"
		out.Signatures = append(out.Signatures, c)
		if inner, inl, ok := s.peel(out, parts[0], decrypt, from, depth+1); ok {
			return inner, inl, true
		}
		return parts[0], false, false

	case media == "multipart/encrypted":
		out.Encrypted = "full"
		parts, err := multipartParts(body, params["boundary"])
		if err != nil || len(parts) < 2 {
			out.DecryptError = "the encrypted message cannot be read"
			return nil, false, false
		}
		if !decrypt {
			return nil, false, false
		}
		f2, b2 := splitEntity(parts[1])
		data, err := decodedBody(f2, b2)
		if err != nil {
			out.DecryptError = "the encrypted message cannot be read"
			return nil, false, false
		}
		inner, sig, err := s.pgpDecrypt(data, from)
		if err != nil {
			s.notOpened(out, FormatOpenPGP, err)
			return nil, false, false
		}
		return s.opened(out, crlf(inner), sig, decrypt, from, depth)

	case isOpaqueSMIME(media):
		der, err := decodedBody(fields, body)
		if err != nil {
			out.DecryptError = "the S/MIME message cannot be read"
			return nil, false, false
		}
		if strings.EqualFold(params["smime-type"], "signed-data") || smimeIsSigned(der) {
			inner, c := s.smimeVerifyOpaque(der, from)
			out.Signed = "full"
			out.Signatures = append(out.Signatures, c)
			if inner == nil {
				return nil, false, false
			}
			if deeper, inl, ok := s.peel(out, crlf(inner), decrypt, from, depth+1); ok {
				return deeper, inl, true
			}
			return crlf(inner), false, true
		}
		out.Encrypted = "full"
		if !decrypt {
			return nil, false, false
		}
		inner, err := s.smimeDecrypt(der)
		if err != nil {
			s.notOpened(out, FormatSMIME, err)
			return nil, false, false
		}
		return s.opened(out, crlf(inner), nil, decrypt, from, depth)
	}

	// Inline OpenPGP in a plain text body.
	text := firstTextBody(fields, body)
	t := bytes.TrimLeft(text, " \t\r\n")
	switch {
	case bytes.HasPrefix(t, []byte("-----BEGIN PGP SIGNED MESSAGE-----")):
		plain, c := s.pgpClearsigned(t, from)
		out.Signed = "full"
		out.Signatures = append(out.Signatures, c)
		return plain, true, true
	case bytes.HasPrefix(t, []byte("-----BEGIN PGP MESSAGE-----")):
		out.Encrypted = "full"
		if !decrypt {
			return nil, false, false
		}
		plain, sig, err := s.pgpDecrypt(t, from)
		if err != nil {
			s.notOpened(out, FormatOpenPGP, err)
			return nil, false, false
		}
		out.Decrypted = true
		if sig != nil {
			out.Signed = "full"
			out.Signatures = append(out.Signatures, *sig)
		}
		return plain, true, true
	}
	return nil, false, false
}

// opened is what decrypting gave: the signature inside, the real subject
// it carries, and the layers under it.
func (s *LocalStore) opened(out *MessageSecurity, inner []byte, sig *SignatureCheck, decrypt bool, from string, depth int) ([]byte, bool, bool) {
	out.Decrypted = true
	if sig != nil {
		out.Signed = "full"
		out.Signatures = append(out.Signatures, *sig)
	}
	fields, _ := splitEntity(inner)
	if _, params := contentType(fields); params["protected-headers"] != "" {
		if subj := outerSubject(fields); subj != "" {
			out.Subject = subj
		}
	}
	if deeper, inl, ok := s.peel(out, inner, decrypt, from, depth+1); ok {
		return deeper, inl, true
	}
	return inner, false, true
}

// notOpened says why an encrypted layer was not opened: where comms-mail
// keeps format's keys is locked, or another reason.
func (s *LocalStore) notOpened(out *MessageSecurity, format string, err error) {
	if errors.Is(err, ErrLocked) {
		out.KeysLocked = format
		out.DecryptError = "where comms-mail keeps its keys is locked"
		return
	}
	out.DecryptError = err.Error()
}

// partsOfType are the decoded bodies of the parts of raw of media type
// media, however deep (not inside encryption).
func partsOfType(entity []byte, media string, depth int) [][]byte {
	if depth >= maxLayers {
		return nil
	}
	fields, body := splitEntity(entity)
	m, params := contentType(fields)
	if m == media {
		if b, err := decodedBody(fields, body); err == nil {
			return [][]byte{b}
		}
		return nil
	}
	if !strings.HasPrefix(m, "multipart/") || m == "multipart/encrypted" {
		return nil
	}
	parts, err := multipartParts(body, params["boundary"])
	if err != nil {
		return nil
	}
	var out [][]byte
	for _, p := range parts {
		out = append(out, partsOfType(p, media, depth+1)...)
	}
	return out
}
