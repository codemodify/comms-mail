package mailui

import (
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/widget"
)

// fitMinWidth raises win's minimum width to what its content needs
// (widget.MinWidthOf) where that is more than the window was made with.
// WindowOptions.MinWidth is chosen by hand before the content exists, and
// a look with larger controls, or a row of buttons that grew icons, would
// otherwise let the window be made narrow enough for its buttons to
// overlap.
func fitMinWidth(win *app.Window, content widget.Component) {
	if win == nil || content == nil || win.Scale() <= 0 {
		return
	}
	need := widget.MinWidthOf(content) / win.Scale()
	if have, _ := win.MinSize(); need > have {
		win.SetMinSize(need, 0)
	}
}

// setContent is win.SetContent, with the window's minimum width raised to
// what content needs (fitMinWidth).
func setContent(win *app.Window, content widget.Component) {
	win.SetContent(content)
	fitMinWidth(win, content)
}
