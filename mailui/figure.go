package mailui

import (
	"math"
	"strings"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// A figure is one of Settings › Security › Educate's pictures: people and
// places in a row, joined by arrows that say what passes between them,
// each with a small mark for what matters there — can read it, locked,
// checked. It is drawn, not shipped as an image: in the look's own colours,
// so it reads in every theme and at every scale, and its words wrap to the
// width it is given.

// pict is what a figure draws: one of the toolkit's icons, or one of the
// few it has none for, drawn here as lines in the same weight.
type pict int

const (
	pictNone pict = iota
	pictPerson
	pictPeople
	pictServer
	pictLetter
	pictKey
	pictLock
	pictPadlockOpen
	pictSeal
	pictEye
	pictCheck
	pictWarning
	pictError
	pictPen
	pictPicture
	pictComputer
)

// tone is what a part of a figure means: plain, good or bad.
type tone int

const (
	tonePlain tone = iota
	toneGood
	toneBad
)

// figNode is one person or place: its picture, the words under it, and a
// mark at its corner.
type figNode struct {
	pict  pict
	label string
	badge pict
	tone  tone
}

// figLink is what passes from one node to the next: words over the arrow,
// a picture on it; plain is a line with no head (a comparison, not a
// journey).
type figLink struct {
	label string
	pict  pict
	tone  tone
	plain bool
}

type figure struct {
	widget.Base
	nodes []figNode
	links []figLink // between nodes[i] and nodes[i+1]
}

// newFigure is a figure of nodes joined by links (one fewer).
func newFigure(nodes []figNode, links ...figLink) *figure {
	f := &figure{nodes: nodes, links: links}
	f.Init(f)
	return f
}

// The figure's lengths, in 1x design pixels.
const (
	figCircle   = 52 // a node's disc
	figBadge    = 20 // the mark at its corner
	figOnArrow  = 22 // a link's picture
	figPad      = 8
	figMinSlot  = 84 // the narrowest a node's column may be
	figMaxLines = 4
)

func (f *figure) dip(v float32) float32 { return style.Dip(f.Look(), v) }

// MinWidth is a column per node at its narrowest.
func (f *figure) MinWidth() float32 { return f.dip(figMinSlot) * float32(len(f.nodes)) }

func (f *figure) Measure(c layout.Constraints) paintengine2d.Point {
	w := f.dip(140) * float32(len(f.nodes))
	if c.HasMaxW() {
		w = c.MaxW
	}
	return c.Constrain(paintengine2d.Pt(w, f.heightAt(w)))
}

func (f *figure) Arrange(r paintengine2d.Rect) { f.SetBounds(r) }

// geometry is where things go at width w: a column (slot) per node, the
// arrows' words in a band over the discs (top is where the discs start),
// and each node's words under its disc.
type figGeometry struct {
	slot, circle, top float32
	band              int // lines in the band over the discs
	linkLines         [][]string
	nodeLines         [][]string
}

func (f *figure) layoutAt(w float32) figGeometry {
	font := f.Look().Font()
	var g figGeometry
	n := float32(max(1, len(f.nodes)))
	g.slot = w / n
	// In a narrow window the discs give up room to the arrows.
	g.circle = min(f.dip(figCircle), g.slot*0.5)
	textW := g.slot - f.dip(figPad/2)
	for _, l := range f.links {
		// Centred over its arrow, a link's words keep clear of the next
		// link's.
		lines := wrapWords(font, l.label, textW)
		g.linkLines = append(g.linkLines, lines)
		g.band = max(g.band, len(lines))
	}
	for _, nd := range f.nodes {
		g.nodeLines = append(g.nodeLines, wrapWords(font, nd.label, textW))
	}
	g.top = f.dip(figPad) + f.dip(figBadge)/2
	if g.band > 0 {
		g.top = f.dip(figPad) + float32(g.band)*font.Height() + f.dip(figBadge)/2
	}
	return g
}

func (f *figure) heightAt(w float32) float32 {
	g := f.layoutAt(w)
	lines := 0
	for _, l := range g.nodeLines {
		lines = max(lines, len(l))
	}
	return g.top + g.circle + f.dip(6) + float32(lines)*f.Look().Font().Height() + f.dip(figPad)
}

// colors are the figure's inks: text, lines, and the good and bad.
type figColors struct {
	text, muted, line, good, bad, disc, discEdge paintengine2d.Color
}

func (f *figure) colors() figColors {
	p := f.Look().Palette()
	return figColors{
		text: p.Text, muted: p.TextMuted, line: p.TextMuted,
		good: p.Ink(p.Success), bad: p.Ink(p.Danger),
		disc: p.Field, discEdge: p.Border,
	}
}

func (c figColors) of(t tone) paintengine2d.Color {
	switch t {
	case toneGood:
		return c.good
	case toneBad:
		return c.bad
	}
	return c.text
}

func (f *figure) Paint(ctx *paintengine2d.Context) {
	b := f.LocalBounds()
	g := f.layoutAt(b.Dx())
	col := f.colors()
	font := f.Look().Font()
	cy := b.Min.Y + g.top + g.circle/2
	center := func(i int) float32 { return b.Min.X + g.slot*(float32(i)+0.5) }

	// The arrows first, under the discs.
	for i, l := range f.links {
		if i+1 >= len(f.nodes) {
			break
		}
		x0, x1 := center(i)+g.circle/2+f.dip(4), center(i+1)-g.circle/2-f.dip(4)
		ink := col.line
		if l.tone != tonePlain {
			ink = col.of(l.tone)
		}
		ctx.DrawLine(paintengine2d.Pt(x0, cy), paintengine2d.Pt(x1, cy), paintengine2d.StrokePaint(ink, f.dip(1.75)))
		if !l.plain {
			head := f.dip(7)
			p := paintengine2d.NewPath()
			p.MoveTo(x1, cy)
			p.LineTo(x1-head, cy-head*0.6)
			p.LineTo(x1-head, cy+head*0.6)
			p.Close()
			ctx.DrawPath(p, paintengine2d.Fill(ink))
		}
		mid := (x0 + x1) / 2
		if l.pict != pictNone {
			s := f.dip(figOnArrow)
			ctx.DrawCircle(paintengine2d.Pt(mid, cy), s/2+f.dip(2), paintengine2d.Fill(col.disc))
			f.drawPict(ctx, l.pict, paintengine2d.XYWH(mid-s/2, cy-s/2, s, s), col.of(l.tone))
		}
		// In the band over the discs, its last line at the band's foot.
		lines := g.linkLines[i]
		y := b.Min.Y + f.dip(figPad) + float32(g.band-len(lines))*font.Height()
		for _, line := range lines {
			font.Draw(ctx, line, paintengine2d.Pt(mid-font.Advance(line)/2, y), col.muted)
			y += font.Height()
		}
	}

	// The people and places.
	for i, nd := range f.nodes {
		cx := center(i)
		r := g.circle / 2
		edge := col.discEdge
		if nd.tone != tonePlain {
			edge = col.of(nd.tone)
		}
		ctx.DrawCircle(paintengine2d.Pt(cx, cy), r, paintengine2d.Fill(col.disc))
		ctx.DrawCircle(paintengine2d.Pt(cx, cy), r, paintengine2d.StrokePaint(edge, f.dip(1.5)))
		s := g.circle * 0.56
		f.drawPict(ctx, nd.pict, paintengine2d.XYWH(cx-s/2, cy-s/2, s, s), col.of(nd.tone))
		if nd.badge != pictNone {
			bs := f.dip(figBadge)
			bx, by := cx+r*0.62, cy-r*0.62
			badgeTone := nd.tone
			switch nd.badge {
			case pictCheck:
				badgeTone = toneGood
			case pictWarning, pictError, pictEye:
				if badgeTone == tonePlain {
					badgeTone = toneBad
				}
			}
			ctx.DrawCircle(paintengine2d.Pt(bx, by), bs/2+f.dip(1), paintengine2d.Fill(col.disc))
			ctx.DrawCircle(paintengine2d.Pt(bx, by), bs/2+f.dip(1), paintengine2d.StrokePaint(col.of(badgeTone), f.dip(1.25)))
			in := bs * 0.7
			f.drawPict(ctx, nd.badge, paintengine2d.XYWH(bx-in/2, by-in/2, in, in), col.of(badgeTone))
		}
		y := cy + r + f.dip(6)
		for _, line := range g.nodeLines[i] {
			font.Draw(ctx, line, paintengine2d.Pt(cx-font.Advance(line)/2, y), col.text)
			y += font.Height()
		}
	}
}

// drawPict draws p in box in ink.
func (f *figure) drawPict(ctx *paintengine2d.Context, p pict, box paintengine2d.Rect, ink paintengine2d.Color) {
	icon := func(i style.ToolIcon) { style.DrawToolIcon(ctx, box, i, ink, style.IconSetOf(f.Look())) }
	switch p {
	case pictPerson:
		icon(style.IconUser)
	case pictLetter:
		icon(style.IconMail)
	case pictLock:
		icon(style.IconLock)
	case pictEye:
		icon(style.IconEye)
	case pictCheck:
		icon(style.IconCheck)
	case pictWarning:
		icon(style.IconWarning)
	case pictError:
		icon(style.IconError)
	case pictPen:
		icon(style.IconPen)
	default:
		f.drawLines(ctx, p, box, ink)
	}
}

// drawLines draws the pictures the toolkit has no icon for, in a 24-unit
// box, as the icons are drawn: strokes of one weight.
func (f *figure) drawLines(ctx *paintengine2d.Context, p pict, box paintengine2d.Rect, ink paintengine2d.Color) {
	u := box.Dx() / 24
	at := func(x, y float32) paintengine2d.Point { return paintengine2d.Pt(box.Min.X+x*u, box.Min.Y+y*u) }
	pen := paintengine2d.StrokePaint(ink, max(1, 1.75*u))
	line := func(x0, y0, x1, y1 float32) { ctx.DrawLine(at(x0, y0), at(x1, y1), pen) }
	rrect := func(x, y, w, h, r float32) {
		ctx.DrawRoundRect(paintengine2d.XYWH(box.Min.X+x*u, box.Min.Y+y*u, w*u, h*u), r*u, r*u, pen)
	}
	circle := func(x, y, r float32) { ctx.DrawCircle(at(x, y), r*u, pen) }
	switch p {
	case pictPeople:
		// Two people, one a little behind the other: the one behind shows
		// only its head and the shoulder that is not hidden.
		circle(15, 8.5, 3.5)
		front := paintengine2d.NewPath()
		front.AddArc(at(15, 21), 6.5*u, 6*u, math.Pi, math.Pi)
		ctx.DrawPath(front, pen)
		circle(7.5, 7, 3)
		back := paintengine2d.NewPath()
		back.AddArc(at(7.5, 18), 5.5*u, 5*u, math.Pi, math.Pi*0.42)
		ctx.DrawPath(back, pen)
	case pictServer:
		// A rack of two: a post office for mail.
		rrect(3.5, 3.5, 17, 7.5, 1.5)
		rrect(3.5, 13, 17, 7.5, 1.5)
		ctx.DrawCircle(at(16.5, 7.25), 1.1*u, paintengine2d.Fill(ink))
		ctx.DrawCircle(at(16.5, 16.75), 1.1*u, paintengine2d.Fill(ink))
		line(6.5, 7.25, 11, 7.25)
		line(6.5, 16.75, 11, 16.75)
	case pictKey:
		circle(7.5, 12, 4)
		line(11.5, 12, 21, 12)
		line(17.5, 12, 17.5, 15.5)
		line(20.5, 12, 20.5, 14.5)
	case pictPadlockOpen:
		// The body, and the shackle lifted off one side: open, to hand out.
		rrect(4.5, 11, 15, 10, 1.5)
		arc := paintengine2d.NewPath()
		arc.AddArc(at(12, 7.5), 4*u, 4*u, math.Pi, math.Pi)
		ctx.DrawPath(arc, pen)
		line(8, 7.5, 8, 11)
		line(16, 7.5, 16, 8.5)
		ctx.DrawCircle(at(12, 16), 1.2*u, paintengine2d.Fill(ink))
	case pictSeal:
		// A rosette with a tick, and its two ribbons.
		circle(12, 9.5, 6.5)
		line(9, 9.75, 11.2, 11.9)
		line(11.2, 11.9, 15.2, 7.6)
		line(8.5, 15, 6.5, 21.5)
		line(6.5, 21.5, 9.5, 20)
		line(15.5, 15, 17.5, 21.5)
		line(17.5, 21.5, 14.5, 20)
	case pictPicture:
		rrect(3, 4.5, 18, 15, 1.5)
		circle(8.5, 9.5, 1.75)
		line(4, 18, 10, 12.5)
		line(10, 12.5, 14, 16)
		line(14, 16, 16.5, 13.5)
		line(16.5, 13.5, 20, 17)
	case pictComputer:
		rrect(3, 4, 18, 12, 1.5)
		line(12, 16, 12, 19.5)
		line(8, 19.75, 16, 19.75)
	}
}

// wrapWords breaks text into lines of at most w, at spaces — and after a
// hyphen, in a word too long for a line — keeping at most figMaxLines; a
// piece still too long is cut to fit.
func wrapWords(font *style.Font, text string, w float32) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var words []string
	for _, word := range strings.Fields(text) {
		for font.Advance(word) > w {
			i := strings.Index(word, "-")
			if i < 0 || i == len(word)-1 {
				break
			}
			words = append(words, word[:i+1])
			word = word[i+1:]
		}
		words = append(words, word)
	}
	var lines []string
	cur := ""
	for _, word := range words {
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if cur != "" && font.Advance(next) > w {
			lines = append(lines, cur)
			cur = word
			continue
		}
		cur = next
	}
	lines = append(lines, cur)
	for i, l := range lines {
		if font.Advance(l) > w {
			lines[i] = font.Fit(l, w)
		}
	}
	if len(lines) > figMaxLines {
		lines = lines[:figMaxLines]
		lines[figMaxLines-1] = font.Fit(lines[figMaxLines-1]+"…", w)
	}
	return lines
}
