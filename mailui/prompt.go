package mailui

import (
	"strings"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widgets"
)

// askName opens a small window asking for one line of text — a folder's
// name — with OK and Cancel; Return is OK. work runs off the UI goroutine
// with the trimmed text; an error it returns is shown and the window stays
// open, else done runs on the UI goroutine and the window closes.
// uitoolkit has no input dialog of its own (uitoolkit-gaps.md #10).
func askName(a *app.Application, title, label, initial, okText string, work func(string) error, done func(string)) *app.Window {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 380, Height: 170, MinWidth: 300, MinHeight: 150,
	})
	if err != nil {
		return nil
	}
	field := widgets.NewTextField(initial, label, nil)
	var okBtn *widgets.Button
	submit := func() {
		name := strings.TrimSpace(field.Text)
		if name == "" {
			return
		}
		if !okBtn.Enabled() {
			return // already working on it
		}
		okBtn.SetEnabled(false)
		runAsync(a, func() (any, error) {
			return nil, work(name)
		}, func(_ any, err error) {
			if err != nil {
				okBtn.SetEnabled(true)
				widgets.Warn(win.Content(), title, err.Error(), nil)
				return
			}
			win.Close()
			if done != nil {
				done(name)
			}
		})
	}
	field.OnSubmit = func(string) { submit() }
	okBtn = widgets.NewButton(okText, submit)
	okBtn.Primary = true
	cancel := widgets.NewButton("Cancel", func() { win.Close() })
	form := widgets.NewColumn(
		widgets.NewLabel(label), field,
		widgets.NewSpacer(),
		widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(okBtn, widgets.RoleAccept),
	).WithGap(8)
	win.SetContent(widgets.NewPad(12, form))
	win.SetInitialFocus(field)
	field.SetSelection(0, len([]rune(initial)))
	return win
}
