package mailcore

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"strings"
)

// A message's raw MIME structure, byte for byte, for signing and checking
// signatures: a signature covers a part exactly as it is written, so parts
// are cut out of the message and put into new ones untouched — never
// parsed and written out again.

// crlf is raw with every line ending CRLF, as signatures are made over.
func crlf(raw []byte) []byte {
	if !bytes.Contains(raw, []byte("\n")) {
		return raw
	}
	var b bytes.Buffer
	b.Grow(len(raw) + len(raw)/40)
	for i, c := range raw {
		if c == '\n' && (i == 0 || raw[i-1] != '\r') {
			b.WriteByte('\r')
		}
		b.WriteByte(c)
	}
	return b.Bytes()
}

// mimeField is one header field as written: its name, and its whole text
// (folded lines and the final CRLF included).
type mimeField struct {
	name string
	raw  string
}

// value is the field's value, unfolded.
func (f mimeField) value() string {
	_, v, _ := strings.Cut(f.raw, ":")
	v = strings.ReplaceAll(v, "\r\n", "")
	return strings.TrimSpace(strings.ReplaceAll(v, "\n", ""))
}

// splitEntity is an entity's header fields and its body. raw is CRLF.
func splitEntity(raw []byte) (fields []mimeField, body []byte) {
	head := raw
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		head, body = raw[:i+2], raw[i+4:]
	} else if bytes.HasPrefix(raw, []byte("\r\n")) {
		head, body = nil, raw[2:]
	}
	for _, line := range strings.SplitAfter(string(head), "\r\n") {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(fields) > 0 {
			fields[len(fields)-1].raw += line
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		fields = append(fields, mimeField{name: strings.TrimSpace(name), raw: line})
	}
	return fields, body
}

// field is the first field named name.
func field(fields []mimeField, name string) (mimeField, bool) {
	for _, f := range fields {
		if strings.EqualFold(f.name, name) {
			return f, true
		}
	}
	return mimeField{}, false
}

// contentType is the entity's media type and parameters, text/plain when
// it says none.
func contentType(fields []mimeField) (string, map[string]string) {
	f, ok := field(fields, "Content-Type")
	if !ok {
		return "text/plain", map[string]string{}
	}
	media, params, err := mime.ParseMediaType(f.value())
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.SplitN(f.value(), ";", 2)[0])), map[string]string{}
	}
	return media, params
}

func isContentField(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "content-")
}

// multipartParts are the entities of a multipart body, each exactly as
// written between its delimiters (RFC 2046 §5.1.1: the CRLF before a
// delimiter belongs to the delimiter).
func multipartParts(body []byte, boundary string) ([][]byte, error) {
	if boundary == "" {
		return nil, errors.New("a multipart without a boundary")
	}
	delim := []byte("\r\n--" + boundary)
	b := append([]byte("\r\n"), body...)
	var parts [][]byte
	i := bytes.Index(b, delim)
	if i < 0 {
		return nil, errors.New("a multipart whose boundary never comes")
	}
	for {
		rest := b[i+len(delim):]
		if bytes.HasPrefix(rest, []byte("--")) {
			return parts, nil
		}
		eol := bytes.Index(rest, []byte("\r\n"))
		if eol < 0 {
			return parts, nil
		}
		start := rest[eol+2:]
		j := bytes.Index(start, delim)
		if j < 0 {
			// No closing delimiter: the rest is the last part.
			return append(parts, start), nil
		}
		parts = append(parts, start[:j])
		i = len(b) - len(start) + j
	}
}

// decodedBody is an entity's body with its transfer encoding undone.
func decodedBody(fields []mimeField, body []byte) ([]byte, error) {
	cte := ""
	if f, ok := field(fields, "Content-Transfer-Encoding"); ok {
		cte = strings.ToLower(f.value())
	}
	switch cte {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, body)
		out := make([]byte, base64.StdEncoding.DecodedLen(len(clean)))
		n, err := base64.StdEncoding.Decode(out, clean)
		if err != nil {
			// Unpadded, as some programs write it.
			if out, err2 := base64.RawStdEncoding.DecodeString(strings.TrimRight(string(clean), "=")); err2 == nil {
				return out, nil
			}
			return nil, err
		}
		return out[:n], nil
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
	}
	return body, nil
}

// sevenBit is entity with every part that is not 7-bit safe re-encoded —
// text as quoted-printable, anything else as base64 — for a signature
// must hold however the message travels (RFC 3156 §3, RFC 8551 §3.1.1).
func sevenBit(entity []byte) []byte {
	fields, body := splitEntity(entity)
	media, params := contentType(fields)
	if strings.HasPrefix(media, "multipart/") {
		parts, err := multipartParts(body, params["boundary"])
		if err != nil {
			return entity
		}
		var b bytes.Buffer
		writeFields(&b, fields)
		b.WriteString("\r\n")
		b.WriteString("--" + params["boundary"] + "\r\n")
		for i, p := range parts {
			if i > 0 {
				b.WriteString("\r\n--" + params["boundary"] + "\r\n")
			}
			b.Write(sevenBit(p))
		}
		b.WriteString("\r\n--" + params["boundary"] + "--\r\n")
		return b.Bytes()
	}
	cte := ""
	if f, ok := field(fields, "Content-Transfer-Encoding"); ok {
		cte = strings.ToLower(f.value())
	}
	if cte == "base64" || cte == "quoted-printable" || (cte == "" || cte == "7bit") && isSevenBit(body) {
		return entity
	}
	var enc bytes.Buffer
	name := "base64"
	if strings.HasPrefix(media, "text/") {
		name = "quoted-printable"
		w := quotedprintable.NewWriter(&enc)
		_, _ = w.Write(body)
		_ = w.Close()
		if !bytes.HasSuffix(enc.Bytes(), []byte("\r\n")) {
			enc.WriteString("\r\n")
		}
	} else {
		writeBase64Lines(&enc, body)
	}
	var b bytes.Buffer
	for _, f := range fields {
		if strings.EqualFold(f.name, "Content-Transfer-Encoding") {
			continue
		}
		b.WriteString(f.raw)
	}
	b.WriteString("Content-Transfer-Encoding: " + name + "\r\n\r\n")
	b.Write(enc.Bytes())
	return b.Bytes()
}

// isSevenBit: ASCII only, no NUL, lines short enough for SMTP.
func isSevenBit(b []byte) bool {
	line := 0
	for _, c := range b {
		if c >= 0x80 || c == 0 {
			return false
		}
		if c == '\n' {
			line = 0
			continue
		}
		if line++; line > 998 {
			return false
		}
	}
	return true
}

func writeFields(b *bytes.Buffer, fields []mimeField) {
	for _, f := range fields {
		b.WriteString(f.raw)
	}
}

// writeBase64Lines writes data as base64 in 76-character lines.
func writeBase64Lines(b *bytes.Buffer, data []byte) {
	s := base64.StdEncoding.EncodeToString(data)
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	if s != "" {
		b.WriteString(s + "\r\n")
	}
}

// splitMessage is a message's own header fields — From, To, Subject and
// the rest — and its content as an entity of its own: the Content-* fields
// and the body, 7-bit safe.
func splitMessage(raw []byte) (outer []mimeField, entity []byte) {
	fields, body := splitEntity(crlf(raw))
	var content []mimeField
	for _, f := range fields {
		switch {
		case isContentField(f.name):
			content = append(content, f)
		case strings.EqualFold(f.name, "MIME-Version"):
		default:
			outer = append(outer, f)
		}
	}
	var b bytes.Buffer
	if len(content) == 0 {
		b.WriteString("Content-Type: text/plain; charset=\"us-ascii\"\r\n")
	}
	writeFields(&b, content)
	b.WriteString("\r\n")
	b.Write(body)
	return outer, sevenBit(b.Bytes())
}

// protectedFields are the header fields an encrypted message carries inside
// as well (RFC 9788), the subject among them.
var protectedFields = []string{"From", "To", "Cc", "Reply-To", "Subject", "Date", "Message-ID", "In-Reply-To", "References"}

// withProtectedHeaders is entity with the message's own fields inside it,
// and protected-headers="v1" on its Content-Type, so the encrypted message
// keeps its real subject while the outside shows "...".
func withProtectedHeaders(outer []mimeField, entity []byte) []byte {
	fields, body := splitEntity(entity)
	var b bytes.Buffer
	for _, name := range protectedFields {
		if f, ok := field(outer, name); ok {
			b.WriteString(f.raw)
		}
	}
	for _, f := range fields {
		if strings.EqualFold(f.name, "Content-Type") {
			v := f.value()
			if !strings.Contains(strings.ToLower(v), "protected-headers") {
				v += `; protected-headers="v1"`
			}
			b.WriteString("Content-Type: " + v + "\r\n")
			continue
		}
		b.WriteString(f.raw)
	}
	b.WriteString("\r\n")
	b.Write(body)
	return b.Bytes()
}

// wrapMessage is outer's fields — the subject replaced by "..." when hide
// is set — with a new Content-Type over body.
func wrapMessage(outer []mimeField, hideSubject bool, ctype string, extra []string, body []byte) []byte {
	var b bytes.Buffer
	for _, f := range outer {
		if hideSubject && strings.EqualFold(f.name, "Subject") {
			b.WriteString("Subject: ...\r\n")
			continue
		}
		b.WriteString(f.raw)
	}
	for _, e := range extra {
		b.WriteString(e + "\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: " + ctype + "\r\n\r\n")
	b.Write(body)
	return b.Bytes()
}

// newBoundary is a boundary no part of the message contains.
func newBoundary(parts ...[]byte) string {
	for {
		bd := "comms-" + randID(12)
		clash := false
		for _, p := range parts {
			if bytes.Contains(p, []byte(bd)) {
				clash = true
				break
			}
		}
		if !clash {
			return bd
		}
	}
}

// signedMultipart is multipart/signed (RFC 1847): the entity as it was
// signed, then the signature part.
func signedMultipart(entity []byte, protocol, micalg string, sigPart []byte) (ctype string, body []byte) {
	bd := newBoundary(entity, sigPart)
	var b bytes.Buffer
	b.WriteString("--" + bd + "\r\n")
	b.Write(entity)
	b.WriteString("\r\n--" + bd + "\r\n")
	b.Write(sigPart)
	b.WriteString("\r\n--" + bd + "--\r\n")
	return fmt.Sprintf(`multipart/signed; boundary="%s"; protocol="%s"; micalg=%s`, bd, protocol, micalg), b.Bytes()
}

// outerSubject is the message's subject, decoded.
func outerSubject(fields []mimeField) string {
	f, ok := field(fields, "Subject")
	if !ok {
		return ""
	}
	if dec, err := new(mime.WordDecoder).DecodeHeader(f.value()); err == nil {
		return dec
	}
	return f.value()
}
