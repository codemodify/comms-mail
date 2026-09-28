package mailui

import (
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
)

// The page Print opens can load nothing: its policy blocks every request,
// remote image sources are taken out, and inline images are embedded.
func TestPrintablePage(t *testing.T) {
	m := mailcore.Message{
		From: "News <n@example.com>", To: "ada@example.com", Subject: "Weekly <b>&</b>",
		Date: time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC),
		HTML: `<p>Hi</p><img src="cid:logo@x" alt="logo"><img alt=pixel src="https://track.example/p.gif">` +
			`<img srcset="https://a.example/x.png 2x"><div background='http://b.example/bg.png'>x</div><img src="data:image/png;base64,AA">`,
		Attachments: []string{"a.pdf"},
	}
	page := printablePage(m, []mailcore.InlineImage{{CID: "logo@x", MIME: "image/png", Data: []byte{1, 2, 3}}})
	for _, want := range []string{
		`content="default-src 'none'; img-src data:;`,
		`<title>Weekly &lt;b&gt;&amp;&lt;/b&gt;</title>`,
		`<td>From</td><td>News &lt;n@example.com&gt;</td>`,
		`src="data:image/png;base64,AQID"`,
		`<img alt=pixel>`,
		`src="data:image/png;base64,AA"`,
		`<td>Attachments</td><td>a.pdf</td>`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}
	for _, bad := range []string{"track.example", "a.example", "b.example", "<script"} {
		if strings.Contains(page, bad) {
			t.Fatalf("page still has %q", bad)
		}
	}
	plain := printablePage(mailcore.Message{Subject: "s", Body: "line <1>\nline 2"}, nil)
	if !strings.Contains(plain, "<pre>line &lt;1&gt;\nline 2</pre>") {
		t.Fatalf("plain page:\n%s", plain)
	}
}

func TestEMLFileName(t *testing.T) {
	for subj, want := range map[string]string{
		"Re: Invoice 3/2026 <final>": "Re Invoice 3 2026 final.eml",
		"":                           "message.eml",
		"...":                        "message.eml",
	} {
		if got := emlFileName(mailcore.Message{Subject: subj}); got != want {
			t.Fatalf("%q → %q want %q", subj, got, want)
		}
	}
}
