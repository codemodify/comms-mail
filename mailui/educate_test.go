package mailui

import (
	"slices"
	"strings"
	"testing"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Educate is one picture: the three hops, numbered 1 to 3 and each with
// its standards drawn on it, and the look-alike, 4, coming in from a
// stranger. Everything is inside the picture; the tags clear of the discs,
// the numbers, the names and one another; the look-alike's line clear of
// the names and the tags. Under it a line for each number, and a word on
// every standard the picture shows or the lines name, and on none they do
// not. All of it at a wide window and one as narrow as Settings goes,
// nothing cut short, and the picture's box as tall as what it draws.
func TestEducateIsOnePicture(t *testing.T) {
	for _, width := range []int{330, 560} { // Educate's pane, narrowest and usual
		a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
		w, err := a.NewWindow(platform.WindowOptions{Title: "Educate", Width: width, Height: 900, Headless: true})
		if err != nil {
			t.Fatal(err)
		}
		page := educateSection()
		w.SetContent(page)
		a.PumpOnce()
		var scenes []*hopsScene
		var numbers []int
		var titles []*strong
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *hopsScene:
				scenes = append(scenes, v)
			case *numberMark:
				numbers = append(numbers, v.n)
			case *strong:
				titles = append(titles, v)
			}
		})
		if len(scenes) != 1 {
			t.Fatalf("at %d: %d pictures", width, len(scenes))
		}
		if len(hops) != 3 || !slices.Equal(numbers, []int{1, 2, 3, 4}) {
			t.Fatalf("at %d: numbered %v, for %d hops and the look-alike", width, numbers, len(hops))
		}
		sc := scenes[0]
		b := sc.Bounds()
		if need := sc.Measure(layout.Constraints{MaxW: b.Dx(), MaxH: -1}); need.Y > b.Dy()+0.5 {
			t.Errorf("at %d: the picture needs %v and has %v", width, need.Y, b.Dy())
		}
		g := sc.layoutAt(b.Dx())
		local := sc.LocalBounds()
		inside := func(r paintengine2d.Rect) bool {
			return r.Min.X >= local.Min.X && r.Max.X <= local.Max.X && r.Min.Y >= local.Min.Y && r.Max.Y <= local.Max.Y
		}
		font := sc.Look().Font()
		fh := font.Height()

		// The names, the stranger's words and DNS's, where they are drawn.
		var words []paintengine2d.Rect
		centred := func(lines []string, cx, y float32) float32 {
			for _, l := range lines {
				if strings.HasSuffix(l, "…") {
					t.Errorf("at %d: words are cut: %q", width, l)
				}
				r := paintengine2d.XYWH(cx-font.Advance(l)/2, y, font.Advance(l), fh)
				if !inside(r) {
					t.Errorf("at %d: %q is outside the picture", width, l)
				}
				words = append(words, r)
				y += fh
			}
			return y
		}
		for i := range sceneNames {
			centred(g.labels[i], g.x(float32(i)), g.labelsY)
		}
		under := g.rowY + g.dnd/2 + style.Dip(sc.Look(), 4)
		centred(g.fake, g.x(0.5), centred(g.stranger, g.x(0.5), under))
		centred(g.dnsLabel, g.x(2), under)
		if strings.Join(g.fake, "") != fakeDomain {
			t.Errorf("at %d: the look-alike shows %q", width, g.fake)
		}

		// What the tags keep clear of: the discs, the numbers, the words.
		disc := func(cx, cy, d float32) paintengine2d.Rect { return paintengine2d.XYWH(cx-d/2, cy-d/2, d, d) }
		var discs []paintengine2d.Rect
		for i := range sceneNames {
			discs = append(discs, disc(g.x(float32(i)), g.y1, g.d))
		}
		discs = append(discs, disc(g.x(0.5), g.rowY, g.dnd), disc(g.x(2), g.rowY, g.dnd))
		keepClear := append(slices.Clone(words), discs...)
		marks := sc.marks(g, local)
		m := style.Dip(sc.Look(), markSize)
		var numberBoxes []paintengine2d.Rect
		for i, mk := range marks {
			r := paintengine2d.XYWH(mk.x-m/2, mk.y-m/2, m, m)
			if mk.n != i+1 || !inside(r) {
				t.Errorf("at %d: number %d is at %v, outside the picture", width, mk.n, r)
			}
			for _, o := range numberBoxes {
				if r.Overlaps(o) {
					t.Errorf("at %d: number %d is over another", width, mk.n)
				}
			}
			for _, o := range append(slices.Clone(words), discs...) {
				if r.Overlaps(o) {
					t.Errorf("at %d: number %d is over words or a disc", width, mk.n)
				}
			}
			numberBoxes = append(numberBoxes, r)
		}
		keepClear = append(keepClear, numberBoxes...)

		drawn := map[string]bool{"DNS": len(g.dnsLabel) == 1 && g.dnsLabel[0] == "DNS"}
		var chipBoxes []paintengine2d.Rect
		for i, c := range g.chips {
			drawn[c.name] = true
			chipBoxes = append(chipBoxes, c.r)
			if !inside(c.r) {
				t.Errorf("at %d: %s is outside the picture: %v", width, c.name, c.r)
			}
			for _, k := range keepClear {
				if c.r.Overlaps(k) {
					t.Errorf("at %d: %s is over a disc, a number or words", width, c.name)
				}
			}
			for _, o := range g.chips[i+1:] {
				if c.r.Overlaps(o.r) {
					t.Errorf("at %d: %s and %s overlap", width, c.name, o.name)
				}
			}
		}

		// The look-alike's line: from the stranger, through its number,
		// into your server, crossing no words and no tags.
		fp := sc.fakePath(g)
		if mk := marks[3]; fp[0].X != mk.x || fp[1].X != mk.x || mk.y > fp[0].Y || mk.y < fp[1].Y {
			t.Errorf("at %d: 4 is not on the look-alike's line", width)
		}
		if end := fp[len(fp)-1]; end.Sub(paintengine2d.Pt(g.x(1), g.y1)).Len() > g.d/2+style.Dip(sc.Look(), 4) {
			t.Errorf("at %d: the look-alike's line ends at %v, not at your server", width, end)
		}
		for i := 1; i < len(fp); i++ {
			for k := float32(0); k <= 1; k += 0.01 {
				p := fp[i-1].Lerp(fp[i], k)
				for _, r := range append(slices.Clone(words), chipBoxes...) {
					if r.Inset(-1).Contains(p) {
						t.Errorf("at %d: the look-alike's line crosses %v at %v", width, r, p)
						break
					}
				}
			}
		}

		// Each hop's tags over it, centred on it; DNS's under it, the end
		// to end ones over everything.
		groups := map[int][]paintengine2d.Rect{}
		names := map[int][]string{}
		for _, c := range g.chips {
			mid := c.r.Center()
			k := -1 // over everything
			switch {
			case mid.Y > g.rowY:
				k = 3
			case mid.Y < g.y1 && c.r.Min.Y > g.spanY:
				k = int(mid.X/g.slot - 0.5) // between disc k and k+1
			}
			groups[k] = append(groups[k], c.r)
			names[k] = append(names[k], c.name)
		}
		check := func(k int, want []string, cx float32) {
			if !slices.Equal(names[k], want) {
				t.Errorf("at %d: tags %v where %v belong", width, names[k], want)
				return
			}
			box := groups[k][0]
			for _, r := range groups[k] {
				box = box.Union(r)
			}
			if c := box.Center().X; c < cx-1 || c > cx+1 {
				t.Errorf("at %d: %v are centred on %v, not %v", width, want, c, cx)
			}
		}
		for i, h := range hops {
			check(i, h.wire, g.x(float32(i)+0.5))
		}
		check(3, asked(), g.x(2))
		check(-1, endToEnd, g.x(1.5))

		// The words and the picture name the same standards, and the
		// lines under it none without a word.
		said := map[string]bool{}
		for _, s := range standards {
			for _, n := range s.names {
				said[n] = true
				if !drawn[n] {
					t.Errorf("at %d: a word on %s, which the picture does not show", width, n)
				}
			}
		}
		for n := range drawn {
			if !said[n] {
				t.Errorf("at %d: the picture shows %s, and no word on it", width, n)
			}
		}
		for _, h := range append(slices.Clone(hops), lookAlike) {
			for _, n := range h.tags() {
				if !said[n] {
					t.Errorf("at %d: %q names %s, and no word on it", width, h.title, n)
				}
			}
		}
		if !strings.Contains(strings.Join(lookAlike.says, " "), fakeDomain) {
			t.Errorf("at %d: the look-alike's words do not name %s", width, fakeDomain)
		}

		if len(titles) != len(hops)+1 {
			t.Errorf("at %d: %d titles for %d hops and the look-alike", width, len(titles), len(hops))
		}
		for _, s := range titles {
			if strings.Join(s.lines, " ") != s.text {
				t.Errorf("at %d: a title is cut: %q", width, s.lines)
			}
		}
		var last *widgets.Label
		widget.Walk(page, func(c widget.Component) {
			if l, ok := c.(*widgets.Label); ok && l.Text == lastWord {
				last = l
			}
		})
		if last == nil {
			t.Errorf("at %d: no last word", width)
		}
		w.Close()
	}
}
