package mailui

import (
	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

// The reading pane's say on who sent a message, under From: the receiving
// server's verdict on the sender's domain when it failed, and the tricks a
// forged sender plays — a name showing another address, a contact's name
// on a stranger's address, a look-alike domain, replies sent elsewhere
// (mailcore.SenderCheck). Nothing shows for a sender with nothing to say.

type senderPart struct {
	view *widgets.FlexBox
	id   mailcore.MessageID
}

func newSenderPart() *senderPart {
	p := &senderPart{view: widgets.NewColumn().WithGap(2)}
	p.view.SetVisible(false)
	return p
}

func (p *senderPart) clear() {
	p.id = ""
	p.view.ClearChildren()
	p.view.SetVisible(false)
}

func (p *senderPart) show(c mailcore.SenderCheck) {
	p.view.ClearChildren()
	line := func(icon style.ToolIcon, text string) {
		l := widgets.NewIconLabel(icon, text)
		l.Wrap = true
		p.view.Add(l)
	}
	for _, w := range c.Warnings {
		line(style.IconWarning, w)
	}
	for _, n := range c.Notes {
		line(style.IconInfo, n)
	}
	p.view.SetVisible(len(c.Warnings)+len(c.Notes) > 0)
	p.view.RequestLayout()
}

// loadSender checks m's sender, once per message shown.
func (r *reader) loadSender(m mailcore.Message) {
	p := r.sender
	if p.id == m.ID || m.ID == "" {
		return
	}
	p.clear()
	p.id = m.ID
	gen, id := r.gen, m.ID
	r.s.async(func() (any, error) {
		return r.s.cli.SenderCheck(id)
	}, func(v any, err error) {
		if err != nil || gen != r.gen || p.id != id {
			return
		}
		p.show(v.(mailcore.SenderCheck))
	})
}
