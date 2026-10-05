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

// Educate is one picture of a message's route, its steps numbered 1 to 8
// once each — on their arrows, or by their names — and a numbered line
// for each step under it. In the picture nothing is outside it, cut
// short, or over anything else, and no line runs through words, tags,
// discs or numbers but its own (OpenPGP and S/MIME sit on the line from
// you to the recipient). Every tag drawn is one of its step's, and every
// step names each of its standards in its words. All of it at a wide
// window, a middling one and one as narrow as Settings goes, the
// picture's box as tall as what it draws.
func TestEducateIsOnePicture(t *testing.T) {
	for _, width := range []int{330, 450, 560} {
		a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
		w, err := a.NewWindow(platform.WindowOptions{Title: "Educate", Width: width, Height: 900, Headless: true})
		if err != nil {
			t.Fatal(err)
		}
		page := educateSection()
		w.SetContent(page)
		a.PumpOnce()
		var scenes []*routeScene
		var numbers []int
		var titles []*strong
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *routeScene:
				scenes = append(scenes, v)
			case *numberMark:
				numbers = append(numbers, v.n)
			case *strong:
				titles = append(titles, v)
			}
		})
		if len(scenes) != 1 || len(steps) != 8 || !slices.Equal(numbers, []int{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Fatalf("at %d: %d pictures, steps numbered %v", width, len(scenes), numbers)
		}
		for _, s := range titles {
			if strings.Join(s.lines, " ") != s.text {
				t.Errorf("at %d: a title is cut: %q", width, s.lines)
			}
		}
		sc := scenes[0]
		b := sc.Bounds()
		if need := sc.Measure(layout.Constraints{MaxW: b.Dx(), MaxH: -1}); need.Y > b.Dy()+0.5 {
			t.Errorf("at %d: the picture needs %v and has %v", width, need.Y, b.Dy())
		}
		g := sc.layoutAt(b.Dx())
		local := sc.LocalBounds()
		lk := sc.Look()
		font := lk.Font()
		m := style.Dip(lk, markSize)

		// Everything, as boxes — discs and numbers as the circles they
		// are — with what it is.
		type box struct {
			what   string
			r      paintengine2d.Rect
			group  string  // a tag's
			radius float32 // a circle's, in r
		}
		var discs, texts, marks, chips []box
		for i, d := range g.discs {
			discs = append(discs, box{"disc " + string(rune('0'+i)) + " " + d.text, paintengine2d.XYWH(d.c.X-d.d/2, d.c.Y-d.d/2, d.d, d.d), "", d.d / 2})
			if d.text != "" && font.Advance(d.text) > d.d-style.Dip(lk, 4) {
				t.Errorf("at %d: %q does not fit its disc", width, d.text)
			}
		}
		for _, tx := range g.texts {
			if strings.HasSuffix(tx.text, "…") {
				t.Errorf("at %d: %q is cut", width, tx.text)
			}
			texts = append(texts, box{"words " + tx.text, paintengine2d.XYWH(tx.at.X, tx.at.Y, font.Advance(tx.text), font.Height()), "", 0})
		}
		seen := map[int]int{}
		for _, mk := range g.marks {
			seen[mk.n]++
			marks = append(marks, box{"number " + string(rune('0'+mk.n)), paintengine2d.XYWH(mk.x-m/2, mk.y-m/2, m, m), "", m / 2})
		}
		for n := 1; n <= 8; n++ {
			if seen[n] != 1 {
				t.Errorf("at %d: %d is in the picture %d times", width, n, seen[n])
			}
		}
		for _, c := range g.chips {
			chips = append(chips, box{"tag " + c.name, c.r, c.group, 0})
		}
		// meet says x and y share a point: circles as circles.
		meet := func(x, y box) bool {
			switch {
			case x.radius > 0 && y.radius > 0:
				return x.r.Center().Sub(y.r.Center()).Len() < x.radius+y.radius
			case x.radius > 0:
				return circleMeets(x.r.Center(), x.radius, y.r)
			case y.radius > 0:
				return circleMeets(y.r.Center(), y.radius, x.r)
			}
			return x.r.Overlaps(y.r)
		}
		inside := func(x box, p paintengine2d.Point) bool {
			if x.radius > 0 {
				return p.Sub(x.r.Center()).Len() < x.radius
			}
			return x.r.Inset(-1).Contains(p)
		}
		all := slices.Concat(discs, texts, marks, chips)
		for i, x := range all {
			if x.r.Min.X < local.Min.X || x.r.Max.X > local.Max.X || x.r.Min.Y < local.Min.Y || x.r.Max.Y > local.Max.Y {
				t.Errorf("at %d: %s is outside the picture: %v", width, x.what, x.r)
			}
			for _, y := range all[i+1:] {
				if meet(x, y) {
					t.Errorf("at %d: %s and %s overlap", width, x.what, y.what)
				}
			}
		}
		// The lines: through nothing but the tags they run behind, and the
		// numbers on them.
		for _, l := range g.lines {
			for k := float32(0.01); k < 1; k += 0.01 {
				p := l.a.Lerp(l.b, k)
				for _, x := range slices.Concat(discs, texts, chips) {
					if x.group != "" && x.group == l.under {
						continue
					}
					if inside(x, p) {
						t.Errorf("at %d: a line from %v to %v runs through %s", width, l.a, l.b, x.what)
						break
					}
				}
			}
		}
		// The steps on arrows have their numbers on them; the others are
		// by their names.
		on := map[int]int{2: inkGood, 4: inkGood, 6: inkGood, 8: inkBad}
		for _, mk := range g.marks {
			c := paintengine2d.Pt(mk.x, mk.y)
			onLine := false
			for _, l := range g.lines {
				if l.head && l.ink == on[mk.n] && distToSegment(c, l.a, l.b) < 0.5 {
					onLine = true
				}
			}
			if _, arrow := on[mk.n]; arrow != onLine {
				t.Errorf("at %d: %d on an arrow: %v", width, mk.n, onLine)
			}
		}
		for i, rn := range routeNames {
			found := false
			for _, mk := range g.marks {
				if mk.n != rn.step {
					continue
				}
				for _, tx := range g.texts {
					if strings.HasPrefix(rn.name, tx.text) && tx.at.X > mk.x && tx.at.X-mk.x < m && tx.at.Y < mk.y && tx.at.Y+font.Height() > mk.y {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("at %d: %d is not by %q (%d)", width, rn.step, rn.name, i)
			}
		}

		// The tags, by where they are drawn, and the steps they belong to.
		byGroup := map[string][]string{}
		for _, c := range g.chips {
			byGroup[c.group] = append(byGroup[c.group], c.name)
		}
		belongs := map[string][]int{"e2e": {1, 7}, "2": {2}, "4": {4}, "6": {6}, "dns": {3, 5}}
		want := map[string][]string{"e2e": e2eTags, "2": hopTags[0], "4": hopTags[1], "6": hopTags[2], "dns": dnsTags}
		for grp, names := range want {
			if !slices.Equal(byGroup[grp], names) {
				t.Errorf("at %d: %s has tags %v, not %v", width, grp, byGroup[grp], names)
			}
			for _, n := range names {
				ok := false
				for _, i := range belongs[grp] {
					ok = ok || slices.Contains(steps[i-1].tags, n)
				}
				if !ok {
					t.Errorf("at %d: %s, drawn by %s, is no tag of steps %v", width, n, grp, belongs[grp])
				}
			}
		}
		if !slices.ContainsFunc(g.texts, func(tx placedText) bool { return tx.text == fakeDomain && tx.bad }) {
			t.Errorf("at %d: the attacker's %s is not drawn", width, fakeDomain)
		}
		w.Close()
	}
	for i, st := range steps {
		for _, n := range st.tags {
			if !strings.Contains(st.says, n) {
				t.Errorf("step %d (%s) has the tag %s and does not say it", i+1, st.title, n)
			}
		}
	}
	if !strings.Contains(steps[7].says, fakeDomain) {
		t.Errorf("step 8 does not name %s", fakeDomain)
	}
}

// circleMeets says the circle at c of radius rad and r share a point.
func circleMeets(c paintengine2d.Point, rad float32, r paintengine2d.Rect) bool {
	nx := max(r.Min.X, min(c.X, r.Max.X))
	ny := max(r.Min.Y, min(c.Y, r.Max.Y))
	return c.Sub(paintengine2d.Pt(nx, ny)).Len() < rad
}

// distToSegment is how far p is from the segment a–b.
func distToSegment(p, a, b paintengine2d.Point) float32 {
	ab := b.Sub(a)
	t := p.Sub(a).Dot(ab) / ab.Dot(ab)
	t = max(0, min(1, t))
	return p.Sub(a.Add(ab.Mul(t))).Len()
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
		if l, ok := c.(*widgets.Label); ok && l.Text == "Step by step" {
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
