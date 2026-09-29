package mailcore

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
)

// OriginalHTML is a message's HTML part exactly as it was sent — only its
// transfer encoding and charset undone — for opening in a browser. The
// copy the window keeps (Message.HTML) has had its scripts, styles and
// frames taken out for the in-app view; this one has not. ok is false for
// a message with no HTML part. An attached .html file is not the message's
// HTML and is not taken.
func OriginalHTML(raw []byte) (html string, ok bool) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", false
	}
	body, _ := io.ReadAll(io.LimitReader(msg.Body, maxPartBytes))
	return findHTMLPart(textproto.MIMEHeader(msg.Header), body, 0)
}

func findHTMLPart(h textproto.MIMEHeader, body []byte, depth int) (string, bool) {
	if depth > 20 {
		return "", false
	}
	media, params, _ := mime.ParseMediaType(h.Get("Content-Type"))
	media = strings.ToLower(media)
	if disp, _, _ := mime.ParseMediaType(h.Get("Content-Disposition")); strings.EqualFold(disp, "attachment") {
		return "", false
	}
	switch {
	case media == "text/html":
		data := decodeCharset(decodeTransfer(body, h.Get("Content-Transfer-Encoding")), params["charset"])
		return string(data), true
	case strings.HasPrefix(media, "multipart/") && params["boundary"] != "":
		r := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			p, err := r.NextRawPart()
			if err != nil {
				return "", false
			}
			pb, _ := io.ReadAll(io.LimitReader(p, maxPartBytes))
			if html, ok := findHTMLPart(p.Header, pb, depth+1); ok {
				return html, true
			}
		}
	}
	return "", false
}
