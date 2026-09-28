package mailui

import (
	"bytes"
	"image"
	"image/png"
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
		`<IMG SRC="https://a.example/x.png?a=1&amp;b=2">`,
	}
	no := []string{
		`<img src="data:image/png;base64,AAAA">`,
		`<img src="cid:logo@1">`, // part of the message: drawn, not blocked
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
	if s.previewRich == nil || s.imgBar == nil {
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
	if !s.imgBar.Visible() {
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
	if s.imgBar.Visible() {
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

func TestHTMLImageSources(t *testing.T) {
	got := htmlImageSources(`<img src="cid:a@x"><img alt='b' src='https://h.example/p.png?x=1&amp;y=2'>` +
		`<img src=//cdn.example/c.gif><img src="data:image/png;base64,AA"><img src="cid:a@x">`)
	want := []string{"cid:a@x", "https://h.example/p.png?x=1&y=2", "//cdn.example/c.gif", "data:image/png;base64,AA"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sources %q", got)
	}
	cids, remote := splitImageSources(got)
	if len(cids) != 1 || len(remote) != 2 {
		t.Fatalf("cids %q remote %q", cids, remote)
	}
}

// The newsletter's inline logo is drawn from the message at once; its
// remote banner waits behind the bar until asked for, and a sender trusted
// with Always is remembered.
func TestInlineImagesDrawAndRemoteWait(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	m, ok, err := s.cli.GetMessage(mailcore.DemoNewsletterID)
	if err != nil || !ok {
		t.Fatalf("newsletter %v %v", ok, err)
	}
	s.showHeaders(m)
	s.showBody(m)
	s.waitIdle()
	a.PumpOnce()
	if s.images[cidKey(m.ID, "logo@news.example")] == nil {
		t.Fatal("the inline logo was not read from the message")
	}
	if got := s.previewRich.ResolveImage("cid:logo@news.example"); got == nil || got.Bounds().Dx() != 96 {
		t.Fatalf("the HTML view does not draw the logo: %v", got)
	}
	if !s.imgBar.Visible() || !strings.Contains(s.alwaysImgs.Tip, "weekly@news.example") {
		t.Fatalf("the remote banner should wait behind the bar (%v %q)", s.imgBar.Visible(), s.alwaysImgs.Tip)
	}
	if s.images[mailcore.DemoNewsletterBanner] != nil {
		t.Fatal("a remote image was loaded without asking")
	}

	// What was fetched is drawn: pretend the banner came back.
	s.cacheImage(mailcore.DemoNewsletterBanner, decodeImage(onePixelPNG(t)))
	s.rerenderHTML(s.imgGen)
	if s.previewRich.ResolveImage(mailcore.DemoNewsletterBanner) == nil {
		t.Fatal("a fetched remote image is not drawn")
	}

	// Always from Sender is kept by the daemon.
	s.alwaysShowImages()
	s.waitIdle()
	senders, _ := s.cli.RemoteImageSenders()
	if len(senders) != 1 || senders[0] != "weekly@news.example" || !s.imgSenders["weekly@news.example"] {
		t.Fatalf("trusted senders %v", senders)
	}
}

func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
