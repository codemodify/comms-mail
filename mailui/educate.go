package mailui

import (
	"strconv"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Educate: what keeps email safe and what does not,
// for someone who has never thought about it — one picture of an email's
// journey with everything in it, numbered, and a line for each number.

// lessons are the picture's numbers, in order, each said in one line.
var lessons = []string{
	"Email is like a postcard: your mail provider and the other person's can read it on the way.",
	"The road between is locked (TLS): nobody in between — on café Wi-Fi, at your internet company — can read or change it.",
	"Your password and your keys are kept locked away: choose where in Security › Passwords and Security › Keys.",
	"A signature is a seal: it proves the mail is from you and that nothing in it was changed.",
	"Encryption locks it in a box only the other person can open — not even the providers. OpenPGP and S/MIME are two ways to do it; comms-mail does both.",
	"Anyone can write any From. Your provider checks who really sent it (SPF, DKIM, DMARC), and comms-mail warns you when it could not confirm.",
	"Look-alikes: paypa1.com is not paypal.com. Check the address letter by letter.",
	"Pictures from the internet tell the sender you opened the mail, and when: comms-mail asks before loading them.",
}

// lastWord is said under the numbers.
const lastWord = "When in doubt, do not click: go to the website yourself, or ask the person another way."

// educateSection is the Educate page: the picture, then a line for each
// number in it.
func educateSection() widget.Component {
	col := widgets.NewColumn(newEducateScene()).WithGap(10)
	for i, l := range lessons {
		row := widgets.NewRow(newNumberMark(i + 1)).WithGap(8).WithAlign(layout.AlignStart)
		row.AddFlex(wrapLabel(l), 1)
		col.Add(row)
	}
	last := wrapLabel(lastWord)
	last.Tone = widgets.ToneMuted
	col.Add(last)
	return widgets.NewScrollView(widgets.NewPad(4, col))
}

// ---- the numbers ----

// numberMark is a number in a disc of the look's accent: the same in the
// picture and beside its line.
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
	return c.Constrain(paintengine2d.Pt(s, max(s, m.Look().Font().Height())))
}

func (m *numberMark) Arrange(r paintengine2d.Rect) { m.SetBounds(r) }

func (m *numberMark) Paint(ctx *paintengine2d.Context) {
	b := m.LocalBounds()
	s := style.Dip(m.Look(), markSize)
	// Level with the first line of the text beside it.
	drawNumber(ctx, m.Look(), m.n, paintengine2d.Pt(b.Min.X+s/2, b.Min.Y+m.Look().Font().Height()/2), s)
}

// drawNumber draws n in a disc of size s centred at c.
func drawNumber(ctx *paintengine2d.Context, lk style.LookAndFeel, n int, c paintengine2d.Point, s float32) {
	p := lk.Palette()
	ctx.DrawCircle(c, s/2, paintengine2d.Fill(p.Accent))
	f := lk.BoldFont()
	t := strconv.Itoa(n)
	f.Draw(ctx, t, paintengine2d.Pt(c.X-f.Advance(t)/2, c.Y-f.Height()/2), p.TextOnAccent)
}

// ---- the picture ----

// educateScene is an email's journey and what threatens it, in one
// picture: you, your provider, theirs and them on a locked road, a sealed
// and locked letter over it; a stranger's mail coming in, checked, from a
// look-alike address; a picture that tells the stranger you looked.
type educateScene struct{ widget.Base }

func newEducateScene() *educateScene {
	s := &educateScene{}
	s.Init(s)
	return s
}

// The picture's lengths, in 1x design pixels.
const (
	sceneNode  = 52 // a person's or a place's disc, at most
	sceneBadge = 20
	sceneDisc  = 22 // a picture on a road
	sceneMin   = 300
)

var sceneNames = [4]string{"You", "Your provider", "Their provider", "The other person"}

// sceneLayout is where everything goes at width w.
type sceneLayout struct {
	slot, d, y1, y2, labelsY, height float32
	labels                           [4][]string
	stranger, picture                []string
}

// sceneMark is a number in the picture, where it is drawn.
type sceneMark struct {
	n    int
	x, y float32
}

func (s *educateScene) dip(v float32) float32 { return style.Dip(s.Look(), v) }

func (s *educateScene) MinWidth() float32 { return s.dip(sceneMin) }

func (s *educateScene) Measure(c layout.Constraints) paintengine2d.Point {
	w := s.dip(560)
	if c.HasMaxW() {
		w = c.MaxW
	}
	return c.Constrain(paintengine2d.Pt(w, s.layoutAt(w).height))
}

func (s *educateScene) Arrange(r paintengine2d.Rect) { s.SetBounds(r) }

func (s *educateScene) layoutAt(w float32) sceneLayout {
	font := s.Look().Font()
	fh := font.Height()
	var g sceneLayout
	g.slot = w / 4
	g.d = min(s.dip(sceneNode), g.slot*0.48)
	lines := 0
	for i, name := range sceneNames {
		g.labels[i] = wrapWords(font, name, g.slot-s.dip(4), 3)
		lines = max(lines, len(g.labels[i]))
	}
	// Over the road: the letter between its seal and padlock, and room
	// for the numbers over the marks at the discs' corners.
	g.y1 = s.dip(8) + s.dip(30) + s.dip(38) + g.d/2
	g.labelsY = g.y1 + g.d/2 + s.dip(6)
	under := g.labelsY + float32(lines)*fh
	// Under it: the stranger, under your provider, and the picture, under
	// you, with room for the stranger's address beside the road up.
	g.y2 = under + s.dip(48) + g.d/2
	g.stranger = wrapWords(font, "A stranger", g.slot-s.dip(4), 2)
	g.picture = wrapWords(font, "A picture", g.slot-s.dip(4), 2)
	g.height = g.y2 + g.d/2 + s.dip(6) + float32(max(len(g.stranger), len(g.picture)))*fh + s.dip(8)
	return g
}

// corner is where a mark sits on node i's disc: its top right, or left.
func (s *educateScene) corner(g sceneLayout, b paintengine2d.Rect, i float32, right bool) paintengine2d.Point {
	dx := g.d / 2 * 0.62
	if !right {
		dx = -dx
	}
	return paintengine2d.Pt(b.Min.X+g.slot*(i+0.5)+dx, b.Min.Y+g.y1-g.d/2*0.62)
}

// letter is where the letter is, and its seal and padlock either side.
func (s *educateScene) letter(g sceneLayout, b paintengine2d.Rect) (at, seal, lock paintengine2d.Point, size float32) {
	size = s.dip(30)
	at = paintengine2d.Pt(b.Min.X+g.slot*2, b.Min.Y+s.dip(8)+size/2)
	gap := size*0.5 + s.dip(sceneBadge)/2 + s.dip(4)
	return at, paintengine2d.Pt(at.X-gap, at.Y), paintengine2d.Pt(at.X+gap, at.Y), size
}

// marks are where the numbers go: over the marks they are about, or
// beside what they are about.
func (s *educateScene) marks(g sceneLayout, b paintengine2d.Rect) []sceneMark {
	x := func(i float32) float32 { return b.Min.X + g.slot*(i+0.5) }
	r := g.d / 2
	m := s.dip(markSize)
	over := func(p paintengine2d.Point) (float32, float32) {
		return p.X, p.Y - s.dip(sceneBadge)/2 - m/2 - s.dip(3)
	}
	_, seal, lock, _ := s.letter(g, b)
	beside := s.dip(sceneBadge)/2 + m/2 + s.dip(3)
	under := b.Min.Y + g.labelsY + float32(len(g.labels[1]))*s.Look().Font().Height()
	mid := (under + b.Min.Y + g.y2 - r) / 2
	x1, y1 := over(s.corner(g, b, 1, true))
	x3, y3 := over(s.corner(g, b, 0, true))
	x6, y6 := over(s.corner(g, b, 1, false))
	return []sceneMark{
		{1, x1, y1},
		{2, x(0) + g.slot/2, b.Min.Y + g.y1 + s.dip(sceneDisc)/2 + m/2 + s.dip(3)},
		{3, x3, y3},
		{4, seal.X - beside, seal.Y},
		{5, lock.X + beside, lock.Y},
		{6, x6, y6},
		{7, x(1) - m/2 - s.dip(8), mid},
		{8, x(0) + r*0.7 + m/2, b.Min.Y + g.y2 - r*0.7},
	}
}

func (s *educateScene) Paint(ctx *paintengine2d.Context) {
	lk := s.Look()
	b := s.LocalBounds()
	g := s.layoutAt(b.Dx())
	in := inksOf(lk)
	font := lk.Font()
	fh := font.Height()
	x := func(i float32) float32 { return b.Min.X + g.slot*(i+0.5) }
	y1, y2 := b.Min.Y+g.y1, b.Min.Y+g.y2
	r := g.d / 2
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
	onDisc := func(p pict, at paintengine2d.Point, size float32, ink paintengine2d.Color) {
		ctx.DrawCircle(at, size/2+s.dip(2), paintengine2d.Fill(in.disc))
		ctx.DrawCircle(at, size/2+s.dip(2), paintengine2d.StrokePaint(ink, s.dip(1.25)))
		inner := size * 0.7
		drawPict(ctx, lk, p, paintengine2d.XYWH(at.X-inner/2, at.Y-inner/2, inner, inner), ink)
	}
	node := func(p pict, at paintengine2d.Point, t tone) {
		edge := in.discEdge
		if t != tonePlain {
			edge = in.of(t)
		}
		ctx.DrawCircle(at, r, paintengine2d.Fill(in.disc))
		ctx.DrawCircle(at, r, paintengine2d.StrokePaint(edge, s.dip(1.5)))
		sz := g.d * 0.56
		drawPict(ctx, lk, p, paintengine2d.XYWH(at.X-sz/2, at.Y-sz/2, sz, sz), in.of(t))
	}
	label := func(lines []string, cx, y float32, c paintengine2d.Color) {
		for _, l := range lines {
			font.Draw(ctx, l, paintengine2d.Pt(cx-font.Advance(l)/2, y), c)
			y += fh
		}
	}
	badge := func(p pict, at paintengine2d.Point, t tone) { onDisc(p, at, s.dip(sceneBadge)*0.9, in.of(t)) }

	// The road: you, your provider, theirs, them — locked between each.
	for i := 0; i < 3; i++ {
		x0, x1 := x(float32(i))+r+s.dip(4), x(float32(i+1))-r-s.dip(4)
		ctx.DrawLine(paintengine2d.Pt(x0, y1), paintengine2d.Pt(x1, y1), pen(in.good, false))
		head(paintengine2d.Pt(x1, y1), 1, 0, in.good)
		onDisc(pictLock, paintengine2d.Pt((x0+x1)/2, y1), s.dip(sceneDisc)*0.8, in.good)
	}
	// The letter over the road, between its seal and its padlock.
	at, seal, lock, ls := s.letter(g, b)
	drawPict(ctx, lk, pictLetter, paintengine2d.XYWH(at.X-ls/2, at.Y-ls/2, ls, ls), in.text)
	badge(pictSeal, seal, toneGood)
	badge(pictLock, lock, toneGood)

	// The stranger's mail up to your provider, from a look-alike address;
	// a picture under you that tells the stranger you looked.
	under := b.Min.Y + g.labelsY + float32(len(g.labels[1]))*fh
	sx := x(1)
	ctx.DrawLine(paintengine2d.Pt(sx, y2-r-s.dip(4)), paintengine2d.Pt(sx, under+s.dip(4)), pen(in.bad, false))
	head(paintengine2d.Pt(sx, under+s.dip(4)), 0, -1, in.bad)
	mid := (under + y2 - r) / 2
	font.Draw(ctx, "From: paypa1.com", paintengine2d.Pt(sx+s.dip(10), mid-fh/2), in.bad)

	px := x(0)
	youUnder := b.Min.Y + g.labelsY + float32(len(g.labels[0]))*fh
	ctx.DrawLine(paintengine2d.Pt(px, youUnder+s.dip(4)), paintengine2d.Pt(px, y2-r*0.8-s.dip(4)), pen(in.bad, true))
	ctx.DrawLine(paintengine2d.Pt(px+r*0.8+s.dip(4), y2), paintengine2d.Pt(sx-r-s.dip(4), y2), pen(in.bad, true))
	head(paintengine2d.Pt(sx-r-s.dip(4), y2), 1, 0, in.bad)

	// The people and places.
	node(pictPerson, paintengine2d.Pt(x(0), y1), tonePlain)
	node(pictServer, paintengine2d.Pt(x(1), y1), tonePlain)
	node(pictServer, paintengine2d.Pt(x(2), y1), tonePlain)
	node(pictPerson, paintengine2d.Pt(x(3), y1), tonePlain)
	for i := range sceneNames {
		label(g.labels[i], x(float32(i)), b.Min.Y+g.labelsY, in.text)
	}
	node(pictPerson, paintengine2d.Pt(sx, y2), toneBad)
	label(g.stranger, sx, y2+r+s.dip(6), in.bad)
	pr := r * 0.8
	ctx.DrawCircle(paintengine2d.Pt(px, y2), pr, paintengine2d.Fill(in.disc))
	ctx.DrawCircle(paintengine2d.Pt(px, y2), pr, paintengine2d.StrokePaint(in.bad, s.dip(1.5)))
	ps := pr * 1.1
	drawPict(ctx, lk, pictPicture, paintengine2d.XYWH(px-ps/2, y2-ps/2, ps, ps), in.bad)
	label(g.picture, px, y2+r+s.dip(6), in.text)

	// The marks at the corners: your key, the providers' eyes, your
	// provider's check.
	badge(pictKey, s.corner(g, b, 0, true), toneGood)
	badge(pictEye, s.corner(g, b, 1, true), toneBad)
	badge(pictEye, s.corner(g, b, 2, true), toneBad)
	badge(pictCheck, s.corner(g, b, 1, false), toneGood)

	// The numbers, last, over everything.
	for _, m := range s.marks(g, b) {
		drawNumber(ctx, lk, m.n, paintengine2d.Pt(m.x, m.y), s.dip(markSize))
	}
}
