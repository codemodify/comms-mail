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

// Educate is every lesson in turn — its heading, its picture, its words —
// and a picture's words are never cut short, in a wide window or one as
// narrow as Settings goes, and its box is as tall as what it draws.
func TestEducateLessons(t *testing.T) {
	ls := lessons()
	if len(ls) < 10 {
		t.Fatalf("%d lessons", len(ls))
	}
	for _, l := range ls {
		if l.title == "" || len(l.says) == 0 {
			t.Fatalf("an empty lesson: %+v", l)
		}
		if l.fig != nil {
			if f := l.fig(); len(f.links) != len(f.nodes)-1 {
				t.Errorf("%s: %d nodes, %d links", l.title, len(f.nodes), len(f.links))
			}
		}
	}
	for _, width := range []int{330, 560} { // Educate's pane, narrowest and usual
		a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
		w, err := a.NewWindow(platform.WindowOptions{Title: "Educate", Width: width, Height: 900, Headless: true})
		if err != nil {
			t.Fatal(err)
		}
		page := educateSection()
		w.SetContent(page)
		a.PumpOnce()
		var titles []string
		var figs []*figure
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *widgets.Label:
				if v.Title {
					titles = append(titles, v.Text)
				}
			case *figure:
				figs = append(figs, v)
			}
		})
		want := 0
		for _, l := range ls {
			if l.fig != nil {
				want++
			}
		}
		if len(titles) != len(ls) || len(figs) != want {
			t.Fatalf("at %d: %d headings, %d pictures", width, len(titles), len(figs))
		}
		for _, f := range figs {
			b := f.Bounds()
			if need := f.Measure(layout.Constraints{MaxW: b.Dx(), MaxH: -1}); need.Y > b.Dy()+0.5 {
				t.Errorf("at %d: a picture needs %v and has %v", width, need.Y, b.Dy())
			}
			g := f.layoutAt(b.Dx())
			for _, lines := range append(g.linkLines, g.nodeLines...) {
				for _, line := range lines {
					if strings.HasSuffix(line, "…") {
						t.Errorf("at %d: a picture's words are cut: %q", width, line)
					}
				}
			}
		}
		w.Close()
	}
}
