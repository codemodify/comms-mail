package mail

import (
	"fmt"
	"strings"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// OpenMessageSource is Thunderbird View → Message Source (Ctrl+U):
// a read-only, selectable, JetBrains Mono window of the stored RFC822.
func OpenMessageSource(a *app.Application, msg Message, rfc822 string) (*app.Window, error) {
	title := "Message Source"
	if subj := strings.TrimSpace(msg.Subject); subj != "" {
		title = "Message Source: " + subj
	}
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 820, Height: 640, MinWidth: 480, MinHeight: 320,
	})
	if err != nil {
		return nil, err
	}
	win.SetContent(MessageSourceApp(win, msg, rfc822))
	return win, nil
}

// MessageSourceApp is the source window chrome.
func MessageSourceApp(win *app.Window, msg Message, rfc822 string) widget.Component {
	body := widgets.NewMonoTextView(rfc822, "No source")
	status := widgets.NewStatusBar(
		fmt.Sprintf("%d bytes", len(rfc822)),
		string(msg.ID),
	)
	menubar := widgets.NewMenuBar(
		widgets.NewMenu("&File",
			widgets.ItemAccel("&Close Window", "Ctrl+W", func() { win.Close() }),
		),
		widgets.NewMenu("&Edit",
			widgets.ItemAccel("Select &All", "Ctrl+A", func() {
				body.SetSelection(0, len([]rune(body.Text)))
			}),
		),
		widgets.NewMenu("&Help",
			widgets.Item("About Message Source", func() {
				widgets.Info(win.Content(), "Message Source",
					"Raw RFC822 as stored by mailclientd (IMAP FETCH / .eml).\n"+
						"JetBrains Mono, read-only, selectable.",
					nil)
			}),
		),
	)
	bodyPad := widgets.NewPad(8, body)
	root := widgets.NewColumn(
		menubar,
		widgets.NewTitleBar(titleFromSource(msg), "View → Message Source"),
		bodyPad,
		status,
	).WithGap(0)
	root.AddFlex(bodyPad, 1)
	return root
}

func titleFromSource(msg Message) string {
	if s := strings.TrimSpace(msg.Subject); s != "" {
		return s
	}
	return "Message Source"
}
