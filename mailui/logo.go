package mailui

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sync"

	commsmail "github.com/codemodify/comms-mail"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/icons"
)

// comms-mail's logo (logo-normal.png at the top of the source), at the
// sizes a desktop asks for: the window icon its task bar, title bar and
// switcher show, and the tray's; and the logo in a seal
// (logo-new-mail.png), the tray's while new mail waits to be seen.

// logoArt is one of the logos, read once.
type logoArt struct {
	png    []byte
	once   sync.Once
	master *image.RGBA // the logo, square, logoMaster pixels across
}

var (
	logo        = &logoArt{png: commsmail.Logo}
	newMailLogo = &logoArt{png: commsmail.LogoNewMail}
)

// logoMaster is the size every icon is made from: the 1100-pixel logo is
// read once, into this, and each size from it.
const logoMaster = 256

func (l *logoArt) source() *image.RGBA {
	l.once.Do(func() {
		src, err := png.Decode(bytes.NewReader(l.png))
		if err != nil {
			return
		}
		rgba := image.NewRGBA(src.Bounds()) // premultiplied, read directly
		draw.Draw(rgba, rgba.Bounds(), src, src.Bounds().Min, draw.Src)
		l.master = scaleSquare(rgba, logoMaster)
	})
	return l.master
}

// AppIcons is the logo at every size a window icon is asked for
// (icons.AppIconSizes), for Application.SetIcon.
func AppIcons() []*paintengine2d.Image {
	var out []*paintengine2d.Image
	for _, side := range icons.AppIconSizes {
		if img := logo.at(side); img != nil {
			out = append(out, img)
		}
	}
	return out
}

// at is the logo side pixels square, or nil if it cannot be read.
func (l *logoArt) at(side int) *paintengine2d.Image {
	src := l.source()
	if src == nil || side <= 0 {
		return nil
	}
	return paintengine2d.NewImageFromNRGBA(scaleSquare(src, side))
}

// scaleSquare draws src, centred in the square of its longer side, side
// pixels across: each pixel the average of the source pixels it covers,
// weighted by how much of each it covers (premultiplied, so the edges stay
// clean however small it gets).
func scaleSquare(src *image.RGBA, side int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	n := max(w, h)
	ox, oy := (n-w)/2, (n-h)/2
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	k := float64(n) / float64(side)
	for y := 0; y < side; y++ {
		y0, y1 := float64(y)*k, float64(y+1)*k
		for x := 0; x < side; x++ {
			x0, x1 := float64(x)*k, float64(x+1)*k
			var sum [4]float64
			var area float64
			for py := int(y0); float64(py) < y1; py++ {
				wy := math.Min(y1, float64(py+1)) - math.Max(y0, float64(py))
				sy := py - oy
				for px := int(x0); float64(px) < x1; px++ {
					wt := wy * (math.Min(x1, float64(px+1)) - math.Max(x0, float64(px)))
					area += wt
					sx := px - ox
					if sx < 0 || sy < 0 || sx >= w || sy >= h {
						continue // the padding is transparent
					}
					p := src.Pix[src.PixOffset(b.Min.X+sx, b.Min.Y+sy):]
					for c := 0; c < 4; c++ {
						sum[c] += float64(p[c]) * wt
					}
				}
			}
			if area == 0 {
				continue
			}
			q := out.Pix[out.PixOffset(x, y):]
			for c := 0; c < 4; c++ {
				q[c] = uint8(math.Round(sum[c] / area))
			}
		}
	}
	return out
}
