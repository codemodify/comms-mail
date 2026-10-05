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
// three hops, each with the standards it uses drawn on it, then what
// happens on each hop, and what each standard does and how. A standard is
// the same tag everywhere, so the picture and the words read together.

// hop is one of the picture's numbered hops.
type hop struct {
	title, says string
	// wire are the standards the message goes over on the hop; asks what
	// the servers look up in DNS for it.
	wire, asks []string
}

var hops = []hop{
	{"From you to your mail server",
		"comms-mail signs and encrypts the message, if you asked, then connects to your mail server, locks the connection, logs in with your password or an OAuth sign-in, and hands the message over.",
		[]string{"SMTP", "TLS", "OAuth"}, nil},
	{"From your mail server to theirs",
		"Your server stamps the message with DKIM, asks DNS which server takes the other domain's mail (its MX record), and hands the message over, the connection locked. Their server asks DNS too: may your server send for your domain (SPF), is the stamp real (DKIM), and what does your domain want done when they fail (DMARC). It writes what it found into the message.",
		[]string{"SMTP", "STARTTLS"}, []string{"MX", "SPF", "DKIM", "DMARC"}},
	{"From their mail server to your friend",
		"Your friend's mail program logs in to their server over a locked connection and fetches the message. It shows what the server found, checks your signature and opens the encryption.",
		[]string{"IMAP", "POP3", "TLS"}, nil},
}

// standard is one standard, or two that do one job: what it does, and how.
type standard struct {
	names []string
	what  string
}

var standards = []standard{
	{[]string{"SMTP"}, "How mail is handed over, from your program to your server and from server to server. The sender says who the mail is from and who it is for, then sends it; each server passes it on toward the address."},
	{[]string{"TLS", "STARTTLS"}, "Locks the connection, so nobody on the way can read or change what goes over it. The two ends agree on a secret key and encrypt everything with it, and the server proves who it is with a certificate. STARTTLS locks a connection that began open; MTA-STS and DANE let a domain say the lock is a must."},
	{[]string{"OAuth"}, "Lets comms-mail log in without your password. Google or Microsoft gives it a token that opens your mail and nothing else, which you can take back at any time."},
	{[]string{"DNS", "MX"}, "The internet's address book. A domain's MX record says which server takes its mail; the domain's SPF, DKIM and DMARC records are kept there too."},
	{[]string{"SPF"}, "Says which servers may send mail for a domain. The domain lists them in DNS; the receiving server checks the server that sent the message against the list."},
	{[]string{"DKIM"}, "A tamper-proof stamp from the sending domain. Its server signs the message with a private key; the receiving server checks the stamp with the public key the domain puts in DNS. A message changed on the way fails."},
	{[]string{"DMARC"}, "Ties SPF and DKIM to the From address you see, and says what to do when they fail: let it through, put it in spam, or refuse it. The receiving server writes its verdict into the message (Authentication-Results); comms-mail reads it, and warns you about mail it could not confirm."},
	{[]string{"IMAP", "POP3"}, "Fetch mail from your mailbox. Your mail program logs in over TLS; IMAP keeps the mail on the server, the same on all your devices, POP3 takes it down."},
	{[]string{"OpenPGP", "S/MIME"}, "Sign and encrypt from end to end, so no server on the way can read or change the message, not even yours. You sign with your private key and encrypt to your friend's public key; only their private key opens it. OpenPGP trusts keys people check themselves; S/MIME trusts certificates from authorities."},
}

// endToEnd are the standards over the whole way, from you to your friend.
var endToEnd = []string{"OpenPGP", "S/MIME"}

// lastWord is said under the standards.
const lastWord = "None of these catches a look-alike address (paypa1.com is not paypal.com): read it letter by letter. Pictures from the internet tell the sender you read the mail, so comms-mail asks before loading them. When in doubt, do not click."

// educateSection is the Educate page: the picture, what happens on each
// hop, and the standards.
func educateSection() widget.Component {
	col := widgets.NewColumn(newHopsScene(), widgets.NewTitle("What happens")).WithGap(10)
	for i, h := range hops {
		about := widgets.NewColumn(newStrong(h.title), chipRow(append(append([]string(nil), h.wire...), h.asks...)), wrapLabel(h.says)).WithGap(4)
		row := widgets.NewRow(newNumberMark(i + 1)).WithGap(8).WithAlign(layout.AlignStart)
		row.AddFlex(about, 1)
		col.Add(row)
	}
	col.Add(widgets.NewTitle("The standards"))
	for _, s := range standards {
		col.Add(widgets.NewColumn(chipRow(s.names), wrapLabel(s.what)).WithGap(4))
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
// on it; DNS under the servers, with what they look up there; and OpenPGP
// and S/MIME over all of it, end to end.
type hopsScene struct{ widget.Base }

func newHopsScene() *hopsScene {
	s := &hopsScene{}
	s.Init(s)
	return s
}

const (
	sceneNode = 52 // a person's or a server's disc, at most
	sceneMin  = 300
)

var sceneNames = [4]string{"You", "Your mail server", "Their mail server", "Your friend"}

// sceneLayout is where everything goes at width w, from the picture's top
// left.
type sceneLayout struct {
	slot, d                       float32
	spanY, y1, labelsY, dnsY, dnd float32
	labels                        [4][]string
	dnsLabel                      []string
	chips                         []placedChip // on the span, over the hops, under DNS
	h                             float32
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
	x := func(i float32) float32 { return g.slot * (i + 0.5) }

	// Over everything: end to end, from you to your friend.
	span, spanH := placeChips(lk, endToEnd, x(1.5), s.dip(6), w*0.6, false)
	g.spanY = s.dip(6) + spanH/2
	g.chips = span

	// Over each hop, clear of the discs, what it goes over.
	var band float32
	for i, h := range hops {
		_, hh := placeChips(lk, h.wire, x(float32(i)+0.5), 0, g.slot-s.dip(6), false)
		band = max(band, hh)
	}
	top := s.dip(6) + spanH + s.dip(12)
	g.y1 = top + band + s.dip(4) + g.d/2
	for i, h := range hops {
		_, hh := placeChips(lk, h.wire, x(float32(i)+0.5), 0, g.slot-s.dip(6), false)
		c, _ := placeChips(lk, h.wire, x(float32(i)+0.5), g.y1-g.d/2-s.dip(4)-hh, g.slot-s.dip(6), false)
		g.chips = append(g.chips, c...)
	}

	lines := 0
	for i, name := range sceneNames {
		g.labels[i] = wrapWords(font, name, g.slot-s.dip(4), 3)
		lines = max(lines, len(g.labels[i]))
	}
	g.labelsY = g.y1 + g.d/2 + s.dip(6)

	// Under the servers, DNS, and what they look up there.
	g.dnd = g.d * 0.7
	g.dnsY = g.labelsY + float32(lines)*fh + s.dip(22) + g.dnd/2
	g.dnsLabel = wrapWords(font, "DNS", g.slot, 1)
	under := g.dnsY + g.dnd/2 + s.dip(4) + fh + gap
	c, ch := placeChips(lk, asked(), x(1.5), under, 3*g.slot-s.dip(6), false)
	g.chips = append(g.chips, c...)
	g.h = under + ch + s.dip(8)
	return g
}

// sceneMark is a number in the picture, where it is drawn.
type sceneMark struct {
	n    int
	x, y float32
}

// marks are where the hops' numbers go: on each hop, between its discs.
func (s *hopsScene) marks(g sceneLayout, b paintengine2d.Rect) []sceneMark {
	var out []sceneMark
	for i := range hops {
		out = append(out, sceneMark{i + 1, b.Min.X + g.slot*(float32(i)+1), b.Min.Y + g.y1})
	}
	return out
}

func (s *hopsScene) Paint(ctx *paintengine2d.Context) {
	lk := s.Look()
	b := s.LocalBounds()
	g := s.layoutAt(b.Dx())
	in := inksOf(lk)
	font := lk.Font()
	fh := font.Height()
	x := func(i float32) float32 { return b.Min.X + g.slot*(i+0.5) }
	y1 := b.Min.Y + g.y1
	r := g.d / 2
	accent := lk.Palette().Ink(lk.Palette().Accent)
	pen := func(c paintengine2d.Color, dashed bool) paintengine2d.Paint {
		p := paintengine2d.StrokePaint(c, s.dip(1.75))
		if dashed {
			p.Stroke.Dash = []float32{s.dip(5), s.dip(4)}
		}
		return p
	}
	head := func(at paintengine2d.Point, dx, dy float32, c paintengine2d.Color) {
		h := s.dip(7)
		p := paintengine2d.NewPath()
		p.MoveTo(at.X, at.Y)
		p.LineTo(at.X-dx*h-dy*h*0.6, at.Y-dy*h+dx*h*0.6)
		p.LineTo(at.X-dx*h+dy*h*0.6, at.Y-dy*h-dx*h*0.6)
		p.Close()
		ctx.DrawPath(p, paintengine2d.Fill(c))
	}
	node := func(p pict, at paintengine2d.Point, size float32) {
		ctx.DrawCircle(at, size/2, paintengine2d.Fill(in.disc))
		ctx.DrawCircle(at, size/2, paintengine2d.StrokePaint(in.discEdge, s.dip(1.5)))
		sz := size * 0.56
		drawPict(ctx, lk, p, paintengine2d.XYWH(at.X-sz/2, at.Y-sz/2, sz, sz), in.text)
	}

	// End to end, from you to your friend, over it all.
	spanY := b.Min.Y + g.spanY
	foot := y1 - r - s.dip(3)
	ctx.DrawLine(paintengine2d.Pt(x(0), foot), paintengine2d.Pt(x(0), spanY), pen(accent, true))
	ctx.DrawLine(paintengine2d.Pt(x(0), spanY), paintengine2d.Pt(x(3), spanY), pen(accent, true))
	ctx.DrawLine(paintengine2d.Pt(x(3), spanY), paintengine2d.Pt(x(3), foot), pen(accent, true))
	head(paintengine2d.Pt(x(3), foot), 0, 1, accent)

	// The three hops.
	for i := 0; i < 3; i++ {
		x0, x1 := x(float32(i))+r+s.dip(4), x(float32(i+1))-r-s.dip(4)
		ctx.DrawLine(paintengine2d.Pt(x0, y1), paintengine2d.Pt(x1, y1), pen(in.good, false))
		head(paintengine2d.Pt(x1, y1), 1, 0, in.good)
	}

	// DNS, under the servers, which both ask.
	dnsY := b.Min.Y + g.dnsY
	dx := x(1.5)
	feet := b.Min.Y + g.labelsY + float32(max(len(g.labels[1]), len(g.labels[2])))*fh + s.dip(4)
	ctx.DrawLine(paintengine2d.Pt(dx-g.dnd*0.4, dnsY-g.dnd*0.3), paintengine2d.Pt(x(1), feet), pen(in.muted, true))
	ctx.DrawLine(paintengine2d.Pt(dx+g.dnd*0.4, dnsY-g.dnd*0.3), paintengine2d.Pt(x(2), feet), pen(in.muted, true))
	node(pictServer, paintengine2d.Pt(dx, dnsY), g.dnd)
	for _, l := range g.dnsLabel {
		font.Draw(ctx, l, paintengine2d.Pt(dx-font.Advance(l)/2, dnsY+g.dnd/2+s.dip(4)), in.text)
	}

	// The people and the servers.
	for i, p := range []pict{pictPerson, pictServer, pictServer, pictPerson} {
		node(p, paintengine2d.Pt(x(float32(i)), y1), g.d)
		y := b.Min.Y + g.labelsY
		for _, l := range g.labels[i] {
			font.Draw(ctx, l, paintengine2d.Pt(x(float32(i))-font.Advance(l)/2, y), in.text)
			y += fh
		}
	}

	// The standards, and the hops' numbers, over everything.
	for _, c := range g.chips {
		drawChip(ctx, lk, c.name, c.r.Translate(b.Min))
	}
	for _, m := range s.marks(g, b) {
		drawNumber(ctx, lk, m.n, paintengine2d.Pt(m.x, m.y), s.dip(markSize))
	}
}
