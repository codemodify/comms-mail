package mailui

import (
	"strconv"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Educate: how an email gets from you to the recipient,
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
		says: []string{"If you asked, comms-mail signs and encrypts the message first. It then hands it to your server over TLS, signed in with your password or OAuth."},
		wire: []string{"SMTP", "TLS", "OAuth"}},
	{title: "From your mail server to theirs",
		says: []string{"Your server signs it with DKIM, finds the recipient's server in DNS (MX) and delivers over STARTTLS. Their server checks SPF, DKIM and DMARC and records the result in the message."},
		wire: []string{"SMTP", "STARTTLS"}, asks: []string{"MX", "SPF", "DKIM", "DMARC"}},
	{title: "From their mail server to the recipient",
		says: []string{"The recipient's mail app fetches it over TLS with IMAP or POP3, shows the server's verdict, verifies your signature and decrypts."},
		wire: []string{"IMAP", "POP3", "TLS"}},
}

// lookAlike is the picture's last step: mail from a look-alike domain,
// which every check passes.
var lookAlike = hop{title: "A look-alike domain",
	says: []string{"Your supplier is acme.com. An attacker registers acrne.com and asks you to pay the next invoice into a new account. SPF, DKIM and DMARC all pass: the domain really is theirs. The checks prove which domain sent the mail, not that it is the one you meant. comms-mail flags domains that resemble ones you write to."},
	also: []string{"SPF", "DKIM", "DMARC"}}

// fakeDomain is the look-alike's domain, as the picture shows it.
const fakeDomain = "acrne.com"

// standard is one standard, or two that do one job: what it does and how.
type standard struct {
	names []string
	about []string
}

var standards = []standard{
	{[]string{"SMTP"}, []string{"Carries mail from your app to your server, then server to server. On its own it verifies nothing: any From address is accepted."}},
	{[]string{"TLS", "STARTTLS"}, []string{"Encrypts the connection and proves the server's identity with a certificate. STARTTLS upgrades a plain connection; MTA-STS and DANE make the upgrade mandatory. Every server still sees the message itself."}},
	{[]string{"OAuth"}, []string{"Signs comms-mail in with a revocable token from your provider, so your password never reaches it."}},
	{[]string{"DNS", "MX"}, []string{"DNS publishes a domain's records. MX names the server that accepts its mail; the SPF, DKIM and DMARC records live there too."}},
	{[]string{"SPF"}, []string{"Lists the servers allowed to send for a domain. It checks the bounce address, not the visible From, and fails on forwarding."}},
	{[]string{"DKIM"}, []string{"A signature by the sending domain, verified with the public key in its DNS. It proves the domain, and that the message was not altered."}},
	{[]string{"DMARC"}, []string{"Requires SPF or DKIM to pass for the visible From domain, and tells receivers to accept, quarantine or reject what fails. The result is written to Authentication-Results, which comms-mail reads and warns on."}},
	{[]string{"IMAP", "POP3"}, []string{"Fetch mail from your server. IMAP keeps it there, in sync across devices; POP3 downloads it to one."}},
	{[]string{"OpenPGP", "S/MIME"}, []string{"Sign and encrypt end to end: only the recipient can read the message, and no server can alter it unnoticed. S/MIME trusts certificates issued by an authority, OpenPGP keys you verify yourself; they do not interoperate. Sender, recipients and time stay visible; comms-mail also hides the subject."}},
}

// endToEnd are the standards over the whole way, from you to the
// recipient.
var endToEnd = []string{"OpenPGP", "S/MIME"}

// lastWord is said under the standards.
const lastWord = "Remote images tell the sender when you opened a message; comms-mail blocks them until you allow them."

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
	col := widgets.NewColumn(widgets.NewTitle("What happens")).WithGap(14)
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
	return newEducatePage(newHopsScene(), col)
}

// educatePage keeps the picture in view while the words under it scroll,
// so each step can be read with the picture beside it — when the page is
// tall enough to leave the words room (pinMin); when not, the picture
// scrolls with them.
type educatePage struct {
	widget.Base
	scene  *hopsScene
	words  widget.Component
	rule   *widgets.Separator
	body   *widgets.FlexBox // what scrolls: the words, and the picture when not pinned
	scroll *widgets.ScrollView
	pinned bool
}

// pinMin is the room the words keep under a pinned picture: some lines
// of them.
const pinMin = 160

func newEducatePage(scene *hopsScene, words widget.Component) *educatePage {
	p := &educatePage{scene: scene, words: words, rule: widgets.NewSeparator()}
	p.Init(p)
	p.body = widgets.NewColumn(scene, words).WithGap(14)
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
	sceneH := p.scene.Measure(layout.Constraints{MaxW: inner, MaxH: -1}).Y
	ruleH := p.rule.Measure(layout.Constraints{MaxW: r.Dx(), MaxH: -1}).Y
	pin := r.Dy()-(pad+sceneH+pad+ruleH) >= style.Dip(p.Look(), pinMin)
	if pin != p.pinned {
		p.pinned = pin
		p.body.ClearChildren()
		if pin {
			p.Remove(p.scroll)
			p.Add(p.scene)
			p.Add(p.rule)
			p.Add(p.scroll)
		} else {
			p.Remove(p.scene)
			p.Remove(p.rule)
			p.body.Add(p.scene)
		}
		p.body.Add(p.words)
		p.scroll.ScrollTo(0)
	}
	if !pin {
		p.scroll.Arrange(r)
		return
	}
	p.scene.Arrange(paintengine2d.XYWH(r.Min.X+pad, r.Min.Y+pad, inner, sceneH))
	top := r.Min.Y + pad + sceneH + pad
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

// hopsScene is the picture: you, your mail server, theirs and the recipient,
// the three hops between them numbered, each with what it goes over drawn
// on it; OpenPGP and S/MIME over all of it, end to end; and under it, a
// attacker's look-alike coming into your mail server, and DNS, which the
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
	rowGap    = 28 // between the names and the row under them: room for 4
)

var sceneNames = [4]string{"You", "Your mail server", "Their mail server", "Recipient"}

// sceneLayout is where everything goes at width w, from the picture's top
// left. The row under the names has the attacker at x(0.5), between you
// and your server, and DNS at x(2), under theirs: their words and DNS's
// tags beside them where they fit (beside), under them where not.
type sceneLayout struct {
	slot, d, dnd             float32
	spanY, y1, labelsY, rowY float32
	labels                   [4][]string
	stranger, fake, dnsLabel []string
	words                    []placedText // the stranger's, and DNS's
	beside                   bool
	chips                    []placedChip // on the span, over the hops, by DNS
	h                        float32
}

// placedText is a line of the picture where it is drawn, from its top
// left; bad is the look-alike's.
type placedText struct {
	text string
	at   paintengine2d.Point
	bad  bool
}

// x is the centre of column i: 0 you, 1 your server, 2 theirs, 3 the
// recipient.
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

	// Over everything: end to end, from you to the recipient.
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
	g.stranger = wrapWords(font, "Attacker", 1.5*g.slot-s.dip(6), 2)
	g.fake = wrapWords(font, fakeDomain, 1.5*g.slot-s.dip(6), 1)
	g.dnsLabel = wrapWords(font, "DNS", g.slot, 1)
	strangerLines := append(append([]string(nil), g.stranger...), g.fake...)
	var wordsW float32
	for _, l := range strangerLines {
		wordsW = max(wordsW, font.Advance(l))
	}
	_, ch := chipSize(lk, "X")
	// Beside: the stranger's words to its right, clear of DNS's disc and
	// name; DNS's tags to its right, in two lines at most.
	wordsX := g.x(0.5) + g.dnd/2 + s.dip(6)
	tagsX := g.x(2) + g.dnd/2 + s.dip(8)
	dnsLeft := g.x(2) - max(g.dnd/2, font.Advance("DNS")/2)
	lines, _ := chipLines(lk, asked(), w-tagsX-s.dip(3))
	g.beside = wordsX+wordsW+s.dip(8) <= dnsLeft && len(lines) <= 2 && len(g.stranger) == 1
	if g.beside {
		y := g.rowY - float32(len(strangerLines))*fh/2
		for i, l := range strangerLines {
			g.words = append(g.words, placedText{l, paintengine2d.Pt(wordsX, y), i >= len(g.stranger)})
			y += fh
		}
		dnsY := g.rowY + g.dnd/2 + s.dip(3)
		for _, l := range g.dnsLabel {
			g.words = append(g.words, placedText{l, paintengine2d.Pt(g.x(2)-font.Advance(l)/2, dnsY), false})
		}
		tagsH := float32(len(lines))*(ch+gap) - gap
		c, _ := placeChips(lk, asked(), tagsX, g.rowY-tagsH/2, w-tagsX-s.dip(3), true)
		g.chips = append(g.chips, c...)
		g.h = max(dnsY+fh, g.rowY+tagsH/2, y) + s.dip(8)
		return g
	}
	// Under: the stranger's words as wide as they need, up to x(1.25),
	// and DNS's tags in what is left.
	under := g.rowY + g.dnd/2 + s.dip(4)
	y := under
	for i, l := range strangerLines {
		g.words = append(g.words, placedText{l, paintengine2d.Pt(g.x(0.5)-font.Advance(l)/2, y), i >= len(g.stranger)})
		y += fh
	}
	for _, l := range g.dnsLabel {
		g.words = append(g.words, placedText{l, paintengine2d.Pt(g.x(2)-font.Advance(l)/2, under), false})
	}
	room := min(g.x(2)-(g.x(0.5)+wordsW/2)-s.dip(8), w-g.x(2)-s.dip(3))
	c, tagsH := placeChips(lk, asked(), g.x(2), under+fh+gap, 2*room, false)
	g.chips = append(g.chips, c...)
	g.h = max(y, under+fh+gap+tagsH) + s.dip(8)
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

// dnsLines are DNS's dashed lines, up to under each server's name.
func (s *hopsScene) dnsLines(g sceneLayout) [][2]paintengine2d.Point {
	feet := g.labelsFoot(s.Look().Font().Height()) + s.dip(4)
	return [][2]paintengine2d.Point{
		{paintengine2d.Pt(g.x(2), g.rowY-g.dnd/2-s.dip(3)), paintengine2d.Pt(g.x(2), feet)},
		{paintengine2d.Pt(g.x(2)-g.dnd*0.45, g.rowY-g.dnd*0.25), paintengine2d.Pt(g.x(1), feet)},
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

	// End to end, from you to the recipient, over it all.
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
	for _, l := range s.dnsLines(g) {
		ctx.DrawLine(at(l[0].X, l[0].Y), at(l[1].X, l[1].Y), pen(in.muted, true))
	}
	node(pictServer, at(x(2), g.rowY), g.dnd, in.text, in.discEdge)

	// The stranger, and the look-alike coming into your server.
	fp := s.fakePath(g)
	for i := 1; i < len(fp); i++ {
		ctx.DrawLine(at(fp[i-1].X, fp[i-1].Y), at(fp[i].X, fp[i].Y), pen(in.bad, false))
	}
	end, from := fp[len(fp)-1], fp[len(fp)-2]
	d := end.Sub(from).Normalize()
	head(at(end.X, end.Y), d.X, d.Y, in.bad)
	node(pictPerson, at(x(0.5), g.rowY), g.dnd, in.bad, in.bad)
	for _, t := range g.words {
		ink := in.text
		if t.bad {
			ink = in.bad
		}
		font.Draw(ctx, t.text, at(t.at.X, t.at.Y), ink)
	}

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
