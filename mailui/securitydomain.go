package mailui

import (
	"strconv"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// The Security tab's say on what the sender's domain publishes about its
// mail (mailcore.DomainReport) — looked up in DNS only when asked, as the
// resolver sees which domain is looked up; an answer is kept an hour, and
// shows for every message from that domain meanwhile.

// secSectionDomain is the tab's section on the sender's domain.
const secSectionDomain = "domain"

// domainRows are the section: what it is, the button to look it up — or
// what was found.
func domainRows(dom string, d *mailcore.DomainReport, looking bool, lookup func(string)) []widget.Component {
	if dom == "" {
		return []widget.Component{iconLine(style.IconInfo, "From has no domain to look up")}
	}
	var rows []widget.Component
	if lookup != nil {
		text := "Look up " + dom + " in DNS"
		if d != nil {
			text = "Look up again"
		}
		btn := newButton(text, func() { lookup(dom) })
		btn.Icon = style.IconSearch
		btn.SetEnabled(!looking)
		if looking {
			btn.Text = "Looking up " + dom + "…"
		}
		rows = append(rows, widgets.NewRow(btn))
	}
	if d == nil {
		return append(rows, iconLine(style.IconInfo, "What "+dom+" publishes about its mail — who may send for it, what to do with forgeries, "+
			"whether mail to it must use TLS — looked up only when you ask: your DNS resolver sees which domain you look up"))
	}
	line := func(icon style.ToolIcon, text string) { rows = append(rows, iconLine(icon, text)) }
	if len(d.MX) > 0 {
		line(style.IconMail, "Its mail servers: "+strings.Join(d.MX, ", "))
	} else {
		line(style.IconWarning, "It has no mail servers (MX): it takes no mail, and replies to it bounce")
	}
	switch {
	case d.SPF == "":
		line(style.IconWarning, "No SPF: it does not say which servers send for it")
	case d.SPFAll == "-all":
		line(style.IconCheck, "SPF ends with -all: mail from any server it does not list is to be refused")
	case d.SPFAll == "~all":
		line(style.IconInfo, "SPF ends with ~all: mail from servers it does not list is only marked")
	case d.SPFAll == "?all":
		line(style.IconWarning, "SPF says nothing of other servers (?all)")
	case d.SPFAll == "+all":
		line(style.IconError, "SPF lets any server send as it (+all): it proves nothing")
	default:
		line(style.IconInfo, "SPF hands over to another domain's record")
	}
	pct := ""
	if d.Pct > 0 && d.Pct < 100 {
		pct = " (for " + strconv.Itoa(d.Pct) + "% of it)"
	}
	switch d.Policy {
	case "reject":
		line(style.IconCheck, "DMARC: mail that fails its checks is to be rejected"+pct)
	case "quarantine":
		line(style.IconCheck, "DMARC: mail that fails its checks is to go to spam"+pct)
	case "none":
		line(style.IconWarning, "DMARC only asks for reports: mail forged in its name is delivered anyway")
	default:
		line(style.IconWarning, "No DMARC: nothing says what to do with mail forged in its name")
	}
	if d.SubPolicy != "" && d.SubPolicy != d.Policy {
		line(style.IconInfo, "For its subdomains: "+d.SubPolicy)
	}
	if d.Reports {
		line(style.IconInfo, "Mail failing its checks is reported to it (DMARC rua)")
	}
	if d.MTASTS != "" {
		line(style.IconCheck, "MTA-STS: it publishes a policy for servers sending to it to use TLS; whether it enforces it is in the policy on its web server, which is not fetched (it would see you)")
	} else {
		line(style.IconInfo, "No MTA-STS: mail sent to it can be made to go in the clear by someone in between")
	}
	for _, h := range d.DANE {
		switch {
		case h.Records > 0 && d.DNSSEC:
			line(style.IconCheck, "DANE for "+h.Host+": "+pluralize(h.Records, "TLSA record")+", signed: servers sending to it must use TLS, with its certificate")
		case h.Records > 0:
			line(style.IconInfo, "DANE for "+h.Host+": "+pluralize(h.Records, "TLSA record")+", but not confirmed signed: they count only with DNSSEC")
		default:
			line(style.IconInfo, "No DANE for "+h.Host)
		}
	}
	if d.TLSRPT {
		line(style.IconInfo, "It asks for reports when TLS to it fails (TLS-RPT)")
	}
	switch {
	case d.BIMI != "" && d.BIMICert:
		line(style.IconCheck, "BIMI: a logo, vouched for by a mark certificate")
	case d.BIMI != "":
		line(style.IconInfo, "BIMI: a logo, with no mark certificate: most mail apps will not show it")
	}
	switch {
	case d.DNSSEC:
		line(style.IconCheck, "DNSSEC: your resolver checked these answers' signatures")
	case d.Checked:
		line(style.IconInfo, "DNSSEC not confirmed: the domain does not sign, or your resolver does not check")
	default:
		line(style.IconInfo, "DNSSEC could not be asked: no resolver in /etc/resolv.conf")
	}
	for _, e := range d.Errors {
		line(style.IconWarning, "Not answered — "+e)
	}
	line(style.IconInfo, "Looked up "+d.At.Local().Format("15:04")+"; kept for an hour")
	return rows
}

// lookUpDomain looks domain up, and shows what it publishes in the
// Security tab of the message showing, if it is still from there.
func (r *reader) lookUpDomain(domain string) {
	v := r.secView
	v.looking = true
	v.show(r.msg)
	gen := r.gen
	r.s.async(func() (any, error) { return r.s.cli.CheckDomain(domain) }, func(res any, err error) {
		if gen != r.gen {
			return
		}
		v.looking = false
		if err == nil && v.report != nil && fromDomain(r.msg) == domain {
			d := res.(mailcore.DomainReport)
			v.report.Domain = &d
			v.jump = secSectionDomain
		}
		v.show(r.msg)
	})
}
