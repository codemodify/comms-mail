package mailui

import (
	"testing"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// A mark and a long text keep every line of the text in a narrow column:
// the text's box is as tall as the text wrapped to its width, and the text
// is a label of its own — a label with a mark measures as if the mark took
// no room (uitoolkit-gaps.md #49).
func TestIconLineShowsAllOfItsText(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Line", Width: 220, Height: 400, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	line := iconLine(style.IconWarning, "Not available: secretvault is not running (nothing answers at /run/user/1000/secretvault/secretvaultd.sock)")
	w.SetContent(widgets.NewColumn(line))
	a.PumpOnce()
	var text *widgets.Label
	widget.Walk(line, func(c widget.Component) {
		if l, ok := c.(*widgets.Label); ok && l.Text != "" {
			text = l
		}
	})
	if text.Icon != style.IconNone {
		t.Fatal("the text carries the mark: it is measured without the mark's room")
	}
	b := text.Bounds()
	need := text.Measure(layout.Constraints{MaxW: b.Dx(), MaxH: -1})
	if need.Y > b.Dy()+0.5 || b.Dy() < 2*text.Measure(layout.Constraints{MaxW: -1, MaxH: -1}).Y {
		t.Fatalf("the text needs %v and has %v", need, b)
	}
}
