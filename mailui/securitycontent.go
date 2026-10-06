package mailui

import (
	"strconv"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// The Security tab's say on what a message's content does
// (mailcore.ContentReport): its links, the images that report it was
// opened and what else its HTML would load, its forms, its attachments,
// and its other headers. An encrypted message's are checked here, on what
// was decrypted, which never leaves the window.

// The tab's content sections.
const (
	secSectionLinks   = "links"
	secSectionRemote  = "remote"
	secSectionAttach  = "attach"
	secSectionHeaders = "headers"
)

// contentOf is what to say of m's content: the report's, or — once an
// encrypted message is opened — what was decrypted, with the outer
// message's headers.
func contentOf(m mailcore.Message, r *mailcore.SecurityReport, sec *mailcore.MessageSecurity) (mailcore.ContentReport, bool) {
	var out mailcore.ContentReport
	if r != nil {
		out = r.Content
	}
	if sec != nil && sec.Content != nil {
		c := sec.Content
		inner := mailcore.CheckContent(c.HTML, c.Body, c.Parts, nil, nil)
		inner.Headers = out.Headers
		return inner, true
	}
	return out, r != nil
}

// quoted is a link's text, shortened, in quotes; "a link" with none.
func quoted(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "A link"
	}
	if r := []rune(s); len(r) > 48 {
		s = string(r[:47]) + "…"
	}
	return "“" + s + "”"
}

// linkLine is one link worth a word, in words, with its mark.
func linkLine(l mailcore.LinkIssue) (style.ToolIcon, string) {
	what := quoted(l.Text)
	switch l.Kind {
	case mailcore.LinkScript:
		return style.IconError, what + " runs code (" + strings.SplitN(l.Href, ":", 2)[0] + "): comms-mail never runs it"
	case mailcore.LinkLookAlike:
		return style.IconError, what + " goes to " + l.Host + ", which looks like " + l.Like + ", a domain you write to"
	case mailcore.LinkElsewhere:
		return style.IconWarning, what + " shows " + l.Shown + " but goes to " + l.Host
	case mailcore.LinkAddress:
		return style.IconWarning, what + " goes to an address, " + l.Host + ", not a name"
	case mailcore.LinkIDN:
		return style.IconWarning, what + " goes to " + l.Unicode + " (" + l.Host + "): letters of another alphabet can imitate familiar ones"
	case mailcore.LinkShortener:
		return style.IconInfo, what + " is a short link (" + l.Host + "): where it goes is hidden until it is opened"
	}
	return style.IconInfo, what + " goes to " + l.Host
}

// linkRows are the Links section.
func linkRows(c mailcore.ContentReport) []widget.Component {
	if c.LinkCount == 0 {
		return []widget.Component{iconLine(style.IconInfo, "No links")}
	}
	var rows []widget.Component
	if len(c.Links) == 0 {
		rows = append(rows, iconLine(style.IconCheck, pluralize(c.LinkCount, "link")+"; each goes where it says"))
	} else {
		worth := strconv.Itoa(len(c.Links)) + " are worth a word"
		if len(c.Links) == 1 {
			worth = "1 is worth a word"
		}
		rows = append(rows, iconLine(style.IconInfo, pluralize(c.LinkCount, "link")+"; "+worth))
	}
	for _, l := range c.Links {
		icon, text := linkLine(l)
		rows = append(rows, iconLine(icon, text))
	}
	return append(rows, iconLine(style.IconInfo, "Clicking a link shows where it goes before it opens"))
}

// remoteRows are the Images and what it would load section, and its
// forms.
func remoteRows(c mailcore.ContentReport) []widget.Component {
	var rows []widget.Component
	if n := len(c.Trackers); n > 0 {
		rows = append(rows, iconLine(style.IconWarning, pluralize(n, "tracking image")+", from "+strings.Join(c.Trackers, ", ")+
			": one pixel, or hidden, there to tell the sender when and where it was opened"))
	}
	if len(c.Remote) > 0 {
		rows = append(rows, iconLine(style.IconInfo, "It would load from "+strings.Join(c.Remote, ", ")+
			"; nothing loads until you choose Show Images, and then each learns you opened it"))
	} else {
		rows = append(rows, iconLine(style.IconCheck, "It loads nothing from elsewhere"))
	}
	for _, f := range c.Forms {
		rows = append(rows, iconLine(style.IconWarning, "A form, sending what is typed in it to "+f+": comms-mail does not send forms"))
	}
	if c.Password {
		rows = append(rows, iconLine(style.IconError, "It asks for a password: no one should ask for one in a message"))
	}
	return rows
}

// attachRows are the Attachments section; none for a message with none.
func attachRows(m mailcore.Message, c mailcore.ContentReport) []widget.Component {
	var rows []widget.Component
	for _, a := range c.Attachments {
		name := a.Name
		var icon style.ToolIcon
		var text string
		switch a.Kind {
		case mailcore.AttachRuns:
			icon, text = style.IconError, name+" is a program, a script or a disk image: opening it runs it"
		case mailcore.AttachHidden:
			icon, text = style.IconError, name+" hides its real ending, which is a program's: "+realEnding(name)
		case mailcore.AttachMacros:
			icon, text = style.IconWarning, name+" can carry macros, programs inside a document: never enable them for it"
		case mailcore.AttachMismatch:
			icon, text = style.IconWarning, name+" says it is "+a.Type+", which its name is not"
		case mailcore.AttachArchive:
			icon, text = style.IconInfo, name+" is an archive: what it holds is not checked"
		default:
			continue
		}
		rows = append(rows, iconLine(icon, text))
	}
	if len(rows) == 0 && len(m.Attachments) > 0 {
		rows = append(rows, iconLine(style.IconCheck, "Nothing about its "+pluralize(len(m.Attachments), "attachment")+" stands out"))
	}
	return rows
}

// realEnding is an attachment's ending as it really is: the last after
// its right-to-left letters are taken out.
func realEnding(name string) string {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '‮', '‭', '‪', '‫', '⁦', '⁧', '⁨', '‏':
			return -1
		}
		return r
	}, name)
	if i := strings.LastIndexByte(clean, '.'); i >= 0 {
		return clean[i:]
	}
	return clean
}

// headerRows are the Other headers section; none when they say nothing.
func headerRows(m mailcore.Message, c mailcore.ContentReport) []widget.Component {
	var rows []widget.Component
	dom := fromDomain(m)
	for _, h := range c.Headers {
		switch h.Kind {
		case mailcore.HeaderReceipt:
			rows = append(rows, iconLine(style.IconInfo, "It asks for a read receipt, to "+h.Value+": comms-mail never sends one"))
		case mailcore.HeaderReturnPath:
			rows = append(rows, iconLine(style.IconInfo, "Bounces go to "+h.Value+", not "+dom+": usually a mailing service sending for it"))
		case mailcore.HeaderMessageID:
			rows = append(rows, iconLine(style.IconInfo, "Its Message-ID was made at "+h.Value+", not "+dom))
		case mailcore.HeaderFuture:
			rows = append(rows, iconLine(style.IconWarning, "It is dated after it arrived ("+h.Value+"): a wrong clock, or a trick to stay on top"))
		case mailcore.HeaderUnsubscribe:
			rows = append(rows, iconLine(style.IconInfo, "A mailing list: it says how to leave it"))
		case mailcore.HeaderOneClick:
			rows = append(rows, iconLine(style.IconInfo, "A mailing list: it says how to leave it, in one click"))
		}
	}
	return rows
}

// contentChips are the chips for the content: risky links, tracking
// images, a form, risky attachments.
func contentChips(c mailcore.ContentReport, add func(secTone, style.ToolIcon, string, string)) {
	var bad, look int
	for _, l := range c.Links {
		switch l.Kind {
		case mailcore.LinkScript, mailcore.LinkLookAlike:
			bad++
		case mailcore.LinkElsewhere, mailcore.LinkAddress, mailcore.LinkIDN:
			look++
		}
	}
	switch {
	case bad > 0:
		add(secBad, style.IconError, pluralize(bad+look, "risky link"), secSectionLinks)
	case look > 0:
		add(secWarn, style.IconWarning, pluralize(look, "link")+" to look at", secSectionLinks)
	}
	if n := len(c.Trackers); n > 0 {
		add(secNeutral, style.IconInfo, pluralize(n, "tracking image"), secSectionRemote)
	}
	switch {
	case c.Password:
		add(secBad, style.IconError, "Asks for a password", secSectionRemote)
	case len(c.Forms) > 0:
		add(secWarn, style.IconWarning, "Has a form", secSectionRemote)
	}
	var risky, odd int
	for _, a := range c.Attachments {
		switch a.Kind {
		case mailcore.AttachRuns, mailcore.AttachHidden:
			risky++
		case mailcore.AttachMacros, mailcore.AttachMismatch:
			odd++
		}
	}
	switch {
	case risky > 0:
		add(secBad, style.IconError, pluralize(risky, "risky attachment"), secSectionAttach)
	case odd > 0:
		add(secWarn, style.IconWarning, pluralize(odd, "attachment")+" to look at", secSectionAttach)
	}
}
