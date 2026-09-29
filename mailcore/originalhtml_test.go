package mailcore

import (
	"strings"
	"testing"
)

// The HTML part as sent — styles and all — out of a multipart message,
// its quoted-printable and charset undone; an attached .html is not it.
func TestOriginalHTML(t *testing.T) {
	raw := "From: a@example.com\r\nSubject: s\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=outer\r\n\r\n" +
		"--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n" +
		"--inner\r\nContent-Type: text/plain\r\n\r\nplain\r\n" +
		"--inner\r\nContent-Type: text/html; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"<html><head><style>p{color:red}</style></head><body><p>Caf=E9</p></body></html>\r\n" +
		"--inner--\r\n" +
		"--outer\r\nContent-Type: text/html; name=page.html\r\nContent-Disposition: attachment; filename=page.html\r\n\r\n<p>attached</p>\r\n" +
		"--outer--\r\n"
	got, ok := OriginalHTML([]byte(raw))
	if !ok || !strings.Contains(got, "<style>p{color:red}</style>") || !strings.Contains(got, "Café") {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := OriginalHTML([]byte("From: a@example.com\r\nSubject: s\r\n\r\nplain text\r\n")); ok {
		t.Fatal("a text message has no HTML")
	}
	attached := "From: a@example.com\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nhi\r\n" +
		"--b\r\nContent-Type: text/html\r\nContent-Disposition: attachment; filename=x.html\r\n\r\n<p>x</p>\r\n--b--\r\n"
	if _, ok := OriginalHTML([]byte(attached)); ok {
		t.Fatal("an attached .html was taken for the message's HTML")
	}
}
