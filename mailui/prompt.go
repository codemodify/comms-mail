package mailui

import (
	"strings"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// askName asks for one line of text — a folder's name — in uitoolkit's
// prompt over the window that holds from: the name is selected, Return is
// OK, and OK stays grey while the field is empty. work runs off the UI
// goroutine with the trimmed text. The prompt closes when OK is pressed,
// so a name work refuses is said in a warning and then asked for again,
// with what was typed; else done runs on the UI goroutine.
func askName(from widget.Component, a *app.Application, title, label, initial string, work func(string) error, done func(string)) *widgets.MessageBox {
	var mb *widgets.MessageBox
	mb = widgets.ShowMessageBox(from, widgets.MessageBoxOptions{
		Title: title, Message: label,
		Kind: widgets.MessageQuestion, Buttons: widgets.ButtonsOKCancel,
		Input: &widgets.MessageBoxInput{Text: initial, Required: true},
		OnResult: func(r widgets.MessageResult) {
			name := strings.TrimSpace(mb.Text())
			if r != widgets.ResultOK || name == "" {
				return
			}
			runAsync(a, func() (any, error) {
				return nil, work(name)
			}, func(_ any, err error) {
				if err != nil {
					widgets.Warn(from, title, err.Error(), func() {
						askName(from, a, title, label, name, work, done)
					})
					return
				}
				if done != nil {
					done(name)
				}
			})
		},
	})
	return mb
}
