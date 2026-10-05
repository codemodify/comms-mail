package mailui

import (
	"math"
	"strings"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/style"
)

// The pictures Settings › Security › Educate draws: the toolkit's icons,
// and line drawings, in the same weight, of what it has no icon for — a
// server, a key, a seal, an open padlock, a picture. They are drawn, not
// shipped as images: in the look's own colours, so they read in every
// theme and at every scale.

// pict is a picture.
type pict int

const (
	pictNone pict = iota
	pictPerson
	pictServer
	pictLetter
	pictKey
	pictLock
	pictPadlockOpen
	pictSeal
	pictEye
	pictCheck
	pictWarning
	pictPicture
)

// tone is what a part of a picture means: plain, good or bad.
type tone int

const (
	tonePlain tone = iota
	toneGood
	toneBad
)

// inks are a picture's colours, from the look.
type inks struct {
	text, muted, line, good, bad, disc, discEdge paintengine2d.Color
}

func inksOf(lk style.LookAndFeel) inks {
	p := lk.Palette()
	return inks{
		text: p.Text, muted: p.TextMuted, line: p.TextMuted,
		good: p.Ink(p.Success), bad: p.Ink(p.Danger),
		disc: p.Field, discEdge: p.Border,
	}
}

func (c inks) of(t tone) paintengine2d.Color {
	switch t {
	case toneGood:
		return c.good
	case toneBad:
		return c.bad
	}
	return c.text
}

// drawPict draws p in box in ink.
func drawPict(ctx *paintengine2d.Context, lk style.LookAndFeel, p pict, box paintengine2d.Rect, ink paintengine2d.Color) {
	icon := func(i style.ToolIcon) { style.DrawToolIcon(ctx, box, i, ink, style.IconSetOf(lk)) }
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
	default:
		drawLines(ctx, p, box, ink)
	}
}

// drawLines draws the pictures the toolkit has no icon for, in a 24-unit
// box, as the icons are drawn: strokes of one weight.
func drawLines(ctx *paintengine2d.Context, p pict, box paintengine2d.Rect, ink paintengine2d.Color) {
	u := box.Dx() / 24
	at := func(x, y float32) paintengine2d.Point { return paintengine2d.Pt(box.Min.X+x*u, box.Min.Y+y*u) }
	pen := paintengine2d.StrokePaint(ink, max(1, 1.75*u))
	line := func(x0, y0, x1, y1 float32) { ctx.DrawLine(at(x0, y0), at(x1, y1), pen) }
	rrect := func(x, y, w, h, r float32) {
		ctx.DrawRoundRect(paintengine2d.XYWH(box.Min.X+x*u, box.Min.Y+y*u, w*u, h*u), r*u, r*u, pen)
	}
	circle := func(x, y, r float32) { ctx.DrawCircle(at(x, y), r*u, pen) }
	switch p {
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
	}
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
