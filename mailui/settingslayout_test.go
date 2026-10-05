package mailui

import (
	"testing"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// The Accounts tab's buttons — Add, Edit, Remove, Import — must be inside the
// window at its smallest allowed size, so no one has to resize the window to
// find Import (they did).
func TestSettingsButtonsFitAtMinSize(t *testing.T) {
	const minW, minH = 650, 440
	cli := demoClient(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: minW, Height: minH, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(PrefsApp(a, w, cli, nil))
	a.PumpOnce()

	want := map[string]bool{"Add": false, "Edit": false, "Remove": false, "Import": false}
	widget.Walk(w.Content(), func(c widget.Component) {
		b, ok := c.(*widgets.Button)
		if !ok {
			return
		}
		if _, tracked := want[b.Text]; !tracked {
			return
		}
		r := widget.LocalToWindow(b, b.LocalBounds())
		if r.Min.Y < 0 || r.Max.Y > minH || r.Min.X < 0 || r.Max.X > minW {
			t.Errorf("%q is clipped: bounds %v in a %dx%d window", b.Text, r, minW, minH)
		}
		want[b.Text] = true
	})
	for name, seen := range want {
		if !seen {
			t.Errorf("%q button not found in the Accounts tab", name)
		}
	}
}
