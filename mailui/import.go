package mailui

import (
	"fmt"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// startImport scans Thunderbird and KMail for accounts and opens a window to
// confirm which to bring in. Accounts already set up here are left out.
// onDone runs after any import so the caller can refresh.
func startImport(a *app.Application, win *app.Window, cli *mailcore.Client, existing []mailcore.Account, onDone func()) {
	have := map[string]bool{}
	for _, acc := range existing {
		have[strings.ToLower(acc.Address)] = true
	}
	runAsync(a, func() (any, error) {
		return cli.ImportScan()
	}, func(v any, err error) {
		if err != nil {
			widgets.Warn(win.Content(), "Import", err.Error(), nil)
			return
		}
		var todo []mailcore.ImportedAccount
		for _, f := range v.([]mailcore.ImportedAccount) {
			if f.Account.Address != "" && !have[strings.ToLower(f.Account.Address)] {
				todo = append(todo, f)
			}
		}
		if len(todo) == 0 {
			widgets.Warn(win.Content(), "Import accounts",
				"No new accounts found in Thunderbird or KMail.\n\nAccounts already set up here are skipped.", nil)
			return
		}
		openImportWindow(a, cli, todo, onDone)
	})
}

// openImportWindow lists the found accounts with a checkbox each and imports
// the ticked ones. Passwords are not imported, so the note says so.
func openImportWindow(a *app.Application, cli *mailcore.Client, found []mailcore.ImportedAccount, onDone func()) {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Import accounts", Width: 560, Height: 460, MinWidth: 420, MinHeight: 320,
	})
	if err != nil {
		return
	}
	checks := make([]*widgets.Checkbox, len(found))
	rows := []widget.Component{
		widgets.NewTitle("Found in Thunderbird and KMail"),
		widgets.NewLabel("Passwords are not imported — set one for each account (or sign in) afterward."),
	}
	for i, f := range found {
		in := f.Account.IMAP
		proto := "IMAP"
		if f.Account.Protocol == "pop3" {
			in = f.Account.POP
			proto = "POP3"
		}
		label := fmt.Sprintf("%s  ·  %s  ·  %s %s", f.Source, f.Account.Address, proto, in.Host)
		checks[i] = widgets.NewCheckbox(label, true, nil)
		rows = append(rows, checks[i])
	}
	status := widgets.NewLabel("")

	imp := widgets.NewButton("Import", func() {
		n := 0
		var last error
		for i, f := range found {
			if !checks[i].Checked {
				continue
			}
			if _, err := cli.PutAccount(f.Account); err != nil {
				last = err
				continue
			}
			n++
		}
		if last != nil {
			widgets.Warn(win.Content(), "Import", last.Error(), nil)
		}
		if onDone != nil {
			onDone()
		}
		status.SetText(fmt.Sprintf("Imported %d account(s). Set a password for each in Accounts.", n))
		win.Close()
	})
	imp.Primary = true
	cancel := widgets.NewButton("Cancel", func() { win.Close() })

	list := widgets.NewColumn(rows...).WithGap(8)
	body := widgets.NewColumn(
		list,
		widgets.NewSpacer(),
		status,
		widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(imp, widgets.RoleAccept),
	).WithGap(10)
	body.AddFlex(list, 1)
	win.SetContent(widgets.NewPad(12, body))
}
