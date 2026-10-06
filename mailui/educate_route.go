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
//	1 You ───2─[✉]───▶ 3 Your server         TARGET's domain: DNS, MX
//	  ┊ OpenPGP|S/MIME           │4 [✉]   SMTP, STARTTLS, MTA-STS|DANE
//	  ▼                          ▼        Your domain: DNS, SPF, DKIM, DMARC
//	7 Recipient ◀───6─[✉]─── 5 Their server ◀──[✉]8── Attacker, acrne.com
//
// The message is an envelope: a padlock when encrypted, a wax seal when
// you signed it, a postmark when your domain did (DKIM). Each connection
// is an arrow it rides on: green with TLS, red in the clear. OpenPGP and
// S/MIME join you and the recipient directly, end to end, past every
// server. DNS is drawn by the server that asks it. Each step's standards
// are drawn by it as tags; alternatives — either one does the job — stand
// across its line: one over the other by a line across, side by side by
// a line down. The picture is as big whatever is ticked: it keeps the
// room the most it can draw takes.

// l is the message and road drawn: its envelope with a seal, a padlock,
// both or neither, and your domain's postmark with DKIM; OpenPGP and
// S/MIME from YOU to TARGET when signed or encrypted; its connections
// green with TLS, red without; and the standards in use, and those that
// are not struck through.
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

// forgesYours is under the attacker without DMARC: it can forge your
// domain too.
const forgesYours = "or your domain"

// dnsRecords are the records the picture draws by DNS — those turned off
// struck through: MX in TARGET's domain's, which YOUR SERVER asks; the
// rest in your domain's, which TARGET SERVER asks.
var dnsRecords = one("MX", "SPF", "DKIM", "DMARC")

// theirsDNS and yoursDNS are the captions over the two.
const theirsDNS, yoursDNS = "TARGET's domain", "Your domain"

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

// routeLine is a line of the picture from a to b, an arrow when head;
// under names the tags it runs behind.
type routeLine struct {
	a, b   paintengine2d.Point
	ink    int
	dashed bool
	head   bool
	under  string
}

// placedText is a line of words where it is drawn, from its top left;
// bad is the attacker's, muted a caption.
type placedText struct {
	text  string
	at    paintengine2d.Point
	bad   bool
	muted bool
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
// side across the lines down, and the lines across long enough for their
// number and envelope, at the smallest discs.
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
	d := min(max(w*0.1, s.dip(32)), s.dip(42))
	r := d / 2
	dnd := d * 0.72
	eb := envBox(lk, paintengine2d.Pt(0, 0))
	thV := eb.Dx() + s.dip(8)
	named := func(i int) float32 { return mk + s.dip(4) + font.Advance(routeNames[i].name) }
	// DNS's captions, and its pill: its records flow after it.
	dnsW := max(font.Advance(dnsName)+s.dip(12), font.Advance(theirsDNS), font.Advance(yoursDNS))
	_, e2eW, _ := placeDown(lk, e2eTags, 0, 0, true)
	_, hop4W, _ := placeDown(lk, relayStep(lesson{tls: true, mtaSTS: true, dane: true}).tags, 0, 0, false)
	var aw float32
	for _, l := range []string{"Attacker", fakeDomain, forgesYours} {
		aw = max(aw, font.Advance(l))
	}
	carries := s.dip(3) + mk + s.dip(4) + eb.Dx() + s.dip(6) + s.dip(9)
	// Room for the names either side.
	between := max(named(0)/2+named(1)/2, named(3)/2+named(2)/2) + s.dip(12)
	return routeColumns{
		between: between,
		L:       m + max(e2eW/2, r, named(0)/2, named(3)/2) + s.dip(2),
		right:   max(max(hop4W, dnsW)+thV/2+s.dip(6), max(dnd/2, aw/2)+dnd/2+s.dip(7)+carries+r, w*0.28),
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
	d := min(max(w*0.1, s.dip(32)), s.dip(42))
	r := d / 2
	dnd := d * 0.72
	eb := envBox(lk, paintengine2d.Pt(0, 0))
	thH, thV := eb.Dy()+s.dip(8), eb.Dx()+s.dip(8) // what an envelope takes on a line across, and down
	head := s.dip(9)
	l := s.l
	// What is turned off still has its place, in red: DNS's four records,
	// MTA-STS and DANE with TLS, and OpenPGP and S/MIME.
	e2e, hop2, hop6 := e2eTags, submitStep(l.tls).tags, fetchStep(l.tls).tags
	hop4Most := relayStep(lesson{tls: true, mtaSTS: true, dane: true}).tags
	hop4 := relayStep(l).tags
	if l.tls {
		hop4 = hop4Most
	}
	// The room each takes is the most it can — with TLS and without, the
	// attacker forging your domain too — so the picture keeps its size
	// whatever is ticked.
	hop2s := [][][]string{submitStep(true).tags, submitStep(false).tags}
	hop6s := [][][]string{fetchStep(true).tags, fetchStep(false).tags}
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
	_, _, e2eH := placeDown(lk, e2e, 0, 0, true)
	attacker := []string{"Attacker", fakeDomain}
	if !l.dmarc {
		attacker = append(attacker, forgesYours) // nothing stops it forging yours
	}
	attackerMost := float32(3) * fh
	var aw float32
	for _, l := range []string{"Attacker", fakeDomain, forgesYours} {
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

	// DNS, in two: TARGET's domain's, which YOUR SERVER asks for its MX
	// record; your domain's, which TARGET SERVER asks for SPF, DKIM and
	// DMARC. Each a caption, and its pill and records in a flow, by the
	// server that asks it.
	_, ch := chipSize(lk, "X")
	dnsW := font.Advance(dnsName) + s.dip(12)
	dnsFlow := func(caption string, records [][]string, x, top, maxW float32, name string, place bool) (wide, high float32) {
		type item struct {
			name string
			w    float32
		}
		items := []item{{dnsName, dnsW}}
		for _, r := range flat(records) {
			cw, _ := chipSize(lk, r)
			items = append(items, item{r, cw})
		}
		wide = font.Advance(caption)
		y := top + fh + s.dip(2)
		cx := x
		for i, it := range items {
			if i > 0 && cx+it.w > x+maxW {
				cx, y = x, y+ch+gap
			}
			wide = max(wide, cx+it.w-x)
			if place {
				r := paintengine2d.XYWH(cx, y, it.w, ch)
				if i == 0 {
					g.discs = append(g.discs, routeDisc{c: r.Center(), d: ch, w: it.w, text: dnsName})
				} else {
					c := placedChip{name: it.name, r: r, group: name, alt: i - 1, missing: missing[it.name]}
					g.chips = append(g.chips, c)
				}
			}
			cx += it.w + gap
		}
		if place {
			g.texts = append(g.texts, placedText{text: caption, at: paintengine2d.Pt(x, top), muted: true})
		}
		return wide, y + ch - top
	}
	theirsRecords, yoursRecords := dnsRecords[:1], dnsRecords[1:]

	// Step 2's tags over the top row, over the discs too; TARGET's
	// domain's DNS beside them, over YOUR SERVER, where there is room.
	var h2, right2 float32
	for _, tags := range hop2s {
		cs, h := placeAlong(lk, tags, (L+R)/2, m, w-2*m, m, w-m)
		h2 = max(h2, h)
		for _, c := range cs {
			right2 = max(right2, c.r.Max.X)
		}
	}
	{
		_, h := placeAlong(lk, hop2, 0, 0, w-2*m, -1e6, 1e6)
		c2, _ := placeAlong(lk, hop2, (L+R)/2, m+h2-h, w-2*m, m, w-m) // down to its line
		group(c2, "2")
	}
	aW, aH := dnsFlow(theirsDNS, theirsRecords, 0, 0, w, "", false)
	aboveYours := w-m-aW >= right2+s.dip(12)
	if aboveYours {
		dnsFlow(theirsDNS, theirsRecords, w-m-aW, m, aW, "dnsT", true)
		h2 = max(h2, aH)
	}
	g.y1 = m + h2 + s.dip(4) + max(r, thH/2)
	nameTop := g.y1 + max(r, thH/2) + s.dip(4)
	f0, _ := name(0, L, nameTop)
	f1, _ := name(1, R, nameTop)
	topFoot := max(f0, f1)

	// Right of line 4: TARGET's domain's DNS, when not over YOUR SERVER;
	// step 4's tags; and your domain's DNS, over TARGET SERVER. In the
	// middle: OpenPGP and S/MIME across the line from YOU to TARGET, and
	// step 6's tags over its line.
	mid := topFoot + s.dip(6)
	rx := R + thV/2 + s.dip(6) // the right column's left
	rcw := w - m - rx          // and its width
	top4tags := mid
	if !aboveYours {
		_, h := dnsFlow(theirsDNS, theirsRecords, rx, mid, rcw, "dnsT", true)
		top4tags = mid + h + s.dip(10)
	}
	c4, _, _ := placeDown(lk, hop4, rx, top4tags, false)
	group(c4, "4")
	_, _, h4 := placeDown(lk, hop4Most, rx, top4tags, false)
	_, bH := dnsFlow(yoursDNS, yoursRecords, rx, 0, rcw, "", false)
	rightFoot := top4tags + h4 + s.dip(10) + bH
	xa := w - m - max(dnd/2, aw/2)
	// The attacker's words under it, beside their server's name, where
	// they fit; over it, under step 4's tags, where not.
	theirW := mk + s.dip(4) + font.Advance(routeNames[2].name)
	under := xa-aw/2-(R+min(theirW/2, w-m-R))-s.dip(8) >= 0
	need := rightFoot + s.dip(6) + max(r, thH/2) // your domain's DNS over TARGET SERVER
	if !under {
		need = rightFoot + s.dip(10) + attackerMost + s.dip(4) + dnd/2
	}
	// Step 6's tags over its line, under OpenPGP and S/MIME, where they
	// fit between the line from YOU to TARGET and line 4.
	left6m, right6m := L+s.dip(10), R-thV/2-s.dip(8)
	var h6m float32
	inMiddle := true
	for _, tags := range hop6s {
		_, h := placeAlong(lk, tags, 0, 0, right6m-left6m, -1e6, 1e6)
		h6m = max(h6m, h)
		inMiddle = inMiddle && widestChip(lk, flat(tags)) <= right6m-left6m
	}
	middle := max(e2eH+s.dip(8), carries)
	if inMiddle {
		middle = max(e2eH+s.dip(10)+h6m, carries)
	}
	g.y2 = max(mid+middle+s.dip(6)+max(r, thH/2), need)
	y2 := g.y2
	top4, foot4 := topFoot+s.dip(4), y2-max(r, thH/2)-s.dip(4) // the lines down

	upper := foot4 - s.dip(4) // the foot of OpenPGP and S/MIME
	if inMiddle {
		_, h := placeAlong(lk, hop6, 0, 0, right6m-left6m, -1e6, 1e6)
		c6, _ := placeAlong(lk, hop6, (left6m+right6m)/2, foot4-s.dip(4)-h, right6m-left6m, left6m, right6m)
		group(c6, "6")
		upper = foot4 - s.dip(4) - h6m - s.dip(10)
	}
	{
		cE, _, _ := placeDown(lk, e2e, L, (mid+upper-e2eH)/2, true)
		group(cE, "e2e")
	}
	// Your domain's DNS just over TARGET SERVER, where the attacker's
	// words are under it; else straight under step 4's tags.
	bTop := y2 - max(r, thH/2) - s.dip(6) - bH
	if !under {
		bTop = top4tags + h4 + s.dip(10)
	}
	dnsFlow(yoursDNS, yoursRecords, rx, bTop, rcw, "dnsY", true)
	ay := y2 - dnd/2 - s.dip(4) - float32(len(attacker))*fh // over it, under your domain's DNS
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
	// Else under its line, between the names; else under the names.
	if !inMiddle {
		left6, right6 := L+recipHalf+s.dip(8), R-theirHalf-s.dip(8)
		top6, cx6, w6, lo6, hi6 := y2+thH/2+s.dip(4), (left6+right6)/2, right6-left6, left6, right6
		for _, tags := range hop6s {
			if c, _ := placeAlong(lk, tags, cx6, top6, w6, lo6, hi6); !fitsIn(c, left6, right6) {
				// Else under the names.
				top6, cx6, w6, lo6, hi6 = foot+s.dip(6), (L+R)/2, w-2*m, m, w-m
			}
		}
		var h6 float32
		for _, tags := range hop6s {
			_, h := placeAlong(lk, tags, cx6, top6, w6, lo6, hi6)
			h6 = max(h6, h)
		}
		c6, _ := placeAlong(lk, hop6, cx6, top6, w6, lo6, hi6)
		group(c6, "6")
		foot = max(foot, top6+h6)
	}
	if under {
		foot = max(foot, ay+attackerMost)
	}
	g.h = foot + m

	g.discs = append(g.discs, []routeDisc{
		{c: paintengine2d.Pt(L, g.y1), d: d, pict: pictPerson},
		{c: paintengine2d.Pt(R, g.y1), d: d, pict: pictServer},
		{c: paintengine2d.Pt(R, y2), d: d, pict: pictServer},
		{c: paintengine2d.Pt(L, y2), d: d, pict: pictPerson},
		{c: paintengine2d.Pt(xa, y2), d: dnd, pict: pictPerson, bad: true},
	}...)
	pt := paintengine2d.Pt
	// The connections: green arrows with TLS; without, red — in the
	// clear.
	ink := inkGood
	if !l.tls {
		ink = inkBad
	}
	line2 := routeLine{a: pt(L+r+s.dip(4), g.y1), b: pt(R-r-s.dip(4), g.y1), ink: ink, head: true}
	line4 := routeLine{a: pt(R, top4), b: pt(R, foot4), ink: ink, head: true}
	line6 := routeLine{a: pt(R-r-s.dip(4), y2), b: pt(L+r+s.dip(4), y2), ink: ink, head: true}
	fake := routeLine{a: pt(xa-dnd/2-s.dip(3), y2), b: pt(R+r+s.dip(4), y2), ink: inkBad, head: true}
	if endToEnd {
		g.lines = append(g.lines, routeLine{a: pt(L, top4), b: pt(L, foot4), ink: inkAccent, dashed: true, head: true})
	}
	g.lines = append(g.lines,
		line2, line4, line6, fake,
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
	carry(line2, 2, sent, false)
	sent.stamp = l.dkim
	carry(line4, 4, sent, false)
	// DMARC with neither SPF nor DKIM: TARGET SERVER rejects it.
	carry(line6, 6, sent, rejected(l))
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
	ctx.DrawCircle(c, d/2, paintengine2d.Fill(whiteOn(p.Ink(p.Danger))))
	sz := d * 0.6
	style.DrawToolIcon(ctx, paintengine2d.XYWH(c.X-sz/2, c.Y-sz/2, sz, sz), style.IconClose, white, style.IconSetOf(lk))
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
		if t.muted {
			ink = in.muted
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
