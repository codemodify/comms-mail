package mailui

import (
	"strings"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// askName asks for one line of text — a folder's name — in uitoolkit's
// prompt over the window that holds from: the name is selected, Return is
// the button, which says what it does (accept: "Create", "Rename") and is
// grey while the field is empty. work runs off the UI goroutine with the
// trimmed text while the prompt stays up with its button busy
// (ValidateAsync); a name work refuses is said under the field, with what
// was typed still there, and else the prompt closes and done runs on the
// UI goroutine.
func askName(from widget.Component, a *app.Application, title, label, initial, accept string, work func(string) error, done func(string)) *widgets.MessageBox {
	return widgets.PromptFor(from, title, label, widgets.MessageBoxInput{
		Text: initial, Required: true, AcceptLabel: accept,
		ValidateAsync: func(text string, finish func(error)) {
			name := strings.TrimSpace(text)
			runAsync(a, func() (any, error) {
				return nil, work(name)
			}, func(_ any, err error) { finish(err) })
		},
	}, func(text string, ok bool) {
		if ok && done != nil {
			done(strings.TrimSpace(text))
		}
	})
}
