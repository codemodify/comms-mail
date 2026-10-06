package mailui

import (
	"math"
	"strconv"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Educate: email security, step by step, as a
// message goes from YOU to TARGET — the picture of its route
// (educate_route.go), each step numbered and its standards drawn by it as
// tags, and under it each step again: its points, a few words each, of
// what does what. A standard is the same tag everywhere, so the picture
// and the words read together.

// step is one of the picture's numbered steps. tags are the standards it
// uses, each named in its points, in groups: a group of more than one is
// alternatives, either one doing the job — drawn across the step's line in
// the picture, and joined by "or" under it.
type step struct {
	title  string
	points []point
	tags   [][]string
}

// point is one of a step's points, a few words of what does what, and its
// sub-points: the detail that clears up what it could be taken to mean.
type point struct {
	text string
	sub  []string
}

// pt is a point, and its sub-points.
func pt(text string, sub ...string) point { return point{text, sub} }

// alt is a group of alternatives; one is a group of one.
func alt(names ...string) []string { return names }

func one(names ...string) [][]string {
	out := make([][]string, len(names))
	for i, n := range names {
		out[i] = []string{n}
	}
	return out
}

// e2eTags are the standards from YOU to TARGET, past every server: for
// signing, and for encrypting.
var e2eTags = [][]string{alt("OpenPGP", "S/MIME")}

// What YOU do, by what is done: sign, encrypt, and what either brings.
var (
	signPoints = []point{
		pt("Your private key signs it",
			"A hash of the message, signed with that key",
			"Anyone checks it with your public key",
			"Any change after signing breaks it"),
	}
	encryptPoints = []point{
		pt("A fresh session key encrypts it",
			"A random AES key, for this message only",
			"Not derived from any public key",
			"Encrypts the whole message once: fast"),
		pt("The session key is encrypted with TARGET's public key",
			"Only TARGET's private key can decrypt it",
			"A second copy, with your public key: for Sent",
			"Both copies travel with the message"),
		pt("How, depends on the kind of TARGET's key",
			"Public keys are RSA, or elliptic curve",
			"Keys comms-mail makes for OpenPGP: elliptic curve (X25519)",
			"Most S/MIME certificates: RSA",
			"RSA: encrypts the session key directly",
			"Elliptic curve: a shared secret instead (ECDH)",
			"A one-time key pair is made for this",
			"One-time private key with TARGET's public key: shared secret",
			"A key from that secret encrypts the session key",
			"The one-time public key travels with the message",
			"TARGET rebuilds the secret with its private key"),
	}
	keyPoints = []point{
		pt("OpenPGP: your own keys, trusted by fingerprint",
			"Fingerprint: a hash of the public key",
			"Compare it with the owner another way"),
		pt("S/MIME: certificates from an authority",
			"The authority vouches the key is the address's"),
	}
	hiddenPoints = []point{
		pt("The subject is encrypted too; outside shows \"...\""),
		pt("Still visible: sender, recipients, date, size"),
		pt("No forward secrecy: a stolen key decrypts old mail",
			"TARGET's key is long-term; TLS keys are thrown away"),
	}
)

// lesson is the message and the road Educate explains: what YOU do to
// the message, whether its connections use TLS, and what the domains on
// the way publish and do.
type lesson struct {
	signed, encrypted, tls bool
	mx, spf, dkim, dmarc   bool
	mtaSTS, dane           bool
}

// firstLesson is the one shown first: nothing done to the message, no
// TLS, and the domains doing all they can — turned off one by one to see
// what each is for.
var firstLesson = lesson{mx: true, spf: true, dkim: true, dmarc: true, mtaSTS: true, dane: true}

// scenarioSteps are the eight steps of l's message: what is done, and
// what follows from what is not. A protocol's tag has what it runs over,
// and its default port: SMTP:TCP:25, SMTP:TLS:TCP:465,
// SMTP:STARTTLS:TCP:587, DNS:UDP:53.
func scenarioSteps(l lesson) []step {
	return []step{youStep(l.signed, l.encrypted), submitStep(l.tls), domainStep(l), relayStep(l),
		checkStep(l), fetchStep(l.tls), targetStep(l.signed, l.encrypted), throughStep(l)}
}

// domainStep is 3: YOUR SERVER signs it, with DKIM, and finds TARGET
// SERVER, by its MX record.
func domainStep(l lesson) step {
	title := "YOUR SERVER finds TARGET SERVER"
	points := []point{pt("It asks DNS over UDP, port 53",
		"Big answers, such as DNSSEC's, over TCP")}
	tags := [][]string{alt(dnsName)}
	if l.dkim {
		title = "YOUR SERVER signs it and finds TARGET SERVER"
		points = append(points,
			pt("DKIM signs body and headers, for your domain",
				"With the domain's key, kept on the server",
				"Not your personal key: it proves the domain"),
			pt("Its public key is in your domain's DNS",
				"At selector._domainkey.yourdomain"))
		tags = append(tags, alt("DKIM"))
	} else {
		points = append(points, pt("No DKIM: your server adds no domain signature",
			"Nothing proves it left your domain unchanged"))
	}
	if l.mx {
		points = append(points, pt("The MX record in DNS names TARGET SERVER"))
		tags = append(tags, alt("MX"))
	} else {
		points = append(points, pt("No MX record: the domain's own address is used",
			"Its A or AAAA record in DNS",
			"Neither there: the mail cannot be delivered"))
	}
	return step{title, points, tags}
}

// throughStep is 8: what gets through anyway, and what catches it.
func throughStep(l lesson) step {
	points := []point{
		pt("acrne.com, posing as your acme.com",
			"\"rn\" reads as \"m\""),
		pt("Its own SPF, DKIM and DMARC: all pass",
			"The attacker owns that domain",
			"They prove the domain, not the person"),
		pt("A real account, broken into: passes too"),
	}
	if !l.dmarc {
		points = append(points, pt("Without DMARC: your exact domain, forged",
			"From: you@yourdomain, sent by anyone"))
	}
	// What catches it.
	sign := pt("Your OpenPGP or S/MIME signature proves you",
		"Verified with a key TARGET knows is yours",
		"A look-alike has no such key",
		"Nor a broken-into mailbox: your key is not there")
	if l.signed {
		sign.sub = append(sign.sub, "Yours are signed: an unsigned one stands out")
	} else {
		sign.sub = append(sign.sub, "Yours are not signed: nothing tells them apart")
	}
	points = append(points, sign,
		pt("comms-mail warns of a look-alike domain",
			"One close to a domain you write to",
			"Or in letters of another alphabet"),
		pt("BIMI: a brand's verified logo by its mail",
			"Only with DMARC enforced and a mark certificate",
			"A look-alike gets none; not all apps show it"),
		pt("Confirm payment changes another way",
			"A number you already have, not the mail's"),
		pt("Distrust attachments and links you did not expect"))
	return step{"What gets through, and what catches it", points,
		[][]string{alt("SPF"), alt("DKIM"), alt("DMARC"), alt("OpenPGP", "S/MIME"), alt("BIMI")}}
}

// submitStep is 2: comms-mail hands it to YOUR SERVER.
func submitStep(tls bool) step {
	if !tls {
		return step{"comms-mail hands it to YOUR SERVER", []point{
			pt("First, DNS gives YOUR SERVER's address",
				"A forged answer leads elsewhere, unnoticed"),
			pt("SMTP in the clear: TCP port 587, or 25"),
			pt("Anyone on the network can read and change it"),
			pt("Signs in: password, or OAuth token",
				"In the clear: anyone on the network sees it"),
			pt("comms-mail does this only when set up to"),
			pt("SMTP hands the message over",
				"First who from and who to, then the message"),
		}, [][]string{alt("SMTP:TCP:587", "SMTP:TCP:25"), alt("Password", "OAuth")}}
	}
	return step{"comms-mail hands it to YOUR SERVER", []point{
		pt("First, DNS gives YOUR SERVER's address",
			"A forged answer fails the certificate check"),
		pt("SMTP inside TLS from the start: TCP port 465"),
		pt("Or SMTP with STARTTLS: TCP port 587",
			"Starts plain, then upgrades to TLS",
			"No upgrade offered: comms-mail does not send"),
		pt("The server's certificate is checked",
			"Issued by an authority, for that server's name"),
		pt("Signs in: password, or OAuth token"),
		pt("OAuth: scoped, revocable, no password",
			"You sign in on the provider's own page",
			"comms-mail gets a token for mail only"),
		pt("SMTP hands the message over",
			"First who from and who to, then the message"),
	}, [][]string{alt("SMTP:TLS:TCP:465", "SMTP:STARTTLS:TCP:587"), alt("Password", "OAuth")}}
}

// fetchStep is 6: TARGET fetches it.
func fetchStep(tls bool) step {
	if !tls {
		return step{"TARGET fetches it", []point{
			pt("First, DNS gives TARGET SERVER's address",
				"A forged answer leads elsewhere, unnoticed"),
			pt("IMAP (TCP 143): stays on the server, synced"),
			pt("POP3 (TCP 110): downloaded to one device"),
			pt("In the clear: the sign-in and the message",
				"Anyone on the network can read them"),
		}, [][]string{alt("IMAP:TCP:143", "POP3:TCP:110")}}
	}
	return step{"TARGET fetches it", []point{
		pt("First, DNS gives TARGET SERVER's address",
			"A forged answer fails the certificate check"),
		pt("Signs in, over TLS"),
		pt("IMAP (TCP 993): stays on the server, synced"),
		pt("POP3 (TCP 995): downloaded to one device"),
		pt("Or STARTTLS: IMAP on TCP 143, POP3 on 110",
			"Starts plain, then upgrades to TLS"),
	}, [][]string{alt("IMAP:TLS:TCP:993", "IMAP:STARTTLS:TCP:143", "POP3:TLS:TCP:995", "POP3:STARTTLS:TCP:110")}}
}

// youStep is 1: what YOU do to it.
func youStep(signed, encrypted bool) step {
	var points []point
	title := "YOU send it as written"
	switch {
	case signed && encrypted:
		title = "YOU sign and encrypt it"
	case signed:
		title = "YOU sign it"
	case encrypted:
		title = "YOU encrypt it"
	}
	if signed {
		points = append(points, signPoints...)
	}
	if encrypted {
		points = append(points, encryptPoints...)
	}
	if signed || encrypted {
		points = append(points, keyPoints...)
	}
	if encrypted {
		points = append(points, hiddenPoints...)
	} else {
		points = append(points, pt("Every server it passes can read it"))
	}
	if !signed && !encrypted {
		return step{title, append([]point{pt("Headers, body and attachments, as written")}, points...), nil}
	}
	return step{title, points, e2eTags}
}

// relayStep is 4: server to server — encrypted with STARTTLS when the
// servers use TLS, and that a must with MTA-STS or DANE.
func relayStep(l lesson) step {
	last := pt("Each server reads the message")
	if l.tls {
		last.sub = []string{"TLS encrypts the connection, not the stored mail"}
	}
	if l.encrypted {
		last = pt("Each server has the message, still encrypted",
			"Step 1's encryption keeps what it says hidden")
		if l.tls {
			last.sub = append([]string{"TLS encrypts the connection, not the stored mail"}, last.sub...)
		}
	}
	if !l.tls {
		return step{"Server to server", []point{
			pt("SMTP, on TCP port 25"),
			pt("In the clear: anyone on the way reads it"),
			last,
		}, [][]string{alt("SMTP:TCP:25")}}
	}
	points := []point{
		pt("SMTP, on TCP port 25"),
		pt("STARTTLS encrypts, if both offer it",
			"Often without checking the certificate"),
	}
	var must []string
	if l.mtaSTS || l.dane {
		points = append(points, pt("An attacker stripping the offer stops the mail",
			"It does not go on unencrypted"))
	} else {
		points = append(points, pt("An attacker in between can strip the offer",
			"The mail then goes on unencrypted",
			"Nothing here makes TLS a must"))
	}
	if l.mtaSTS {
		points = append(points, pt("MTA-STS: a policy over HTTPS makes TLS a must",
			"Published at mta-sts.domain, kept by senders"))
		must = append(must, "MTA-STS")
	}
	if l.dane {
		dane := pt("DANE: the certificate pinned in DNSSEC",
			"TLSA records, in signed DNS")
		if l.mtaSTS {
			dane.sub = append(dane.sub, "With both, a sender that knows DANE uses it")
		}
		points = append(points, dane)
		must = append(must, "DANE")
	}
	tags := [][]string{alt("SMTP:STARTTLS:TCP:25")}
	if len(must) > 0 {
		tags = append(tags, must)
	}
	return step{"Server to server", append(points, last), tags}
}

// checkStep is 5: TARGET SERVER checks the sender — with what your
// domain publishes — and keeps the message.
func checkStep(l lesson) step {
	var points []point
	var tags [][]string
	if l.spf {
		points = append(points, pt("SPF: is the sending server on the domain's list?",
			"Checks the bounce address, not the visible From",
			"Fails when mail is forwarded"))
		tags = append(tags, alt("SPF"))
	} else {
		points = append(points, pt("No SPF: no list of your domain's servers",
			"The check finds none: it proves nothing"))
	}
	if l.dkim {
		points = append(points, pt("DKIM: does the signature verify, unchanged?",
			"Survives forwarding; mailing lists can break it"))
		tags = append(tags, alt("DKIM"))
	} else {
		points = append(points, pt("No DKIM signature to check",
			"Nothing shows the message arrived unchanged"))
	}
	switch {
	case l.dmarc && !l.spf && !l.dkim:
		points = append(points, pt("DMARC, with neither SPF nor DKIM: all fails",
			"Even your own mail fails it",
			"Quarantine or reject: none of it arrives"))
		tags = append(tags, alt("DMARC"))
	case l.dmarc:
		pass := pt("DMARC: does one pass for the visible From?",
			"Aligned: the domain that passed is From's")
		if !l.dkim {
			pass.sub = append(pass.sub, "Only SPF can pass it: forwarding breaks that")
		}
		if !l.spf {
			pass.sub = append(pass.sub, "Only DKIM can pass it")
		}
		points = append(points, pass, pt("DMARC, failing: none, quarantine or reject",
			"The domain's owner chooses, in its DNS"))
		tags = append(tags, alt("DMARC"))
	default:
		points = append(points, pt("No DMARC: nothing ties SPF, DKIM to From",
			"A forged From with your domain may pass",
			"TARGET SERVER falls back to its own guesses"))
	}
	stored := "As it came: readable by TARGET's provider"
	if l.encrypted {
		stored = "As it came: what it says stays encrypted"
	}
	points = append(points,
		pt("ARC: keeps results through forwarders"),
		pt("The verdict goes into Authentication-Results"),
		pt("Spam and malware filtered, then stored", stored))
	return step{"TARGET SERVER checks the sender", points, append(tags, alt("ARC"))}
}

// targetStep is 7: what TARGET's app does with it.
func targetStep(signed, encrypted bool) step {
	title := "TARGET opens it"
	switch {
	case signed && encrypted:
		title = "TARGET decrypts, verifies and opens it"
	case signed:
		title = "TARGET verifies and opens it"
	case encrypted:
		title = "TARGET decrypts and opens it"
	}
	points := []point{
		pt("Trusts only its own provider's verdict",
			"The topmost Authentication-Results",
			"Senders can forge the ones under it"),
		pt("Warns when a check failed"),
	}
	if encrypted {
		decrypt := pt("TARGET's private OpenPGP or S/MIME key decrypts it",
			"It decrypts the session key; that decrypts the message")
		if !signed {
			decrypt.sub = append(decrypt.sub, "Shows it was for TARGET; not who sent it")
		}
		points = append(points, decrypt)
	}
	if signed {
		points = append(points,
			pt("Verifies the OpenPGP or S/MIME signature",
				"With the sender's public key"),
			pt("The signer must be the From address",
				"Else anyone's valid signature would pass"))
	}
	points = append(points,
		pt("comms-mail flags look-alikes and false names"),
		pt("Remote images blocked: they report opens",
			"Loading one tells when, from where, with what"))
	var tags [][]string
	if signed || encrypted {
		tags = e2eTags
	}
	return step{title, points, tags}
}

// problem is what goes wrong on a lesson's road, in a few words, and
// what follows.
type problem struct {
	text string
	sub  []string
}

// problems are what goes wrong with what l leaves out — the picture
// shows where, in red — on the road's order: the message, the
// connections, the domains. None, and nothing on the way reads or changes
// the message.
func problems(l lesson) []problem {
	var out []problem
	add := func(text string, sub ...string) { out = append(out, problem{text, sub}) }
	if !l.signed {
		add("Not signed: nothing proves you wrote it",
			"A look-alike passes as you: step 8")
	}
	if !l.encrypted {
		add("Not encrypted: every server on the way reads it",
			"Stored readable at both providers")
	}
	if !l.tls {
		add("No TLS: anyone on the network reads it",
			"Your password or sign-in token too",
			"The red lines: connections in the clear")
	} else if !l.mtaSTS && !l.dane {
		add("Between servers, TLS can be stripped",
			"An attacker in between makes it plain")
	}
	if !l.mx {
		add("No MX: YOUR SERVER cannot look TARGET SERVER up",
			"It tries the domain's own address (A or AAAA)",
			"None there: the mail bounces back to you")
	}
	if !l.spf {
		add("No SPF: nothing lists your domain's servers",
			"Any server can claim to send for it")
	}
	if !l.dkim {
		add("No DKIM: nothing shows it arrived unchanged",
			"Forwarded mail loses all proof")
	}
	switch {
	case l.dmarc && !l.spf && !l.dkim:
		add("DMARC fails it: TARGET SERVER rejects it",
			"TARGET never gets it, nor your domain's other mail")
	case l.dmarc && !l.dkim:
		add("DMARC rests on SPF: forwarded mail fails it")
	case !l.dmarc:
		add("No DMARC: your exact domain can be forged",
			"TARGET SERVER only guesses")
	}
	return out
}

// problemList is what goes wrong, in red; or, when nothing does, that.
func problemList(ps []problem) widget.Component {
	col := widgets.NewColumn().WithGap(6)
	if len(ps) == 0 {
		ok := widgets.NewIconLabel(style.IconCheck, "")
		ok.Tone = widgets.ToneSuccess
		row := widgets.NewRow(ok).WithGap(6).WithAlign(layout.AlignStart)
		says := widgets.NewColumn(wrapLabel("Nothing on the way reads or changes it"))
		hint := wrapLabel("Look-alikes still get through: step 8 says what catches them")
		hint.Tone = widgets.ToneMuted
		says.Add(hint)
		row.AddFlex(says, 1)
		col.Add(row)
		return col
	}
	for _, p := range ps {
		mark := widgets.NewIconLabel(style.IconWarning, "")
		mark.Tone = widgets.ToneDanger
		row := widgets.NewRow(mark).WithGap(6).WithAlign(layout.AlignStart)
		text := wrapLabel(p.text)
		text.Tone = widgets.ToneDanger
		says := widgets.NewColumn(text).WithGap(2)
		for _, sub := range p.sub {
			l := wrapLabel(sub)
			l.Tone = widgets.ToneMuted
			says.Add(l)
		}
		row.AddFlex(says, 1)
		col.Add(row)
	}
	return col
}

// fakeDomain is the attacker's look-alike domain, as the picture shows it.
const fakeDomain = "acrne.com"

// educateSection is the Educate page: the picture, what its symbols mean,
// which message and road, and each step.
//
// The picture, what its symbols mean — all of them, always — and which
// message and road — what YOU do to it, TLS or not, what the domains
// publish — stay together, in view; under them its steps, the picture
// showing that one, and nothing of what is turned off; and beside the
// steps, email's ports and how they came to be.
func educateSection() widget.Component {
	scene := newRouteScene()
	key := widgets.NewWrap()
	key.Gap, key.LineGap = 16, 6
	for _, sy := range symbols {
		key.Add(newSymbolItem(sy))
	}
	// The steps, a tab each — 1 to 8, as the picture numbers them —
	// rather than one after another.
	pages := make([]*widgets.FlexBox, 8)
	var tabs []widgets.Tab
	for i := range pages {
		pages[i] = widgets.NewColumn()
		tabs = append(tabs, widgets.Tab{Title: strconv.Itoa(i + 1), Content: widgets.NewPad(8, pages[i])})
	}
	stepTabs := widgets.NewTabView(tabs...)
	wrong := widgets.NewColumn()
	l := firstLesson
	var mtaSTS, dane *widgets.Checkbox
	show := func() {
		scene.l = l
		scene.RequestLayout()
		scene.Invalidate()
		// MTA-STS and DANE make TLS between servers a must: without TLS
		// there is nothing for them to do.
		if mtaSTS != nil {
			mtaSTS.SetEnabled(l.tls)
			dane.SetEnabled(l.tls)
		}
		wrong.ClearChildren()
		wrong.Add(problemList(problems(l)))
		wrong.RequestLayout()
		for i, st := range scenarioSteps(l) {
			about := widgets.NewColumn(newStrong(st.title)).WithGap(6)
			if len(st.tags) > 0 {
				about.Add(altRow(st.tags))
			}
			about.Add(pointList(st.points))
			row := widgets.NewRow(newNumberMark(i + 1)).WithGap(8).WithAlign(layout.AlignStart)
			row.AddFlex(about, 1)
			pages[i].ClearChildren()
			pages[i].Add(row)
			pages[i].RequestLayout()
		}
		stepTabs.RequestLayout()
		stepTabs.Invalidate()
	}
	check := func(text string, on *bool) *widgets.Checkbox {
		c := widgets.NewCheckbox(text, *on, nil)
		c.OnChange = func(v bool) { *on = v; show() }
		return c
	}
	// All the ticks on one line: the message's, the connections', the
	// domains' — a little room between the three.
	mtaSTS, dane = check("MTA-STS", &l.mtaSTS), check("DANE", &l.dane)
	ticks := widgets.NewWrap()
	ticks.Gap, ticks.LineGap = 8, 6
	for i, grp := range [][]*widgets.Checkbox{
		{check("Signed", &l.signed), check("Encrypted", &l.encrypted)},
		{check("TLS", &l.tls), mtaSTS, dane},
		{check("MX", &l.mx), check("SPF", &l.spf), check("DKIM", &l.dkim), check("DMARC", &l.dmarc)},
	} {
		if i > 0 {
			ticks.Add(widgets.NewSpacerSize(6, 0))
		}
		for _, c := range grp {
			ticks.Add(c)
		}
	}
	show()
	side := widgets.NewColumn(widgets.NewTitle("What goes wrong"), wrong).WithGap(10)
	return newEducatePage(widgets.NewColumn(scene, key).WithGap(10), ticks, side, stepTabs, portList(), scene)
}

// port is a port email uses, by its tag, and how it came to be: a few
// words a line, the year first.
type port struct {
	tag  string
	says []string
}

// ports are email's ports: SMTP's, IMAP's, POP3's, and DNS's and HTTPS's,
// which mail asks on the way.
var ports = []port{
	{"SMTP:TCP:25", []string{"1982: server to server", "No encryption, no sign-in"}},
	{"SMTP:STARTTLS:TCP:25", []string{"1999: upgrades to TLS, starts in the clear",
		"Anyone in between can strip the upgrade", "2015 DANE, 2018 MTA-STS: TLS a must"}},
	{"SMTP:TCP:587", []string{"1998: apps hand mail in here, signed in", "Port 25 left to servers"}},
	{"SMTP:STARTTLS:TCP:587", []string{"1999: upgrades to TLS, starts in the clear",
		"The app must insist on it: comms-mail does", "MTA-STS and DANE are for port 25 only"}},
	{"SMTP:TLS:TCP:465", []string{"1997: SMTP over SSL, withdrawn in 1998", "Kept in use anyway",
		"2018: official again, TLS from the start", "Preferred over STARTTLS"}},
	{"IMAP:TCP:143", []string{"1988: mail kept on the server", "In the clear"}},
	{"IMAP:STARTTLS:TCP:143", []string{"1999: upgrades to TLS, starts in the clear"}},
	{"IMAP:TLS:TCP:993", []string{"Late 1990s: TLS from the start", "2018: preferred over STARTTLS"}},
	{"POP3:TCP:110", []string{"1988: mail downloaded to one device", "In the clear"}},
	{"POP3:STARTTLS:TCP:110", []string{"1999: upgrades to TLS, starts in the clear"}},
	{"POP3:TLS:TCP:995", []string{"Late 1990s: TLS from the start", "2018: preferred over STARTTLS"}},
	{"DNS:UDP:53", []string{"1987: answers in the clear, unsigned", "A forged one sends mail astray",
		"2005: DNSSEC signs them; DANE needs it"}},
	{"HTTPS:TLS:TCP:443", []string{"2018: MTA-STS's policy is published here", "OAuth's sign-in page too"}},
}

// portList is the ports, each its tag and how it came to be.
func portList() widget.Component {
	col := widgets.NewColumn(widgets.NewTitle("Ports, then and now")).WithGap(12)
	for _, pt := range ports {
		says := widgets.NewColumn().WithGap(2)
		for i, line := range pt.says {
			l := wrapLabel(line)
			if i > 0 {
				l.Tone = widgets.ToneMuted
			}
			says.Add(l)
		}
		col.Add(widgets.NewColumn(widgets.NewRow(newProtoChip(pt.tag)), says).WithGap(4))
	}
	return col
}

// educatePage keeps the picture and the ticks under it in view while the
// words under them scroll, so each step can be read with the picture
// beside it — when the page is tall enough to leave the words room
// (pinMin); when not, the picture scrolls with them. What goes wrong
// stands beside the picture, in view with it, and the ports beside the
// steps, where the page is wide enough for both; else each under the
// other.
type educatePage struct {
	widget.Base
	picture widget.Component // the picture, and its key
	ticks   widget.Component // which message and road
	side    widget.Component // what goes wrong
	steps   widget.Component
	ports   widget.Component
	scene   *routeScene
	rule    *widgets.Separator
	words   *pair            // the steps, the ports beside them
	body    *widgets.FlexBox // what scrolls: the words, and the picture when not pinned
	scroll  *widgets.ScrollView
	built   bool // pinned and beside say how it is put together
	pinned  bool
	beside  bool
}

// pinMin is the room the words keep under a pinned picture: some lines
// of them.
const pinMin = 120

// sideMin is the least what goes wrong takes beside the picture; sideGap
// is between them.
const (
	sideMin = 210
	sideGap = 16
)

func newEducatePage(picture, ticks, side, steps, ports widget.Component, scene *routeScene) *educatePage {
	p := &educatePage{picture: picture, ticks: ticks, side: side, steps: steps, ports: ports, scene: scene, rule: widgets.NewSeparator()}
	p.Init(p)
	p.words = newPair(steps, ports)
	p.body = widgets.NewColumn().WithGap(14)
	p.scroll = widgets.NewScrollView(widgets.NewPad(4, p.body))
	p.Add(p.scroll)
	return p
}

func (p *educatePage) MinWidth() float32 { return p.scene.MinWidth() + style.Dip(p.Look(), 8) }

func (p *educatePage) Measure(c layout.Constraints) paintengine2d.Point {
	w, h := style.Dip(p.Look(), 560), style.Dip(p.Look(), 600)
	if c.HasMaxW() {
		w = c.MaxW
	}
	if c.HasMaxH() {
		h = c.MaxH
	}
	return c.Constrain(paintengine2d.Pt(w, h))
}

// sideWidth is what goes wrong's width beside a picture in inner.
func (p *educatePage) sideWidth(inner float32) float32 {
	return min(max(inner*0.28, style.Dip(p.Look(), sideMin)), style.Dip(p.Look(), 340))
}

func (p *educatePage) Arrange(r paintengine2d.Rect) {
	p.SetBounds(r)
	lk := p.Look()
	pad := style.Dip(lk, 4)
	inner := r.Dx() - 2*pad
	ruleH := p.rule.Measure(layout.Constraints{MaxW: r.Dx(), MaxH: -1}).Y
	sw := p.sideWidth(inner)
	pw := inner - sw - style.Dip(lk, sideGap)
	// The ticks under the picture, across the page.
	gap := style.Dip(lk, 10)
	ticksH := p.ticks.Measure(layout.Constraints{MaxW: inner, MaxH: -1}).Y
	pins := func(picH float32) bool { return r.Dy()-(pad+picH+gap+ticksH+pad+ruleH) >= style.Dip(lk, pinMin) }
	// What goes wrong beside the picture, both in view, where they fit;
	// else the picture alone in view; else everything scrolls.
	beside := pw >= p.scene.MinWidth()
	var picH float32
	if beside {
		picH = max(p.picture.Measure(layout.Constraints{MaxW: pw, MaxH: -1}).Y, p.side.Measure(layout.Constraints{MaxW: sw, MaxH: -1}).Y)
		beside = pins(picH)
	}
	if !beside {
		picH = p.picture.Measure(layout.Constraints{MaxW: inner, MaxH: -1}).Y
	}
	pin := pins(picH)
	// The ports beside the steps, under what goes wrong, when it is
	// beside the picture: the steps as wide as the picture.
	p.words.aw, p.words.gap = 0, 0
	if beside {
		p.words.aw, p.words.gap = pw, style.Dip(lk, sideGap)
	}
	if !p.built || pin != p.pinned || beside != p.beside {
		p.built, p.pinned, p.beside = true, pin, beside
		for _, c := range []widget.Component{p.picture, p.ticks, p.side, p.rule, p.scroll} {
			p.Remove(c)
		}
		p.body.ClearChildren()
		if beside {
			p.Add(p.picture)
			p.Add(p.side)
		}
		if pin {
			if !beside {
				p.Add(p.picture)
			}
			p.Add(p.ticks)
			p.Add(p.rule)
		} else {
			p.body.Add(p.picture)
			p.body.Add(p.ticks)
		}
		if !beside {
			p.body.Add(p.side)
		}
		p.body.Add(p.words)
		p.Add(p.scroll)
		p.scroll.ScrollTo(0)
	}
	if !pin {
		p.scroll.Arrange(r)
		return
	}
	if beside {
		p.picture.Arrange(paintengine2d.XYWH(r.Min.X+pad, r.Min.Y+pad, pw, picH))
		p.side.Arrange(paintengine2d.XYWH(r.Min.X+pad+pw+style.Dip(lk, sideGap), r.Min.Y+pad, sw, picH))
	} else {
		p.picture.Arrange(paintengine2d.XYWH(r.Min.X+pad, r.Min.Y+pad, inner, picH))
	}
	p.ticks.Arrange(paintengine2d.XYWH(r.Min.X+pad, r.Min.Y+pad+picH+gap, inner, ticksH))
	top := r.Min.Y + pad + picH + gap + ticksH + pad
	p.rule.Arrange(paintengine2d.XYWH(r.Min.X, top, r.Dx(), ruleH))
	p.scroll.Arrange(paintengine2d.Rect{Min: paintengine2d.Pt(r.Min.X, top+ruleH), Max: r.Max})
}

// pair is a and b side by side, a aw wide and b gap after it; or, with
// aw 0, b under a.
type pair struct {
	widget.Base
	a, b    widget.Component
	aw, gap float32
}

func newPair(a, b widget.Component) *pair {
	p := &pair{a: a, b: b}
	p.Init(p)
	p.Add(a)
	p.Add(b)
	return p
}

// split is a's width and height, b's, at width w; side by side, or b
// under a.
func (p *pair) split(w float32) (aw, ah, bw, bh float32) {
	aw, bw = w, w
	if p.aw > 0 {
		aw, bw = min(p.aw, w), max(w-p.aw-p.gap, 0)
	}
	return aw, p.a.Measure(layout.Constraints{MaxW: aw, MaxH: -1}).Y, bw, p.b.Measure(layout.Constraints{MaxW: bw, MaxH: -1}).Y
}

func (p *pair) Measure(c layout.Constraints) paintengine2d.Point {
	w := style.Dip(p.Look(), 560)
	if c.HasMaxW() {
		w = c.MaxW
	}
	_, ah, _, bh := p.split(w)
	if p.aw > 0 {
		return c.Constrain(paintengine2d.Pt(w, max(ah, bh)))
	}
	return c.Constrain(paintengine2d.Pt(w, ah+style.Dip(p.Look(), 14)+bh))
}

func (p *pair) Arrange(r paintengine2d.Rect) {
	p.SetBounds(r)
	aw, ah, bw, bh := p.split(r.Dx())
	p.a.Arrange(paintengine2d.XYWH(r.Min.X, r.Min.Y, aw, ah))
	if p.aw > 0 {
		p.b.Arrange(paintengine2d.XYWH(r.Min.X+aw+p.gap, r.Min.Y, bw, bh))
		return
	}
	p.b.Arrange(paintengine2d.XYWH(r.Min.X, r.Min.Y+ah+style.Dip(p.Look(), 14), bw, bh))
}

// ---- marks ----

// numberMark is a number in a disc of the look's accent: the same in the
// picture and beside its words.
type numberMark struct {
	widget.Base
	n int
}

func newNumberMark(n int) *numberMark {
	m := &numberMark{n: n}
	m.Init(m)
	return m
}

const markSize = 20

func (m *numberMark) Measure(c layout.Constraints) paintengine2d.Point {
	s := style.Dip(m.Look(), markSize)
	return c.Constrain(paintengine2d.Pt(s, max(s, m.Look().BoldFont().Height())))
}

func (m *numberMark) Arrange(r paintengine2d.Rect) { m.SetBounds(r) }

func (m *numberMark) Paint(ctx *paintengine2d.Context) {
	b := m.LocalBounds()
	s := style.Dip(m.Look(), markSize)
	// Level with the first line of the words beside it.
	drawNumber(ctx, m.Look(), m.n, paintengine2d.Pt(b.Min.X+s/2, b.Min.Y+max(s, m.Look().BoldFont().Height())/2), s)
}

// drawNumber draws n in a disc of size s centred at c: white on the
// look's blue.
func drawNumber(ctx *paintengine2d.Context, lk style.LookAndFeel, n int, c paintengine2d.Point, s float32) {
	disc, ink := numberInks(lk)
	ctx.DrawCircle(c, s/2, paintengine2d.Fill(disc))
	f := lk.BoldFont()
	t := strconv.Itoa(n)
	f.Draw(ctx, t, paintengine2d.Pt(c.X-f.Advance(t)/2, c.Y-f.Height()/2), ink)
}

// white is the numbers' and the stop's ink, whatever the look.
var white = paintengine2d.RGB(1, 1, 1)

// numberInks are a number's disc and figure: the look's accent, darkened
// as far as white on it needs to be read (4.5:1), and white — where a
// look's own text-on-accent can be black, on a light blue.
func numberInks(lk style.LookAndFeel) (disc, ink paintengine2d.Color) {
	return whiteOn(lk.Palette().Accent), white
}

// whiteOn is c, darkened toward black until white on it reads at 4.5:1.
func whiteOn(c paintengine2d.Color) paintengine2d.Color {
	c.A = 1
	for i := 0; i < 20 && contrast(white, c) < 4.5; i++ {
		c = c.Lerp(paintengine2d.RGB(0, 0, 0), 0.1)
		c.A = 1
	}
	return c
}

// contrast is the WCAG contrast ratio of a and b.
func contrast(a, b paintengine2d.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// luminance is c's relative luminance (WCAG).
func luminance(c paintengine2d.Color) float64 {
	lin := func(v float32) float64 {
		x := float64(v)
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// ---- tags ----

const chipGap = 5

// chipSize is the size of name's tag.
func chipSize(lk style.LookAndFeel, name string) (w, h float32) {
	f := lk.Font()
	return f.Advance(name) + style.Dip(lk, 12), f.Height() + style.Dip(lk, 4)
}

// drawChip draws name as a tag in r: an outline and text in the accent.
func drawChip(ctx *paintengine2d.Context, lk style.LookAndFeel, name string, r paintengine2d.Rect) {
	p := lk.Palette()
	ink := p.Ink(p.Accent)
	rad := r.Dy() / 2
	ctx.DrawRoundRect(r, rad, rad, paintengine2d.Fill(p.Field))
	ctx.DrawRoundRect(r, rad, rad, paintengine2d.StrokePaint(ink, style.Dip(lk, 1.25)))
	f := lk.Font()
	f.Draw(ctx, name, paintengine2d.Pt(r.Min.X+(r.Dx()-f.Advance(name))/2, r.Min.Y+(r.Dy()-f.Height())/2), ink)
}

// drawMissingChip draws name as a tag that is not there: dashed and in
// red, struck through — where it would be, so its absence shows.
func drawMissingChip(ctx *paintengine2d.Context, lk style.LookAndFeel, name string, r paintengine2d.Rect) {
	p := lk.Palette()
	ink := p.Ink(p.Danger)
	rad := r.Dy() / 2
	pen := paintengine2d.StrokePaint(ink, style.Dip(lk, 1.25))
	pen.Stroke.Dash = []float32{style.Dip(lk, 3), style.Dip(lk, 2)}
	ctx.DrawRoundRect(r, rad, rad, paintengine2d.Fill(p.Field))
	ctx.DrawRoundRect(r, rad, rad, pen)
	f := lk.Font()
	x := r.Min.X + (r.Dx()-f.Advance(name))/2
	f.Draw(ctx, name, paintengine2d.Pt(x, r.Min.Y+(r.Dy()-f.Height())/2), ink)
	y := r.Center().Y
	ctx.DrawLine(paintengine2d.Pt(x-style.Dip(lk, 2), y), paintengine2d.Pt(x+f.Advance(name)+style.Dip(lk, 2), y), paintengine2d.StrokePaint(ink, style.Dip(lk, 1.25)))
}

// chipLines breaks names into lines of tags no wider than w, and says how
// wide each line is; a tag wider than w has a line of its own.
func chipLines(lk style.LookAndFeel, names []string, w float32) (lines [][]string, widths []float32) {
	gap := style.Dip(lk, chipGap)
	var cur []string
	var curW float32
	for _, n := range names {
		cw, _ := chipSize(lk, n)
		if len(cur) > 0 && curW+gap+cw > w {
			lines, widths = append(lines, cur), append(widths, curW)
			cur, curW = nil, 0
		}
		if len(cur) > 0 {
			curW += gap
		}
		cur, curW = append(cur, n), curW+cw
	}
	if len(cur) > 0 {
		lines, widths = append(lines, cur), append(widths, curW)
	}
	return lines, widths
}

// placedChip is a tag where it is drawn; group is what it is drawn by in
// the picture, and alt which of that step's groups of alternatives it is
// in. missing is a standard turned off: drawn where it would be, in red,
// struck through.
type placedChip struct {
	name    string
	r       paintengine2d.Rect
	group   string
	alt     int
	missing bool
}

// placeChips lays names out in lines no wider than w from top, each line
// centred on cx, or from cx when left; and says how tall they are.
func placeChips(lk style.LookAndFeel, names []string, cx, top, w float32, left bool) ([]placedChip, float32) {
	lines, widths := chipLines(lk, names, w)
	gap := style.Dip(lk, chipGap)
	_, ch := chipSize(lk, "X")
	var out []placedChip
	y := top
	for i, line := range lines {
		x := cx
		if !left {
			x -= widths[i] / 2
		}
		for _, n := range line {
			cw, _ := chipSize(lk, n)
			out = append(out, placedChip{name: n, r: paintengine2d.XYWH(x, y, cw, ch)})
			x += cw + gap
		}
		y += ch + gap
	}
	if len(lines) == 0 {
		return nil, 0
	}
	return out, y - gap - top
}

// protoChip is one standard's tag, as the picture draws it.
type protoChip struct {
	widget.Base
	name string
}

func newProtoChip(name string) *protoChip {
	c := &protoChip{name: name}
	c.Init(c)
	return c
}

func (c *protoChip) Measure(k layout.Constraints) paintengine2d.Point {
	return k.Constrain(paintengine2d.Pt(chipSize(c.Look(), c.name)))
}

func (c *protoChip) Arrange(r paintengine2d.Rect) { c.SetBounds(r) }

func (c *protoChip) Paint(ctx *paintengine2d.Context) {
	b := c.LocalBounds()
	w, h := chipSize(c.Look(), c.name)
	drawChip(ctx, c.Look(), c.name, paintengine2d.XYWH(b.Min.X, b.Min.Y, w, h))
}

// pointList is a step's points, lettered a, b, c…, and under each its
// sub-points, numbered i, ii, iii…: a few words each.
func pointList(points []point) *widgets.FlexBox {
	col := widgets.NewColumn().WithGap(2)
	for i, p := range points {
		row := widgets.NewRow(newLetter(string(rune('a'+i)), 14)).WithGap(6).WithAlign(layout.AlignStart)
		row.AddFlex(wrapLabel(p.text), 1)
		col.Add(row)
		for k, sub := range p.sub {
			row := widgets.NewRow(widgets.NewSpacerSize(14, 0), newLetter(roman(k+1), 22)).WithGap(6).WithAlign(layout.AlignStart)
			text := wrapLabel(sub)
			text.Tone = widgets.ToneMuted
			row.AddFlex(text, 1)
			col.Add(row)
		}
	}
	return col
}

// roman is n, 1 to 39, in small roman numerals: a sub-point's number.
func roman(n int) string {
	out := ""
	for _, r := range []struct {
		v int
		s string
	}{{10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}} {
		for n >= r.v {
			out, n = out+r.s, n-r.v
		}
	}
	return out
}

// letter is a point's letter, or a sub-point's numeral, muted, right
// aligned in a column w wide so the points line up.
type letter struct {
	widget.Base
	text string
	w    float32
}

func newLetter(text string, w float32) *letter {
	l := &letter{text: text, w: w}
	l.Init(l)
	return l
}

func (l *letter) Measure(c layout.Constraints) paintengine2d.Point {
	return c.Constrain(paintengine2d.Pt(style.Dip(l.Look(), l.w), l.Look().Font().Height()))
}

func (l *letter) Arrange(r paintengine2d.Rect) { l.SetBounds(r) }

func (l *letter) Paint(ctx *paintengine2d.Context) {
	b := l.LocalBounds()
	f := l.Look().Font()
	f.Draw(ctx, l.text, paintengine2d.Pt(b.Max.X-f.Advance(l.text), b.Min.Y), l.Look().Palette().TextMuted)
}

// altRow is groups' tags in a row that wraps, a group of alternatives
// kept together and joined by "or".
func altRow(groups [][]string) *widgets.Wrap {
	w := widgets.NewWrap()
	w.Gap = chipGap + 3
	for _, grp := range groups {
		if len(grp) == 1 {
			w.Add(newProtoChip(grp[0]))
			continue
		}
		row := widgets.NewRow().WithGap(chipGap).WithAlign(layout.AlignCenter)
		for i, n := range grp {
			if i > 0 {
				or := widgets.NewLabel("or")
				or.Tone = widgets.ToneMuted
				row.Add(or)
			}
			row.Add(newProtoChip(n))
		}
		w.Add(row)
	}
	return w
}

// ---- what the symbols mean ----

// symbol is one of the picture's symbols, and what it means.
type symbol struct {
	draw func(ctx *paintengine2d.Context, lk style.LookAndFeel, box paintengine2d.Rect)
	says string
}

var symbols = []symbol{
	{envelopeSymbol(envelope{}), "A message"},
	{envelopeSymbol(envelope{seal: true}), "Signed by you"},
	{envelopeSymbol(envelope{lock: true}), "Encrypted to TARGET"},
	{envelopeSymbol(envelope{stamp: true}), "Signed by its domain"},
	{func(ctx *paintengine2d.Context, lk style.LookAndFeel, box paintengine2d.Rect) {
		y := box.Center().Y
		drawArrow(ctx, lk, paintengine2d.Pt(box.Min.X+style.Dip(lk, 1), y), paintengine2d.Pt(box.Max.X-style.Dip(lk, 1), y), inksOf(lk).good)
	}, "TLS"},
}

// envelopeSymbol draws e in the middle of a symbol's box.
func envelopeSymbol(e envelope) func(*paintengine2d.Context, style.LookAndFeel, paintengine2d.Rect) {
	return func(ctx *paintengine2d.Context, lk style.LookAndFeel, box paintengine2d.Rect) {
		w, _ := envSize(lk)
		drawEnvelope(ctx, lk, paintengine2d.Pt(box.Min.X+w/2+style.Dip(lk, 2), box.Center().Y), e)
	}
}

// symbolItem is a symbol and what it means, in a line.
type symbolItem struct {
	widget.Base
	sy symbol
}

func newSymbolItem(sy symbol) *symbolItem {
	it := &symbolItem{sy: sy}
	it.Init(it)
	return it
}

// iconBox is the room a symbol is drawn in.
func (it *symbolItem) iconBox() (w, h float32) {
	ew, eh := envSize(it.Look())
	return ew + style.Dip(it.Look(), 10), eh * 1.5
}

func (it *symbolItem) Measure(c layout.Constraints) paintengine2d.Point {
	iw, ih := it.iconBox()
	f := it.Look().Font()
	return c.Constrain(paintengine2d.Pt(iw+style.Dip(it.Look(), 6)+f.Advance(it.sy.says), max(ih, f.Height())))
}

func (it *symbolItem) Arrange(r paintengine2d.Rect) { it.SetBounds(r) }

func (it *symbolItem) Paint(ctx *paintengine2d.Context) {
	b := it.LocalBounds()
	iw, ih := it.iconBox()
	f := it.Look().Font()
	it.sy.draw(ctx, it.Look(), paintengine2d.XYWH(b.Min.X, b.Min.Y+(b.Dy()-ih)/2, iw, ih))
	f.Draw(ctx, it.sy.says, paintengine2d.Pt(b.Min.X+iw+style.Dip(it.Look(), 6), b.Min.Y+(b.Dy()-f.Height())/2), it.Look().Palette().Text)
}

// strong is a line of bold text that wraps: a step's name over its words.
// (A Label is plain or a Title, which is the page title's size;
// uitoolkit-gaps.md #50.)
type strong struct {
	widget.Base
	text  string
	lines []string
}

func newStrong(text string) *strong {
	s := &strong{text: text}
	s.Init(s)
	return s
}

func (s *strong) Measure(c layout.Constraints) paintengine2d.Point {
	f := s.Look().BoldFont()
	w := f.Advance(s.text)
	if c.HasMaxW() {
		w = min(w, c.MaxW)
	}
	return c.Constrain(paintengine2d.Pt(w, float32(len(wrapWords(f, s.text, w, 4)))*f.Height()))
}

func (s *strong) Arrange(r paintengine2d.Rect) {
	s.SetBounds(r)
	s.lines = wrapWords(s.Look().BoldFont(), s.text, r.Dx(), 4)
}

func (s *strong) Paint(ctx *paintengine2d.Context) {
	b := s.LocalBounds()
	f := s.Look().BoldFont()
	for i, l := range s.lines {
		f.Draw(ctx, l, paintengine2d.Pt(b.Min.X, b.Min.Y+float32(i)*f.Height()), s.Look().Palette().Text)
	}
}
