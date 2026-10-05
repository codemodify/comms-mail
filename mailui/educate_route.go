package mailui

import (
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// The picture on Settings › Security › Educate: a message's route, step by
// step, round a U — you and the recipient on the left, your server and
// theirs on the right:
//
//	1 You ─────2────▶ 3 Your server
//	  ┊ OpenPGP   MX, SPF… (DNS)  │4 STARTTLS, MTA-STS, DANE
//	  ▼ S/MIME                    ▼
//	7 Recipient ◀──6── 5 Their server ◀──8── Attacker, acrne.com
//
// The mail goes round through both servers; OpenPGP and S/MIME join you
// and the recipient directly, end to end, past every server. DNS, in the
// middle, is asked by both servers. Each step's standards are drawn by it
// as tags, and its number is on its arrow or by its name.

type routeScene struct{ widget.Base }

func newRouteScene() *routeScene {
	s := &routeScene{}
	s.Init(s)
	return s
}

const routeMin = 300

// The picture's tags, by where they are drawn.
var (
	e2eTags = []string{"OpenPGP", "S/MIME"}
	hopTags = [3][]string{{"SMTP", "TLS", "OAuth"}, {"STARTTLS", "MTA-STS", "DANE"}, {"IMAP", "POP3", "TLS"}}
	dnsTags = []string{"MX", "SPF", "DKIM", "DMARC"}
)

// routeNames are the four, with the number of the step each takes:
// you, your server, their server, the recipient.
var routeNames = [4]struct {
	name string
	step int
}{{"You", 1}, {"Your server", 3}, {"Their server", 5}, {"Recipient", 7}}

// routeDisc is a person or a server; text, when set, is written in it in
// place of a picture.
type routeDisc struct {
	c    paintengine2d.Point
	d    float32
	pict pict
	bad  bool
	text string
}

// The inks of a line.
const (
	inkGood = iota
	inkAccent
	inkMuted
	inkBad
)

// routeLine is a line of the picture, an arrow when head; under names the
// tags it runs behind.
type routeLine struct {
	a, b   paintengine2d.Point
	ink    int
	dashed bool
	head   bool
	under  string
}

// placedText is a line of words where it is drawn, from its top left;
// bad is the attacker's.
type placedText struct {
	text string
	at   paintengine2d.Point
	bad  bool
}

// sceneMark is a number where it is drawn.
type sceneMark struct {
	n    int
	x, y float32
}

// routeLayout is everything the picture draws at a width, from its top
// left.
type routeLayout struct {
	L, R, y1, y2 float32 // the columns, and the rows' centres
	discs        []routeDisc
	lines        []routeLine
	texts        []placedText
	marks        []sceneMark
	chips        []placedChip
	h            float32
}

func (s *routeScene) dip(v float32) float32 { return style.Dip(s.Look(), v) }

func (s *routeScene) MinWidth() float32 { return s.dip(routeMin) }

func (s *routeScene) Measure(c layout.Constraints) paintengine2d.Point {
	w := s.dip(560)
	if c.HasMaxW() {
		w = c.MaxW
	}
	return c.Constrain(paintengine2d.Pt(w, s.layoutAt(w).h))
}

func (s *routeScene) Arrange(r paintengine2d.Rect) { s.SetBounds(r) }

func (s *routeScene) layoutAt(w float32) routeLayout {
	lk := s.Look()
	font := lk.Font()
	fh := font.Height()
	m, mk, gap := s.dip(4), s.dip(markSize), s.dip(chipGap)
	var g routeLayout
	d := min(max(w*0.11, s.dip(34)), s.dip(48))
	r := d / 2
	dnd := d * 0.72
	widest := func(names []string) float32 {
		var out float32
		for _, n := range names {
			cw, _ := chipSize(lk, n)
			out = max(out, cw)
		}
		return out
	}
	named := func(i int) float32 { return mk + s.dip(4) + font.Advance(routeNames[i].name) }
	e2eW := widest(e2eTags)
	g.L = m + max(e2eW/2, r, named(0)/2, named(3)/2) + s.dip(2)
	g.R = w - m - min(max(w*0.32, s.dip(112)), s.dip(170))
	L, R := g.L, g.R
	group := func(cs []placedChip, name string) {
		for i := range cs {
			cs[i].group = name
		}
		g.chips = append(g.chips, cs...)
	}
	lineH := max(fh, mk)
	// name places node i's name, its number first, centred on cx from top,
	// and says where it ends.
	name := func(i int, cx, top float32) (foot, half float32) {
		maxW := 2 * min(cx-m, w-m-cx)
		lines := wrapWords(font, routeNames[i].name, maxW-mk-s.dip(4), 2)
		y := top
		for k, l := range lines {
			if k == 0 {
				lw := mk + s.dip(4) + font.Advance(l)
				x := cx - lw/2
				g.marks = append(g.marks, sceneMark{routeNames[i].step, x + mk/2, y + lineH/2})
				g.texts = append(g.texts, placedText{text: l, at: paintengine2d.Pt(x+mk+s.dip(4), y+(lineH-fh)/2)})
				half = max(half, lw/2)
				y += lineH
				continue
			}
			g.texts = append(g.texts, placedText{text: l, at: paintengine2d.Pt(cx-font.Advance(l)/2, y)})
			half = max(half, font.Advance(l)/2)
			y += fh
		}
		return y, half
	}

	// Step 2's tags over the top row: over the discs too, which are under
	// them.
	c2, h2 := placeChips(lk, hopTags[0], (L+R)/2, m, R-L+d-s.dip(8), false)
	group(c2, "2")
	g.y1 = m + h2 + s.dip(6) + r
	topFoot := max(firstOf(name(0, L, g.y1+r+s.dip(6))), firstOf(name(1, R, g.y1+r+s.dip(6))))

	// The middle: OpenPGP and S/MIME on the line from you to the
	// recipient; DNS between the servers; step 4's tags right of its
	// arrow, the attacker's words under them.
	mid := topFoot + s.dip(8)
	_, e2eH := placeChips(lk, e2eTags, L, 0, e2eW, false)
	// DNS: its name in its disc, what the servers look up there beside it
	// (the disc nearer the servers) where that takes two lines at most,
	// else under it.
	dnsD := max(dnd, font.Advance("DNS")+s.dip(10))
	dnsLeft, dnsRight := L+e2eW/2+s.dip(10), R-mk/2-s.dip(8)
	besideW := dnsRight - dnsLeft - dnsD - s.dip(8)
	lines, widths := chipLines(lk, dnsTags, besideW)
	beside := len(lines) <= 2 && widest(dnsTags) <= besideW
	xd, tagsX, tagsW := (dnsLeft+dnsRight)/2, (dnsLeft+dnsRight)/2, dnsRight-dnsLeft
	if beside {
		var tw float32
		for _, lw := range widths {
			tw = max(tw, lw)
		}
		x0 := (dnsLeft + dnsRight - (tw + s.dip(8) + dnsD)) / 2
		xd, tagsX, tagsW = x0+tw+s.dip(8)+dnsD/2, x0+tw/2, tw
	}
	_, dnsH := placeChips(lk, dnsTags, tagsX, 0, tagsW, false)
	dnsGroup := dnsD + gap + dnsH
	if beside {
		dnsGroup = max(dnsD, dnsH)
	}
	x4 := R + mk/2 + s.dip(6)
	c4, h4 := placeChips(lk, hopTags[1], x4, mid, w-m-x4, true)
	group(c4, "4")
	attacker := []string{"Attacker", fakeDomain}
	var aw float32
	for _, l := range attacker {
		aw = max(aw, font.Advance(l))
	}
	xa := w - m - max(dnd/2, aw/2)
	// The attacker's words under it, beside their server's name, where
	// they fit; over it, under step 4's tags, where not.
	theirW := mk + s.dip(4) + font.Advance(routeNames[2].name)
	under := xa-aw/2-(R+min(theirW/2, w-m-R))-s.dip(8) >= 0
	need := mid + h4 + s.dip(4) + dnd/2
	if !under {
		need = mid + h4 + s.dip(10) + 2*fh + s.dip(4) + dnd/2
	}
	g.y2 = max(mid+max(e2eH+s.dip(8), dnsGroup, mk+s.dip(16))+s.dip(8)+r, need)
	y2 := g.y2
	top4, foot4 := topFoot+s.dip(4), y2-r-s.dip(4) // the vertical arrows

	cE, _ := placeChips(lk, e2eTags, L, (top4+foot4-e2eH)/2, e2eW, false)
	group(cE, "e2e")
	groupTop := mid + (y2-r-s.dip(8)-mid-dnsGroup)/2
	yd, tagsTop := groupTop+dnsD/2, groupTop+dnsD+gap
	if beside {
		yd, tagsTop = groupTop+dnsGroup/2, groupTop+(dnsGroup-dnsH)/2
	}
	cD, _ := placeChips(lk, dnsTags, tagsX, tagsTop, tagsW, false)
	group(cD, "dns")
	ay := y2 - dnd/2 - s.dip(4) - 2*fh
	if under {
		ay = y2 + dnd/2 + s.dip(4)
	}
	for i, l := range attacker {
		g.texts = append(g.texts, placedText{text: l, at: paintengine2d.Pt(xa-font.Advance(l)/2, ay+float32(i)*fh), bad: i > 0})
	}

	// The bottom row: the recipient, their server, the attacker.
	bottom := y2 + r + s.dip(6)
	recipFoot, recipHalf := name(3, L, bottom)
	theirFoot, theirHalf := name(2, R, bottom)
	foot := max(recipFoot, theirFoot)
	// Step 6's tags under its arrow, between the names; under the names
	// where they do not fit there.
	left6, right6 := L+recipHalf+s.dip(8), R-theirHalf-s.dip(8)
	if right6-left6 >= widest(hopTags[2]) {
		c6, h6 := placeChips(lk, hopTags[2], (left6+right6)/2, y2+mk/2+s.dip(4), right6-left6, false)
		group(c6, "6")
		foot = max(foot, y2+mk/2+s.dip(4)+h6)
	} else {
		c6, h6 := placeChips(lk, hopTags[2], (L+R)/2, foot+s.dip(6), R-L+d-s.dip(8), false)
		group(c6, "6")
		foot += s.dip(6) + h6
	}
	if under {
		foot = max(foot, ay+2*fh)
	}
	g.h = foot + m

	g.discs = []routeDisc{
		{paintengine2d.Pt(L, g.y1), d, pictPerson, false, ""},
		{paintengine2d.Pt(R, g.y1), d, pictServer, false, ""},
		{paintengine2d.Pt(R, y2), d, pictServer, false, ""},
		{paintengine2d.Pt(L, y2), d, pictPerson, false, ""},
		{c: paintengine2d.Pt(xd, yd), d: dnsD, text: "DNS"},
		{paintengine2d.Pt(xa, y2), dnd, pictPerson, true, ""},
	}
	pt := paintengine2d.Pt
	off := (r + s.dip(3)) * 0.707
	doff := (dnsD/2 + s.dip(2)) * 0.707
	toTheirs := pt(xd, tagsTop+dnsH+s.dip(3))
	if beside {
		toTheirs = pt(xd+doff, yd+doff)
	}
	g.lines = []routeLine{
		{a: pt(L, top4), b: pt(L, foot4), ink: inkAccent, dashed: true, head: true, under: "e2e"},
		{a: pt(L+r+s.dip(4), g.y1), b: pt(R-r-s.dip(4), g.y1), ink: inkGood, head: true},
		{a: pt(R, top4), b: pt(R, foot4), ink: inkGood, head: true},
		{a: pt(R-r-s.dip(4), y2), b: pt(L+r+s.dip(4), y2), ink: inkGood, head: true},
		{a: pt(xa-dnd/2-s.dip(3), y2), b: pt(R+r+s.dip(4), y2), ink: inkBad, head: true},
		{a: pt(xd+doff, yd-doff), b: pt(R-mk/2-s.dip(10), topFoot+s.dip(3)), ink: inkMuted, dashed: true},
		{a: toTheirs, b: pt(R-off, y2-off), ink: inkMuted, dashed: true},
	}
	g.marks = append(g.marks,
		sceneMark{2, (L + R) / 2, g.y1},
		sceneMark{4, R, (top4 + foot4) / 2},
		sceneMark{6, (L + R) / 2, y2},
		sceneMark{8, (xa - dnd/2 + R + r) / 2, y2},
	)
	return g
}

// firstOf is a pair's first.
func firstOf(a, _ float32) float32 { return a }

func (s *routeScene) Paint(ctx *paintengine2d.Context) {
	lk := s.Look()
	b := s.LocalBounds()
	g := s.layoutAt(b.Dx())
	in := inksOf(lk)
	font := lk.Font()
	at := func(p paintengine2d.Point) paintengine2d.Point { return p.Add(b.Min) }
	inks := [...]paintengine2d.Color{inkGood: in.good, inkAccent: lk.Palette().Ink(lk.Palette().Accent), inkMuted: in.muted, inkBad: in.bad}
	for _, l := range g.lines {
		c := inks[l.ink]
		p := paintengine2d.StrokePaint(c, s.dip(1.75))
		if l.dashed {
			p.Stroke.Dash = []float32{s.dip(5), s.dip(4)}
		}
		ctx.DrawLine(at(l.a), at(l.b), p)
		if l.head {
			dir := l.b.Sub(l.a).Normalize()
			h, tip := s.dip(7), at(l.b)
			path := paintengine2d.NewPath()
			path.MoveTo(tip.X, tip.Y)
			path.LineTo(tip.X-dir.X*h-dir.Y*h*0.6, tip.Y-dir.Y*h+dir.X*h*0.6)
			path.LineTo(tip.X-dir.X*h+dir.Y*h*0.6, tip.Y-dir.Y*h-dir.X*h*0.6)
			path.Close()
			ctx.DrawPath(path, paintengine2d.Fill(c))
		}
	}
	for _, d := range g.discs {
		ink, edge := in.text, in.discEdge
		if d.bad {
			ink, edge = in.bad, in.bad
		}
		c := at(d.c)
		ctx.DrawCircle(c, d.d/2, paintengine2d.Fill(in.disc))
		ctx.DrawCircle(c, d.d/2, paintengine2d.StrokePaint(edge, s.dip(1.5)))
		if d.text != "" {
			font.Draw(ctx, d.text, paintengine2d.Pt(c.X-font.Advance(d.text)/2, c.Y-font.Height()/2), ink)
			continue
		}
		sz := d.d * 0.56
		drawPict(ctx, lk, d.pict, paintengine2d.XYWH(c.X-sz/2, c.Y-sz/2, sz, sz), ink)
	}
	for _, t := range g.texts {
		ink := in.text
		if t.bad {
			ink = in.bad
		}
		font.Draw(ctx, t.text, at(t.at), ink)
	}
	for _, c := range g.chips {
		drawChip(ctx, lk, c.name, c.r.Translate(b.Min))
	}
	for _, m := range g.marks {
		drawNumber(ctx, lk, m.n, at(paintengine2d.Pt(m.x, m.y)), s.dip(markSize))
	}
}
