package mail

import (
	"fmt"
	"mime"
	"net/mail"
	"strings"
)

// Header construction. Everything that reaches the wire goes through here so
// a CR/LF in a To: field (or a subject, or a filename) can never inject a
// header or a body — see BuildRFC822.

// hasCTL reports whether s contains a control character that would break
// header framing.
func hasCTL(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' || c == 0 {
			return true
		}
	}
	return false
}

// sanitizeHeaderValue folds CR/LF/NUL to a space and collapses the runs, so
// an unfoldable value degrades to a single line instead of injecting.
func sanitizeHeaderValue(s string) string {
	if !hasCTL(s) {
		return strings.TrimSpace(s)
	}
	r := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\x00", "")
	out := r.Replace(s)
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	return strings.TrimSpace(out)
}

// ErrHeaderInjection is returned by the strict builders when a value cannot
// be represented on one header line.
type ErrHeaderInjection struct {
	Field string
	Value string
}

func (e ErrHeaderInjection) Error() string {
	return fmt.Sprintf("mail: illegal CR/LF in %s header value %q", e.Field, truncate(e.Value, 60))
}

// encodeAddressList parses an address list and re-serialises it with RFC 2047
// encoded display names. Unparseable input is sanitised and passed through so
// a hand-typed "postmaster" still works, but never with a newline in it.
func encodeAddressList(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if hasCTL(v) {
		return "", ErrHeaderInjection{Field: field, Value: v}
	}
	list, err := mail.ParseAddressList(v)
	if err != nil {
		return sanitizeHeaderValue(v), nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Address == "" {
			continue
		}
		if hasCTL(a.Address) || hasCTL(a.Name) {
			return "", ErrHeaderInjection{Field: field, Value: v}
		}
		if a.Name == "" {
			// Plain addr-spec reads better than <addr> and is still valid.
			out = append(out, a.Address)
			continue
		}
		out = append(out, (&mail.Address{Name: a.Name, Address: a.Address}).String())
	}
	if len(out) == 0 {
		return sanitizeHeaderValue(v), nil
	}
	return strings.Join(out, ", "), nil
}

// encodeUnstructured RFC 2047-encodes a free-text header (Subject, and the
// name= / filename= parameters).
func encodeUnstructured(field, v string) (string, error) {
	if hasCTL(v) {
		return "", ErrHeaderInjection{Field: field, Value: v}
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	return mime.QEncoding.Encode("utf-8", v), nil
}

// encodeMsgIDList validates Message-ID / In-Reply-To / References. Only
// angle-addr tokens survive; anything else is dropped rather than guessed.
func encodeMsgIDList(field, v string) (string, error) {
	if hasCTL(v) {
		return "", ErrHeaderInjection{Field: field, Value: v}
	}
	var out []string
	for _, f := range strings.Fields(v) {
		id := strings.Trim(f, "<>")
		if id == "" || strings.ContainsAny(id, "<> \t") {
			continue
		}
		out = append(out, "<"+id+">")
	}
	return strings.Join(out, " "), nil
}

// paramValue quotes a MIME parameter (a filename) and refuses control
// characters and embedded quotes that would break the parameter list.
func paramValue(field, v string) (string, error) {
	if hasCTL(v) {
		return "", ErrHeaderInjection{Field: field, Value: v}
	}
	v = strings.ReplaceAll(v, `"`, "")
	v = strings.ReplaceAll(v, `\`, "")
	return v, nil
}
