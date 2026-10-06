package mailui

import (
	"slices"
	"strings"
	"testing"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Educate is one picture of a message's route, its steps numbered 1 to 8
// once each — on their lines, or by their names — what its symbols mean,
// and a numbered line for each step. In the picture nothing is outside
// it, cut short, or over anything else; lines run through nothing but
// their own number and envelope. The message is an envelope
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
	if narrowest > 460 {
		t.Fatalf("the picture takes %d at least", narrowest)
	}
	for _, width := range []int{narrowest, 460, 560} {
		for _, les := range scenarios() {
			a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
			w, err := a.NewWindow(platform.WindowOptions{Title: "Educate", Width: width, Height: 900, Headless: true})
			if err != nil {
				t.Fatal(err)
			}
			page := educateSection()
			w.SetContent(page)
			a.PumpOnce()
			// Which message and road: the first lesson first, the others
			// by ticking and unticking.
			choose(t, a, page, les)
			steps := scenarioSteps(les)
			var scenes []*routeScene
			var numbers []int
			var titles []*strong
			var keys []*symbolItem
			var letters []string
			var stepTabs *widgets.TabView
			ors := 0
			widget.Walk(page, func(c widget.Component) {
				switch v := c.(type) {
				case *routeScene:
					scenes = append(scenes, v)
				case *symbolItem:
					keys = append(keys, v)
				case *widgets.TabView:
					stepTabs = v
				}
			})
			// The steps are a tab each, titled by their number: each one
			// seen by showing it.
			if stepTabs == nil || !slices.Equal(stepTabs.Bar().Titles, []string{"1", "2", "3", "4", "5", "6", "7", "8"}) {
				t.Fatalf("at %d: the steps are not tabs 1 to 8", width)
			}
			for i := range 8 {
				stepTabs.Select(i)
				a.PumpOnce()
				widget.Walk(stepTabs, func(c widget.Component) {
					switch v := c.(type) {
					case *numberMark:
						numbers = append(numbers, v.n)
					case *strong:
						titles = append(titles, v)
					case *widgets.Label:
						if v.Text == "or" {
							ors++
						}
					case *letter:
						letters = append(letters, v.text)
					}
				})
			}
			stepTabs.Select(0)
			a.PumpOnce()
			if len(scenes) != 1 || len(steps) != 8 || !slices.Equal(numbers, []int{1, 2, 3, 4, 5, 6, 7, 8}) {
				t.Fatalf("at %d: %d pictures, steps numbered %v", width, len(scenes), numbers)
			}
			var wantLetters []string
			for _, st := range steps {
				for i, p := range st.points {
					wantLetters = append(wantLetters, string(rune('a'+i)))
					for k := range p.sub {
						wantLetters = append(wantLetters, roman(k+1))
					}
				}
			}
			if !slices.Equal(letters, wantLetters) {
				t.Errorf("at %d: the points are lettered %v, want %v", width, letters, wantLetters)
			}
			wantOrs := 0
			for _, st := range steps {
				for _, grp := range st.tags {
					wantOrs += len(grp) - 1
				}
			}
			if ors != wantOrs {
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
			// What the symbols mean: all of them, whatever is drawn.
			var explained, every []string
			for _, k := range keys {
				explained = append(explained, k.sy.says)
			}
			for _, sy := range symbols {
				every = append(every, sy.says)
			}
			if !slices.Equal(explained, every) || len(every) != 5 {
				t.Errorf("at %d, %+v: the key explains %q, want %q", width, les, explained, every)
			}
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
				radius := d.d / 2 // a pill is a box
				if d.w > 0 {
					radius = 0
				}
				discs = append(discs, box{"disc " + string(rune('0'+i)) + " " + d.text, d.box(), radius, 0})
				if d.text != "" && font.Advance(d.text) > d.box().Dx()-style.Dip(lk, 4) {
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
			// A stop is where the message would have been, on step 6's line.
			stop := style.Dip(lk, stopSize)
			for _, st := range g.stops {
				envs = append(envs, box{"stop", paintengine2d.XYWH(st.X-stop/2, st.Y-stop/2, stop, stop), stop / 2, 6})
			}
			if (len(g.stops) == 1) != rejected(les) || len(g.stops) > 1 {
				t.Errorf("at %d, %+v: %d stops", width, les, len(g.stops))
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

			// The lines: through nothing but their own number and envelope.
			stepOf := map[routeLine]int{}
			carried := slices.Clone(g.envs)
			for _, st := range g.stops {
				carried = append(carried, placedEnv{c: st, step: 6})
			}
			for _, e := range carried {
				for _, l := range g.lines {
					if l.head && l.ink != inkAccent && distToSegment(e.c, l.a, l.b) < 3 {
						stepOf[l] = e.step
					}
				}
			}
			for _, l := range g.lines {
				own := stepOf[l]
				for k := float32(0.01); k < 1; k += 0.01 {
					p := l.a.Lerp(l.b, k)
					for _, x := range slices.Concat(discs, texts, marks, chips, envs) {
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
			// Each carried message: on its line; with
			// what it carries by then; its number on the line too.
			want := map[int]envelope{2: {lock: true, seal: true}, 4: {lock: true, seal: true, stamp: true}, 6: {lock: true, seal: true, stamp: true}, 8: {stamp: true, bad: true}}
			want[2] = envelope{lock: les.encrypted, seal: les.signed}
			want[4] = envelope{lock: les.encrypted, seal: les.signed, stamp: les.dkim}
			want[6] = want[4]
			got := map[int]envelope{}
			for _, e := range g.envs {
				got[e.step] = e.e
			}
			if rejected(les) {
				delete(want, 6) // stopped: it goes no further
			}
			for n, e := range want {
				if got[n] != e {
					t.Errorf("at %d: step %d carries %+v, want %+v", width, n, got[n], e)
				}
			}
			if _, carriedOn := got[6]; carriedOn == rejected(les) {
				t.Errorf("at %d, %+v: step 6 carries it: %v", width, les, carriedOn)
			}
			for l, n := range stepOf {
				if l.ink != inkBad && l.ink != inkGood || (l.ink == inkGood) != (les.tls && n != 8) {
					t.Errorf("at %d, %+v: step %d's arrow is not green with TLS, red without", width, les, n)
				}
				for _, mk := range g.marks {
					if mk.n == n && distToSegment(paintengine2d.Pt(mk.x, mk.y), l.a, l.b) > 0.5 {
						t.Errorf("at %d: %d is not on its line", width, n)
					}
				}
			}
			if len(stepOf) != 4 {
				t.Errorf("at %d: %d lines carry the message or stop it, want 4", width, len(stepOf))
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
			// What is in use is drawn as it is; what is turned off where it
			// would be, in red, struck through.
			byGroup := map[string][][]string{}
			chipsOf := map[string]map[int][]placedChip{}
			var gone []string
			for _, c := range g.chips {
				if chipsOf[c.group] == nil {
					chipsOf[c.group] = map[int][]placedChip{}
				}
				chipsOf[c.group][c.alt] = append(chipsOf[c.group][c.alt], c)
				if c.missing {
					gone = append(gone, c.name)
				}
			}
			for grp, alts := range chipsOf {
				for i := 0; i < len(alts); i++ {
					var names []string
					for _, c := range alts[i] {
						if !c.missing {
							names = append(names, c.name)
						}
					}
					if len(names) > 0 {
						byGroup[grp] = append(byGroup[grp], names)
					}
				}
			}
			var off []string
			for _, o := range []struct {
				name string
				off  bool
			}{
				{"OpenPGP", !les.signed && !les.encrypted}, {"S/MIME", !les.signed && !les.encrypted},
				{"MTA-STS", les.tls && !les.mtaSTS}, {"DANE", les.tls && !les.dane},
				{"MX", !les.mx}, {"SPF", !les.spf}, {"DKIM", !les.dkim}, {"DMARC", !les.dmarc},
			} {
				if o.off {
					off = append(off, o.name)
				}
			}
			slices.Sort(gone)
			slices.Sort(off)
			if !slices.Equal(gone, off) {
				t.Errorf("at %d, %+v: drawn as missing %v, want %v", width, les, gone, off)
			}
			wantTags := map[string][][]string{"e2e": steps[0].tags, "2": steps[1].tags, "4": steps[3].tags, "6": steps[5].tags}
			// DNS in two: TARGET's domain's MX, your domain's SPF, DKIM
			// and DMARC.
			for _, grp := range dnsTagsFor(les) {
				in := "dnsY"
				if grp[0] == "MX" {
					in = "dnsT"
				}
				wantTags[in] = append(wantTags[in], grp)
			}
			if !les.signed && !les.encrypted {
				delete(wantTags, "e2e")
				if slices.ContainsFunc(g.lines, func(l routeLine) bool { return l.ink == inkAccent }) {
					t.Errorf("at %d: a plain message, drawn end to end", width)
				}
			}
			// Red where it goes wrong: the connections without TLS. No
			// other lines but the one from YOU to TARGET.
			for _, l := range g.lines {
				switch {
				case l.head && l.ink != inkAccent && stepOf[l] >= 2 && stepOf[l] <= 6:
					if (l.ink == inkBad) != !les.tls {
						t.Errorf("at %d, %+v: step %d's connection red: %v", width, les, stepOf[l], l.ink == inkBad)
					}
				case l.dashed && l.ink != inkAccent:
					t.Errorf("at %d: a dotted line %v to %v", width, l.a, l.b)
				}
			}
			// Each DNS by the server that asks it: TARGET's domain's over
			// YOUR SERVER or beside it, your domain's over TARGET SERVER.
			var pills []routeDisc
			for _, dc := range g.discs {
				if dc.text == dnsName {
					pills = append(pills, dc)
				}
			}
			if len(pills) != 2 || !(pills[0].c.Y < g.y1 || pills[0].c.X > g.R) || pills[0].c.Y >= pills[1].c.Y || pills[1].c.Y <= g.y1 || pills[1].c.Y >= g.y2 || pills[1].c.X <= g.R {
				t.Errorf("at %d: DNS drawn at %v, YOUR SERVER at %v,%v, TARGET SERVER at %v,%v", width, pills, g.R, g.y1, g.R, g.y2)
			}
			for _, cap := range []string{theirsDNS, yoursDNS} {
				if !slices.ContainsFunc(g.texts, func(tx placedText) bool { return tx.text == cap && tx.muted }) {
					t.Errorf("at %d: no %q over its DNS", width, cap)
				}
			}
			if forged := slices.ContainsFunc(g.texts, func(tx placedText) bool { return tx.text == "or your domain" && tx.bad }); forged != !les.dmarc {
				t.Errorf("at %d, %+v: the attacker forges your domain: %v", width, les, forged)
			}
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
			for _, l := range g.lines {
				if l.ink != inkAccent {
					continue
				}
				for _, alts := range chipsOf["e2e"] {
					if len(alts) != 2 || !(alts[0].r.Max.X < l.a.X && alts[1].r.Min.X > l.a.X) {
						t.Errorf("at %d: the line from you to the recipient does not go between %v", width, alts)
					}
				}
			}
			if !slices.ContainsFunc(g.texts, func(tx placedText) bool { return tx.text == fakeDomain && tx.bad }) {
				t.Errorf("at %d: the attacker's %s is not drawn", width, fakeDomain)
			}
			w.Close()
		}
	}
	var all []step
	for _, sc := range lessons() {
		all = append(all, scenarioSteps(sc)...)
	}
	for i, st := range all {
		var all []string
		for _, p := range st.points {
			all = append(all, p.text)
			all = append(all, p.sub...)
		}
		says := strings.ToLower(strings.Join(all, "\n"))
		for _, n := range flat(st.tags) {
			// A protocol's tag, with its port: each part said.
			for _, part := range strings.Split(n, ":") {
				if !strings.Contains(says, strings.ToLower(part)) {
					t.Errorf("step %d (%s) has the tag %s and does not say %s", i%8+1, st.title, n, part)
				}
			}
		}
		// A few words each, and each one whole: none ends where the next
		// goes on.
		for _, p := range all {
			if n := len(strings.Fields(p)); n > 9 {
				t.Errorf("step %d: %q is %d words", i+1, p, n)
			}
			if first := []rune(p)[0]; first >= 'a' && first <= 'z' && !strings.HasPrefix(p, "comms-mail") && !strings.Contains(p, ".") {
				t.Errorf("step %d: %q goes on from the point before", i+1, p)
			}
		}
	}
	if !strings.Contains(throughStep(firstLesson).points[0].text, fakeDomain) {
		t.Errorf("step 8 does not name %s", fakeDomain)
	}
}

func abs32(v float32) float32 { return max(v, -v) }

// scenarios are the lessons the picture is checked at: the first, and
// signed or not, encrypted or not, TLS or not, with the domains doing all
// they can; then each of the domains' standards turned off, all of them,
// and DMARC with neither SPF nor DKIM.
func scenarios() []lesson {
	var out []lesson
	for _, tls := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			for _, encrypted := range []bool{false, true} {
				l := firstLesson
				l.signed, l.encrypted, l.tls = signed, encrypted, tls
				out = append(out, l)
			}
		}
	}
	all := lesson{signed: true, encrypted: true, tls: true, mx: true, spf: true, dkim: true, dmarc: true, mtaSTS: true, dane: true}
	for _, off := range []func(*lesson){
		func(l *lesson) { l.mx = false }, func(l *lesson) { l.spf = false }, func(l *lesson) { l.dkim = false },
		func(l *lesson) { l.dmarc = false }, func(l *lesson) { l.mtaSTS = false }, func(l *lesson) { l.dane = false },
		func(l *lesson) { l.mtaSTS, l.dane = false, false }, func(l *lesson) { l.spf, l.dkim = false, false },
		func(l *lesson) {
			l.mx, l.spf, l.dkim, l.dmarc, l.mtaSTS, l.dane = false, false, false, false, false, false
		},
	} {
		l := all
		off(&l)
		out = append(out, l)
	}
	return out
}

// lessons are every lesson there is: 2⁹.
func lessons() []lesson {
	var out []lesson
	for n := range 1 << 9 {
		b := func(i int) bool { return n&(1<<i) != 0 }
		out = append(out, lesson{b(0), b(1), b(2), b(3), b(4), b(5), b(6), b(7), b(8)})
	}
	return out
}

// choose sets page's ticks to l: the first lesson's are checked first —
// nothing done to the message, no TLS, the domains doing all they can —
// and MTA-STS and DANE can be ticked only with TLS.
func choose(t *testing.T, a *app.Application, page widget.Component, l lesson) {
	t.Helper()
	boxes := map[string]*widgets.Checkbox{}
	widget.Walk(page, func(c widget.Component) {
		if v, ok := c.(*widgets.Checkbox); ok {
			boxes[v.Text] = v
		}
	})
	want := []struct {
		name  string
		first bool
		to    bool
	}{
		{"Signed", false, l.signed}, {"Encrypted", false, l.encrypted}, {"TLS", false, l.tls},
		{"MTA-STS", true, l.mtaSTS}, {"DANE", true, l.dane},
		{"MX", true, l.mx}, {"SPF", true, l.spf}, {"DKIM", true, l.dkim}, {"DMARC", true, l.dmarc},
	}
	if len(boxes) != len(want) {
		t.Fatalf("%d ticks, want %d", len(boxes), len(want))
	}
	for _, w := range want {
		b := boxes[w.name]
		if b == nil || b.Checked != w.first {
			t.Fatalf("%s first ticked %v, want %v", w.name, b != nil && b.Checked, w.first)
		}
	}
	if boxes["MTA-STS"].Enabled() || boxes["DANE"].Enabled() {
		t.Fatal("MTA-STS and DANE can be ticked without TLS")
	}
	for _, w := range want {
		if b := boxes[w.name]; b.Checked != w.to {
			b.Checked = w.to
			b.OnChange(w.to)
		}
	}
	a.PumpOnce()
	if boxes["MTA-STS"].Enabled() != l.tls {
		t.Fatalf("MTA-STS can be ticked: %v, with TLS %v", boxes["MTA-STS"].Enabled(), l.tls)
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

// The picture is as big whatever is ticked — its rows and columns where
// they were — so ticking does not move what is under it.
func TestEducateKeepsItsSize(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Educate", Width: 560, Height: 600, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	sc := newRouteScene()
	w.SetContent(sc)
	a.PumpOnce()
	for _, width := range []float32{sc.MinWidth(), 460, 482, 560, 700} {
		sc.l = firstLesson
		first := sc.layoutAt(width)
		for _, l := range lessons() {
			sc.l = l
			g := sc.layoutAt(width)
			if g.h != first.h || g.y1 != first.y1 || g.y2 != first.y2 || g.L != first.L || g.R != first.R {
				t.Fatalf("at %v, %+v: %v tall, rows %v %v, columns %v %v; first %v, %v %v, %v %v", width, l, g.h, g.y1, g.y2, g.L, g.R, first.h, first.y1, first.y2, first.L, first.R)
			}
		}
	}
}

// The picture, what its symbols mean and the ticks stay in view while
// the words under them scroll — when the page leaves the words room —
// and scroll with them when it does not,
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
	var steps *widgets.TabView
	var key *symbolItem
	var tick *widgets.Checkbox
	widget.Walk(page, func(c widget.Component) {
		if v, ok := c.(*widgets.TabView); ok && steps == nil {
			steps = v
		}
		if v, ok := c.(*widgets.Checkbox); ok && tick == nil {
			tick = v
		}
		if k, ok := c.(*symbolItem); ok && key == nil {
			key = k
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
		scene, words, legend, ticks := widget.DeviceOrigin(page.scene), widget.DeviceOrigin(steps), widget.DeviceOrigin(key), widget.DeviceOrigin(tick)
		page.scroll.ScrollTo(150)
		a.PumpOnce()
		if page.scroll.OffsetY == 0 {
			t.Fatalf("tall %v: the words did not scroll", tall)
		}
		if moved := widget.DeviceOrigin(page.scene) != scene; moved == tall {
			t.Errorf("tall %v: the picture moved with the words: %v", tall, moved)
		}
		if moved := widget.DeviceOrigin(key) != legend; moved == tall {
			t.Errorf("tall %v: what the symbols mean moved with the words: %v", tall, moved)
		}
		if moved := widget.DeviceOrigin(tick) != ticks; moved == tall {
			t.Errorf("tall %v: the ticks moved with the words: %v", tall, moved)
		}
		if tall && widget.DeviceOrigin(tick).Y < legend.Y {
			t.Errorf("the ticks are over what the symbols mean")
		}
		if widget.DeviceOrigin(steps) == words {
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

// Each message's steps say what is done to it and nothing of what is not:
// unsigned, steps 1 and 7 say nothing of a signature; unencrypted,
// nothing of a session key or decrypting; and no point is only that
// something is not done.
func TestEducateSaysOnlyWhatIsDone(t *testing.T) {
	for _, sc := range lessons() {
		{
			signed, encrypted := sc.signed, sc.encrypted
			st := scenarioSteps(sc)
			// Without TLS, steps 2, 4 and 6 say nothing of it; with it,
			// they say so.
			for _, n := range []int{1, 3, 5} {
				var words []string
				for _, p := range st[n].points {
					words = append(words, p.text)
					words = append(words, p.sub...)
				}
				said := strings.Join(words, "\n")
				for _, w := range []string{"TLS", "certificate", "MTA-STS", "DANE"} {
					if !sc.tls && strings.Contains(said, w) {
						t.Errorf("%+v: step %d speaks of %s:\n%s", sc, n+1, w, said)
					}
				}
				if sc.tls && !strings.Contains(said, "TLS") {
					t.Errorf("%+v: step %d leaves out TLS:\n%s", sc, n+1, said)
				}
			}
			for _, n := range []int{0, 6} {
				var words []string
				words = append(words, st[n].title)
				for _, p := range st[n].points {
					words = append(words, p.text)
					words = append(words, p.sub...)
				}
				said := strings.ToLower(strings.Join(words, "\n"))
				if !signed && (strings.Contains(said, "signature") || strings.Contains(said, "sign")) {
					t.Errorf("signed %v, encrypted %v: step %d speaks of signing:\n%s", signed, encrypted, n+1, said)
				}
				if !encrypted && (strings.Contains(said, "session key") || strings.Contains(said, "decrypt") || strings.Contains(said, "encrypt")) {
					t.Errorf("signed %v, encrypted %v: step %d speaks of encrypting:\n%s", signed, encrypted, n+1, said)
				}
				signs, crypts := "signs it", "encrypts it" // what YOU do, in step 1
				if n == 6 {
					signs, crypts = "signature", "decrypts it" // and TARGET, in 7
				}
				if signed && !strings.Contains(said, signs) || encrypted && !strings.Contains(said, crypts) {
					t.Errorf("signed %v, encrypted %v: step %d leaves out what is done:\n%s", signed, encrypted, n+1, said)
				}
			}
			for _, s := range st {
				for _, p := range s.points {
					for _, text := range append([]string{p.text}, p.sub...) {
						if strings.HasPrefix(text, "No signature") || strings.HasPrefix(text, "No encryption") {
							t.Errorf("signed %v, encrypted %v: %q says only what is not done", signed, encrypted, text)
						}
					}
				}
			}
		}
	}
}

// Turning off what the domains publish says what follows, and the
// picture's tags lose it: no MX, the domain's own address; no DKIM, no
// domain signature to check; DMARC with neither SPF nor DKIM, even your
// own mail fails; with SPF alone, forwarding breaks it; no DMARC, your
// exact domain can be forged; TLS between servers that nothing makes a
// must can be stripped, and with MTA-STS or DANE stripping stops the mail.
func TestEducateExplainsWhatIsOff(t *testing.T) {
	for _, l := range lessons() {
		st := scenarioSteps(l)
		said := func(n int) string {
			var words []string
			for _, p := range st[n].points {
				words = append(words, p.text)
				words = append(words, p.sub...)
			}
			return strings.Join(words, "\n")
		}
		tagged := func(n int, name string) bool { return slices.Contains(flat(st[n].tags), name) }
		check := func(ok bool, what string) {
			t.Helper()
			if !ok {
				t.Errorf("%+v: %s", l, what)
			}
		}
		check(l.mx == strings.Contains(said(2), "The MX record in DNS names TARGET SERVER"), "step 3 and MX")
		check(!l.mx == strings.Contains(said(2), "A or AAAA"), "step 3 without MX")
		check(l.mx == tagged(2, "MX") && l.dkim == tagged(2, "DKIM"), "step 3's tags")
		check(!l.dkim == (strings.Contains(said(2), "No DKIM") && strings.Contains(said(4), "No DKIM signature")), "steps 3 and 5 without DKIM")
		check(!l.spf == strings.Contains(said(4), "No SPF"), "step 5 without SPF")
		check((l.dmarc && !l.spf && !l.dkim) == strings.Contains(said(4), "Even your own mail fails it"), "DMARC with neither")
		check((l.dmarc && l.spf && !l.dkim) == strings.Contains(said(4), "Only SPF can pass it"), "DMARC with SPF alone")
		check((l.dmarc && !l.spf && l.dkim) == strings.Contains(said(4), "Only DKIM can pass it"), "DMARC with DKIM alone")
		check(!l.dmarc == (strings.Contains(said(4), "No DMARC") && strings.Contains(said(7), "Without DMARC")), "steps 5 and 8 without DMARC")
		check(l.spf == tagged(4, "SPF") && l.dkim == tagged(4, "DKIM") && l.dmarc == tagged(4, "DMARC"), "step 5's tags")
		check((l.tls && !l.mtaSTS && !l.dane) == strings.Contains(said(3), "Nothing here makes TLS a must"), "TLS nothing makes a must")
		check((l.tls && (l.mtaSTS || l.dane)) == strings.Contains(said(3), "stops the mail"), "TLS made a must")
		check((l.tls && l.mtaSTS) == tagged(3, "MTA-STS") && (l.tls && l.dane) == tagged(3, "DANE"), "step 4's tags")
		var dns []string
		for _, grp := range dnsTagsFor(l) {
			dns = append(dns, grp...)
		}
		for _, r := range []struct {
			on   bool
			name string
		}{{l.mx, "MX"}, {l.spf, "SPF"}, {l.dkim, "DKIM"}, {l.dmarc, "DMARC"}} {
			check(r.on == slices.Contains(dns, r.name), "DNS's tags: "+r.name)
		}
	}
}

// Every port a step's tags name is among the ports, each saying when it
// came to be first.
func TestEducateListsPorts(t *testing.T) {
	listed := map[string]bool{}
	for _, p := range ports {
		listed[p.tag] = true
		if len(p.says) == 0 || !(strings.HasPrefix(p.says[0], "Late 1990s: ") || len(p.says[0]) > 6 && p.says[0][4:6] == ": " && strings.Trim(p.says[0][:4], "0123456789") == "") {
			t.Errorf("%s does not start with when: %q", p.tag, p.says)
		}
	}
	for _, l := range lessons() {
		for _, st := range scenarioSteps(l) {
			for _, n := range flat(st.tags) {
				if strings.Contains(n, ":TCP:") || strings.Contains(n, ":UDP:") {
					if !listed[n] {
						t.Errorf("%s is not among the ports", n)
					}
				}
			}
		}
	}
}

// What goes wrong is said for every road: each thing left out, what
// follows from it, and nothing when nothing is left out. The page shows
// it under the ticks, in red, as they change.
func TestEducateSaysWhatGoesWrong(t *testing.T) {
	for _, l := range lessons() {
		var said []string
		for _, p := range problems(l) {
			said = append(said, p.text)
		}
		has := func(prefix string) bool {
			return slices.ContainsFunc(said, func(s string) bool { return strings.HasPrefix(s, prefix) })
		}
		for _, c := range []struct {
			when   bool
			prefix string
		}{
			{!l.signed, "Not signed"}, {!l.encrypted, "Not encrypted"},
			{!l.tls, "No TLS"}, {l.tls && !l.mtaSTS && !l.dane, "Between servers, TLS can be stripped"},
			{!l.mx, "No MX"}, {!l.spf, "No SPF"}, {!l.dkim, "No DKIM"},
			{rejected(l), "DMARC fails it"}, {l.dmarc && l.spf && !l.dkim, "DMARC rests on SPF"},
			{!l.dmarc, "No DMARC"},
		} {
			if has(c.prefix) != c.when {
				t.Errorf("%+v: %q said: %v", l, c.prefix, has(c.prefix))
			}
		}
		all := l.signed && l.encrypted && l.tls && l.mx && l.spf && l.dkim && l.dmarc && (l.mtaSTS || l.dane)
		if (len(said) == 0) != all {
			t.Errorf("%+v: %d things go wrong", l, len(said))
		}
	}

	// On the page: under the ticks, red, changing with them.
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Educate", Width: 560, Height: 900, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	page := educateSection()
	w.SetContent(page)
	a.PumpOnce()
	shown := func() (red []string) {
		widget.Walk(page, func(c widget.Component) {
			if v, ok := c.(*widgets.Label); ok && v.Tone == widgets.ToneDanger && v.Text != "" {
				red = append(red, v.Text)
			}
		})
		return red
	}
	for _, l := range []lesson{firstLesson, func() lesson { l := firstLesson; l.mx = false; return l }()} {
		choose(t, a, page, l)
		var want []string
		for _, p := range problems(l) {
			want = append(want, p.text)
		}
		if got := shown(); !slices.Equal(got, want) {
			t.Errorf("%+v: the page says %q goes wrong, want %q", l, got, want)
		}
		page = educateSection() // the ticks start over
		w.SetContent(page)
		a.PumpOnce()
	}
}

// The numbers are white on the look's blue, readable (4.5:1) whatever the
// look — the light and dark ones, and metal-ocean, whose text on its
// light blue accent is black.
func TestEducateNumbersAreWhiteOnBlue(t *testing.T) {
	for _, lk := range []style.LookAndFeel{style.LightLook(), style.DarkLook(),
		style.Themed(style.LightLook(), style.ThemeOverride{Pack: "metal-ocean"})} {
		disc, ink := numberInks(lk)
		if ink != white || contrast(ink, disc) < 4.5 {
			t.Errorf("%s: %v on %v, %.1f:1", lk.Name(), ink, disc, contrast(ink, disc))
		}
		// Still the look's blue, only darker: the same hue's leaning.
		a := lk.Palette().Accent
		if (a.B >= a.R) != (disc.B >= disc.R) {
			t.Errorf("%s: the disc %v is not the accent %v darkened", lk.Name(), disc, a)
		}
	}
	for _, c := range []paintengine2d.Color{paintengine2d.RGB(0.72, 0.81, 0.9), paintengine2d.RGB(1, 1, 0.4), paintengine2d.RGB(0, 0, 0.5)} {
		if contrast(white, whiteOn(c)) < 4.5 {
			t.Errorf("white on %v: %.1f:1", whiteOn(c), contrast(white, whiteOn(c)))
		}
	}
}
