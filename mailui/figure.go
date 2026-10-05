package mailui

import (
	"strings"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/style"
)

// The pictures Settings › Security › Educate draws: the toolkit's icons,
// and a line drawing, in the same weight, of what it has no icon for — a
// server. They are drawn, not shipped as images: in the look's own
// colours, so they read in every theme and at every scale.

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
