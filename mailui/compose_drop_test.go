package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Files dropped on the message text are attached, not typed into it as
// their paths — a file manager offers the paths as text too.
func TestFilesDroppedOnTheBodyAreAttached(t *testing.T) {
	cli := demoClient(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Write", Width: 760, Height: 640, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(ComposeApp(a, w, cli, ComposeOptions{}))
	a.PumpOnce()
	var body *widgets.TextArea
	var status *widgets.StatusBar
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextArea:
			if !v.ReadOnly {
				body = v
			}
		case *widgets.StatusBar:
			status = v
		}
	})
	off, ok := w.Surface().(*platform.Offscreen)
	if !ok || body == nil || status == nil {
		t.Skip("no offscreen surface or no body")
	}
	before := body.Text
	o := widget.DeviceOrigin(body)
	b := body.LocalBounds()
	off.SimulateDrop(paintengine2d.Pt(o.X+b.Dx()/2, o.Y+b.Dy()/2), map[string][]byte{
		"text/uri-list": []byte("file:///tmp/report.pdf\r\n"),
		"text/plain":    []byte("/tmp/report.pdf"),
	})
	a.PumpOnce()
	if body.Text != before {
		t.Fatalf("the path went into the text: %q", body.Text)
	}
	if got := status.Parts()[0]; !strings.Contains(got, "Attached /tmp/report.pdf") {
		t.Fatalf("status %q", got)
	}
}
