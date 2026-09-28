package mailui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// importScan is what a scan of Thunderbird and KMail found: account settings
// and on-disk mail.
type importScan struct {
	accounts []mailcore.ImportedAccount
	mail     []mailcore.LocalMailStore
}

// startImport scans Thunderbird and KMail for account settings and local
// mail, then opens a window to choose what to bring in. Accounts already set
// up here are left out. onDone runs after an import so the caller refreshes.
func startImport(a *app.Application, win *app.Window, cli *mailcore.Client, existing []mailcore.Account, onDone func()) {
	have := map[string]bool{}
	for _, acc := range existing {
		have[strings.ToLower(acc.Address)] = true
	}
	runAsync(a, func() (any, error) {
		accs, accErr := cli.ImportScan()
		mail, _ := cli.ImportMailScan()
		if accErr != nil && len(mail) == 0 {
			return nil, accErr
		}
		return importScan{accounts: accs, mail: mail}, nil
	}, func(v any, err error) {
		if err != nil {
			widgets.Warn(win.Content(), "Import", err.Error(), nil)
			return
		}
		sc := v.(importScan)
		var todo []mailcore.ImportedAccount
		for _, f := range sc.accounts {
			if f.Account.Address != "" && !have[strings.ToLower(f.Account.Address)] {
				todo = append(todo, f)
			}
		}
		if len(todo) == 0 && len(sc.mail) == 0 {
			widgets.Warn(win.Content(), "Import",
				"Nothing to import from Thunderbird or KMail.\n\nAccounts already set up here are skipped.", nil)
			return
		}
		openImportWindow(a, cli, todo, sc.mail, onDone)
	})
}

// openImportWindow offers two things to bring in, each with its own
// checkboxes: account settings (one per account) and local mail (the on-disk
// folders, into the "On This Computer" account). Passwords are never read.
func openImportWindow(a *app.Application, cli *mailcore.Client, accounts []mailcore.ImportedAccount, mail []mailcore.LocalMailStore, onDone func()) {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Import", Width: 600, Height: 520, MinWidth: 460, MinHeight: 380,
	})
	if err != nil {
		return
	}
	rows := []widget.Component{widgets.NewTitle("Import from Thunderbird and KMail")}

	// Account settings.
	acctChecks := make([]*widgets.Checkbox, len(accounts))
	if len(accounts) > 0 {
		rows = append(rows,
			wrapLabel("Account settings — servers, identities and signatures. Passwords are not imported; set one per account (or sign in) afterward."),
		)
		for i, f := range accounts {
			in := f.Account.IMAP
			proto := "IMAP"
			if f.Account.Protocol == "pop3" {
				in, proto = f.Account.POP, "POP3"
			}
			label := fmt.Sprintf("%s  ·  %s  ·  %s %s", f.Source, f.Account.Address, proto, in.Host)
			acctChecks[i] = widgets.NewCheckbox(label, true, nil)
			rows = append(rows, acctChecks[i])
		}
	}

	// Local mail.
	var mailCheck *widgets.Checkbox
	if len(mail) > 0 {
		rows = append(rows, widgets.NewSeparator())
		rows = append(rows,
			wrapLabel("Local mail — messages that live only on disk (Thunderbird Local Folders, KMail maildir). IMAP mail is not listed: it re-syncs from its server."),
		)
		mailCheck = widgets.NewCheckbox(localMailSummary(mail), true, nil)
		rows = append(rows, mailCheck)
		rows = append(rows, widgets.NewLabel(localMailFolderList(mail, 8)))
	}

	status := widgets.NewLabel("")
	var imp *widgets.Button
	imp = widgets.NewButton("Import", func() {
		imp.SetEnabled(false)
		n := 0
		var errs []string
		for i, f := range accounts {
			if acctChecks[i] == nil || !acctChecks[i].Checked {
				continue
			}
			if _, err := cli.PutAccount(f.Account); err != nil {
				errs = append(errs, f.Account.Address+": "+err.Error())
				continue
			}
			n++
		}
		finish := func(mailRes mailcore.ImportResult, mailErr error, didMail bool) {
			var b strings.Builder
			if n > 0 {
				fmt.Fprintf(&b, "Imported %d account(s) — set a password for each in Accounts.\n", n)
			}
			if didMail {
				if mailErr != nil {
					fmt.Fprintf(&b, "Local mail: %s\n", mailErr)
				} else {
					fmt.Fprintf(&b, "Imported %d message(s) into %d folder(s) under “%s”.\n", mailRes.Messages, mailRes.Folders, mailcore.LocalAccountName)
				}
			}
			for _, e := range errs {
				fmt.Fprintf(&b, "%s\n", e)
			}
			if onDone != nil {
				onDone()
			}
			msg := strings.TrimSpace(b.String())
			if msg == "" {
				msg = "Nothing was selected."
			}
			widgets.Warn(win.Content(), "Import", msg, func() { win.Close() })
		}
		if mailCheck != nil && mailCheck.Checked {
			status.SetText("Importing local mail…")
			runAsync(a, func() (any, error) {
				return cli.ImportMail()
			}, func(v any, err error) {
				var r mailcore.ImportResult
				if err == nil {
					r = v.(mailcore.ImportResult)
				}
				finish(r, err, true)
			})
			return
		}
		finish(mailcore.ImportResult{}, nil, false)
	})
	imp.Primary = true
	cancel := widgets.NewButton("Cancel", func() { win.Close() })

	list := widgets.NewColumn(rows...).WithGap(8)
	scroll := widgets.NewScrollView(list)
	body := widgets.NewColumn(
		scroll,
		status,
		widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(imp, widgets.RoleAccept),
	).WithGap(10)
	body.AddFlex(scroll, 1)
	win.SetContent(widgets.NewPad(12, body))
}

// wrapLabel is a label that wraps to its width instead of eliding.
func wrapLabel(text string) *widgets.Label {
	l := widgets.NewLabel(text)
	l.Wrap = true
	return l
}

// localMailSummary is the local-mail checkbox's label: how many folders, from
// which clients.
func localMailSummary(mail []mailcore.LocalMailStore) string {
	by := map[string]int{}
	for _, m := range mail {
		by[m.Source]++
	}
	var srcs []string
	for s := range by {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	var parts []string
	for _, s := range srcs {
		parts = append(parts, fmt.Sprintf("%d from %s", by[s], s))
	}
	return fmt.Sprintf("Import local mail — %d folder(s): %s", len(mail), strings.Join(parts, ", "))
}

// localMailFolderList names the folders that will be imported, up to max.
func localMailFolderList(mail []mailcore.LocalMailStore, max int) string {
	var names []string
	for i, m := range mail {
		if i == max {
			names = append(names, fmt.Sprintf("… and %d more", len(mail)-max))
			break
		}
		names = append(names, "   "+m.Source+" · "+m.Name)
	}
	return strings.Join(names, "\n")
}
