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

// Educate is one picture of the three hops, numbered 1 to 3 and each with
// its standards drawn on it, inside the picture and clear of the discs,
// the numbers and one another; a line for each hop under it; and a word
// on every standard the picture shows, and on none it does not — in a
// wide window and one as narrow as Settings goes, nothing cut short and
// the picture's box as tall as what it draws.
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
		if len(hops) != 3 || !slices.Equal(numbers, []int{1, 2, 3}) {
			t.Fatalf("at %d: hops numbered %v, for %d hops", width, numbers, len(hops))
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

		// What the tags must keep clear of: the discs, and the numbers.
		var keepClear []paintengine2d.Rect
		for i := range sceneNames {
			cx := g.slot * (float32(i) + 0.5)
			keepClear = append(keepClear, paintengine2d.XYWH(cx-g.d/2, g.y1-g.d/2, g.d, g.d))
		}
		keepClear = append(keepClear, paintengine2d.XYWH(g.slot*2-g.dnd/2, g.dnsY-g.dnd/2, g.dnd, g.dnd))
		marks := sc.marks(g, local)
		m := style.Dip(sc.Look(), markSize)
		for i, mk := range marks {
			r := paintengine2d.XYWH(mk.x-m/2, mk.y-m/2, m, m)
			if mk.n != i+1 || !inside(r) {
				t.Errorf("at %d: number %d is at %v, outside the picture", width, mk.n, r)
			}
			keepClear = append(keepClear, r)
		}

		drawn := map[string]bool{"DNS": len(g.dnsLabel) == 1 && g.dnsLabel[0] == "DNS"}
		for i, c := range g.chips {
			drawn[c.name] = true
			if !inside(c.r) {
				t.Errorf("at %d: %s is outside the picture: %v", width, c.name, c.r)
			}
			for _, k := range keepClear {
				if c.r.Overlaps(k) {
					t.Errorf("at %d: %s is over a disc or a number", width, c.name)
				}
			}
			for _, o := range g.chips[i+1:] {
				if c.r.Overlaps(o.r) {
					t.Errorf("at %d: %s and %s overlap", width, c.name, o.name)
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
			case mid.Y > g.dnsY:
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
			check(i, h.wire, g.slot*float32(i+1))
		}
		check(3, asked(), g.slot*2)
		check(-1, endToEnd, g.slot*2)

		// The words and the picture name the same standards.
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

		for _, lines := range append(g.labels[:], g.dnsLabel) {
			for _, l := range lines {
				if strings.HasSuffix(l, "…") {
					t.Errorf("at %d: a name is cut: %q", width, l)
				}
			}
		}
		if len(titles) != len(hops) {
			t.Errorf("at %d: %d hop titles for %d hops", width, len(titles), len(hops))
		}
		for _, s := range titles {
			if strings.Join(s.lines, " ") != s.text {
				t.Errorf("at %d: a hop's title is cut: %q", width, s.lines)
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
