package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
)

// The page the browser opens is the HTML as sent — styles and remote
// images kept — with its inline images embedded, our charset first and
// scripts, plugins, frames and forms blocked.
func TestBrowserPageIsTheHTMLAsSent(t *testing.T) {
	sent := `<html><head><meta charset="iso-8859-1"><style>p{color:red}</style></head>` +
		`<body><p>Hi</p><img src="cid:logo@x"><img src="https://track.example/p.gif"><script>alert(1)</script></body></html>`
	page := browserPage(sent, []mailcore.InlineImage{{CID: "logo@x", MIME: "image/png", Data: []byte{1, 2, 3}}})
	head := page[:strings.Index(page, "<html>")]
	if !strings.Contains(head, `<meta charset="utf-8">`) || !strings.Contains(head, "script-src 'none'") || !strings.Contains(head, "form-action 'none'") {
		t.Fatalf("the page does not start with our charset and policy:\n%s", head)
	}
	for _, want := range []string{"<style>p{color:red}</style>", `src="data:image/png;base64,AQID"`, `src="https://track.example/p.gif"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(page, "cid:logo@x") {
		t.Error("the inline image was left as cid:")
	}
}
