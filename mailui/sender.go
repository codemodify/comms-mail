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
	line := func(icon style.ToolIcon, text string) { p.view.Add(iconLine(icon, text)) }
	for _, w := range c.Warnings {
		line(style.IconWarning, w)
	}
	for _, n := range c.Notes {
		line(style.IconInfo, n)
	}
	p.view.SetVisible(len(c.Warnings)+len(c.Notes) > 0)
	p.view.RequestLayout()
}

// loadSender checks m's sender, once per message shown, with the rest of
// its security report: the warnings go under From, the rest to the chips
// and the Security tab (securitytab.go).
func (r *reader) loadSender(m mailcore.Message) {
	p := r.sender
	if p.id == m.ID || m.ID == "" {
		return
	}
	p.clear()
	p.id = m.ID
	gen, id := r.gen, m.ID
	r.s.async(func() (any, error) {
		return r.s.cli.SecurityReport(id)
	}, func(v any, err error) {
		if err != nil || gen != r.gen || p.id != id {
			return
		}
		rep := v.(mailcore.SecurityReport)
		p.show(rep.Sender)
		r.secView.report = &rep
		r.secView.show(r.msg)
	})
}
