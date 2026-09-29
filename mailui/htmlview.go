package mailui

import (
	"bytes"
	"image"
	_ "image/gif"  // images in HTML mail
	_ "image/jpeg" // images in HTML mail
	_ "image/png"  // images in HTML mail
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/richtext"
	"github.com/codemodify/uitoolkit/widgets"
	_ "golang.org/x/image/webp" // images in HTML mail
)

// HTML mail is shown formatted, not stripped to text: comms-maild hands the
// message's HTML over and the window renders it with a read-only RichText
// (uitoolkit's HTML subset — no scripts, no external CSS). Links are
// followed on a click, after the real target is shown, so link text cannot
// disguise where it goes.
//
// Images: an inline `data:` image draws as it is; a `cid:` image is a part
// of the message itself and is drawn from it (nothing leaves the machine);
// a remote `http(s)` image is how a sender learns a message was opened, so
// it stays a placeholder until the user asks — Show Images for this
// message, or Always for everything from its sender. comms-maild does the
// fetching, from public addresses only.

// docImages is the images a rendered document asks for, each once, in
// the two kinds that are fetched: cid: parts of the message and remote
// web addresses. A data: image needs neither, and a local file or any
// other scheme is never loaded.
func docImages(d *richtext.Doc) (cids, remote []string) {
	seen := map[string]bool{}
	for _, im := range d.Images() {
		src := im.Src
		t := strings.TrimSpace(src)
		if t == "" || seen[src] {
			continue
		}
		seen[src] = true
		switch imageKind(t) {
		case richtext.ImageInline:
			if hasPrefixFold(t, "cid:") {
				cids = append(cids, src)
			}
		case richtext.ImageRemote:
			if hasPrefixFold(t, "http:") || hasPrefixFold(t, "https:") || strings.HasPrefix(t, "//") {
				remote = append(remote, src)
			}
		}
	}
	return cids, remote
}

// imageKind is richtext.ClassifyImageSrc, except that a protocol-relative
// address (//cdn.example/pixel.gif) is the remote fetch it is: the toolkit
// reads it as a path beside the document (uitoolkit-gaps.md #20).
func imageKind(src string) richtext.ImageKind {
	if strings.HasPrefix(strings.TrimSpace(src), "//") {
		return richtext.ImageRemote
	}
	return richtext.ClassifyImageSrc(src)
}

func hasPrefixFold(s, prefix string) bool {
	_, ok := cutFold(s, prefix)
	return ok
}

// maxImagePixels is the largest image decoded for the HTML view; a bigger
// one stays a placeholder rather than cost hundreds of megabytes.
const maxImagePixels = 4096 * 4096

// decodeImage turns PNG, JPEG, GIF or WebP bytes into pixels, or nil.
func decodeImage(data []byte) *paintengine2d.Image {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxImagePixels {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return paintengine2d.NewImageFromNRGBA(img)
}

// blockedImageResolver is a RichText.ResolveImageKind that draws nothing:
// data: images are decoded by the widget before it is consulted, and
// returning nil leaves a placeholder, with the image's alt text, rather
// than fetching anything.
func blockedImageResolver(string, richtext.ImageKind) *paintengine2d.Image { return nil }

// newReadOnlyRich is a read-only HTML view: links followed through onLink,
// no network for images.
func newReadOnlyRich(onLink func(string)) *widgets.RichText {
	rt := widgets.NewRichText("")
	rt.ReadOnly = true
	rt.MinRows = 8
	rt.OnLink = onLink
	rt.ResolveImageKind = blockedImageResolver
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
