package mailui

import (
	"slices"
	"testing"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Educate has the room its picture needs at
// Settings' smallest size, and at its first keeps the picture and the
// ticks in view, what goes wrong beside it and the ports beside the steps
// — as first shown, and with everything turned on.
func TestEducateFitsSettings(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	cli := demoClient(t)
	for _, sz := range []struct {
		w, h   int
		pinned bool
	}{{710, 440, false}, {960, 900, true}} { // Settings' smallest, and its first
		a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
		w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: sz.w, Height: sz.h, Headless: true})
		if err != nil {
			t.Fatal(err)
		}
		w.SetContent(PrefsApp(a, w, cli, nil))
		a.PumpOnce()
		var tv *widgets.TabView
		widget.Walk(w.Content(), func(c widget.Component) {
			if v, ok := c.(*widgets.TabView); ok && tv == nil {
				tv = v
			}
		})
		page := selectTab(a, tv, "Security")
		var list *widgets.ListView
		widget.Walk(page, func(c widget.Component) {
			if v, ok := c.(*widgets.ListView); ok {
				list = v
			}
		})
		for i := 0; i < list.Count; i++ {
			if list.ItemText(i) == "Educate" {
				list.Selected = i
				list.OnSelect(i)
			}
		}
		a.PumpOnce()
		var ed *educatePage
		widget.Walk(page, func(c widget.Component) {
			if v, ok := c.(*educatePage); ok {
				ed = v
			}
		})
		if ed == nil {
			t.Fatalf("%dx%d: no Educate page", sz.w, sz.h)
		}
		if got, need := ed.scene.Bounds().Dx(), ed.scene.MinWidth(); got < need {
			t.Errorf("%dx%d: the picture has %v wide and needs %v", sz.w, sz.h, got, need)
		}
		if ed.pinned != sz.pinned || ed.beside != sz.pinned {
			t.Errorf("%dx%d: the picture pinned %v, what goes wrong beside it %v", sz.w, sz.h, ed.pinned, ed.beside)
		}
		if sz.pinned {
			// The ticks all on one line.
			var ys []float32
			widget.Walk(ed, func(c widget.Component) {
				if v, ok := c.(*widgets.Checkbox); ok {
					ys = append(ys, widget.DeviceOrigin(v).Y)
				}
			})
			if len(ys) != 9 || slices.Min(ys) != slices.Max(ys) {
				t.Errorf("%dx%d: the ticks are not on one line: %v", sz.w, sz.h, ys)
			}
			// The ports beside the steps, under what goes wrong.
			steps, ports, side := widget.DeviceOrigin(ed.steps), widget.DeviceOrigin(ed.ports), widget.DeviceOrigin(ed.side)
			if ports.X < steps.X+float32(ed.steps.Bounds().Dx()) || ports.Y != steps.Y || ports.X != side.X {
				t.Errorf("%dx%d: the ports at %v, the steps at %v, what goes wrong at %v", sz.w, sz.h, ports, steps, side)
			}
			// Everything turned on draws the most, and stays in view too.
			all := lesson{signed: true, encrypted: true, tls: true, mx: true, spf: true, dkim: true, dmarc: true, mtaSTS: true, dane: true}
			choose(t, a, page, all)
			a.PumpOnce()
			if !ed.pinned || !ed.beside {
				t.Errorf("%dx%d: everything on, the picture pinned %v, what goes wrong beside it %v", sz.w, sz.h, ed.pinned, ed.beside)
			}
		}
		w.Close()
	}
}
