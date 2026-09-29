package mailui

import (
	"errors"
	"strings"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// errKeepOpen keeps a prompt up without a message: its answer is on its
// way from the daemon.
var errKeepOpen = errors.New("")

// askName asks for one line of text — a folder's name — in uitoolkit's
// prompt over the window that holds from: the name is selected, Return is
// the button, which says what it does (accept: "Create", "Rename") and is
// grey while the field is empty. work runs off the UI goroutine with the
// trimmed text while the prompt stays up; a name it refuses is said under
// the field, with what was typed still there, and else the prompt closes
// and done runs on the UI goroutine.
func askName(from widget.Component, a *app.Application, title, label, initial, accept string, work func(string) error, done func(string)) *widgets.MessageBox {
	var mb *widgets.MessageBox
	busy := false
	mb = widgets.PromptFor(from, title, label, widgets.MessageBoxInput{
		Text: initial, Required: true, AcceptLabel: accept,
		Validate: func(text string) error {
			name := strings.TrimSpace(text)
			if busy || name == "" {
				return errKeepOpen
			}
			busy = true
			// The work may finish before runAsync returns (a window with no
			// loop runs it in place): a refusal then goes back through
			// Validate's own answer, which the prompt shows last.
			inPlace := true
			var refused error
			runAsync(a, func() (any, error) {
				return nil, work(name)
			}, func(_ any, err error) {
				busy = false
				if err != nil {
					if inPlace {
						refused = err
					} else {
						mb.SetInputError(err.Error())
					}
					return
				}
				widget.DismissOverlay(mb.Overlay())
				if done != nil {
					done(name)
				}
			})
			inPlace = false
			if refused != nil {
				return refused
			}
			return errKeepOpen
		},
	}, nil)
	return mb
}
