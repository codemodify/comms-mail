package mailui

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/richtext"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// docOf is html as the reading pane's rich-text view reads it.
func docOf(html string) *richtext.Doc {
	rt := newReadOnlyRich(nil)
	rt.SetHTML(html)
	return rt.Document()
}

func TestRemoteImagesAreFound(t *testing.T) {
	yes := []string{
		`<img src="https://track.example/pixel.gif">`,
		`<img alt=x src='http://a/b.png'>`,
		`<img src=//cdn.example/x.jpg>`,
		`<IMG SRC="https://a.example/x.png?a=1&amp;b=2">`,
	}
	no := []string{
		`<img src="data:image/png;base64,AAAA">`,
		`<img src="cid:logo@1">`, // part of the message: drawn, not blocked
		`<img src="file:///etc/passwd">`,
		`<img src="images/logo.png">`,
		`<p>no images at all</p>`,
	}
	for _, h := range yes {
		if _, remote := docImages(docOf(h)); len(remote) != 1 {
			t.Errorf("should be flagged: %q", h)
		}
	}
	for _, h := range no {
		if _, remote := docImages(docOf(h)); len(remote) != 0 {
			t.Errorf("should not be flagged: %q", h)
		}
	}
}

// The Message tab stays text. The HTML tab draws nothing of the HTML: it
// offers to open it in the browser. The Markdown tab renders the message
// (its HTML, through Markdown), with the blocked-images line for remote
// images; a plain message renders there as its text, and has no HTML to
// open.
func TestMessageTabIsTextHTMLOpensAndMarkdownRenders(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	if s.rd.md.rich == nil || s.rd.md.bar == nil {
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

	if got := s.rd.text.Text; !strings.Contains(got, "Plain text version.") {
		t.Fatalf("Message tab = %q, want the text/plain part", got)
	}
	if !s.rd.htmlBtn.Visible() || !s.rd.actions.Visible() {
		t.Fatal("an HTML message has no Open HTML button")
	}
	if got := s.rd.md.rich.PlainText(); !strings.Contains(got, "Title") || !strings.Contains(got, "link") {
		t.Fatalf("Markdown render = %q", got)
	}
	if !s.rd.md.bar.Visible() {
		t.Fatal("a remote image should raise the blocked-images line")
	}

	// A plain-only message: text on Message, rendered on Markdown, nothing
	// to open in the browser, no blocked-images line.
	s.showBody(mailcore.Message{ID: "y", Subject: "hi", Body: "just plain"})
	a.PumpOnce()
	if s.rd.text.Text != "just plain" {
		t.Fatalf("Message tab = %q", s.rd.text.Text)
	}
	if s.rd.md.rich.PlainText() != "just plain" {
		t.Fatalf("Markdown render of a plain message = %q", s.rd.md.rich.PlainText())
	}
	if s.rd.htmlBtn.Visible() || s.rd.actions.Visible() {
		t.Fatal("a plain message offers Open HTML")
	}
	if s.rd.md.bar.Visible() {
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
	cids, remote := docImages(docOf(`<p><img src="cid:a@x"><img alt='b' src='https://h.example/p.png?x=1&amp;y=2'>` +
		`<img src=//cdn.example/c.gif><img src="data:image/png;base64,AA"><img src="cid:a@x"></p>`))
	if strings.Join(cids, "|") != "cid:a@x" {
		t.Fatalf("cids %q", cids)
	}
	if strings.Join(remote, "|") != "https://h.example/p.png?x=1&y=2|//cdn.example/c.gif" {
		t.Fatalf("remote %q", remote)
	}
}

// An image that is not drawn shows what its alt text says, not an empty
// box.
func TestBlockedImageShowsItsAltText(t *testing.T) {
	d := docOf(`<p><img src="https://track.example/banner.png" alt="Autumn sale"></p>`)
	if un := d.UnresolvedImages(); len(un) != 1 || un[0].Alt != "Autumn sale" {
		t.Fatalf("unresolved %v", un)
	}
	if got := d.PlainText(); !strings.Contains(got, "Autumn sale") {
		t.Fatalf("the placeholder does not carry the alt text: %q", got)
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
	if got := s.rd.md.rich.ResolveImageKind("cid:logo@news.example", richtext.ImageInline); got == nil || got.Bounds().Dx() != 96 {
		t.Fatalf("the HTML view does not draw the logo: %v", got)
	}
	for _, im := range s.rd.md.rich.Document().UnresolvedImages() {
		if im.Kind() == richtext.ImageInline {
			t.Fatalf("the logo is still a placeholder: %q", im.Src)
		}
	}
	if !s.rd.md.bar.Visible() || !strings.Contains(s.rd.md.always.Tip, "weekly@news.example") {
		t.Fatalf("the remote banner should wait behind the bar (%v %q)", s.rd.md.bar.Visible(), s.rd.md.always.Tip)
	}
	if s.images[mailcore.DemoNewsletterBanner] != nil {
		t.Fatal("a remote image was loaded without asking")
	}

	// What was fetched is drawn: pretend the banner came back.
	s.cacheImage(mailcore.DemoNewsletterBanner, decodeImage(onePixelPNG(t)))
	s.rd.md.rerender(s.rd.md.gen)
	if s.rd.md.rich.ResolveImageKind(mailcore.DemoNewsletterBanner, richtext.ImageRemote) == nil {
		t.Fatal("a fetched remote image is not drawn")
	}
	if n := len(s.rd.md.rich.Document().UnresolvedImages()); n != 0 {
		t.Fatalf("%d images still placeholders after the banner came back", n)
	}

	// Always from Sender is kept by the daemon.
	s.rd.md.alwaysShow()
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

// Settings → Privacy lists the trusted senders and takes one back.
func TestPrivacyTabRemovesATrustedSender(t *testing.T) {
	cli := demoClient(t)
	if err := cli.AllowRemoteImages("news@example.com", true); err != nil {
		t.Fatal(err)
	}
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 560, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	changed := 0
	col := prefsPrivacy(a, w, cli, func() { changed++ })
	w.SetContent(col)
	a.PumpOnce()
	var remove *widgets.Button
	var table *widgets.TableView
	widget.Walk(col, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Button:
			if v.Text == "Remove" {
				remove = v
			}
		case *widgets.TableView:
			table = v
		}
	})
	if table == nil || table.RowCount != 1 || remove == nil || !remove.Enabled() {
		t.Fatalf("privacy tab: table %v remove %v", table, remove)
	}
	remove.OnClick()
	if got, _ := cli.RemoteImageSenders(); len(got) != 0 || table.RowCount != 0 || changed != 1 || remove.Enabled() {
		t.Fatalf("after remove: %v rows %d changed %d", got, table.RowCount, changed)
	}
}

// The reading pane and a message's own tab have the same three tabs —
// Message, Source, Markdown — and no HTML tab: HTML opens in the browser
// from a button.
func TestReaderTabsAreTheSameEverywhere(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	want := []string{"Message", "Source", "Markdown"}
	titles := func() [][]string {
		var out [][]string
		widget.Walk(mailTree(w), func(c widget.Component) {
			if tv, ok := c.(*widgets.TabView); ok && tv.Visible() {
				out = append(out, tv.Bar().Titles)
			}
		})
		return out
	}
	if got := titles(); len(got) != 1 || strings.Join(got[0], "|") != strings.Join(want, "|") {
		t.Fatalf("reading pane tabs %q", got)
	}
	s.selected = []mailcore.MessageID{mailcore.DemoNewsletterID}
	s.loadPreview()
	s.waitIdle()
	s.openInTab()
	s.waitIdle()
	a.PumpOnce()
	mt, ok := s.activeTab()
	if !ok {
		t.Fatal("no message tab")
	}
	if got := mt.rd.tabs.Bar().Titles; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("message tab tabs %q", got)
	}
	// The tab is the reading pane, larger: the header, the actions, the
	// source when asked for.
	if !mt.rd.htmlBtn.Visible() || mt.rd.subj.Text == "" || !strings.HasPrefix(mt.rd.from.Text, "From: ") {
		t.Fatalf("tab header %q %q, Open HTML %v", mt.rd.subj.Text, mt.rd.from.Text, mt.rd.htmlBtn.Visible())
	}
	mt.rd.tabs.Select(readerTabSource)
	s.waitIdle()
	if !strings.Contains(mt.rd.source.Text, "Subject:") {
		t.Fatalf("tab source %q", mt.rd.source.Text)
	}
}

// Markdown's tables, quotes and rules are drawn as such in the rendered
// view, not as lines of text.
func TestMarkdownRendersTablesQuotesAndRules(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	s.showBody(mailcore.Message{ID: "t", Subject: "prices", Body: "| Item | Price |\n| --- | --- |\n| Tea | £3 |\n\n> said before\n\n---\n\nend"})
	a.PumpOnce()
	kinds := map[richtext.Kind]int{}
	for _, b := range s.rd.md.rich.Document().Blocks() {
		kinds[b.Kind]++
	}
	if kinds[richtext.TableRow] != 2 || kinds[richtext.Quote] == 0 || kinds[richtext.Rule] != 1 {
		t.Fatalf("blocks by kind %v", kinds)
	}
}

// Always holds only for a message the mail server confirmed came from its
// sender: a forged sender cannot borrow it, and it is not offered for one.
func TestAlwaysImagesNeedAConfirmedSender(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	m, ok, err := s.cli.GetMessage(mailcore.DemoNewsletterID)
	if err != nil || !ok {
		t.Fatalf("newsletter %v %v", ok, err)
	}
	s.imgSenders = map[string]bool{"weekly@news.example": true}

	forged := m
	forged.Auth, forged.AuthWhy = mailcore.AuthFail, "dmarc"
	s.showHeaders(forged)
	s.showBody(forged)
	s.waitIdle()
	a.PumpOnce()
	p := s.rd.md
	if p.always.Visible() || !p.bar.Visible() || !strings.Contains(p.notice.Text, "did not confirm") || strings.Contains(p.notice.Text, "Loading") {
		t.Fatalf("unconfirmed: always %v, bar %v, notice %q", p.always.Visible(), p.bar.Visible(), p.notice.Text)
	}

	s.imgSenders = map[string]bool{}
	confirmed := m
	confirmed.ID = "confirmed"
	confirmed.Auth = mailcore.AuthPass
	p.show(confirmed)
	if !p.always.Visible() || strings.Contains(p.notice.Text, "did not confirm") {
		t.Fatalf("confirmed: always %v, notice %q", p.always.Visible(), p.notice.Text)
	}
}
