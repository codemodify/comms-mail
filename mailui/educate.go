package mailui

import (
	"strconv"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Educate: how an email gets from you to a friend,
// and the standards that keep it safe on the way — one picture of the
// three hops, each with the standards it uses drawn on it, and of a
// look-alike coming in; then what happens on each, and what each standard
// does and how, told plainly. A standard is the same tag everywhere, so
// the picture and the words read together.

// hop is one of the picture's numbered steps: a hop on the way, or the
// look-alike.
type hop struct {
	title string
	says  []string // paragraphs
	// wire are the standards the message goes over on the hop, and asks
	// what the servers look up in DNS for it: both drawn in the picture.
	// also are named in the words only.
	wire, asks, also []string
}

// tags are the hop's standards, as its words name them.
func (h hop) tags() []string {
	return append(append(append([]string(nil), h.wire...), h.asks...), h.also...)
}

var hops = []hop{
	{title: "From you to your mail server",
		says: []string{
			"You press Send. If you asked for it, comms-mail first signs the message, and locks it so only your friend can open it (OpenPGP or S/MIME).",
			"comms-mail then connects to your mail server, locks the connection, and logs in: with your password, or with an OAuth sign-in, so your password never leaves your provider. It hands the message over with SMTP, and from here on your server is in charge.",
		},
		wire: []string{"SMTP", "TLS", "OAuth"}},
	{title: "From your mail server to theirs",
		says: []string{
			"Your server seals the message with your domain's DKIM signature. It looks up your friend's domain in DNS to learn which server takes its mail (the MX record), connects to it, locks the connection with STARTTLS, and hands the message over with SMTP.",
			"Their server checks the message before taking it in. It asks DNS whether your server may send mail for your domain (SPF), whether the seal is real and unbroken (DKIM), and what your domain wants done when those fail (DMARC). It writes what it found at the top of the message, and files it in your friend's mailbox.",
		},
		wire: []string{"SMTP", "STARTTLS"}, asks: []string{"MX", "SPF", "DKIM", "DMARC"}},
	{title: "From their mail server to your friend",
		says: []string{
			"Your friend's mail program logs in to their server, over a locked connection, and fetches the message with IMAP or POP3.",
			"It shows what the server found out about the sender. If you signed the message, it checks your signature; if you encrypted it, it opens it with your friend's private key, which only they have.",
		},
		wire: []string{"IMAP", "POP3", "TLS"}},
}

// lookAlike is the picture's last step: mail from a look-alike address,
// which every check passes.
var lookAlike = hop{title: "A look-alike",
	says: []string{
		"A stranger registers paypa1.com, with the digit 1 where PayPal has the letter l, and sends you mail as “PayPal”. It travels the same road as any other mail, and every check passes: SPF, DKIM and DMARC all say it really comes from paypa1.com. It does. The stranger owns that domain, and set it all up properly.",
		"The standards prove which domain sent a message. They cannot tell you whether it is the one you think. comms-mail warns you when a domain looks like one you write to, or uses letters from another alphabet that look like ours, but the last check is you: read the address, letter by letter, before you click, pay or answer.",
	},
	also: []string{"SPF", "DKIM", "DMARC"}}

// fakeDomain is the look-alike's domain, as the picture shows it.
const fakeDomain = "paypa1.com"

// standard is one standard, or two that do one job: what it does and how,
// in paragraphs.
type standard struct {
	names []string
	about []string
}

var standards = []standard{
	{[]string{"SMTP"}, []string{
		"SMTP is how email is sent. Each time a message moves toward its destination, from your mail program to your server or from one server to the next, it goes by SMTP.",
		"It works like a short conversation. The sender connects, says who the mail is from and who it is for, and hands the message over. The receiver says it has it, and the job is now its own. If it cannot deliver yet, it keeps trying for a few days before sending back a bounce.",
		"SMTP dates from 1982, when the few computers on the network trusted one another. On its own it checks nothing: anyone can write any From address, and the message goes as plain text. Everything else on this page was added later to fix that.",
	}},
	{[]string{"TLS", "STARTTLS"}, []string{
		"TLS locks the connection between two computers. It is the same lock as the padlock in your browser's address bar: nobody in between, on café Wi-Fi or at an internet provider, can read or change what passes through.",
		"When the connection opens, the server shows a certificate that proves its name, issued by an authority your computer already trusts. The two sides then agree on a fresh secret key that only they know, and encrypt everything with it.",
		"STARTTLS is the same lock, switched on once the connection has started. Servers use it to talk to one another. Its weak spot is that someone in the middle can hide the offer, and the two servers may carry on unlocked. MTA-STS and DANE close that gap: the receiving domain publishes that the lock is required, and which certificate to expect. comms-mail never quietly goes without the lock: if your server does not offer it, comms-mail does not connect, unless you set that server up without one.",
		"TLS protects mail only while it moves. Each server along the way unlocks it, can read it, and stores it as it is.",
	}},
	{[]string{"OAuth"}, []string{
		"OAuth lets comms-mail into your mailbox without ever seeing your password.",
		"Instead of typing your password into comms-mail, you sign in on your provider's own page, in your browser, two-step code and all. Your provider then hands comms-mail a token: a key that opens your mail and nothing else. Your password stays with them.",
		"You can take the token back at any time from your account's security page, and comms-mail is locked out without you changing your password. Big providers like Google and Microsoft push for OAuth, and some no longer take a plain password at all.",
	}},
	{[]string{"DNS", "MX"}, []string{
		"DNS is the internet's address book. Computers use it to turn a name like example.com into the address of a machine.",
		"For mail, each domain has an entry called an MX record, which says which server takes its mail. When you write to friend@example.com, your server looks up example.com's MX record and delivers to the server it names.",
		"Domains also keep their rules for mail in DNS: SPF, DKIM and DMARC, below. Only a domain's owner can change its entries, which is why servers trust what they find there.",
	}},
	{[]string{"SPF"}, []string{
		"SPF is a domain's list of the servers allowed to send its mail.",
		"The owner of example.com publishes the list in DNS. When a message arrives saying it is from example.com, the receiving server checks whether the server that delivered it is on the list. If it is not, the message is suspect.",
		"SPF has two blind spots. It checks the hidden return address used for bounces, not the From address you see. And it fails when mail is forwarded, because the forwarding server is not on the list. DKIM and DMARC cover for both.",
	}},
	{[]string{"DKIM"}, []string{
		"DKIM is a tamper-proof seal that the sending domain puts on every message.",
		"The sending server signs the message, its text and its main headers such as From and Subject, with a private key that only it has. The matching public key is published in the domain's DNS. The receiving server looks it up and checks the seal. If anyone changed the message on the way, or made it up, the seal is broken.",
		"Unlike SPF, the seal survives forwarding. It proves which domain sent the message, and that nothing changed. It does not hide anything, though: every server on the way can still read the message.",
	}},
	{[]string{"DMARC"}, []string{
		"DMARC ties SPF and DKIM to the From address you actually see, and tells servers what to do when the checks fail.",
		"On their own, SPF and DKIM can pass for a domain other than the one in From: a forger can set them up for a domain of their own. DMARC asks for at least one of them to pass for the From domain itself. The domain's owner publishes a rule in DNS: let failing mail through, send it to spam, or refuse it. Many banks and big companies say refuse.",
		"The receiving server writes what it found into the message, in a header called Authentication-Results. comms-mail reads the one your own provider added, and warns you when a message failed the checks.",
	}},
	{[]string{"IMAP", "POP3"}, []string{
		"IMAP and POP3 are how a mail program collects mail from its server. SMTP sends; these fetch.",
		"IMAP leaves your mail on the server and shows it to you there, with its folders, flags and read marks. Read a message on your phone, and it shows as read on your laptop too. It is what nearly everyone uses today.",
		"POP3 is the older way. It downloads mail to one device, and usually deletes it from the server. It is simple and works offline, but your devices never agree with each other.",
		"Both log in with your password or an OAuth token, inside TLS, so the login and the mail are locked on their way to you.",
	}},
	{[]string{"OpenPGP", "S/MIME"}, []string{
		"Both sign and encrypt email so only the recipient can read it. They differ in how you get and trust keys.",
		"S/MIME uses X.509 certificates, the same kind of certificate a website uses. A certificate authority (or your company) issues one tied to your email address. Trust comes from that authority: if you trust the CA, you trust the certificate. It is built into Outlook, Apple Mail and Thunderbird. Companies like it because IT can issue and revoke certificates. You usually pay for a public certificate, or get one from work.",
		"OpenPGP uses keys you create yourself. There is no central authority. You share your public key, and other people decide to trust it (directly, or through people they already trust). It is free and works across mail providers. Thunderbird has it built in, and GnuPG is the classic tool. The hard part is getting the other person a key you both believe is theirs.",
		"Same job, different trust model: S/MIME says “a CA vouches for this address.” OpenPGP says “I vouch for this key, or someone I trust does.” They do not work together: a message encrypted with one cannot be opened with the other. comms-mail does both (Keys, above).",
		"Unlike everything else here, they protect the message itself, from your device to your friend's: no server on the way, not even yours, can read it, or change it unnoticed. Neither hides who emailed whom, or when. Only the body and attachments are protected, and the subject usually stays visible; comms-mail moves the subject inside the encryption too, so the outside shows only dots.",
	}},
}

// endToEnd are the standards over the whole way, from you to your friend.
var endToEnd = []string{"OpenPGP", "S/MIME"}

// lastWord is said under the standards.
const lastWord = "One more: pictures from the internet in a message tell the sender that you opened it, and when. comms-mail asks before loading them. When in doubt, do not click: go to the website yourself, or ask the person another way."

// paragraphs are paras, each its own wrapping label.
func paragraphs(paras []string) *widgets.FlexBox {
	col := widgets.NewColumn().WithGap(6)
	for _, p := range paras {
		col.Add(wrapLabel(p))
	}
	return col
}

// educateSection is the Educate page: the picture, what happens on each
// step, and the standards.
func educateSection() widget.Component {
	col := widgets.NewColumn(newHopsScene(), widgets.NewTitle("What happens")).WithGap(14)
	for i, h := range append(append([]hop(nil), hops...), lookAlike) {
		about := widgets.NewColumn(newStrong(h.title), chipRow(h.tags()), paragraphs(h.says)).WithGap(6)
		row := widgets.NewRow(newNumberMark(i + 1)).WithGap(8).WithAlign(layout.AlignStart)
		row.AddFlex(about, 1)
		col.Add(row)
	}
	col.Add(widgets.NewTitle("The standards"))
	for _, s := range standards {
		col.Add(widgets.NewColumn(chipRow(s.names), paragraphs(s.about)).WithGap(6))
	}
	last := wrapLabel(lastWord)
	last.Tone = widgets.ToneMuted
	col.Add(last)
	return widgets.NewScrollView(widgets.NewPad(4, col))
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

// placedChip is a tag where it is drawn.
type placedChip struct {
	name string
	r    paintengine2d.Rect
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
			out = append(out, placedChip{n, paintengine2d.XYWH(x, y, cw, ch)})
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

// chipRow is names' tags in a row that wraps.
func chipRow(names []string) *widgets.Wrap {
	w := widgets.NewWrap()
	w.Gap = chipGap
	for _, n := range names {
		w.Add(newProtoChip(n))
	}
	return w
}

// strong is a line of bold text that wraps: a hop's name over its words.
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

// ---- the picture ----

// hopsScene is the picture: you, your mail server, theirs and your friend,
// the three hops between them numbered, each with what it goes over drawn
// on it; OpenPGP and S/MIME over all of it, end to end; and under it, a
// stranger's look-alike coming into your mail server, and DNS, which the
// servers ask, with what they look up there.
type hopsScene struct{ widget.Base }

func newHopsScene() *hopsScene {
	s := &hopsScene{}
	s.Init(s)
	return s
}

const (
	sceneNode = 52 // a person's or a server's disc, at most
	sceneMin  = 300
	rowGap    = 32 // between the names and the row under them: room for 4
)

var sceneNames = [4]string{"You", "Your mail server", "Their mail server", "Your friend"}

// sceneLayout is where everything goes at width w, from the picture's top
// left. The row under the names has the stranger at x(0.5), between you
// and your server, and DNS at x(2), under theirs.
type sceneLayout struct {
	slot, d, dnd             float32
	spanY, y1, labelsY, rowY float32
	labels                   [4][]string
	stranger, fake, dnsLabel []string
	chips                    []placedChip // on the span, over the hops, under DNS
	h                        float32
}

// x is the centre of column i: 0 you, 1 your server, 2 theirs, 3 your
// friend.
func (g sceneLayout) x(i float32) float32 { return g.slot * (i + 0.5) }

// labelsFoot is under the names.
func (g sceneLayout) labelsFoot(fh float32) float32 {
	lines := 0
	for _, l := range g.labels {
		lines = max(lines, len(l))
	}
	return g.labelsY + float32(lines)*fh
}

func (s *hopsScene) dip(v float32) float32 { return style.Dip(s.Look(), v) }

func (s *hopsScene) MinWidth() float32 { return s.dip(sceneMin) }

func (s *hopsScene) Measure(c layout.Constraints) paintengine2d.Point {
	w := s.dip(560)
	if c.HasMaxW() {
		w = c.MaxW
	}
	return c.Constrain(paintengine2d.Pt(w, s.layoutAt(w).h))
}

func (s *hopsScene) Arrange(r paintengine2d.Rect) { s.SetBounds(r) }

// asked is what the servers look up in DNS, over all the hops.
func asked() []string {
	var out []string
	for _, h := range hops {
		out = append(out, h.asks...)
	}
	return out
}

func (s *hopsScene) layoutAt(w float32) sceneLayout {
	lk := s.Look()
	font := lk.Font()
	fh := font.Height()
	gap := s.dip(chipGap)
	var g sceneLayout
	g.slot = w / 4
	g.d = min(s.dip(sceneNode), g.slot*0.48)
	g.dnd = g.d * 0.7

	// Over everything: end to end, from you to your friend.
	span, spanH := placeChips(lk, endToEnd, g.x(1.5), s.dip(6), w*0.6, false)
	g.spanY = s.dip(6) + spanH/2
	g.chips = span

	// Over each hop, clear of the discs, what it goes over.
	var band float32
	for i, h := range hops {
		_, hh := placeChips(lk, h.wire, g.x(float32(i)+0.5), 0, g.slot-s.dip(6), false)
		band = max(band, hh)
	}
	top := s.dip(6) + spanH + s.dip(12)
	g.y1 = top + band + s.dip(4) + g.d/2
	for i, h := range hops {
		_, hh := placeChips(lk, h.wire, g.x(float32(i)+0.5), 0, g.slot-s.dip(6), false)
		c, _ := placeChips(lk, h.wire, g.x(float32(i)+0.5), g.y1-g.d/2-s.dip(4)-hh, g.slot-s.dip(6), false)
		g.chips = append(g.chips, c...)
	}

	// The names, clear of the columns' edges, where the look-alike's
	// line goes up between them.
	for i, name := range sceneNames {
		g.labels[i] = wrapWords(font, name, g.slot-s.dip(10), 3)
	}
	g.labelsY = g.y1 + g.d/2 + s.dip(6)

	// The row under them: the stranger, and DNS with what the servers
	// look up there.
	g.rowY = g.labelsFoot(fh) + s.dip(rowGap) + g.dnd/2
	under := g.rowY + g.dnd/2 + s.dip(4)
	// The stranger's words and DNS's tags share the row: the words as
	// wide as they need, up to x(1.25), and the tags in what is left.
	g.stranger = wrapWords(font, "A stranger", 1.5*g.slot-s.dip(6), 2)
	g.fake = wrapWords(font, fakeDomain, 1.5*g.slot-s.dip(6), 1)
	var words float32
	for _, l := range append(append([]string(nil), g.stranger...), g.fake...) {
		words = max(words, font.Advance(l))
	}
	room := min(g.x(2)-(g.x(0.5)+words/2)-s.dip(8), w-g.x(2)-s.dip(3))
	g.dnsLabel = wrapWords(font, "DNS", g.slot, 1)
	c, ch := placeChips(lk, asked(), g.x(2), under+fh+gap, 2*room, false)
	g.chips = append(g.chips, c...)
	g.h = max(under+float32(len(g.stranger)+len(g.fake))*fh, under+fh+gap+ch) + s.dip(8)
	return g
}

// sceneMark is a number in the picture, where it is drawn.
type sceneMark struct {
	n    int
	x, y float32
}

// marks are where the numbers go: each hop's on it, between its discs;
// the look-alike's on its line, between the names and the stranger.
func (s *hopsScene) marks(g sceneLayout, b paintengine2d.Rect) []sceneMark {
	var out []sceneMark
	for i := range hops {
		out = append(out, sceneMark{i + 1, b.Min.X + g.x(float32(i)+0.5), b.Min.Y + g.y1})
	}
	fh := s.Look().Font().Height()
	return append(out, sceneMark{len(hops) + 1, b.Min.X + g.x(0.5), b.Min.Y + g.labelsFoot(fh) + s.dip(rowGap)/2})
}

// fakePath is the look-alike's line, from the stranger up between your
// name and your server's, and into your server's disc from below left.
func (s *hopsScene) fakePath(g sceneLayout) []paintengine2d.Point {
	r := g.d/2 + s.dip(3)
	return []paintengine2d.Point{
		paintengine2d.Pt(g.x(0.5), g.rowY-g.dnd/2-s.dip(3)),
		paintengine2d.Pt(g.x(0.5), g.labelsY-s.dip(2)),
		paintengine2d.Pt(g.x(1)-r*0.707, g.y1+r*0.707),
	}
}

func (s *hopsScene) Paint(ctx *paintengine2d.Context) {
	lk := s.Look()
	b := s.LocalBounds()
	g := s.layoutAt(b.Dx())
	in := inksOf(lk)
	font := lk.Font()
	fh := font.Height()
	at := func(x, y float32) paintengine2d.Point { return paintengine2d.Pt(b.Min.X+x, b.Min.Y+y) }
	x := g.x
	y1 := g.y1
	r := g.d / 2
	accent := lk.Palette().Ink(lk.Palette().Accent)
	pen := func(c paintengine2d.Color, dashed bool) paintengine2d.Paint {
		p := paintengine2d.StrokePaint(c, s.dip(1.75))
		if dashed {
			p.Stroke.Dash = []float32{s.dip(5), s.dip(4)}
		}
		return p
	}
	head := func(tip paintengine2d.Point, dx, dy float32, c paintengine2d.Color) {
		h := s.dip(7)
		p := paintengine2d.NewPath()
		p.MoveTo(tip.X, tip.Y)
		p.LineTo(tip.X-dx*h-dy*h*0.6, tip.Y-dy*h+dx*h*0.6)
		p.LineTo(tip.X-dx*h+dy*h*0.6, tip.Y-dy*h-dx*h*0.6)
		p.Close()
		ctx.DrawPath(p, paintengine2d.Fill(c))
	}
	node := func(p pict, c paintengine2d.Point, size float32, ink, edge paintengine2d.Color) {
		ctx.DrawCircle(c, size/2, paintengine2d.Fill(in.disc))
		ctx.DrawCircle(c, size/2, paintengine2d.StrokePaint(edge, s.dip(1.5)))
		sz := size * 0.56
		drawPict(ctx, lk, p, paintengine2d.XYWH(c.X-sz/2, c.Y-sz/2, sz, sz), ink)
	}
	centred := func(lines []string, cx, y float32, ink paintengine2d.Color) float32 {
		for _, l := range lines {
			font.Draw(ctx, l, at(cx-font.Advance(l)/2, y), ink)
			y += fh
		}
		return y
	}

	// End to end, from you to your friend, over it all.
	foot := y1 - r - s.dip(3)
	ctx.DrawLine(at(x(0), foot), at(x(0), g.spanY), pen(accent, true))
	ctx.DrawLine(at(x(0), g.spanY), at(x(3), g.spanY), pen(accent, true))
	ctx.DrawLine(at(x(3), g.spanY), at(x(3), foot), pen(accent, true))
	head(at(x(3), foot), 0, 1, accent)

	// The three hops.
	for i := 0; i < 3; i++ {
		x0, x1 := x(float32(i))+r+s.dip(4), x(float32(i+1))-r-s.dip(4)
		ctx.DrawLine(at(x0, y1), at(x1, y1), pen(in.good, false))
		head(at(x1, y1), 1, 0, in.good)
	}

	// DNS, under their server, which both servers ask.
	feet := g.labelsFoot(fh) + s.dip(4)
	ctx.DrawLine(at(x(2), g.rowY-g.dnd/2-s.dip(3)), at(x(2), feet), pen(in.muted, true))
	ctx.DrawLine(at(x(2)-g.dnd*0.45, g.rowY-g.dnd*0.25), at(x(1), feet), pen(in.muted, true))
	node(pictServer, at(x(2), g.rowY), g.dnd, in.text, in.discEdge)
	centred(g.dnsLabel, x(2), g.rowY+g.dnd/2+s.dip(4), in.text)

	// The stranger, and the look-alike coming into your server.
	fp := s.fakePath(g)
	for i := 1; i < len(fp); i++ {
		ctx.DrawLine(at(fp[i-1].X, fp[i-1].Y), at(fp[i].X, fp[i].Y), pen(in.bad, false))
	}
	end, from := fp[len(fp)-1], fp[len(fp)-2]
	d := end.Sub(from).Normalize()
	head(at(end.X, end.Y), d.X, d.Y, in.bad)
	node(pictPerson, at(x(0.5), g.rowY), g.dnd, in.bad, in.bad)
	y := centred(g.stranger, x(0.5), g.rowY+g.dnd/2+s.dip(4), in.text)
	centred(g.fake, x(0.5), y, in.bad)

	// The people and the servers.
	for i, p := range []pict{pictPerson, pictServer, pictServer, pictPerson} {
		node(p, at(x(float32(i)), y1), g.d, in.text, in.discEdge)
		centred(g.labels[i], x(float32(i)), g.labelsY, in.text)
	}

	// The standards, and the numbers, over everything.
	for _, c := range g.chips {
		drawChip(ctx, lk, c.name, c.r.Translate(b.Min))
	}
	for _, m := range s.marks(g, b) {
		drawNumber(ctx, lk, m.n, paintengine2d.Pt(m.x, m.y), s.dip(markSize))
	}
}
