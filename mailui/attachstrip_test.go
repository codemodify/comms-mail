package mailui

import (
	"fmt"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// The attachment strip is the same height for four attachments or twelve
// (the rest scroll), a message without attachments gives the room back,
// and the message text is never squeezed under the header.
func TestAttachmentStripKeepsItsSize(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	_, winH := w.Size()
	withAttachments := func(n int) mailcore.Message {
		var names []string
		for i := 0; i < n; i++ {
			names = append(names, fmt.Sprintf("file-%02d.pdf", i))
		}
		return mailcore.Message{ID: mailcore.MessageID(fmt.Sprintf("x%d", n)), Subject: "s", From: "a@b",
			HasAttach: n > 0, Attachments: names, Body: "the text"}
	}
	show := func(m mailcore.Message) (tabsTop, paneH float32) {
		s.showHeaders(m)
		s.showBody(m)
		a.PumpOnce()
		// The text starts under the header, in the Message tab.
		tabsTop = widget.LocalToWindow(s.rd.text, s.rd.text.LocalBounds()).Min.Y
		if s.rd.attStrip.Visible() {
			paneH = s.rd.attStrip.LocalBounds().Dy()
		}
		return
	}
	plainTop, _ := show(withAttachments(0))
	fourTop, fourH := show(withAttachments(4))
	twelveTop, twelveH := show(withAttachments(12))
	againTop, _ := show(withAttachments(0))
	t.Logf("text at: plain %v, 4 attachments %v, 12 attachments %v, plain again %v (window %d)", plainTop, fourTop, twelveTop, againTop, winH)

	if fourH != twelveH || fourTop != twelveTop {
		t.Fatalf("the strip grew with the attachments: %v → %v (tabs %v → %v)", fourH, twelveH, fourTop, twelveTop)
	}
	if againTop != plainTop || againTop >= fourTop {
		t.Fatalf("a message without attachments did not get the room back: %v, first %v", againTop, plainTop)
	}
	if plainTop > float32(winH)/2 || twelveTop > float32(winH)*0.6 {
		t.Fatalf("the header takes most of the pane: text starts at %v of %d", twelveTop, winH)
	}
}
