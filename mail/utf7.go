package mail

import (
	"encoding/base64"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Modified UTF-7 for IMAP mailbox names (RFC 3501 §5.1.3). Mailbox names on
// the wire are ASCII: non-ASCII runs are base64 of big-endian UTF-16 with
// '+' as the shift character, ',' instead of '/', and no '=' padding.

var utf7Enc = base64.StdEncoding.WithPadding(base64.NoPadding)

// encodeIMAPUTF7 converts a display mailbox name to modified UTF-7.
func encodeIMAPUTF7(s string) string {
	if isASCIIPrintable(s) && !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	var pending []rune
	flush := func() {
		if len(pending) == 0 {
			return
		}
		u16 := utf16.Encode(pending)
		raw := make([]byte, 0, len(u16)*2)
		for _, u := range u16 {
			raw = append(raw, byte(u>>8), byte(u))
		}
		b.WriteByte('&')
		b.WriteString(strings.ReplaceAll(utf7Enc.EncodeToString(raw), "/", ","))
		b.WriteByte('-')
		pending = pending[:0]
	}
	for _, r := range s {
		if r == '&' {
			flush()
			b.WriteString("&-")
			continue
		}
		if r >= 0x20 && r < 0x7f {
			flush()
			b.WriteRune(r)
			continue
		}
		pending = append(pending, r)
	}
	flush()
	return b.String()
}

// decodeIMAPUTF7 converts a wire mailbox name back to UTF-8. Malformed input
// is returned unchanged rather than mangled.
func decodeIMAPUTF7(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '&' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(s[i+1:], '-')
		if end < 0 {
			return s
		}
		chunk := s[i+1 : i+1+end]
		i += end + 2
		if chunk == "" {
			b.WriteByte('&')
			continue
		}
		raw, err := utf7Enc.DecodeString(strings.ReplaceAll(chunk, ",", "/"))
		if err != nil || len(raw)%2 != 0 {
			return s
		}
		u16 := make([]uint16, 0, len(raw)/2)
		for j := 0; j+1 < len(raw); j += 2 {
			u16 = append(u16, uint16(raw[j])<<8|uint16(raw[j+1]))
		}
		b.WriteString(string(utf16.Decode(u16)))
	}
	return b.String()
}

func isASCIIPrintable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			return false
		}
	}
	return utf8.ValidString(s)
}

// validMailboxName rejects names that cannot be sent safely in a command.
func validMailboxName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == '\r' || r == '\n' || r == 0 {
			return false
		}
	}
	return true
}

// validPartID gates the RPC-supplied section identifier that is interpolated
// into BODY.PEEK[...]: digits, dots, and the named section suffixes only.
func validPartID(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 64 {
		return false
	}
	up := strings.ToUpper(s)
	for _, suffix := range []string{".MIME", ".TEXT", ".HEADER"} {
		up = strings.TrimSuffix(up, suffix)
	}
	if up == "" || up == "TEXT" || up == "HEADER" {
		return true
	}
	for _, r := range up {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}
