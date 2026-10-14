package mailcore

import (
	"os"
	"path/filepath"
	"testing"
)

// shopHTML is a marketing message as they are written: a title commented
// out, an Outlook-only block, a preview line padded with invisible
// characters, a logo, a tracking pixel, a row of links in a table, a menu
// commented out, and a paragraph for every client but Outlook.
const shopHTML = `<!DOCTYPE html><html><head>
<meta charset="utf-8">
<!--<title>+ Save big (Up to $100 Off)</title>-->
<!--[if (gte mso 9)|(IE)]><xml><o:OfficeDocumentSettings><o:PixelsPerInch>96</o:PixelsPerInch></o:OfficeDocumentSettings></xml><![endif]-->
<style>td { color: red }</style>
</head><body>
<div style="display:none;max-height:0px;overflow:hidden;">+ Save big (Up to $100 Off)
&#847; &zwnj; &nbsp;
&#847; &zwnj; &nbsp;
&#847; &zwnj; &nbsp;
</div>
<table><tr><td><a href="https://shop.example/web">View as a web page</a></td></tr>
<tr><td><a href="https://shop.example/"><img src="logo.png" alt="Shop Pro"></a></td></tr>
<tr><td><img src="https://t.example/p.gif" width="1" height="1" alt=""></td></tr>
</table>
<table><tr>
<td><a href="https://shop.example/a">Paint</a></td>
<td><a href="https://shop.example/b">Tools</a></td>
<td><a href="https://shop.example/c">Bulk</a></td>
</tr></table>
<!--<table><tr><td>Old menu</td></tr></table>-->
<!--[if !mso]><!--><p>For every client but Outlook</p><!--<![endif]-->
<p>Prices valid through
   10/12.</p>
<p>&copy;2026 Shop &amp; Co.</p>
</body></html>`

const shopText = "+ Save big (Up to $100 Off)\n" +
	"View as a web page\n" +
	"Shop Pro\n" +
	"Paint  Tools  Bulk\n" +
	"\n" +
	"For every client but Outlook\n" +
	"\n" +
	"Prices valid through 10/12.\n" +
	"\n" +
	"©2026 Shop & Co."

// An HTML message reads as its words: nothing from a comment, the head or
// Outlook's block, no entity left undecoded, no line of invisible padding,
// a row of links on one line, and a picture as its description.
func TestHTMLToTextReadsAsTheMessageDoes(t *testing.T) {
	if got := HTMLToText(shopHTML); got != shopText {
		t.Fatalf("got\n%s\n---\nwant\n%s", got, shopText)
	}
}

func TestHTMLToTextLayout(t *testing.T) {
	for in, want := range map[string]string{
		"<p>a<br>b<br><br>c</p>":                                          "a\nb\n\nc",
		"<pre>x\n  y</pre>":                                               "x\n  y",
		"<p>Hello\n    world</p>":                                         "Hello world",
		"<ul><li>one</li><li>two</li></ul>":                               "- one\n- two",
		"<head><title>T</title><body>Hi":                                  "Hi",
		"<div></div><div><div></div></div><p>x</p>":                       "x",
		"a&zwnj;b &eacute; &#8364;":                                       "ab é €",
		"<p>\u200d \u200d</p><p>x</p>":                                    "x",
		"<p>Family \U0001F468\u200d\U0001F469\u200d\U0001F467 here</p>":   "Family \U0001F468\u200d\U0001F469\u200d\U0001F467 here",
		"<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>": "a  b\nc",
	} {
		if got := HTMLToText(in); got != want {
			t.Errorf("HTMLToText(%q) = %q, want %q", in, got, want)
		}
	}
}

// A message with no plain-text part has its text made from the HTML, and
// says which HTMLToText made it; one with a plain-text part keeps the
// sender's, whichever part comes first.
func TestParsedTextSaysWhereItCameFrom(t *testing.T) {
	htmlOnly := "From: a@example.com\r\nSubject: s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hi&zwnj;</p>\r\n"
	m, err := ParseRFC822([]byte(htmlOnly), "inbox", "acct")
	if err != nil || m.Body != "Hi" || m.HTMLText != htmlTextVersion {
		t.Fatalf("html only: %q, version %d, %v", m.Body, m.HTMLText, err)
	}
	both := "From: a@example.com\r\nSubject: s\r\nContent-Type: multipart/alternative; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/html\r\n\r\n<p>From the HTML</p>\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nThe sender's own\r\n--b--\r\n"
	m, err = ParseRFC822([]byte(both), "inbox", "acct")
	if err != nil || m.Body != "The sender's own" || m.HTMLText != 0 {
		t.Fatalf("both: %q, version %d, %v", m.Body, m.HTMLText, err)
	}
}

// A message cached by an older comms-mail, its text made by the old
// converter, is made again when it is opened — from its raw message, or
// from the HTML kept when the raw message is not in the cache — and only
// once.
func TestOldHTMLTextIsMadeAgainWhenOpened(t *testing.T) {
	st := newImportStore(t)
	dir := t.TempDir()
	raw := "From: Shop <news@shop.example>\nTo: me@example.org\nSubject: Sale\nMIME-Version: 1.0\n" +
		"Content-Type: multipart/related; boundary=\"b\"\n\n--b\nContent-Type: text/html; charset=UTF-8\n\n" + shopHTML + "\n--b--\n"
	if err := os.WriteFile(filepath.Join(dir, "a.eml"), lf2crlf(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ImportLocalMail([]LocalMailStore{{Source: "Folder", Name: "Inbox", Path: dir, Kind: StoreEML}}); err != nil {
		t.Fatal(err)
	}
	var id MessageID
	for _, f := range st.ListFolders(LocalAccountID) {
		for _, m := range st.ListMessages(f.ID) {
			id = m.ID
		}
	}
	stale := func() {
		st.mu.Lock()
		i, _ := st.indexLocked(id)
		st.Messages[i].Body = "+ Save big (Up to $100 Off)-->\n\n96\n\n\u034f &zwnj;"
		st.Messages[i].Snippet = "+ Save big (Up to $100 Off)-->"
		st.Messages[i].HTMLText = 0
		st.mu.Unlock()
	}
	stale()
	m, ok := st.GetMessage(id)
	if !ok || m.Body != shopText || m.HTMLText != htmlTextVersion || m.Snippet != SnippetOf(shopText) {
		t.Fatalf("opened: body %q, version %d, snippet %q", m.Body, m.HTMLText, m.Snippet)
	}
	// Made once: a text this version made is kept as it is.
	st.mu.Lock()
	i, _ := st.indexLocked(id)
	st.Messages[i].Body = "kept"
	st.mu.Unlock()
	if m, _ := st.GetMessage(id); m.Body != "kept" {
		t.Fatalf("made again: %q", m.Body)
	}
	// Without the raw message, from the HTML kept.
	st.mu.Lock()
	i, _ = st.indexLocked(id)
	p, _ := st.rawPath(st.Messages[i])
	st.mu.Unlock()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	stale()
	if m, _ := st.GetMessage(id); m.HTMLText != htmlTextVersion || m.Body == "" || m.Body[:len("+ Save big")] != "+ Save big" {
		t.Fatalf("from the HTML kept: %q", m.Body)
	}
}
