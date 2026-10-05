package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Educate is one picture, its numbers 1 to 8 each once, inside it and
// clear of one another, and a line for each number under it — in a wide
// window and one as narrow as Settings goes, its names never cut short
// and its box as tall as what it draws.
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
		var scenes []*educateScene
		var numbers []int
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *educateScene:
				scenes = append(scenes, v)
			case *numberMark:
				numbers = append(numbers, v.n)
			}
		})
		if len(scenes) != 1 {
			t.Fatalf("at %d: %d pictures", width, len(scenes))
		}
		if len(numbers) != len(lessons) || len(lessons) != 8 {
			t.Fatalf("at %d: %d numbered lines for %d lessons", width, len(numbers), len(lessons))
		}
		sc := scenes[0]
		b := sc.Bounds()
		if need := sc.Measure(layout.Constraints{MaxW: b.Dx(), MaxH: -1}); need.Y > b.Dy()+0.5 {
			t.Errorf("at %d: the picture needs %v and has %v", width, need.Y, b.Dy())
		}
		g := sc.layoutAt(b.Dx())
		local := sc.LocalBounds()
		marks := sc.marks(g, local)
		seen := map[int]bool{}
		m := style.Dip(sc.Look(), markSize)
		for i, mk := range marks {
			seen[mk.n] = true
			if mk.x-m/2 < local.Min.X || mk.x+m/2 > local.Max.X || mk.y-m/2 < local.Min.Y || mk.y+m/2 > local.Max.Y {
				t.Errorf("at %d: number %d is outside the picture", width, mk.n)
			}
			for _, other := range marks[i+1:] {
				if dx, dy := mk.x-other.x, mk.y-other.y; dx*dx+dy*dy < m*m {
					t.Errorf("at %d: numbers %d and %d overlap", width, mk.n, other.n)
				}
			}
		}
		for n := 1; n <= 8; n++ {
			if !seen[n] {
				t.Errorf("at %d: no %d in the picture", width, n)
			}
		}
		for _, lines := range append(g.labels[:], g.stranger, g.picture) {
			for _, l := range lines {
				if strings.HasSuffix(l, "…") {
					t.Errorf("at %d: a name is cut: %q", width, l)
				}
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
