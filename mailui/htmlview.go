package mailui

import (
	"regexp"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/widgets"
)

// HTML mail is shown formatted, not stripped to text: comms-maild hands the
// message's HTML over and the window renders it with a read-only RichText
// (uitoolkit's HTML subset — no scripts, no external CSS). Links are
// followed on a click, after the real target is shown, so link text cannot
// disguise where it goes.
//
// Images are the one thing HTML mail fetches from the network, and a remote
// image is how a sender learns a message was opened. So the renderer never
// reaches the network: an inline `data:` image (a small logo carried in the
// message) draws, and every `cid:` or `http(s)` image is left as a
// placeholder. A message with such images shows a line saying so.

var remoteImgRe = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc\s*=\s*["']?\s*(https?:|//|cid:)`)

// htmlHasRemoteImages reports whether html references an image the renderer
// will not draw: a remote URL, or a cid: part it does not fetch.
func htmlHasRemoteImages(html string) bool {
	return remoteImgRe.MatchString(html)
}

// blockedImageResolver is a RichText.ResolveImage that draws nothing:
// data: images are decoded by the widget before it is consulted, so every
// src that reaches it is remote or a cid: part, and returning nil leaves a
// placeholder rather than fetching it.
func blockedImageResolver(string) *paintengine2d.Image { return nil }

// newReadOnlyRich is a read-only HTML view: links followed through onLink,
// no network for images.
func newReadOnlyRich(onLink func(string)) *widgets.RichText {
	rt := widgets.NewRichText("")
	rt.ReadOnly = true
	rt.MinRows = 8
	rt.OnLink = onLink
	rt.ResolveImage = blockedImageResolver
	return rt
}

// openLink follows a link from a message: a mailto: opens a pre-addressed
// Write window, anything else opens in the desktop's browser once the user
// has seen where it goes.
func (s *session) openLink(href string) {
	href = strings.TrimSpace(href)
	if href == "" {
		return
	}
	if rest, ok := cutFold(href, "mailto:"); ok {
		to, _, _ := strings.Cut(rest, "?")
		msg := mailcore.Message{To: to}
		if _, err := OpenCompose(s.app, s.cli, ComposeOptions{Draft: &msg, OnChange: s.refreshAll}); err != nil {
			widgets.Warn(s.win.Content(), "Write", err.Error(), nil)
		}
		return
	}
	widgets.Confirm(s.win.Content(), "Open link?", href, func(yes bool) {
		if yes {
			widgets.OpenLink(s.win.Content(), href, nil)
		}
	})
}

// cutFold is strings.CutPrefix, case-insensitive on the prefix.
func cutFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
