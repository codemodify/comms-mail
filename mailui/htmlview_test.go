package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

func TestHasHTMLBody(t *testing.T) {
	cases := []struct {
		name string
		m    mailcore.Message
		html bool
	}{
		{"html only", mailcore.Message{HTML: "<p>hi</p>"}, true},
		{"html with empty text", mailcore.Message{Body: "  ", HTML: "<p>hi</p>"}, true},
		{"real text part wins", mailcore.Message{Body: "plain words here", HTML: "<p>hi</p>"}, false},
		{"text that is really html", mailcore.Message{Body: "<div>x</div>", HTML: "<div>x</div>"}, true},
		{"no html", mailcore.Message{Body: "just text"}, false},
	}
	for _, c := range cases {
		if got := hasHTMLBody(c.m); got != c.html {
			t.Errorf("%s: hasHTMLBody = %v, want %v", c.name, got, c.html)
		}
	}
}

func TestHTMLHasRemoteImages(t *testing.T) {
	yes := []string{
		`<img src="https://track.example/pixel.gif">`,
		`<img alt=x src='http://a/b.png'>`,
		`<img src=//cdn.example/x.jpg>`,
		`<img src="cid:logo@1">`,
	}
	no := []string{
		`<img src="data:image/png;base64,AAAA">`,
		`<p>no images at all</p>`,
	}
	for _, h := range yes {
		if !htmlHasRemoteImages(h) {
			t.Errorf("should be flagged: %q", h)
		}
	}
	for _, h := range no {
		if htmlHasRemoteImages(h) {
			t.Errorf("should not be flagged: %q", h)
		}
	}
}

// renderMessage shows the HTML view for HTML mail (formatted, tags gone) and
// the plain view for text mail, and flags blocked images.
func TestRenderMessagePicksTheRightView(t *testing.T) {
	rich := newReadOnlyRich(nil)
	plain := widgets.NewTextView("", "")
	img := widgets.NewLabel("")

	renderMessage(rich, plain, img, mailcore.Message{
		Subject: "s", HTML: `<p>Hello <b>world</b></p><img src="https://x/p.gif">`,
	})
	if !rich.Visible() || plain.Visible() {
		t.Fatal("HTML mail should show the rich view, not the plain one")
	}
	if got := rich.PlainText(); !strings.Contains(got, "Hello world") {
		t.Fatalf("rich text = %q, want the words without tags", got)
	}
	if strings.Contains(rich.PlainText(), "<b>") {
		t.Fatal("tags leaked into the rendered text")
	}
	if !img.Visible() {
		t.Fatal("a remote image should raise the blocked-images line")
	}

	renderMessage(rich, plain, img, mailcore.Message{Subject: "s", Body: "just plain words"})
	if rich.Visible() || !plain.Visible() {
		t.Fatal("plain mail should show the plain view")
	}
	if img.Visible() {
		t.Fatal("plain mail has no images to block")
	}
	if plain.Text != "just plain words" {
		t.Fatalf("plain text = %q", plain.Text)
	}
}

// The preview pane builds both views and shows HTML mail formatted.
func TestPreviewRendersHTMLMessage(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	if s.previewRich == nil || s.previewImg == nil {
		t.Fatal("the preview pane has no HTML view")
	}
	s.showHeaders(mailcore.Message{Subject: "hi"})
	s.showBody(mailcore.Message{
		ID: "x", Subject: "hi",
		HTML: `<h1>Title</h1><p>Body with a <a href="https://example.com">link</a>.</p>`,
	})
	a.PumpOnce()
	if !s.previewRich.Visible() || s.preview.Visible() {
		t.Fatal("showBody did not switch to the HTML view for HTML mail")
	}
	if got := s.previewRich.PlainText(); !strings.Contains(got, "Title") || !strings.Contains(got, "link") {
		t.Fatalf("rendered text = %q", got)
	}
	// A plain follow-up goes back to the plain view.
	s.showBody(mailcore.Message{ID: "y", Subject: "hi", Body: "plain now"})
	a.PumpOnce()
	if s.previewRich.Visible() || !s.preview.Visible() {
		t.Fatal("plain mail should show the plain view")
	}
}
