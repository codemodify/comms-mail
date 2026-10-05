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
// once each — on their lines, or by their names — what its symbols mean,
// and a numbered line for each step. In the picture nothing is outside
// it, cut short, or over anything else; tunnels and lines run through
// nothing but their own number and envelope. The message is an envelope
// carrying what it should at each step: your seal and padlock from the
// start, your domain's postmark from your server on, the attacker's only
// its own domain's postmark. Every step's standards are drawn by it, and
// its alternatives stand across its line: one over the other by a line
// across, side by side by a line down — OpenPGP and S/MIME either side
// of the line from you to the recipient. Every step names its standards
// in its words, its alternatives joined by "or". All of it at a wide
// window, a middling one and the narrowest the picture takes, its box as
// tall as what it draws.
func TestEducateIsOnePicture(t *testing.T) {
	probe := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	pw, err := probe.NewWindow(platform.WindowOptions{Title: "Educate", Width: 560, Height: 600, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	ps := newRouteScene()
	pw.SetContent(ps)
	probe.PumpOnce()
	narrowest := int(ps.MinWidth()) + 9 // the page pads it, 4 a side
	pw.Close()
	if narrowest > 400 {
		t.Fatalf("the picture takes %d at least", narrowest)
	}
	for _, width := range []int{narrowest, 460, 560} {
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
		var keys []*symbolItem
		var letters []string
		ors := 0
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *routeScene:
				scenes = append(scenes, v)
			case *numberMark:
				numbers = append(numbers, v.n)
			case *strong:
				titles = append(titles, v)
			case *symbolItem:
				keys = append(keys, v)
			case *widgets.Label:
				if v.Text == "or" {
					ors++
				}
			case *letter:
				letters = append(letters, v.text)
			}
		})
		if len(scenes) != 1 || len(steps) != 8 || !slices.Equal(numbers, []int{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Fatalf("at %d: %d pictures, steps numbered %v", width, len(scenes), numbers)
		}
		var wantLetters []string
		for _, st := range steps {
			for i := range st.points {
				wantLetters = append(wantLetters, string(rune('a'+i)))
			}
		}
		if !slices.Equal(letters, wantLetters) {
			t.Errorf("at %d: the points are lettered %v, want %v", width, letters, wantLetters)
		}
		if len(keys) != len(symbols) || len(symbols) != 6 {
			t.Errorf("at %d: %d symbols explained of %d", width, len(keys), len(symbols))
		}
		wantOrs := 0
		for _, st := range steps {
			for _, grp := range st.tags {
				wantOrs += len(grp) - 1
			}
		}
		if ors != wantOrs || wantOrs == 0 {
			t.Errorf("at %d: %d alternatives joined by \"or\", want %d", width, ors, wantOrs)
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
		// are — with what it is, and the line it is on.
		type box struct {
			what   string
			r      paintengine2d.Rect
			radius float32 // a circle's, in r
			on     int     // the step whose line it is on, 0 for none
		}
		var discs, texts, marks, chips, envs []box
		for i, d := range g.discs {
			discs = append(discs, box{"disc " + string(rune('0'+i)) + " " + d.text, paintengine2d.XYWH(d.c.X-d.d/2, d.c.Y-d.d/2, d.d, d.d), d.d / 2, 0})
			if d.text != "" && font.Advance(d.text) > d.d-style.Dip(lk, 4) {
				t.Errorf("at %d: %q does not fit its disc", width, d.text)
			}
		}
		for _, tx := range g.texts {
			if strings.HasSuffix(tx.text, "…") {
				t.Errorf("at %d: %q is cut", width, tx.text)
			}
			texts = append(texts, box{"words " + tx.text, paintengine2d.XYWH(tx.at.X, tx.at.Y, font.Advance(tx.text), font.Height()), 0, 0})
		}
		seen := map[int]int{}
		onLine := map[int]bool{2: true, 4: true, 6: true, 8: true}
		for _, mk := range g.marks {
			seen[mk.n]++
			on := 0
			if onLine[mk.n] {
				on = mk.n
			}
			marks = append(marks, box{"number " + string(rune('0'+mk.n)), paintengine2d.XYWH(mk.x-m/2, mk.y-m/2, m, m), m / 2, on})
		}
		for n := 1; n <= 8; n++ {
			if seen[n] != 1 {
				t.Errorf("at %d: %d is in the picture %d times", width, n, seen[n])
			}
		}
		for _, c := range g.chips {
			chips = append(chips, box{"tag " + c.name, c.r, 0, 0})
		}
		for _, e := range g.envs {
			envs = append(envs, box{"envelope " + string(rune('0'+e.step)), envBox(lk, e.c), 0, e.step})
		}
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
		all := slices.Concat(discs, texts, marks, chips, envs)
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

		// The lines and tunnels: through nothing but their own number and
		// envelope; a line through no tunnel either.
		stepOf := map[routeLine]int{}
		for _, e := range g.envs {
			for _, l := range g.lines {
				if l.head && l.ink != inkAccent && (l.tube > 0 && l.body().Contains(e.c) || l.tube == 0 && distToSegment(e.c, l.a, l.b) < 3) {
					stepOf[l] = e.step
				}
			}
		}
		var tubes []box
		for _, l := range g.lines {
			if l.tube > 0 {
				tubes = append(tubes, box{"tunnel " + string(rune('0'+stepOf[l])), l.body(), 0, stepOf[l]})
			}
		}
		for _, l := range g.lines {
			own := stepOf[l]
			if l.tube > 0 {
				body := box{r: l.body()}
				for _, x := range slices.Concat(discs, texts, marks, chips, envs, tubes) {
					if (x.on != 0 && x.on == own) || x.r == body.r {
						continue
					}
					if meet(body, x) {
						t.Errorf("at %d: tunnel %d runs through %s", width, own, x.what)
					}
				}
				continue
			}
			for k := float32(0.01); k < 1; k += 0.01 {
				p := l.a.Lerp(l.b, k)
				for _, x := range slices.Concat(discs, texts, marks, chips, envs, tubes) {
					if x.on != 0 && x.on == own {
						continue
					}
					in := x.r.Inset(-1).Contains(p)
					if x.radius > 0 {
						in = p.Sub(x.r.Center()).Len() < x.radius
					}
					if in {
						t.Errorf("at %d: a line from %v to %v runs through %s", width, l.a, l.b, x.what)
						break
					}
				}
			}
		}
		// Each carried message: inside its tunnel, or on its line; with
		// what it carries by then; its number on the line too.
		want := map[int]envelope{2: {lock: true, seal: true}, 4: {lock: true, seal: true, stamp: true}, 6: {lock: true, seal: true, stamp: true}, 8: {stamp: true, bad: true}}
		got := map[int]envelope{}
		for _, e := range g.envs {
			got[e.step] = e.e
		}
		for n, e := range want {
			if got[n] != e {
				t.Errorf("at %d: step %d carries %+v, want %+v", width, n, got[n], e)
			}
		}
		for l, n := range stepOf {
			for _, e := range envs {
				if e.on == n && l.tube > 0 && (e.r.Min.X < l.body().Min.X || e.r.Max.X > l.body().Max.X || e.r.Min.Y < l.body().Min.Y-1 || e.r.Max.Y > l.body().Max.Y+1) {
					t.Errorf("at %d: envelope %d sticks out of its tunnel: %v in %v", width, n, e.r, l.body())
				}
			}
			for _, mk := range g.marks {
				if mk.n == n && distToSegment(paintengine2d.Pt(mk.x, mk.y), l.a, l.b) > 0.5 {
					t.Errorf("at %d: %d is not on its line", width, n)
				}
			}
		}
		if len(stepOf) != 4 {
			t.Errorf("at %d: %d lines carry the message, want 4", width, len(stepOf))
		}
		for _, rn := range routeNames {
			found := false
			for _, mk := range g.marks {
				for _, tx := range g.texts {
					if mk.n == rn.step && strings.HasPrefix(rn.name, tx.text) && tx.at.X > mk.x && tx.at.X-mk.x < m && tx.at.Y < mk.y && tx.at.Y+font.Height() > mk.y {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("at %d: %d is not by %q", width, rn.step, rn.name)
			}
		}

		// The tags: each step's groups, drawn by it; alternatives across
		// its line.
		byGroup := map[string][][]string{}
		chipsOf := map[string]map[int][]placedChip{}
		for _, c := range g.chips {
			if chipsOf[c.group] == nil {
				chipsOf[c.group] = map[int][]placedChip{}
			}
			chipsOf[c.group][c.alt] = append(chipsOf[c.group][c.alt], c)
		}
		for grp, alts := range chipsOf {
			for i := 0; i < len(alts); i++ {
				var names []string
				for _, c := range alts[i] {
					names = append(names, c.name)
				}
				byGroup[grp] = append(byGroup[grp], names)
			}
		}
		wantTags := map[string][][]string{"e2e": steps[0].tags, "2": steps[1].tags, "4": steps[3].tags, "6": steps[5].tags, "dns": dnsTags}
		for grp, tags := range wantTags {
			if !slices.EqualFunc(byGroup[grp], tags, slices.Equal) {
				t.Errorf("at %d: %s has tags %v, not %v", width, grp, byGroup[grp], tags)
			}
		}
		across := map[string]bool{"2": true, "6": true, "4": false, "e2e": false}
		for grp, horizontal := range across {
			for _, alts := range chipsOf[grp] {
				for i := 1; i < len(alts); i++ {
					p, q := alts[i-1].r, alts[i].r
					if horizontal && (abs32(p.Center().X-q.Center().X) > 0.5 || q.Min.Y <= p.Max.Y-0.5) {
						t.Errorf("at %d: %s and %s are not one over the other", width, alts[i-1].name, alts[i].name)
					}
					if !horizontal && (abs32(p.Center().Y-q.Center().Y) > 0.5 || q.Min.X <= p.Max.X-0.5) {
						t.Errorf("at %d: %s and %s are not side by side", width, alts[i-1].name, alts[i].name)
					}
				}
			}
		}
		e2eLine := g.lines[0]
		for _, alts := range chipsOf["e2e"] {
			if len(alts) != 2 || !(alts[0].r.Max.X < e2eLine.a.X && alts[1].r.Min.X > e2eLine.a.X) {
				t.Errorf("at %d: the line from you to the recipient does not go between %v", width, alts)
			}
		}
		if !slices.ContainsFunc(g.texts, func(tx placedText) bool { return tx.text == fakeDomain && tx.bad }) {
			t.Errorf("at %d: the attacker's %s is not drawn", width, fakeDomain)
		}
		w.Close()
	}
	for i, st := range steps {
		says := strings.ToLower(strings.Join(st.points, "\n"))
		for _, n := range flat(st.tags) {
			if !strings.Contains(says, strings.ToLower(n)) {
				t.Errorf("step %d (%s) has the tag %s and does not say it", i+1, st.title, n)
			}
		}
		// A few words each.
		for _, p := range st.points {
			if n := len(strings.Fields(p)); n > 9 {
				t.Errorf("step %d: %q is %d words", i+1, p, n)
			}
		}
	}
	if !strings.Contains(strings.Join(steps[7].points, " "), fakeDomain) {
		t.Errorf("step 8 does not name %s", fakeDomain)
	}
}

func abs32(v float32) float32 { return max(v, -v) }

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
