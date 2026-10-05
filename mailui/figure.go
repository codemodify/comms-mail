package mailui

import (
	"math"
	"strings"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/style"
)

// The pictures Settings › Security › Educate draws: the toolkit's icons,
// and line drawings, in the same weight, of what it has no icon for — a
// server, a message as an envelope with its padlock, seal and postmark,
// an encrypted connection as a tunnel. They are drawn, not shipped as
// images: in the look's own colours, so they read in every theme and at
// every scale.

// pict is a picture.
type pict int

const (
	pictPerson pict = iota
	pictServer
)

// inks are a picture's colours, from the look.
type inks struct {
	text, muted, good, bad, disc, discEdge paintengine2d.Color
}

func inksOf(lk style.LookAndFeel) inks {
	p := lk.Palette()
	return inks{
		text: p.Text, muted: p.TextMuted, good: p.Ink(p.Success), bad: p.Ink(p.Danger),
		disc: p.Field, discEdge: p.Border,
	}
}

// drawPict draws p in box in ink.
func drawPict(ctx *paintengine2d.Context, lk style.LookAndFeel, p pict, box paintengine2d.Rect, ink paintengine2d.Color) {
	switch p {
	case pictPerson:
		style.DrawToolIcon(ctx, box, style.IconUser, ink, style.IconSetOf(lk))
	case pictServer:
		drawServer(ctx, box, ink)
	}
}

// drawServer draws a server, which the toolkit has no icon for, in a
// 24-unit box, as the icons are drawn: strokes of one weight. A rack of
// two: a post office for mail.
func drawServer(ctx *paintengine2d.Context, box paintengine2d.Rect, ink paintengine2d.Color) {
	u := box.Dx() / 24
	at := func(x, y float32) paintengine2d.Point { return paintengine2d.Pt(box.Min.X+x*u, box.Min.Y+y*u) }
	pen := paintengine2d.StrokePaint(ink, max(1, 1.75*u))
	line := func(x0, y0, x1, y1 float32) { ctx.DrawLine(at(x0, y0), at(x1, y1), pen) }
	rrect := func(x, y, w, h, r float32) {
		ctx.DrawRoundRect(paintengine2d.XYWH(box.Min.X+x*u, box.Min.Y+y*u, w*u, h*u), r*u, r*u, pen)
	}
	rrect(3.5, 3.5, 17, 7.5, 1.5)
	rrect(3.5, 13, 17, 7.5, 1.5)
	ctx.DrawCircle(at(16.5, 7.25), 1.1*u, paintengine2d.Fill(ink))
	ctx.DrawCircle(at(16.5, 16.75), 1.1*u, paintengine2d.Fill(ink))
	line(6.5, 7.25, 11, 7.25)
	line(6.5, 16.75, 11, 16.75)
}

// wrapWords breaks text into lines of at most w, at spaces, keeping at
// most maxLines; a word still too long is cut to fit.
func wrapWords(font *style.Font, text string, w float32, maxLines int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
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
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = font.Fit(lines[maxLines-1]+"…", w)
	}
	return lines
}

// ---- the message and the road ----

// envelope is a message as the picture draws it, and what it carries:
// encrypted (a padlock), signed by you (a wax seal), signed by its domain
// (a postmark); bad is an attacker's.
type envelope struct{ lock, seal, stamp, bad bool }

// envSize is an envelope's size, the letter alone: wider than tall.
func envSize(lk style.LookAndFeel) (w, h float32) {
	h = style.Dip(lk, 18)
	return h * 1.45, h
}

// envBox is where an envelope centred at c draws, its marks included:
// they stand out over its edges.
func envBox(lk style.LookAndFeel, c paintengine2d.Point) paintengine2d.Rect {
	w, h := envSize(lk)
	return paintengine2d.XYWH(c.X-w/2, c.Y-h/2-0.2*h, w+0.3*h, h+0.4*h)
}

// drawEnvelope draws e centred at c.
func drawEnvelope(ctx *paintengine2d.Context, lk style.LookAndFeel, c paintengine2d.Point, e envelope) {
	p := lk.Palette()
	in := inksOf(lk)
	w, h := envSize(lk)
	r := paintengine2d.XYWH(c.X-w/2, c.Y-h/2, w, h)
	line := in.text
	if e.bad {
		line = in.bad
	}
	pen := paintengine2d.StrokePaint(line, max(1, h/11))
	ctx.DrawRoundRect(r, h/9, h/9, paintengine2d.Fill(p.Field))
	ctx.DrawRoundRect(r, h/9, h/9, pen)
	tip := paintengine2d.Pt(c.X, r.Min.Y+h*0.58)
	flap := paintengine2d.NewPath()
	flap.MoveTo(r.Min.X+h*0.08, r.Min.Y+h*0.08)
	flap.LineTo(tip.X, tip.Y)
	flap.LineTo(r.Max.X-h*0.08, r.Min.Y+h*0.08)
	ctx.DrawPath(flap, pen)
	if e.seal {
		// Wax on the flap's point, pressed: a disc and a ring in it.
		ctx.DrawCircle(tip, h*0.27, paintengine2d.Fill(p.Ink(p.Warning)))
		ctx.DrawCircle(tip, h*0.15, paintengine2d.StrokePaint(p.Field, max(1, h/16)))
	}
	if e.stamp {
		// A postmark over the top right corner: two rings.
		pc := paintengine2d.Pt(r.Max.X-h*0.06, r.Min.Y+h*0.04)
		ctx.DrawCircle(pc, h*0.24, paintengine2d.Fill(p.Field))
		ctx.DrawCircle(pc, h*0.24, paintengine2d.StrokePaint(in.muted, max(1, h/14)))
		ctx.DrawCircle(pc, h*0.12, paintengine2d.StrokePaint(in.muted, max(1, h/14)))
	}
	if e.lock {
		drawPadlock(ctx, lk, paintengine2d.Pt(r.Max.X-h*0.02, r.Max.Y-h*0.12), h*0.62)
	}
}

// drawPadlock draws a closed padlock of size s centred at c, in the
// look's accent: what encryption is everywhere in the picture.
func drawPadlock(ctx *paintengine2d.Context, lk style.LookAndFeel, c paintengine2d.Point, s float32) {
	p := lk.Palette()
	ink := p.Ink(p.Accent)
	body := paintengine2d.XYWH(c.X-s*0.42, c.Y-s*0.08, s*0.84, s*0.58)
	shackle := paintengine2d.NewPath()
	shackle.AddArc(paintengine2d.Pt(c.X, c.Y-s*0.08), s*0.26, s*0.3, math.Pi, math.Pi)
	ctx.DrawPath(shackle, paintengine2d.StrokePaint(ink, max(1, s*0.14)))
	ctx.DrawRoundRect(body, s*0.1, s*0.1, paintengine2d.Fill(ink))
	ctx.DrawCircle(paintengine2d.Pt(c.X, body.Min.Y+body.Dy()*0.45), s*0.08, paintengine2d.Fill(p.Field))
}

// drawTube draws an encrypted connection from a to b, which share an x or
// a y: a tunnel of thickness th, its two mouths open.
func drawTube(ctx *paintengine2d.Context, lk style.LookAndFeel, a, b paintengine2d.Point, th float32) {
	ink := inksOf(lk).good
	pen := paintengine2d.StrokePaint(ink, style.Dip(lk, 1.5))
	mouth := th * 0.34
	if a.Y == b.Y {
		x0, x1 := min(a.X, b.X), max(a.X, b.X)
		ctx.DrawRect(paintengine2d.XYWH(x0, a.Y-th/2, x1-x0, th), paintengine2d.Fill(ink.WithAlpha(0.12)))
		ctx.DrawLine(paintengine2d.Pt(x0, a.Y-th/2), paintengine2d.Pt(x1, a.Y-th/2), pen)
		ctx.DrawLine(paintengine2d.Pt(x0, a.Y+th/2), paintengine2d.Pt(x1, a.Y+th/2), pen)
		for _, x := range []float32{x0, x1} {
			ctx.DrawOval(paintengine2d.XYWH(x-mouth/2, a.Y-th/2, mouth, th), paintengine2d.Fill(ink.WithAlpha(0.22)))
			ctx.DrawOval(paintengine2d.XYWH(x-mouth/2, a.Y-th/2, mouth, th), pen)
		}
		return
	}
	y0, y1 := min(a.Y, b.Y), max(a.Y, b.Y)
	ctx.DrawRect(paintengine2d.XYWH(a.X-th/2, y0, th, y1-y0), paintengine2d.Fill(ink.WithAlpha(0.12)))
	ctx.DrawLine(paintengine2d.Pt(a.X-th/2, y0), paintengine2d.Pt(a.X-th/2, y1), pen)
	ctx.DrawLine(paintengine2d.Pt(a.X+th/2, y0), paintengine2d.Pt(a.X+th/2, y1), pen)
	for _, y := range []float32{y0, y1} {
		ctx.DrawOval(paintengine2d.XYWH(a.X-th/2, y-mouth/2, th, mouth), paintengine2d.Fill(ink.WithAlpha(0.22)))
		ctx.DrawOval(paintengine2d.XYWH(a.X-th/2, y-mouth/2, th, mouth), pen)
	}
}

// drawPlainLine draws a connection in the clear from a to b: a plain line
// and its head, where an encrypted one is a tunnel.
func drawPlainLine(ctx *paintengine2d.Context, lk style.LookAndFeel, a, b paintengine2d.Point) {
	ink := inksOf(lk).muted
	h := style.Dip(lk, 7)
	dir := b.Sub(a).Normalize()
	ctx.DrawLine(a, b.Sub(dir.Mul(h/2)), paintengine2d.StrokePaint(ink, style.Dip(lk, 1.75)))
	head := paintengine2d.NewPath()
	head.MoveTo(b.X, b.Y)
	head.LineTo(b.X-dir.X*h-dir.Y*h*0.6, b.Y-dir.Y*h+dir.X*h*0.6)
	head.LineTo(b.X-dir.X*h+dir.Y*h*0.6, b.Y-dir.Y*h-dir.X*h*0.6)
	head.Close()
	ctx.DrawPath(head, paintengine2d.Fill(ink))
}
