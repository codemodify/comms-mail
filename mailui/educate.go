package mailui

import (
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

// The steps every message takes the same way: 3 and 8.
var (
	domainStep = step{"YOUR SERVER signs it and finds TARGET SERVER", []point{
		pt("DKIM signs body and headers, for your domain",
			"With the domain's key, kept on the server",
			"Not your personal key: it proves the domain"),
		pt("Its public key is in your domain's DNS",
			"At selector._domainkey.yourdomain"),
		pt("The MX record in DNS names TARGET SERVER"),
	}, one("DKIM", "MX")}
	throughStep = step{"What gets through anyway", []point{
		pt("acrne.com, posing as your supplier acme.com",
			"\"rn\" reads as \"m\""),
		pt("Its own SPF, DKIM and DMARC: all pass",
			"The attacker owns that domain"),
		pt("A real account, broken into: passes too"),
		pt("Checks prove the domain, not the person"),
		pt("Confirm payment changes another way"),
		pt("Distrust attachments and links you did not expect"),
	}, one("SPF", "DKIM", "DMARC")}
)

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

// scenarioSteps are the eight steps of a message signed or not, encrypted
// or not, over connections with TLS or without: each saying what is done,
// and nothing of what is not. A protocol's tag has its default port:
// SMTP:25, SMTP:TLS:465, SMTP:STARTTLS:587.
func scenarioSteps(signed, encrypted, tls bool) []step {
	return []step{youStep(signed, encrypted), submitStep(tls), domainStep, relayStep(encrypted, tls),
		checkStep(encrypted), fetchStep(tls), targetStep(signed, encrypted), throughStep}
}

// submitStep is 2: comms-mail hands it to YOUR SERVER.
func submitStep(tls bool) step {
	if !tls {
		return step{"comms-mail hands it to YOUR SERVER", []point{
			pt("SMTP in the clear: port 587, or 25"),
			pt("Anyone on the network can read and change it"),
			pt("Signs in: password, or OAuth token",
				"In the clear: anyone on the network sees it"),
			pt("comms-mail does this only when set up to"),
			pt("SMTP hands the message over",
				"First who from and who to, then the message"),
		}, [][]string{alt("SMTP:587", "SMTP:25"), alt("Password", "OAuth")}}
	}
	return step{"comms-mail hands it to YOUR SERVER", []point{
		pt("SMTP with TLS from the first byte: port 465"),
		pt("Or SMTP with STARTTLS: port 587",
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
	}, [][]string{alt("SMTP:TLS:465", "SMTP:STARTTLS:587"), alt("Password", "OAuth")}}
}

// fetchStep is 6: TARGET fetches it.
func fetchStep(tls bool) step {
	if !tls {
		return step{"TARGET fetches it", []point{
			pt("IMAP (143): stays on the server, synced"),
			pt("POP3 (110): downloaded to one device"),
			pt("In the clear: the sign-in and the message",
				"Anyone on the network can read them"),
		}, [][]string{alt("IMAP:143", "POP3:110")}}
	}
	return step{"TARGET fetches it", []point{
		pt("Signs in, over TLS"),
		pt("IMAP (993): stays on the server, synced"),
		pt("POP3 (995): downloaded to one device"),
		pt("Or with STARTTLS: IMAP on 143, POP3 on 110",
			"Starts plain, then upgrades to TLS"),
	}, [][]string{alt("IMAP:TLS:993", "IMAP:STARTTLS:143", "POP3:TLS:995", "POP3:STARTTLS:110")}}
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

// relayStep is 4: server to server.
func relayStep(encrypted, tls bool) step {
	last := pt("Each server reads the message")
	if tls {
		last.sub = []string{"TLS encrypts the connection, not the stored mail"}
	}
	if encrypted {
		last = pt("Each server has the message, still encrypted",
			"Step 1's encryption keeps what it says hidden")
		if tls {
			last.sub = append([]string{"TLS encrypts the connection, not the stored mail"}, last.sub...)
		}
	}
	if !tls {
		return step{"Server to server", []point{
			pt("SMTP, on port 25"),
			pt("In the clear: anyone on the way reads it"),
			last,
		}, [][]string{alt("SMTP:25")}}
	}
	return step{"Server to server", []point{
		pt("SMTP, on port 25"),
		pt("STARTTLS encrypts, if both offer it",
			"Often without checking the certificate"),
		pt("An attacker in between can strip the offer",
			"The mail then goes on unencrypted"),
		pt("MTA-STS: a policy over HTTPS makes TLS a must",
			"Published at mta-sts.domain, kept by senders"),
		pt("DANE: the certificate pinned in DNSSEC",
			"TLSA records, in signed DNS"),
		last,
	}, [][]string{alt("SMTP:STARTTLS:25"), alt("MTA-STS", "DANE")}}
}

// checkStep is 5: TARGET SERVER checks the sender, and keeps the message.
func checkStep(encrypted bool) step {
	stored := "As it came: readable by TARGET's provider"
	if encrypted {
		stored = "As it came: what it says stays encrypted"
	}
	return step{"TARGET SERVER checks the sender", []point{
		pt("SPF: is the sending server on the domain's list?",
			"Checks the bounce address, not the visible From",
			"Fails when mail is forwarded"),
		pt("DKIM: does the signature verify, unchanged?",
			"Survives forwarding; mailing lists can break it"),
		pt("DMARC: does one pass for the visible From?",
			"Aligned: the domain that passed is From's"),
		pt("DMARC, failing: none, quarantine or reject",
			"The domain's owner chooses, in its DNS"),
		pt("ARC: keeps results through forwarders"),
		pt("The verdict goes into Authentication-Results"),
		pt("Spam and malware filtered, then stored", stored),
	}, one("SPF", "DKIM", "DMARC", "ARC")}
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

// fakeDomain is the attacker's look-alike domain, as the picture shows it.
const fakeDomain = "acrne.com"

// educateSection is the Educate page: the picture, what its symbols mean,
// and each step.
//
// The picture and what its symbols mean stay together, in view; under
// them, which message — signed or not, encrypted or not — and its steps,
// the picture showing that one.
func educateSection() widget.Component {
	scene := newRouteScene()
	key := widgets.NewWrap()
	key.Gap, key.LineGap = 16, 6
	for _, sy := range symbols {
		key.Add(newSymbolItem(sy))
	}
	list := widgets.NewColumn().WithGap(14)
	signed, encrypted, tls := false, false, true
	show := func() {
		scene.signed, scene.encrypted, scene.tls = signed, encrypted, tls
		scene.RequestLayout()
		scene.Invalidate()
		list.ClearChildren()
		for i, st := range scenarioSteps(signed, encrypted, tls) {
			about := widgets.NewColumn(newStrong(st.title)).WithGap(6)
			if len(st.tags) > 0 {
				about.Add(altRow(st.tags))
			}
			about.Add(pointList(st.points))
			row := widgets.NewRow(newNumberMark(i + 1)).WithGap(8).WithAlign(layout.AlignStart)
			row.AddFlex(about, 1)
			list.Add(row)
		}
		list.RequestLayout()
		list.Invalidate()
	}
	// Which message: signed or not, encrypted or not, over connections
	// with TLS or without — eight, each its steps, the picture showing it.
	sign := widgets.NewSegmented([]string{"Not signed", "Signed"}, 0, nil)
	sign.OnChange = func(i int) { signed = i == 1; show() }
	encrypt := widgets.NewSegmented([]string{"Not encrypted", "Encrypted"}, 0, nil)
	encrypt.OnChange = func(i int) { encrypted = i == 1; show() }
	conn := widgets.NewSegmented([]string{"With TLS", "Without TLS"}, 0, nil)
	conn.OnChange = func(i int) { tls = i == 0; show() }
	show()
	which := widgets.NewWrap(sign, encrypt, conn)
	which.Gap, which.LineGap = 12, 8
	words := widgets.NewColumn(which, widgets.NewTitle("Step by step"), list).WithGap(14)
	return newEducatePage(widgets.NewColumn(scene, key).WithGap(10), scene, words)
}

// educatePage keeps the picture in view while the words under it scroll,
// so each step can be read with the picture beside it — when the page is
// tall enough to leave the words room (pinMin); when not, the picture
// scrolls with them.
type educatePage struct {
	widget.Base
	top    widget.Component // what stays in view: the picture, and its key
	scene  *routeScene
	words  widget.Component
	rule   *widgets.Separator
	body   *widgets.FlexBox // what scrolls: the words, and the picture when not pinned
	scroll *widgets.ScrollView
	pinned bool
}

// pinMin is the room the words keep under a pinned picture: some lines
// of them.
const pinMin = 120

func newEducatePage(top widget.Component, scene *routeScene, words widget.Component) *educatePage {
	p := &educatePage{top: top, scene: scene, words: words, rule: widgets.NewSeparator()}
	p.Init(p)
	p.body = widgets.NewColumn(top, words).WithGap(14)
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

func (p *educatePage) Arrange(r paintengine2d.Rect) {
	p.SetBounds(r)
	pad := style.Dip(p.Look(), 4)
	inner := r.Dx() - 2*pad
	topH := p.top.Measure(layout.Constraints{MaxW: inner, MaxH: -1}).Y
	ruleH := p.rule.Measure(layout.Constraints{MaxW: r.Dx(), MaxH: -1}).Y
	pin := r.Dy()-(pad+topH+pad+ruleH) >= style.Dip(p.Look(), pinMin)
	if pin != p.pinned {
		p.pinned = pin
		p.body.ClearChildren()
		if pin {
			p.Remove(p.scroll)
			p.Add(p.top)
			p.Add(p.rule)
			p.Add(p.scroll)
		} else {
			p.Remove(p.top)
			p.Remove(p.rule)
			p.body.Add(p.top)
		}
		p.body.Add(p.words)
		p.scroll.ScrollTo(0)
	}
	if !pin {
		p.scroll.Arrange(r)
		return
	}
	p.top.Arrange(paintengine2d.XYWH(r.Min.X+pad, r.Min.Y+pad, inner, topH))
	top := r.Min.Y + pad + topH + pad
	p.rule.Arrange(paintengine2d.XYWH(r.Min.X, top, r.Dx(), ruleH))
	p.scroll.Arrange(paintengine2d.Rect{Min: paintengine2d.Pt(r.Min.X, top+ruleH), Max: r.Max})
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

// drawNumber draws n in a disc of size s centred at c.
func drawNumber(ctx *paintengine2d.Context, lk style.LookAndFeel, n int, c paintengine2d.Point, s float32) {
	p := lk.Palette()
	ctx.DrawCircle(c, s/2, paintengine2d.Fill(p.Accent))
	f := lk.BoldFont()
	t := strconv.Itoa(n)
	f.Draw(ctx, t, paintengine2d.Pt(c.X-f.Advance(t)/2, c.Y-f.Height()/2), p.TextOnAccent)
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
// in.
type placedChip struct {
	name  string
	r     paintengine2d.Rect
	group string
	alt   int
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
	{envelopeSymbol(envelope{stamp: true}), "Signed by your domain"},
	{func(ctx *paintengine2d.Context, lk style.LookAndFeel, box paintengine2d.Rect) {
		y := box.Center().Y
		drawTube(ctx, lk, paintengine2d.Pt(box.Min.X+style.Dip(lk, 3), y), paintengine2d.Pt(box.Max.X-style.Dip(lk, 3), y), box.Dy()*0.8)
	}, "TLS"},
	{func(ctx *paintengine2d.Context, lk style.LookAndFeel, box paintengine2d.Rect) {
		y := box.Center().Y
		drawPlainLine(ctx, lk, paintengine2d.Pt(box.Min.X+style.Dip(lk, 3), y), paintengine2d.Pt(box.Max.X-style.Dip(lk, 3), y))
	}, "No TLS"},
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
