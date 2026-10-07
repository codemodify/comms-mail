package mailui

import (
	"testing"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

// A mark and a long text keep every line of the text in a narrow column:
// the line is as tall as the text wrapped to the width the mark leaves it.
func TestIconLineShowsAllOfItsText(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Line", Width: 220, Height: 400, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	const text = "Not available: secretvault is not running (nothing answers at /run/user/1000/secretvault/secretvaultd.sock)"
	line := iconLine(style.IconWarning, text)
	w.SetContent(widgets.NewColumn(line))
	a.PumpOnce()
	plain := wrapLabel(text)
	plain.SetHost(line.Host())
	// What the mark takes out of the text's width: a line's width with
	// the mark, less its width without.
	unb := layout.Constraints{MaxW: -1, MaxH: -1}
	mark := line.Measure(unb).X - plain.Measure(unb).X
	b := line.Bounds()
	need := plain.Measure(layout.Constraints{MaxW: b.Dx() - mark, MaxH: -1})
	if mark <= 0 || b.Dy()+0.5 < need.Y || need.Y < 2*plain.Measure(unb).Y {
		t.Fatalf("the mark takes %v; the text needs %v beside it and has %v", mark, need, b)
	}
}
