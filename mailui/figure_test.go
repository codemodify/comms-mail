package mailui

import (
	"testing"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/style"
)

// The mail server is an icon of comms-mail's own, registered with the
// toolkit: it draws wherever a toolkit icon does, in every set, in the ink
// it is given.
func TestServerIsARegisteredIcon(t *testing.T) {
	if icon, ok := style.RegisteredIcon("server"); !ok || icon != iconServer || iconServer == style.IconNone {
		t.Fatalf("server registered as %v (%v), iconServer %v", icon, ok, iconServer)
	}
	for _, set := range []style.IconSetName{style.IconSetClassic, style.IconSetSharp} {
		img := paintengine2d.NewImage(48, 48)
		ink := paintengine2d.RGBA(200, 30, 30, 255)
		style.DrawToolIcon(paintengine2d.NewContext(img), paintengine2d.XYWH(0, 0, 48, 48), iconServer, ink, set)
		inked := 0
		for y := 0; y < 48; y++ {
			for x := 0; x < 48; x++ {
				if r, _, _, a := img.PremulAt(x, y); a > 128 && r > 128 {
					inked++
				}
			}
		}
		if inked < 40 {
			t.Errorf("set %v: %d pixels of ink, want the server", set, inked)
		}
	}
}
