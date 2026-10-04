package mailui

import (
	"strconv"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/richtext"
	"github.com/codemodify/uitoolkit/widgets"
)

// The HTML view's pictures. Inline (cid:) images come from the message
// itself and are drawn as soon as they are read; remote ones wait for Show
// Images (this message) or Always (every message from its sender), and
// comms-maild fetches them. Whatever was drawn is kept for the session, so
// going back to a message — or opening it in a tab — does not fetch its
// images again.

// maxCachedImages bounds the session's picture cache; past it, it starts
// over.
const maxCachedImages = 400

const remoteImagesNotice = "Remote images are not shown — loading them tells the sender you opened this."

func cidKey(id mailcore.MessageID, cid string) string {
	return "cid:" + string(id) + ":" + strings.Trim(strings.TrimSpace(cid), "<>")
}

// htmlPane is a message's HTML: the rendering, and above it the bar that
// offers its remote images. The reading pane has one, and so does every
// message tab.
type htmlPane struct {
	s      *session
	rich   *widgets.RichText
	notice *widgets.Label
	always *widgets.Button
	bar    *widgets.FlexBox
	view   *widgets.FlexBox // the bar over the rendering
	msg    mailcore.Message
	shown  bool
	gen    uint64 // one per message shown: late answers for another are dropped
}

func newHTMLPane(s *session) *htmlPane {
	p := &htmlPane{s: s}
	p.rich = newReadOnlyRich(s.openLink)
	p.rich.Placeholder = "This message has no HTML part."
	p.notice = wrapLabel(remoteImagesNotice)
	p.always = newButton("Always", p.alwaysShow)
	p.bar = widgets.NewColumn(p.notice,
		foldRow(newButton("Show Images", p.showRemote), p.always)).WithGap(4)
	p.bar.SetVisible(false)
	p.view = widgets.NewColumn(p.bar, p.rich).WithGap(4)
	p.view.AddFlex(p.rich, 1)
	return p
}

// show renders m's HTML part, or leaves the placeholder for a message that
// has none. The renderer makes no network request.
func (p *htmlPane) show(m mailcore.Message) {
	p.gen++
	p.msg, p.shown = m, true
	if strings.TrimSpace(m.HTML) == "" {
		p.rich.SetHTML("")
		p.bar.SetVisible(false)
		return
	}
	p.rich.ResolveImageKind = p.s.imageResolver(m.ID)
	p.rich.SetHTML(m.HTML)
	p.loadImages()
}

// clear empties the pane (loading, errors, no selection).
func (p *htmlPane) clear() {
	p.gen++
	p.msg, p.shown = mailcore.Message{}, false
	p.rich.SetHTML("")
	p.bar.SetVisible(false)
}

// loadImages fetches what the HTML needs and is allowed: its inline parts
// always, its remote images when the sender is trusted. Otherwise the bar
// offers them.
func (p *htmlPane) loadImages() {
	s, m := p.s, p.msg
	cids, remote := docImages(p.rich.Document())
	missingCID := false
	for _, c := range cids {
		if rest, _ := cutFold(strings.TrimSpace(c), "cid:"); s.images[cidKey(m.ID, rest)] == nil {
			missingCID = true
		}
	}
	var missing []string
	for _, u := range remote {
		if s.images[u] == nil {
			missing = append(missing, u)
		}
	}
	if missingCID {
		gen, id := p.gen, m.ID
		s.async(func() (any, error) {
			return s.cli.InlineImages(id)
		}, func(v any, err error) {
			if err != nil || gen != p.gen {
				return
			}
			for _, im := range v.([]mailcore.InlineImage) {
				s.cacheImage(cidKey(id, im.CID), decodeImage(im.Data))
			}
			p.rerender(gen)
		})
	}
	p.bar.SetVisible(len(missing) > 0)
	if len(missing) == 0 {
		return
	}
	// Always — this sender's images load without asking — holds only for
	// a message the receiving server confirmed came from the sender's
	// domain: a forged sender must not be able to borrow it.
	sender := strings.ToLower(mailcore.ExtractAddr(m.From))
	confirmed := m.Auth == mailcore.AuthPass
	p.always.Tip = "Load remote images in every message from " + sender
	p.always.SetVisible(strings.Contains(sender, "@") && confirmed)
	switch {
	case s.imgSenders[sender] && confirmed:
		p.fetchRemote(missing)
	case s.imgSenders[sender]:
		p.notice.SetText(remoteImagesNotice + " " + unconfirmedImagesNote)
	default:
		p.notice.SetText(remoteImagesNotice)
	}
}

// unconfirmedImagesNote is why a sender whose images always load did not
// have them load this time.
const unconfirmedImagesNote = "This sender's images load by themselves only when your mail server confirms the message came from them, and it did not confirm this one."

// rerender draws the HTML again, now that more of its images are in —
// unless another message is showing by now.
func (p *htmlPane) rerender(gen uint64) {
	if gen != p.gen || !p.shown {
		return
	}
	p.rich.SetHTML(p.msg.HTML)
	p.rich.Invalidate()
}

// fetchRemote has the daemon download urls for the message showing.
func (p *htmlPane) fetchRemote(urls []string) {
	s, gen := p.s, p.gen
	p.notice.SetText("Loading images…")
	s.async(func() (any, error) {
		return s.cli.FetchImages(urls)
	}, func(v any, err error) {
		if gen != p.gen {
			return
		}
		if err != nil {
			p.notice.SetText("Images could not be loaded: " + err.Error())
			return
		}
		failed := 0
		for _, im := range v.([]mailcore.RemoteImage) {
			img := decodeImage(im.Data)
			if img == nil {
				failed++
				continue
			}
			s.cacheImage(im.URL, img)
		}
		p.notice.SetText(remoteImagesNotice)
		if failed > 0 {
			s.mark(pluralize(failed, "image") + " could not be loaded")
		}
		p.bar.SetVisible(false)
		p.rerender(gen)
	})
}

// showRemote loads the remote images of the message showing, once.
func (p *htmlPane) showRemote() {
	if !p.shown {
		return
	}
	if _, remote := docImages(p.rich.Document()); len(remote) > 0 {
		p.fetchRemote(remote)
	}
}

// alwaysShow trusts the message's sender with remote images from now on,
// and loads this message's.
func (p *htmlPane) alwaysShow() {
	if !p.shown {
		return
	}
	sender := strings.ToLower(mailcore.ExtractAddr(p.msg.From))
	if !strings.Contains(sender, "@") {
		return
	}
	p.s.trustImageSender(sender, true)
	p.showRemote()
}

// imageResolver is the HTML view's ResolveImageKind for message id: cid:
// parts and remote images from the cache (a remote one is there only once
// the user asked for it), a placeholder for everything else — a local
// file is never read for a message.
func (s *session) imageResolver(id mailcore.MessageID) func(string, richtext.ImageKind) *paintengine2d.Image {
	return func(src string, kind richtext.ImageKind) *paintengine2d.Image {
		switch kind {
		case richtext.ImageInline:
			if rest, ok := cutFold(strings.TrimSpace(src), "cid:"); ok {
				return s.images[cidKey(id, rest)]
			}
		case richtext.ImageRemote:
			return s.images[src]
		}
		return nil
	}
}

func (s *session) cacheImage(key string, img *paintengine2d.Image) {
	if img == nil {
		return
	}
	if s.images == nil || len(s.images) >= maxCachedImages {
		s.images = map[string]*paintengine2d.Image{}
	}
	s.images[key] = img
}

// trustImageSender remembers (or forgets) a sender whose remote images
// load without asking, here and in the daemon.
func (s *session) trustImageSender(sender string, allow bool) {
	if s.imgSenders == nil {
		s.imgSenders = map[string]bool{}
	}
	if allow {
		s.imgSenders[sender] = true
		s.mark("Images from " + sender + " will load without asking")
	} else {
		delete(s.imgSenders, sender)
		s.mark("Images from " + sender + " will wait to be asked for")
	}
	s.async(func() (any, error) {
		return nil, s.cli.AllowRemoteImages(sender, allow)
	}, nil)
}

// loadImageSenders reads the trusted senders from the daemon.
func (s *session) loadImageSenders() {
	if s.cli == nil {
		return
	}
	s.async(func() (any, error) {
		return s.cli.RemoteImageSenders()
	}, func(v any, err error) {
		if err != nil {
			return
		}
		s.imgSenders = map[string]bool{}
		for _, a := range v.([]string) {
			s.imgSenders[a] = true
		}
	})
}

func pluralize(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
