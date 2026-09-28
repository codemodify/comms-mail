package mailui

import (
	"strconv"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
)

// The HTML view's pictures. Inline (cid:) images come from the message
// itself and are drawn as soon as they are read; remote ones wait for Show
// Images (this message) or Always from Sender (every message from it), and
// comms-maild fetches them. Whatever was drawn is kept for the session, so
// going back to a message does not fetch its images again.

// maxCachedImages bounds the session's picture cache; past it, it starts
// over.
const maxCachedImages = 400

func cidKey(id mailcore.MessageID, cid string) string {
	return "cid:" + string(id) + ":" + strings.Trim(strings.TrimSpace(cid), "<>")
}

// imageResolver is the HTML view's ResolveImage for message id: cid:
// parts and remote URLs from the cache, a placeholder for the rest.
func (s *session) imageResolver(id mailcore.MessageID) func(string) *paintengine2d.Image {
	return func(src string) *paintengine2d.Image {
		if rest, ok := cutFold(strings.TrimSpace(src), "cid:"); ok {
			return s.images[cidKey(id, rest)]
		}
		return s.images[src]
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

// loadMessageImages fetches what m's HTML needs and is allowed: its inline
// parts always, its remote images when the sender is trusted. Otherwise
// the bar offers them.
func (s *session) loadMessageImages(m mailcore.Message) {
	cids, remote := splitImageSources(htmlImageSources(m.HTML))
	var missingCID bool
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
		gen, id := s.imgGen, m.ID
		s.async(func() (any, error) {
			return s.cli.InlineImages(id)
		}, func(v any, err error) {
			if err != nil || gen != s.imgGen {
				return
			}
			for _, im := range v.([]mailcore.InlineImage) {
				s.cacheImage(cidKey(id, im.CID), decodeImage(im.Data))
			}
			s.rerenderHTML(gen)
		})
	}
	if s.imgBar == nil {
		return
	}
	s.imgBar.SetVisible(len(missing) > 0)
	if len(missing) == 0 {
		return
	}
	sender := strings.ToLower(mailcore.ExtractAddr(m.From))
	s.alwaysImgs.Tip = "Load remote images in every message from " + sender
	s.alwaysImgs.SetVisible(strings.Contains(sender, "@"))
	if s.imgSenders[sender] {
		s.fetchRemoteImages(missing)
	}
}

// rerenderHTML draws the shown message's HTML again, now that more of its
// images are in — unless another message is showing by now.
func (s *session) rerenderHTML(gen uint64) {
	if gen != s.imgGen || !s.shownOK || s.previewRich == nil {
		return
	}
	s.previewRich.SetHTML(s.shown.HTML)
	s.previewRich.Invalidate()
}

// fetchRemoteImages has the daemon download urls for the message showing.
func (s *session) fetchRemoteImages(urls []string) {
	gen := s.imgGen
	s.previewImg.SetText("Loading images…")
	s.async(func() (any, error) {
		return s.cli.FetchImages(urls)
	}, func(v any, err error) {
		if gen != s.imgGen {
			return
		}
		if err != nil {
			s.previewImg.SetText("Images could not be loaded: " + err.Error())
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
		s.previewImg.SetText("Remote images are not shown — loading them tells the sender you opened this.")
		if failed > 0 {
			s.mark(pluralize(failed, "image") + " could not be loaded")
		}
		s.imgBar.SetVisible(false)
		s.rerenderHTML(gen)
	})
}

// showRemoteImages loads the remote images of the message showing, once.
func (s *session) showRemoteImages() {
	if !s.shownOK {
		return
	}
	_, remote := splitImageSources(htmlImageSources(s.shown.HTML))
	if len(remote) > 0 {
		s.fetchRemoteImages(remote)
	}
}

// alwaysShowImages trusts the showing message's sender with remote images
// from now on, and loads this message's.
func (s *session) alwaysShowImages() {
	if !s.shownOK {
		return
	}
	sender := strings.ToLower(mailcore.ExtractAddr(s.shown.From))
	if !strings.Contains(sender, "@") {
		return
	}
	if s.imgSenders == nil {
		s.imgSenders = map[string]bool{}
	}
	s.imgSenders[sender] = true
	s.async(func() (any, error) {
		return nil, s.cli.AllowRemoteImages(sender, true)
	}, nil)
	s.mark("Images from " + sender + " will load without asking")
	s.showRemoteImages()
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
