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
		var row []string
		for _, w := range g.words {
			row = append(row, w.text)
			r := paintengine2d.XYWH(w.at.X, w.at.Y, font.Advance(w.text), fh)
			if strings.HasSuffix(w.text, "…") || !inside(r) {
				t.Errorf("at %d: %q is cut, or outside the picture", width, w.text)
			}
			for _, o := range words {
				if r.Overlaps(o) {
					t.Errorf("at %d: %q is over other words", width, w.text)
				}
			}
			words = append(words, r)
		}
		if want := slices.Concat(g.stranger, g.fake, g.dnsLabel); !slices.Equal(row, want) {
			t.Errorf("at %d: the row under the names says %q, not %q", width, row, want)
		}
		if strings.Join(g.fake, "") != fakeDomain {
			t.Errorf("at %d: the look-alike shows %q", width, g.fake)
		}
		// Both ways of laying out that row are tried: beside at the usual
		// width, under at the narrowest.
		if g.beside != (width == 560) {
			t.Errorf("at %d: the row's words and tags beside their discs: %v", width, g.beside)
		}

		// What the tags keep clear of: the discs, the numbers, the words.
		disc := func(cx, cy, d float32) paintengine2d.Rect { return paintengine2d.XYWH(cx-d/2, cy-d/2, d, d) }
		var discs []paintengine2d.Rect
		for i := range sceneNames {
			discs = append(discs, disc(g.x(float32(i)), g.y1, g.d))
		}
		discs = append(discs, disc(g.x(0.5), g.rowY, g.dnd), disc(g.x(2), g.rowY, g.dnd))
		for _, w := range words {
			for _, d := range discs {
				if w.Overlaps(d) {
					t.Errorf("at %d: words %v are over a disc", width, w)
				}
			}
		}
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
		// DNS's lines cross no words and no tags either.
		for _, l := range sc.dnsLines(g) {
			for k := float32(0); k <= 1; k += 0.01 {
				p := l[0].Lerp(l[1], k)
				for _, r := range append(slices.Clone(words), chipBoxes...) {
					if r.Inset(-1).Contains(p) {
						t.Errorf("at %d: a line to DNS crosses %v at %v", width, r, p)
						break
					}
				}
			}
		}
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
			case c.r.Min.Y > g.labelsFoot(fh):
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
		if g.beside {
			// DNS's from its right, level with it.
			if !slices.Equal(names[3], asked()) {
				t.Errorf("at %d: tags %v by DNS", width, names[3])
			} else {
				box := groups[3][0]
				for _, r := range groups[3] {
					box = box.Union(r)
				}
				if x := g.x(2) + g.dnd/2 + style.Dip(sc.Look(), 8); box.Min.X < x-1 || box.Min.X > x+1 {
					t.Errorf("at %d: DNS's tags start at %v, not %v", width, box.Min.X, x)
				}
				if c := box.Center().Y; c < g.rowY-1 || c > g.rowY+1 {
					t.Errorf("at %d: DNS's tags are centred on %v, not level with it at %v", width, c, g.rowY)
				}
			}
		} else {
			check(3, asked(), g.x(2))
		}
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

// The picture stays in view while the words under it scroll — when the
// page leaves the words room — and scrolls with them when it does not,
// changing back and forth as the window is made taller and shorter.
func TestEducatePinsThePicture(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Educate", Width: 560, Height: 900, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	page := educateSection().(*educatePage)
	w.SetContent(page)
	a.PumpOnce()
	var title *widgets.Label
	widget.Walk(page, func(c widget.Component) {
		if l, ok := c.(*widgets.Label); ok && l.Text == "What happens" {
			title = l
		}
	})
	pinned := func(tall bool) {
		t.Helper()
		if page.pinned != tall || widget.Contains(page.scroll, page.scene) == tall {
			t.Fatalf("tall %v: pinned %v, the picture in what scrolls %v", tall, page.pinned, widget.Contains(page.scroll, page.scene))
		}
		if tall && page.scroll.Bounds().Min.Y < page.scene.Bounds().Max.Y {
			t.Fatalf("the words scroll over the picture: %v under %v", page.scroll.Bounds(), page.scene.Bounds())
		}
		page.scroll.ScrollTo(0)
		a.PumpOnce()
		scene, words := widget.DeviceOrigin(page.scene), widget.DeviceOrigin(title)
		page.scroll.ScrollTo(150)
		a.PumpOnce()
		if page.scroll.OffsetY == 0 {
			t.Fatalf("tall %v: the words did not scroll", tall)
		}
		if moved := widget.DeviceOrigin(page.scene) != scene; moved == tall {
			t.Errorf("tall %v: the picture moved with the words: %v", tall, moved)
		}
		if widget.DeviceOrigin(title) == words {
			t.Errorf("tall %v: the words stayed where they were", tall)
		}
	}
	pinned(true)
	w.SetSize(560, 400)
	a.PumpOnce()
	pinned(false)
	w.SetSize(560, 900)
	a.PumpOnce()
	pinned(true)
}
