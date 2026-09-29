package mailui

import (
	"encoding/base64"
	"fmt"
	stdhtml "html"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
)

// HTML: nothing of the HTML is drawn in the window. A message that has an
// HTML part has an Open HTML button (reader.go) that opens it in the
// browser, as it was sent — its styles, its remote images — with scripts
// blocked, since it opens as a local file.

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
