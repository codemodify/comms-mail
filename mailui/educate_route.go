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
//	1 You ═══2═[✉]═══▶ 3 Your server
//	  ┊ OpenPGP|S/MIME   DNS     ║4 [✉]   SMTP, STARTTLS, MTA-STS|DANE
//	  ▼                          ▼
//	7 Recipient ◀═══6═[✉]═══ 5 Their server ◀──[✉]8── Attacker, acrne.com
//
// The message is an envelope: a padlock when encrypted, a wax seal when
// you signed it, a postmark when your domain did (DKIM). Each encrypted
// connection is a tunnel it goes through. OpenPGP and S/MIME join you and
// the recipient directly, end to end, past every server. DNS, in the
// middle, is what both servers ask. Each step's standards are drawn by it
// as tags; alternatives — either one does the job — stand across its line:
// one over the other by a line across, side by side by a line down.

// l is the message and road drawn: its envelope with a seal, a padlock,
// both or neither, and your domain's postmark with DKIM; OpenPGP and
// S/MIME from YOU to TARGET when signed or encrypted; its connections
// tunnels with TLS, plain lines without; and the standards in use, and
// none that are not.
type routeScene struct {
	widget.Base
	l lesson
}

func newRouteScene() *routeScene {
	s := &routeScene{l: firstLesson}
	s.Init(s)
	return s
}

// dnsTagsFor are what the servers look up in DNS, drawn by it: what the
// domains publish, of MX, SPF, DKIM and DMARC. dnsName is its name, and
// port, in its pill.
func dnsTagsFor(l lesson) [][]string {
	var out [][]string
	for _, r := range []struct {
		on   bool
		name string
	}{{l.mx, "MX"}, {l.spf, "SPF"}, {l.dkim, "DKIM"}, {l.dmarc, "DMARC"}} {
		if r.on {
			out = append(out, alt(r.name))
		}
	}
	return out
}

const dnsName = "DNS:UDP:53"

// routeNames are the four, with the number of the step each takes.
var routeNames = [4]struct {
	name string
	step int
}{{"YOU", 1}, {"YOUR SERVER", 3}, {"TARGET SERVER", 5}, {"TARGET", 7}}

// routeDisc is a person or a server, a disc d across; text, when set, is
// written in it in place of a picture — in a pill w wide and d tall, when
// w is set.
type routeDisc struct {
	c    paintengine2d.Point
	d    float32
	pict pict
	bad  bool
	text string
	w    float32
}

// box is where it draws.
func (d routeDisc) box() paintengine2d.Rect {
	w := max(d.w, d.d)
	return paintengine2d.XYWH(d.c.X-w/2, d.c.Y-d.d/2, w, d.d)
}

// The inks of a line.
const (
	inkGood = iota
	inkAccent
	inkMuted
	inkBad
)

// routeLine is a line of the picture from a to b, an arrow when head; a
// tunnel of that thickness when tube; under names the tags it runs
// behind.
type routeLine struct {
	a, b   paintengine2d.Point
	ink    int
	dashed bool
	head   bool
	tube   float32
	under  string
}

// body is where a tunnel draws: the rectangle round its line.
func (l routeLine) body() paintengine2d.Rect {
	h := l.tube / 2
	r := paintengine2d.Rect{Min: paintengine2d.Pt(min(l.a.X, l.b.X), min(l.a.Y, l.b.Y)), Max: paintengine2d.Pt(max(l.a.X, l.b.X), max(l.a.Y, l.b.Y))}
	if l.a.Y == l.b.Y {
		r.Min.Y, r.Max.Y = r.Min.Y-h, r.Max.Y+h
	} else {
		r.Min.X, r.Max.X = r.Min.X-h, r.Max.X+h
	}
	return r
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

// placedEnv is a message where it is drawn, on the line numbered step.
type placedEnv struct {
	c    paintengine2d.Point
	e    envelope
	step int
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
	envs         []placedEnv
	stops        []paintengine2d.Point // where the message goes no further
	h            float32
}

func (s *routeScene) dip(v float32) float32 { return style.Dip(s.Look(), v) }

// MinWidth is as narrow as the picture goes: the alternatives side by
// side across the lines down, and the tunnels across long enough for
// their number and envelope, at the smallest discs.
func (s *routeScene) MinWidth() float32 {
	// The discs, and the room right of the servers, grow with the width:
	// find the width they fit at.
	w := float32(0)
	for range 8 {
		cols := s.columns(w)
		w = cols.L + max(2*cols.r+s.dip(8)+cols.carries, cols.between) + cols.right + s.dip(4)
	}
	return w
}

// routeColumns are the picture's columns at width w: where you and the
// recipient are (L), how far right of the servers it reaches (right),
// the discs' radius, and how long a line carrying a number and an
// envelope must be. w 0 is the narrowest.
type routeColumns struct {
	L, right, r, carries float32
	between              float32 // the least from L to the servers: room for the names either side
}

func (s *routeScene) columns(w float32) routeColumns {
	lk := s.Look()
	font := lk.Font()
	m, mk := s.dip(4), s.dip(markSize)
	d := min(max(w*0.11, s.dip(34)), s.dip(48))
	r := d / 2
	dnd := d * 0.72
	eb := envBox(lk, paintengine2d.Pt(0, 0))
	thV := eb.Dx() + s.dip(8)
	named := func(i int) float32 { return mk + s.dip(4) + font.Advance(routeNames[i].name) }
	_, e2eW, _ := placeDown(lk, e2eTags, 0, 0, true)
	_, hop4W, _ := placeDown(lk, relayStep(lesson{tls: true, mtaSTS: true, dane: true}).tags, 0, 0, false)
	var aw float32
	for _, l := range []string{"Attacker", fakeDomain} {
		aw = max(aw, font.Advance(l))
	}
	carries := s.dip(3) + mk + s.dip(4) + eb.Dx() + s.dip(6) + s.dip(9)
	// Room for the names either side, and for DNS's pill between the line
	// from YOU to TARGET and tunnel 4.
	between := max(named(0)/2+named(1)/2, named(3)/2+named(2)/2, font.Advance(dnsName)+s.dip(16)+s.dip(18)+thV/2) + s.dip(12)
	return routeColumns{
		between: between,
		L:       m + max(e2eW/2, r, named(0)/2, named(3)/2) + s.dip(2),
		right:   max(hop4W+thV/2+s.dip(6), max(dnd/2, aw/2)+dnd/2+s.dip(7)+carries+r, w*0.3),
		r:       r,
		carries: carries,
	}
}

func (s *routeScene) Measure(c layout.Constraints) paintengine2d.Point {
	w := s.dip(560)
	if c.HasMaxW() {
		w = c.MaxW
	}
	return c.Constrain(paintengine2d.Pt(w, s.layoutAt(w).h))
}

func (s *routeScene) Arrange(r paintengine2d.Rect) { s.SetBounds(r) }

// groupGap is between groups of tags along a line.
const groupGap = 8

// placeAlong lays groups out along a line across (left to right, rows no
// wider than maxW centred on cx — moved in, where that would put them
// past lo or hi — from top), each group's alternatives one over the
// other; and says how tall they are.
func placeAlong(lk style.LookAndFeel, groups [][]string, cx, top, maxW, lo, hi float32) ([]placedChip, float32) {
	gap, gg := style.Dip(lk, chipGap), style.Dip(lk, groupGap)
	_, ch := chipSize(lk, "X")
	size := func(grp []string) (w, h float32) {
		return widestChip(lk, grp), float32(len(grp))*(ch+gap) - gap
	}
	var out []placedChip
	y := top
	for i := 0; i < len(groups); {
		// A row: as many groups as fit.
		j, rowW, rowH := i, float32(0), float32(0)
		for j < len(groups) {
			gw, gh := size(groups[j])
			next := rowW + gw
			if j > i {
				next += gg
			}
			if j > i && next > maxW {
				break
			}
			rowW, rowH = next, max(rowH, gh)
			j++
		}
		x := min(max(cx-rowW/2, lo), hi-rowW)
		for k := i; k < j; k++ {
			gw, gh := size(groups[k])
			gy := y + (rowH-gh)/2
			for a, n := range groups[k] {
				cw, _ := chipSize(lk, n)
				out = append(out, placedChip{name: n, alt: k, r: paintengine2d.XYWH(x+(gw-cw)/2, gy+float32(a)*(ch+gap), cw, ch)})
			}
			x += gw + gg
		}
		y += rowH + gg
		i = j
	}
	if len(out) == 0 {
		return nil, 0
	}
	return out, y - gg - top
}

// placeDown lays groups out along a line down, from top: a group under
// the one before, its alternatives side by side — from x, or centred on x
// when centred; and says how wide and tall they are.
func placeDown(lk style.LookAndFeel, groups [][]string, x, top float32, centred bool) ([]placedChip, float32, float32) {
	gap := style.Dip(lk, chipGap)
	_, ch := chipSize(lk, "X")
	var out []placedChip
	var widest float32
	y := top
	for k, grp := range groups {
		var rw float32
		for i, n := range grp {
			cw, _ := chipSize(lk, n)
			if i > 0 {
				rw += gap
			}
			rw += cw
		}
		widest = max(widest, rw)
		cx := x
		if centred {
			cx -= rw / 2
			if len(grp) == 2 {
				// The line goes between the two.
				w0, _ := chipSize(lk, grp[0])
				cx = x - w0 - gap/2
				widest = max(widest, 2*max(w0, rw-w0-gap)+gap)
			}
		}
		for _, n := range grp {
			cw, _ := chipSize(lk, n)
			out = append(out, placedChip{name: n, alt: k, r: paintengine2d.XYWH(cx, y, cw, ch)})
			cx += cw + gap
		}
		y += ch + gap
	}
	if len(out) == 0 {
		return nil, 0, 0
	}
	return out, widest, y - gap - top
}

func (s *routeScene) layoutAt(w float32) routeLayout {
	lk := s.Look()
	font := lk.Font()
	fh := font.Height()
	m, mk, gap := s.dip(4), s.dip(markSize), s.dip(chipGap)
	var g routeLayout
	d := min(max(w*0.11, s.dip(34)), s.dip(48))
	r := d / 2
	dnd := d * 0.72
	eb := envBox(lk, paintengine2d.Pt(0, 0))
	thH, thV := eb.Dy()+s.dip(8), eb.Dx()+s.dip(8) // the tunnels across, and down
	head := s.dip(9)
	l := s.l
	// What is turned off still has its place, in red: DNS's four records,
	// MTA-STS and DANE with TLS, and OpenPGP and S/MIME.
	e2e, hop2, hop6 := e2eTags, submitStep(l.tls).tags, fetchStep(l.tls).tags
	hop4 := relayStep(l).tags
	if l.tls {
		hop4 = relayStep(lesson{tls: true, mtaSTS: true, dane: true}).tags
	}
	dnsTags := one("MX", "SPF", "DKIM", "DMARC")
	endToEnd := l.signed || l.encrypted
	missing := map[string]bool{"MX": !l.mx, "SPF": !l.spf, "DKIM": !l.dkim, "DMARC": !l.dmarc,
		"MTA-STS": l.tls && !l.mtaSTS, "DANE": l.tls && !l.dane, "OpenPGP": !endToEnd, "S/MIME": !endToEnd}
	group := func(cs []placedChip, name string) {
		for i := range cs {
			cs[i].group = name
			cs[i].missing = missing[cs[i].name] && name != "2" && name != "6"
		}
		g.chips = append(g.chips, cs...)
	}
	_, e2eW, e2eH := placeDown(lk, e2e, 0, 0, true)
	attacker := []string{"Attacker", fakeDomain}
	if !l.dmarc {
		attacker = append(attacker, "or your domain") // nothing stops it forging yours
	}
	var aw float32
	for _, l := range attacker {
		aw = max(aw, font.Advance(l))
	}
	cols := s.columns(w)
	carries := cols.carries // a line with a number and an envelope on it
	g.L, g.R = cols.L, w-m-cols.right
	L, R := g.L, g.R
	lineH := max(fh, mk)
	// name places node i's name, its number first, centred on cx from top,
	// and says where it ends and how far it reaches either side.
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

	// Step 2's tags over the top row, over the discs too.
	c2, h2 := placeAlong(lk, hop2, (L+R)/2, m, w-2*m, m, w-m)
	group(c2, "2")
	g.y1 = m + h2 + s.dip(6) + max(r, thH/2)
	nameTop := g.y1 + max(r, thH/2) + s.dip(6)
	f0, _ := name(0, L, nameTop)
	f1, _ := name(1, R, nameTop)
	topFoot := max(f0, f1)

	// The middle: OpenPGP and S/MIME across the line from you to the
	// recipient; DNS between the servers; step 4's tags right of its
	// tunnel.
	mid := topFoot + s.dip(8)
	// DNS is a pill with its name and port: as wide as they are, a line
	// tall.
	dnsW, dnsT := font.Advance(dnsName)+s.dip(16), fh+s.dip(10)
	// DNS beside OpenPGP and S/MIME where there is room, else under them,
	// right of the line from you to the recipient.
	dnsLeft, dnsRight := L+e2eW/2+s.dip(10), R-thV/2-s.dip(8)
	stacked := dnsRight-dnsLeft < max(dnsW, widestChip(lk, flat(dnsTags)))
	if stacked {
		dnsLeft = L + s.dip(10)
	}
	besideW := dnsRight - dnsLeft - dnsW - s.dip(8)
	lines, widths := chipLines(lk, flat(dnsTags), besideW)
	beside := len(lines) <= 2 && widestChip(lk, flat(dnsTags)) <= besideW
	xd, tagsX, tagsW := (dnsLeft+dnsRight)/2, (dnsLeft+dnsRight)/2, dnsRight-dnsLeft
	if beside {
		var tw float32
		for _, lw := range widths {
			tw = max(tw, lw)
		}
		x0 := (dnsLeft + dnsRight - (tw + s.dip(8) + dnsW)) / 2
		xd, tagsX, tagsW = x0+tw+s.dip(8)+dnsW/2, x0+tw/2, tw
	} else if stacked {
		// Under OpenPGP and S/MIME: the disc by the servers, its line to
		// yours clear of them.
		xd = dnsRight - dnsW/2
	}
	_, dnsH := placeChips(lk, flat(dnsTags), tagsX, 0, tagsW, false)
	dnsGroup := dnsT + gap + dnsH
	if beside {
		dnsGroup = max(dnsT, dnsH)
	}
	c4, _, h4 := placeDown(lk, hop4, R+thV/2+s.dip(6), mid, false)
	group(c4, "4")
	xa := w - m - max(dnd/2, aw/2)
	// The attacker's words under it, beside their server's name, where
	// they fit; over it, under step 4's tags, where not.
	theirW := mk + s.dip(4) + font.Advance(routeNames[2].name)
	under := xa-aw/2-(R+min(theirW/2, w-m-R))-s.dip(8) >= 0
	need := mid + h4 + s.dip(4) + dnd/2
	if !under {
		need = mid + h4 + s.dip(10) + float32(len(attacker))*fh + s.dip(4) + dnd/2
	}
	// Step 6's tags: over its tunnel, under DNS, where they fit between
	// the line from YOU to TARGET and tunnel 4.
	left6m, right6m := L+s.dip(10), R-thV/2-s.dip(8)
	_, h6m := placeAlong(lk, hop6, 0, 0, right6m-left6m, -1e6, 1e6)
	inMiddle := !stacked && widestChip(lk, flat(hop6)) <= right6m-left6m
	middle := max(e2eH+s.dip(8), dnsGroup, carries)
	if stacked {
		middle = max(e2eH+s.dip(10)+dnsGroup, carries)
	}
	if inMiddle {
		middle = max(max(e2eH+s.dip(8), dnsGroup)+s.dip(10)+h6m, carries)
	}
	g.y2 = max(mid+middle+s.dip(8)+max(r, thH/2), need)
	y2 := g.y2
	top4, foot4 := topFoot+s.dip(4), y2-max(r, thH/2)-s.dip(4) // the lines down

	upper := foot4 - s.dip(4) // the foot of OpenPGP, S/MIME and DNS
	if inMiddle {
		c6, _ := placeAlong(lk, hop6, (left6m+right6m)/2, foot4-s.dip(4)-h6m, right6m-left6m, left6m, right6m)
		group(c6, "6")
		upper = foot4 - s.dip(4) - h6m - s.dip(10)
	}
	e2eTop := (mid + upper - e2eH) / 2
	groupTop := mid + (upper-mid-dnsGroup)/2
	if stacked {
		// DNS on top, its lines up to your server and down to theirs
		// clear of OpenPGP and S/MIME, which go under it.
		groupTop = mid
		e2eTop = foot4 - s.dip(4) - e2eH
	}
	{
		cE, _, _ := placeDown(lk, e2e, L, e2eTop, true)
		group(cE, "e2e")
	}
	yd, tagsTop := groupTop+dnsT/2, groupTop+dnsT+gap
	if beside {
		yd, tagsTop = groupTop+dnsGroup/2, groupTop+(dnsGroup-dnsH)/2
	}
	cD, _ := placeChips(lk, flat(dnsTags), tagsX, tagsTop, tagsW, false)
	for i := range cD {
		cD[i].alt = i
	}
	group(cD, "dns")
	ay := y2 - dnd/2 - s.dip(4) - float32(len(attacker))*fh
	if under {
		ay = y2 + dnd/2 + s.dip(4)
	}
	for i, l := range attacker {
		g.texts = append(g.texts, placedText{text: l, at: paintengine2d.Pt(xa-font.Advance(l)/2, ay+float32(i)*fh), bad: i > 0})
	}

	// The bottom row: the recipient, their server, the attacker.
	bottom := y2 + max(r, thH/2) + s.dip(6)
	recipFoot, recipHalf := name(3, L, bottom)
	theirFoot, theirHalf := name(2, R, bottom)
	foot := max(recipFoot, theirFoot)
	// Else under its tunnel, between the names; else under the names.
	if !inMiddle {
		left6, right6 := L+recipHalf+s.dip(8), R-theirHalf-s.dip(8)
		c6, h6 := placeAlong(lk, hop6, (left6+right6)/2, y2+thH/2+s.dip(4), right6-left6, left6, right6)
		if fitsIn(c6, left6, right6) {
			foot = max(foot, y2+thH/2+s.dip(4)+h6)
		} else {
			c6, h6 = placeAlong(lk, hop6, (L+R)/2, foot+s.dip(6), w-2*m, m, w-m)
			foot += s.dip(6) + h6
		}
		group(c6, "6")
	}
	if under {
		foot = max(foot, ay+float32(len(attacker))*fh)
	}
	g.h = foot + m

	g.discs = []routeDisc{
		{c: paintengine2d.Pt(L, g.y1), d: d, pict: pictPerson},
		{c: paintengine2d.Pt(R, g.y1), d: d, pict: pictServer},
		{c: paintengine2d.Pt(R, y2), d: d, pict: pictServer},
		{c: paintengine2d.Pt(L, y2), d: d, pict: pictPerson},
		{c: paintengine2d.Pt(xd, yd), d: dnsT, w: dnsW, text: dnsName},
		{c: paintengine2d.Pt(xa, y2), d: dnd, pict: pictPerson, bad: true},
	}
	pt := paintengine2d.Pt
	// The lines leave the pill from its corners on the servers' side.
	dnsTop := pt(xd+dnsW/2-dnsT/2, yd-dnsT/2-s.dip(2))
	// To theirs: across from DNS's right to beside tunnel 4, then down
	// along it — clear of whatever is under DNS.
	along := R - thV/2 - s.dip(5)
	corner := pt(along, yd)
	// The connections: tunnels with TLS, plain lines without.
	// The connections: tunnels with TLS; without, red lines — in the
	// clear.
	ink, tH, tV := inkGood, thH, thV
	if !l.tls {
		ink, tH, tV = inkBad, 0, 0
	}
	// DNS's lines are red where what they look up is not there: the MX
	// record your server asks, the records their server checks.
	askYours, askTheirs := inkMuted, inkMuted
	if !l.mx {
		askYours = inkBad
	}
	if !l.spf || !l.dkim || !l.dmarc {
		askTheirs = inkBad
	}
	tunnel2 := routeLine{a: pt(L+r+s.dip(4), g.y1), b: pt(R-r-s.dip(4), g.y1), ink: ink, head: true, tube: tH}
	tunnel4 := routeLine{a: pt(R, top4), b: pt(R, foot4), ink: ink, head: true, tube: tV}
	tunnel6 := routeLine{a: pt(R-r-s.dip(4), y2), b: pt(L+r+s.dip(4), y2), ink: ink, head: true, tube: tH}
	fake := routeLine{a: pt(xa-dnd/2-s.dip(3), y2), b: pt(R+r+s.dip(4), y2), ink: inkBad, head: true}
	if endToEnd {
		g.lines = append(g.lines, routeLine{a: pt(L, top4), b: pt(L, foot4), ink: inkAccent, dashed: true, head: true})
	}
	g.lines = append(g.lines,
		tunnel2, tunnel4, tunnel6, fake,
		routeLine{a: dnsTop, b: pt(R-thV/2-s.dip(4), topFoot+s.dip(3)), ink: askYours, dashed: true},
		routeLine{a: pt(xd+dnsW/2+s.dip(2), yd), b: corner, ink: askTheirs, dashed: true},
		routeLine{a: corner, b: pt(along, y2-thH/2-s.dip(3)), ink: askTheirs, dashed: true},
	)
	// On each line that carries the message: its number at the start, the
	// envelope between it and the arrow's head.
	carry := func(l routeLine, n int, e envelope, stopped bool) {
		dir := l.b.Sub(l.a).Normalize()
		at := l.a.Add(dir.Mul(s.dip(3) + mk/2))
		g.marks = append(g.marks, sceneMark{n, at.X, at.Y})
		from := l.a.Add(dir.Mul(s.dip(3) + mk + s.dip(4)))
		to := l.b.Sub(dir.Mul(head + s.dip(2)))
		c := from.Lerp(to, 0.5)
		// The envelope's marks stand out to its right and over and under
		// it: centre the whole of it.
		if stopped {
			// It goes no further: a stop where it would be.
			g.stops = append(g.stops, c)
			return
		}
		box := envBox(lk, c)
		c = c.Sub(box.Center().Sub(c))
		g.envs = append(g.envs, placedEnv{c: c, e: e, step: n})
	}
	sent := envelope{lock: l.encrypted, seal: l.signed}
	carry(tunnel2, 2, sent, false)
	sent.stamp = l.dkim
	carry(tunnel4, 4, sent, false)
	// DMARC with neither SPF nor DKIM: TARGET SERVER rejects it.
	carry(tunnel6, 6, sent, rejected(l))
	carry(fake, 8, envelope{stamp: true, bad: true}, false)
	return g
}

// rejected says TARGET SERVER refuses l's message: DMARC with nothing
// that can pass it.
func rejected(l lesson) bool { return l.dmarc && !l.spf && !l.dkim }

// stopSize is a stop's size across.
const stopSize = 22

// drawStop draws a stop centred at c: a red disc with a cross.
func drawStop(ctx *paintengine2d.Context, lk style.LookAndFeel, c paintengine2d.Point) {
	p := lk.Palette()
	d := style.Dip(lk, stopSize)
	ctx.DrawCircle(c, d/2, paintengine2d.Fill(p.Ink(p.Danger)))
	sz := d * 0.6
	style.DrawToolIcon(ctx, paintengine2d.XYWH(c.X-sz/2, c.Y-sz/2, sz, sz), style.IconClose, p.TextOnAccent, style.IconSetOf(lk))
}

// flat is groups' tags, one after another.
func flat(groups [][]string) []string {
	var out []string
	for _, grp := range groups {
		out = append(out, grp...)
	}
	return out
}

// widestChip is the widest of names' tags.
func widestChip(lk style.LookAndFeel, names []string) float32 {
	var out float32
	for _, n := range names {
		cw, _ := chipSize(lk, n)
		out = max(out, cw)
	}
	return out
}

// fitsIn says cs are all between left and right.
func fitsIn(cs []placedChip, left, right float32) bool {
	for _, c := range cs {
		if c.r.Min.X < left || c.r.Max.X > right {
			return false
		}
	}
	return true
}

func (s *routeScene) Paint(ctx *paintengine2d.Context) {
	lk := s.Look()
	b := s.LocalBounds()
	g := s.layoutAt(b.Dx())
	in := inksOf(lk)
	font := lk.Font()
	at := func(p paintengine2d.Point) paintengine2d.Point { return p.Add(b.Min) }
	inks := [...]paintengine2d.Color{inkGood: in.good, inkAccent: lk.Palette().Ink(lk.Palette().Accent), inkMuted: in.muted, inkBad: in.bad}
	arrowHead := func(tip, dir paintengine2d.Point, c paintengine2d.Color, h float32) {
		path := paintengine2d.NewPath()
		path.MoveTo(tip.X, tip.Y)
		path.LineTo(tip.X-dir.X*h-dir.Y*h*0.6, tip.Y-dir.Y*h+dir.X*h*0.6)
		path.LineTo(tip.X-dir.X*h+dir.Y*h*0.6, tip.Y-dir.Y*h-dir.X*h*0.6)
		path.Close()
		ctx.DrawPath(path, paintengine2d.Fill(c))
	}
	for _, l := range g.lines {
		c := inks[l.ink]
		dir := l.b.Sub(l.a).Normalize()
		if l.tube > 0 {
			h := s.dip(9)
			drawTube(ctx, lk, at(l.a), at(l.b.Sub(dir.Mul(h+s.dip(2)))), l.tube)
			arrowHead(at(l.b), dir, c, h)
			continue
		}
		p := paintengine2d.StrokePaint(c, s.dip(1.75))
		if l.dashed {
			p.Stroke.Dash = []float32{s.dip(5), s.dip(4)}
		}
		ctx.DrawLine(at(l.a), at(l.b), p)
		if l.head {
			arrowHead(at(l.b), dir, c, s.dip(7))
		}
	}
	for _, d := range g.discs {
		ink, edge := in.text, in.discEdge
		if d.bad {
			ink, edge = in.bad, in.bad
		}
		c := at(d.c)
		if d.w > 0 {
			box := d.box().Translate(b.Min)
			ctx.DrawRoundRect(box, d.d/2, d.d/2, paintengine2d.Fill(in.disc))
			ctx.DrawRoundRect(box, d.d/2, d.d/2, paintengine2d.StrokePaint(edge, s.dip(1.5)))
		} else {
			ctx.DrawCircle(c, d.d/2, paintengine2d.Fill(in.disc))
			ctx.DrawCircle(c, d.d/2, paintengine2d.StrokePaint(edge, s.dip(1.5)))
		}
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
	for _, st := range g.stops {
		drawStop(ctx, lk, at(st))
	}
	for _, c := range g.chips {
		if c.missing {
			drawMissingChip(ctx, lk, c.name, c.r.Translate(b.Min))
			continue
		}
		drawChip(ctx, lk, c.name, c.r.Translate(b.Min))
	}
	for _, e := range g.envs {
		drawEnvelope(ctx, lk, at(e.c), e.e)
	}
	for _, m := range g.marks {
		drawNumber(ctx, lk, m.n, at(paintengine2d.Pt(m.x, m.y)), s.dip(markSize))
	}
}
