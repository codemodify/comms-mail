package mailui

import (
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

// The reading pane's say on signed and encrypted mail: a line for the
// encryption and one for each signature, each with its mark, in the
// Security tab (securitytab.go), the buttons to unlock under the header.
// The engine chosen for the message's format — secretvault, or
// comms-mail's own — checks it and opens what is encrypted to you
// (mailcore.MessageSecurity); what is decrypted is shown and held in this
// window only — never cached or indexed.

// securityPart is the lines and the buttons to unlock secretvault, or
// where comms-mail keeps its own keys.
type securityPart struct {
	view       *widgets.FlexBox
	lines      *widgets.FlexBox
	unlock     *widgets.Button
	unlockKeys *widgets.Button
	// keysFormat is the format whose own keys are locked.
	keysFormat string
	// id is the message the lines are about; content what secretvault
	// decrypted of it.
	id      mailcore.MessageID
	content *mailcore.Message
}

func newSecurityPart() *securityPart {
	p := &securityPart{lines: widgets.NewColumn().WithGap(2)}
	p.unlock = newButton("Unlock secretvault…", nil)
	p.unlock.SetVisible(false)
	p.unlockKeys = newButton("Unlock your keys…", nil)
	p.unlockKeys.Icon = style.IconLock
	p.unlockKeys.SetVisible(false)
	p.view = widgets.NewColumn(foldRow(p.unlock, p.unlockKeys)).WithGap(4)
	p.view.SetVisible(false)
	return p
}

func (p *securityPart) clear() {
	p.id, p.content = "", nil
	p.lines.ClearChildren()
	p.unlock.SetVisible(false)
	p.unlockKeys.SetVisible(false)
	p.view.SetVisible(false)
}

func (p *securityPart) line(icon style.ToolIcon, text string) {
	p.lines.Add(iconLine(icon, text))
}

// show lays out what secretvault said of the message.
func (p *securityPart) show(sec mailcore.MessageSecurity) {
	p.lines.ClearChildren()
	if sec.Encrypted != "none" {
		switch {
		case sec.Decrypted:
			p.line(style.IconLock, "Encrypted"+coverage(sec.Encrypted)+": opened with your key")
		case sec.DecryptError != "":
			p.line(style.IconLock, "Encrypted"+coverage(sec.Encrypted)+": not opened ("+sec.DecryptError+")")
		default:
			p.line(style.IconLock, "Encrypted"+coverage(sec.Encrypted)+": not opened")
		}
	}
	for _, sig := range sec.Signatures {
		icon, text := signatureLine(sig)
		p.line(icon, text)
	}
	if !sec.Checked && sec.Why != "" && (sec.Signed != "none" || sec.Encrypted != "none") {
		what := "Signed"
		if sec.Encrypted != "none" {
			what = "Encrypted"
		}
		p.line(style.IconInfo, what+", not checked: "+sec.Why)
	}
	for _, w := range sec.Warnings {
		p.line(style.IconWarning, w)
	}
	p.unlock.SetVisible(sec.Locked)
	p.keysFormat = sec.KeysLocked
	p.unlockKeys.SetVisible(sec.KeysLocked != "" && !sec.Locked)
	p.view.SetVisible(sec.Locked || sec.KeysLocked != "")
	p.view.RequestLayout()
	p.lines.RequestLayout()
}

func coverage(c string) string {
	if c == "partial" {
		return " (only part of the message)"
	}
	return ""
}

// signatureLine says what one signature means, with the mark for it: a
// check for a signature that holds from a key verified as the sender's,
// a question for one that holds from a key nobody has verified, a warning
// for anything that should worry the reader.
func signatureLine(sig mailcore.SignatureCheck) (style.ToolIcon, string) {
	who := sig.Signer
	if who == "" {
		who = "an unknown signer"
	}
	part := ""
	if sig.Part {
		part = " (only part of the message is signed)"
	}
	reason := ""
	if len(sig.Reasons) > 0 {
		reason = ": " + sig.Reasons[0]
	}
	switch sig.Status {
	case "valid":
	case "bad":
		return style.IconError, "The signature does not match: the message was changed after " + who + " signed it" + part
	case "unknown-key":
		return style.IconQuestion, "Signed with a key you do not have, so it cannot be checked" + part
	default:
		problem := sig.Problem
		if problem == "" {
			problem = strings.ReplaceAll(sig.Status, "-", " ")
		}
		return style.IconWarning, "Signed by " + who + ", but the signature cannot be trusted: " + problem + part
	}
	switch sig.Trust {
	case "verified":
		return style.IconCheck, "Signed by " + who + ", " + levelWords(sig.Level) + part
	case "suspicious":
		return style.IconWarning, "Signed with a key that is not the sender's: the address belongs to someone else's key" + reason + part
	case "unverified":
		return style.IconQuestion, "Signed by " + who + "; the signature holds, but whose key it is has not been verified" + reason + part
	default:
		return style.IconQuestion, "Signed by " + who + ", with a key not among your contacts" + part
	}
}

// levelWords is how secretvault came to trust a key.
func levelWords(level string) string {
	switch level {
	case "in-person":
		return "verified in person"
	case "organisation":
		return "vouched for by the organisation"
	case "published":
		return "published by the address's own domain"
	case "logged":
		return "recorded in a transparency log"
	case "tofu":
		return "seen before (not verified)"
	case "own":
		return "your own key"
	case "imported":
		return "with a key you brought in yourself"
	case "certificate":
		return "certified by an authority your computer trusts"
	}
	return "verified"
}

// loadSecurity has secretvault check m, which is signed, encrypted or
// carries a key, and shows what it said; an encrypted message's text is
// what secretvault decrypted.
func (r *reader) loadSecurity(m mailcore.Message) {
	p := r.sec
	if p.id == m.ID {
		return
	}
	p.clear()
	p.id = m.ID
	if m.Encrypted {
		r.text.Placeholder = "This message is encrypted. Asking secretvault to open it…"
		r.text.SetText("")
		r.md.clear()
	}
	gen := r.gen
	id := m.ID
	r.s.async(func() (any, error) {
		return r.s.cli.MessageSecurity(id, m.Encrypted)
	}, func(v any, err error) {
		if gen != r.gen || p.id != id {
			return
		}
		if err != nil {
			p.lines.ClearChildren()
			p.line(style.IconWarning, "Could not check this message: "+err.Error())
			r.secView.sec = &mailcore.MessageSecurity{Signed: "none", Encrypted: "none"}
			r.secView.show(r.msg)
			return
		}
		sec := v.(mailcore.MessageSecurity)
		p.show(sec)
		r.secView.sec = &sec
		r.secView.show(r.msg)
		if sec.Subject != "" {
			r.subj.SetText(sec.Subject)
		}
		if sec.Content != nil {
			p.content = sec.Content
			r.showContent(*sec.Content)
		} else if m.Encrypted {
			r.text.Placeholder = "This message is encrypted, and was not opened."
			r.text.SetText("")
		}
	})
}

// showContent puts what secretvault decrypted in the Message and
// Markdown tabs. The message the window acts on stays the one received.
func (r *reader) showContent(c mailcore.Message) {
	r.text.Placeholder = "The decrypted message has no text"
	r.text.SetText(mailcore.DisplayBody(c))
	md := c
	md.HTML = mailcore.MarkdownToHTML(mailcore.BodyMarkdown(c))
	r.md.show(md)
}

// unlockKeys opens where comms-mail keeps the locked format's keys — the
// encrypted file asks for its passphrase, the keyring and secretvault show
// their own prompts — then checks the message showing again.
func (r *reader) unlockKeys() {
	m, format := r.msg, r.sec.keysFormat
	again := func() {
		if r.msg.ID == m.ID {
			r.sec.id = ""
			r.loadSecurity(m)
		}
	}
	r.s.async(func() (any, error) { return r.s.cli.KeysView(format) }, func(v any, err error) {
		if err != nil {
			widgets.Warn(r.view, "Your keys", err.Error(), nil)
			return
		}
		if v.(mailcore.KeysView).Place.Store == mailcore.StoreEncrypted {
			openUnlockKeys(r.s.app, r.s.cli, format, again)
			return
		}
		r.s.async(func() (any, error) { return nil, r.s.cli.UnlockKeys(format, nil) }, func(_ any, err error) {
			if err != nil {
				widgets.Warn(r.view, "Your keys", "They stayed locked: "+err.Error(), nil)
				return
			}
			again()
		})
	})
}

// unlockSecretVault asks secretvault to unlock (with its own prompt), then
// checks the message showing again.
func (r *reader) unlockSecretVault() {
	m, format := r.msg, r.sec.keysFormat
	r.s.async(func() (any, error) {
		if format != "" {
			return nil, r.s.cli.UnlockKeys(format, nil) // the vault the keys are in
		}
		return nil, r.s.cli.UnlockSecrets("")
	}, func(_ any, err error) {
		if err != nil {
			widgets.Warn(r.view, "secretvault", "secretvault stayed locked: "+err.Error(), nil)
			return
		}
		if r.msg.ID == m.ID {
			r.sec.id = ""
			r.loadSecurity(m)
		}
	})
}
