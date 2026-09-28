package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
)

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

// The Message tab stays text; the HTML tab renders the HTML part formatted,
// with the blocked-images line for remote images. A message with both parts
// (multipart/alternative, the common case) shows text on Message and the
// render on HTML — the earlier build showed only the text and never the
// render.
func TestMessageTabIsTextAndHTMLTabRenders(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	if s.previewRich == nil || s.previewImg == nil {
		t.Fatal("the preview pane has no HTML tab")
	}
	m := mailcore.Message{
		ID: "x", Subject: "hi",
		Body: "Plain text version.",
		HTML: `<h1>Title</h1><p>Body with a <a href="https://example.com">link</a>.</p>` +
			`<img src="https://track.example/p.gif">`,
	}
	s.showHeaders(m)
	s.showBody(m)
	a.PumpOnce()

	if got := s.preview.Text; !strings.Contains(got, "Plain text version.") {
		t.Fatalf("Message tab = %q, want the text/plain part", got)
	}
	if got := s.previewRich.PlainText(); !strings.Contains(got, "Title") || !strings.Contains(got, "link") {
		t.Fatalf("HTML tab render = %q", got)
	}
	if !s.previewImg.Visible() {
		t.Fatal("a remote image should raise the blocked-images line")
	}

	// A plain-only message: text on Message, HTML tab empty with its
	// placeholder, no blocked-images line.
	s.showBody(mailcore.Message{ID: "y", Subject: "hi", Body: "just plain"})
	a.PumpOnce()
	if s.preview.Text != "just plain" {
		t.Fatalf("Message tab = %q", s.preview.Text)
	}
	if s.previewRich.PlainText() != "" {
		t.Fatalf("HTML tab should be empty for a plain message, got %q", s.previewRich.PlainText())
	}
	if s.previewImg.Visible() {
		t.Fatal("plain mail has no images to block")
	}
}

// A link in the HTML tab is clickable: clicking it (read-only RichText)
// routes through the session's link handler. A mailto: opens a Write window.
func TestHTMLLinkOpensCompose(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	before := len(s.app.Windows())
	// openLink is what a click on a link calls (RichText.OnLink); a mailto:
	// opens compose without a confirm dialog.
	s.openLink("mailto:help@shop.example?subject=Hi")
	a.PumpOnce()
	if len(s.app.Windows()) != before+1 {
		t.Fatalf("mailto: link did not open a Write window (windows %d -> %d)", before, len(s.app.Windows()))
	}
}
