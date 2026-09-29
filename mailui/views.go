package mailui

import (
	"encoding/base64"
	"fmt"
	stdhtml "html"
	"os"
	"path/filepath"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/widgets"
)

// Two of a message's views.
//
// HTML: nothing of the HTML is drawn in the window. A message that has an
// HTML part offers to open it in the browser, as it was sent — its styles,
// its remote images — with scripts blocked, since it opens as a local file.
//
// Markdown: the message as Markdown (mailcore.MessageMarkdown), rendered in
// the window's rich-text view — a clean reading view of HTML mail, and real
// formatting for mail written in Markdown — with the Markdown text beside
// it. The rendering draws inline images; remote ones wait for Show Images,
// as before (htmlPane).

// browserPane is the HTML tab.
type browserPane struct {
	s    *session
	view *widgets.FlexBox
	note *widgets.Label
	warn *widgets.Label
	open *widgets.Button
	msg  mailcore.Message
}

func newBrowserPane(s *session) *browserPane {
	p := &browserPane{s: s}
	p.note = wrapLabel("Select a message.")
	p.warn = wrapLabel("It opens as it was sent, remote images and all: loading them tells the sender you opened it. Scripts are blocked.")
	p.open = widgets.NewButton("Open in browser", p.openInBrowser)
	p.view = widgets.NewColumn(p.note, widgets.NewRow(p.open).WithGap(8), p.warn).WithGap(8)
	p.view.AddFlex(widgets.NewSpacer(), 1)
	p.clear()
	return p
}

// show is m's HTML tab: whether it has an HTML version to open.
func (p *browserPane) show(m mailcore.Message) {
	p.msg = m
	has := strings.TrimSpace(m.HTML) != ""
	if has {
		p.note.SetText("This message has an HTML version.")
	} else {
		p.note.SetText("This message has no HTML version.")
	}
	p.open.SetVisible(has)
	p.warn.SetVisible(has)
}

func (p *browserPane) clear() {
	p.msg = mailcore.Message{}
	p.note.SetText("Select a message.")
	p.open.SetVisible(false)
	p.warn.SetVisible(false)
}

// browserCSP is what the page opened in the browser may not do: run
// scripts (a local file would run them), embed plugins or frames, send a
// form. Everything else — styles, remote images — is as the sender made it.
const browserCSP = "script-src 'none'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'"

// htmlViewPattern names the pages handed to the browser.
const htmlViewPattern = "comms-mail-html-*.html"

// browserPage is the HTML as sent, its inline images embedded, under the
// policy above. A charset of our own comes first: the text is UTF-8 now,
// whatever the part said.
func browserPage(html string, images []mailcore.InlineImage) string {
	return "<!doctype html>\n<meta charset=\"utf-8\">\n<meta http-equiv=\"Content-Security-Policy\" content=\"" + browserCSP + "\">\n" +
		inlineCIDImages(html, images)
}

func (p *browserPane) openInBrowser() {
	m := p.msg
	if m.ID == "" {
		return
	}
	s := p.s
	s.mark("Opening the HTML in your browser…")
	s.async(func() (any, error) {
		raw, err := s.cli.Raw(m.ID)
		if err != nil {
			return nil, err
		}
		html, ok := mailcore.OriginalHTML(raw)
		if !ok {
			html = m.HTML // no part in the source (an imported message): the window's copy
		}
		imgs, _ := s.cli.InlineImages(m.ID)
		mailcore.RemoveOld(filepath.Join(os.TempDir(), htmlViewPattern), mailcore.PrintedKeep)
		f, err := os.CreateTemp("", htmlViewPattern)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if _, err := f.WriteString(browserPage(html, imgs)); err != nil {
			return nil, err
		}
		if !mailcore.OpenWithDesktop(f.Name()) {
			return f.Name(), errNoBrowser(f.Name())
		}
		return f.Name(), nil
	}, func(_ any, err error) {
		if err != nil {
			widgets.Warn(s.win.Content(), "Open in browser", err.Error(), nil)
			return
		}
		s.mark("Opened in your browser")
	})
}

// mdPane is the Markdown tab: the rendering, and the Markdown text.
type mdPane struct {
	s        *session
	view     *widgets.TabView
	rendered *htmlPane
	text     *widgets.TextArea
}

func newMDPane(s *session) *mdPane {
	p := &mdPane{s: s, rendered: newHTMLPane(s)}
	p.rendered.rich.Placeholder = "This message has no text."
	p.text = widgets.NewMonoTextView("", "Select a message")
	p.text.MinRows = 8
	p.view = widgets.NewTabView(
		widgets.Tab{Title: "Rendered", Content: widgets.NewPad(4, p.rendered.view)},
		widgets.Tab{Title: "Text", Content: widgets.NewPad(4, p.text)},
	)
	return p
}

// show renders m as Markdown, and puts its Markdown text beside.
func (p *mdPane) show(m mailcore.Message) {
	r := m
	r.HTML = mailcore.MarkdownToHTML(mailcore.BodyMarkdown(m))
	p.rendered.show(r)
	p.text.SetText(mailcore.MessageMarkdown(m))
}

func (p *mdPane) clear() {
	p.rendered.clear()
	p.text.SetText("")
}

// inlineCIDImages embeds the message's inline images (src="cid:…") as
// data: URIs, which a browser can show from a file; every other source is
// left as it was sent.
func inlineCIDImages(body string, images []mailcore.InlineImage) string {
	if len(images) == 0 {
		return body
	}
	byCID := map[string]mailcore.InlineImage{}
	for _, im := range images {
		byCID[strings.ToLower(im.CID)] = im
	}
	return remoteSrcRe.ReplaceAllStringFunc(body, func(attr string) string {
		m := remoteSrcRe.FindStringSubmatch(attr)
		if strings.ToLower(m[1]) != "src" {
			return attr
		}
		val := stdhtml.UnescapeString(strings.Trim(m[2], `"'`))
		if rest, ok := cutFold(strings.TrimSpace(val), "cid:"); ok {
			if im, ok := byCID[strings.ToLower(strings.Trim(rest, "<>"))]; ok {
				return ` src="data:` + im.MIME + `;base64,` + base64.StdEncoding.EncodeToString(im.Data) + `"`
			}
		}
		return attr
	})
}

func errNoBrowser(path string) error {
	return fmt.Errorf("no browser to open it with — the page is %s", path)
}
