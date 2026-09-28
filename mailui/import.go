package mailui

import (
	"fmt"
	"os"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// startImport scans the other mail clients installed for this user, then
// opens the import window. Accounts already set up here are left out.
// onDone runs after an import so the caller can refresh.
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
		openImportWindow(a, cli, v.([]mailcore.ImportSource), have, onDone)
	})
}

// importSection is one client (or one chosen folder) in the import window:
// a Config checkbox over one checkbox per account, and an Emails checkbox.
type importSection struct {
	src       mailcore.ImportSource
	accounts  []mailcore.ImportedAccount // the ones not already set up here
	cfg       *widgets.Checkbox
	acctBoxes []*widgets.Checkbox
	mail      *widgets.Checkbox
}

// importOutcome is what the Import button did.
type importOutcome struct {
	accounts int
	errs     []string
	didMail  bool
	mail     mailcore.ImportResult
	mailErr  error
}

// openImportWindow lists what each client offers — its account settings and
// its on-disk mail, each behind its own checkbox — and lets the user add a
// folder or mailbox file of their own.
func openImportWindow(a *app.Application, cli *mailcore.Client, sources []mailcore.ImportSource, have map[string]bool, onDone func()) *app.Window {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Import", Width: 660, Height: 600, MinWidth: 500, MinHeight: 440,
	})
	if err != nil {
		return nil
	}
	list := widgets.NewColumn().WithGap(8)
	list.Add(widgets.NewTitle("Import from other mail clients"))
	list.Add(wrapLabel("Pick what to bring in from each client. Passwords are never imported — set one for each account (or sign in) afterward. Mail kept on an IMAP server is not listed: it syncs down once its account is added."))
	none := wrapLabel("No other mail client was found. Add a folder or mailbox file below — an mbox, a maildir, an MH folder, or .eml / .emlx files.")
	list.Add(none)

	var sections []*importSection
	addSection := func(src mailcore.ImportSource) {
		sec := &importSection{src: src}
		for _, acc := range src.Accounts {
			if acc.Account.Address != "" && !have[strings.ToLower(acc.Account.Address)] {
				sec.accounts = append(sec.accounts, acc)
			}
		}
		if len(sec.accounts) == 0 && len(src.Mail) == 0 && src.Note == "" {
			return
		}
		none.SetVisible(false)
		list.Add(widgets.NewSeparator())
		list.Add(widgets.NewTitle(src.Source))
		if src.Note != "" {
			list.Add(wrapLabel(src.Note))
		}
		switch {
		case len(sec.accounts) > 0:
			sec.cfg = widgets.NewCheckbox(fmt.Sprintf("Config — %d account(s): servers, identities, signatures", len(sec.accounts)), true, func(on bool) {
				for _, b := range sec.acctBoxes {
					b.SetChecked(on)
				}
			})
			list.Add(sec.cfg)
			for _, acc := range sec.accounts {
				b := widgets.NewCheckbox(accountLine(acc.Account), true, nil)
				sec.acctBoxes = append(sec.acctBoxes, b)
				list.Add(indent(b))
			}
		case len(src.Accounts) > 0:
			list.Add(indent(widgets.NewLabel("Config — every account here is already set up.")))
		}
		if len(src.Mail) > 0 {
			sec.mail = widgets.NewCheckbox(fmt.Sprintf("Emails — %d folder(s) kept on disk", len(src.Mail)), true, nil)
			list.Add(sec.mail)
			list.Add(indent(widgets.NewLabel(storeList(src.Mail, 8))))
		}
		sections = append(sections, sec)
		list.RequestLayout()
		list.Invalidate()
	}
	for _, src := range sources {
		addSection(src)
	}

	status := widgets.NewLabel("")
	addFolder := widgets.NewButton("Add a folder or mailbox file…", func() {
		home, _ := os.UserHomeDir()
		widgets.ShowFileDialog(win.Content(), widgets.FileDialogOptions{
			Title:      "Choose a mailbox file, or open a mail folder and press Open",
			Mode:       widgets.FileOpen,
			Path:       home,
			OnNavigate: mailDirEntries,
			OnPick: func(path string) {
				if strings.TrimSpace(path) == "" {
					return
				}
				status.SetText("Looking for mail in " + path + "…")
				runAsync(a, func() (any, error) {
					return cli.ImportScanPath(path)
				}, func(v any, err error) {
					status.SetText("")
					if err != nil {
						widgets.Warn(win.Content(), "Import", err.Error(), nil)
						return
					}
					src := v.(mailcore.ImportSource)
					if len(src.Mail) == 0 {
						widgets.Warn(win.Content(), "Import", src.Note, nil)
						return
					}
					addSection(src)
				})
			},
		})
	})

	var imp *widgets.Button
	imp = widgets.NewButton("Import", func() {
		var accounts []mailcore.AccountConfig
		var stores []mailcore.LocalMailStore
		seen := map[string]bool{}
		for _, sec := range sections {
			for i, b := range sec.acctBoxes {
				acc := sec.accounts[i].Account
				key := strings.ToLower(acc.Address)
				if b.Checked && !seen[key] {
					seen[key] = true
					accounts = append(accounts, acc)
				}
			}
			if sec.mail != nil && sec.mail.Checked {
				stores = append(stores, sec.src.Mail...)
			}
		}
		if len(accounts) == 0 && len(stores) == 0 {
			widgets.Warn(win.Content(), "Import", "Nothing is ticked.", nil)
			return
		}
		imp.SetEnabled(false)
		status.SetText("Importing…")
		runAsync(a, func() (any, error) {
			var r importOutcome
			for _, acc := range accounts {
				if _, err := cli.PutAccount(acc); err != nil {
					r.errs = append(r.errs, acc.Address+": "+err.Error())
					continue
				}
				r.accounts++
			}
			if len(stores) > 0 {
				r.didMail = true
				r.mail, r.mailErr = cli.ImportMail(stores)
			}
			return r, nil
		}, func(v any, _ error) {
			status.SetText("")
			if onDone != nil {
				onDone()
			}
			widgets.Warn(win.Content(), "Import", importSummary(v.(importOutcome)), func() { win.Close() })
		})
	})
	imp.Primary = true
	cancel := widgets.NewButton("Cancel", func() { win.Close() })

	scroll := widgets.NewScrollView(list)
	body := widgets.NewColumn(
		scroll,
		status,
		widgets.NewRow(addFolder, widgets.NewSpacer(),
			widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(imp, widgets.RoleAccept)).WithGap(8),
	).WithGap(10)
	body.AddFlex(scroll, 1)
	win.SetContent(widgets.NewPad(12, body))
	return win
}

// importSummary says what an import did, for the closing message.
func importSummary(r importOutcome) string {
	var b strings.Builder
	if r.accounts > 0 {
		fmt.Fprintf(&b, "Added %d account(s) — set a password for each in Accounts (or sign in).\n", r.accounts)
	}
	if r.didMail {
		if r.mailErr != nil {
			fmt.Fprintf(&b, "Emails: %s\n", r.mailErr)
		} else {
			fmt.Fprintf(&b, "Imported %d message(s) into %d folder(s) under “%s”.\n", r.mail.Messages, r.mail.Folders, mailcore.LocalAccountName)
		}
	}
	for _, e := range r.errs {
		fmt.Fprintf(&b, "%s\n", e)
	}
	return strings.TrimSpace(b.String())
}

// accountLine is one account in the import list: address, protocol, host.
func accountLine(a mailcore.AccountConfig) string {
	in, proto := a.IMAP, "IMAP"
	if a.Protocol == mailcore.ProtoPOP3 {
		in, proto = a.POP, "POP3"
	}
	line := fmt.Sprintf("%s  ·  %s %s", a.Address, proto, in.Host)
	if a.Provider != "" {
		line += "  ·  sign in with " + a.Provider
	}
	return line
}

// storeList names the folders an Emails checkbox covers, up to max.
func storeList(st []mailcore.LocalMailStore, max int) string {
	var names []string
	for i, s := range st {
		if i == max {
			names = append(names, fmt.Sprintf("… and %d more", len(st)-max))
			break
		}
		names = append(names, s.Name+"  ("+s.Kind+")")
	}
	return strings.Join(names, "\n")
}

func indent(c widget.Component) widget.Component {
	p := widgets.NewPad(0, c)
	p.L = 24
	return p
}

// wrapLabel is a label that wraps to its width instead of eliding.
func wrapLabel(text string) *widgets.Label {
	l := widgets.NewLabel(text)
	l.Wrap = true
	return l
}
