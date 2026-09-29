package mailui

import (
	"fmt"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// OpenPrefs opens Settings: Accounts / Signatures / Tags.
func OpenPrefs(a *app.Application, cli *mailcore.Client, onChange func()) (*app.Window, error) {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Settings", Width: 700, Height: 560, MinWidth: 520, MinHeight: 440,
	})
	if err != nil {
		return nil, err
	}
	win.SetContent(PrefsApp(a, win, cli, onChange))
	return win, nil
}

// OpenFilters opens Settings (whose Filters tab lists the rules).
func OpenFilters(a *app.Application, cli *mailcore.Client) (*app.Window, error) {
	return OpenPrefs(a, cli, nil)
}

// PrefsApp is tabbed: Accounts and Tags (sidebar Tags / filter pins share this model).
func PrefsApp(a *app.Application, win *app.Window, cli *mailcore.Client, onChange func()) widget.Component {
	st, _ := cli.Status()
	status := widgets.NewStatusBar("comms-maild settings.", st.Backend, "v"+uitoolkit.Version)

	accountsTab := prefsAccounts(a, win, cli, st, onChange)
	tagsTab := prefsTags(a, win, cli, onChange)
	sigTab := prefsSignatures(win, cli)
	privacyTab := prefsPrivacy(a, win, cli, onChange)
	filtersTab := prefsFilters(a, win, cli)
	appearanceTab := prefsAppearance(a)

	tabs := widgets.NewTabView(
		widgets.Tab{Title: "Accounts", Content: widgets.NewPad(10, accountsTab)},
		widgets.Tab{Title: "Signatures", Content: widgets.NewPad(10, sigTab)},
		widgets.Tab{Title: "Tags", Content: widgets.NewPad(10, tagsTab)},
		widgets.Tab{Title: "Filters", Content: widgets.NewPad(10, filtersTab)},
		widgets.Tab{Title: "Privacy", Content: widgets.NewPad(10, privacyTab)},
		widgets.Tab{Title: "Appearance", Content: widgets.NewPad(10, appearanceTab)},
	)
	closeBtn := widgets.NewButton("Close", func() { win.Close() })
	tools := widgets.NewRow(widgets.NewSpacer(), closeBtn).WithGap(8)
	chrome := widgets.NewTitleBar("Settings", "accounts · signatures · tags · filters · privacy · appearance · v"+uitoolkit.Version)
	root := widgets.NewColumn(chrome, tabs, tools, status).WithGap(0)
	root.AddFlex(tabs, 1)
	return root
}

func prefsAccounts(a *app.Application, win *app.Window, cli *mailcore.Client, st mailcore.DaemonStatus, onChange func()) widget.Component {
	accounts, _ := cli.Accounts()
	var table *widgets.TableView
	refresh := func() {
		accounts, _ = cli.Accounts()
		if table != nil {
			table.RowCount = len(accounts)
			if table.Selected >= len(accounts) {
				table.Selected = 0
			}
			table.Invalidate()
		}
	}
	table = widgets.NewTableView([]widgets.TableColumn{
		{Title: "Name", Width: 160, Sortable: true},
		{Title: "Address", Sortable: true},
		{Title: "Protocol", Width: 100, Sortable: true},
		{Title: "ID", Width: 80, Sortable: true},
	}, len(accounts), func(row, col int) string {
		if row < 0 || row >= len(accounts) {
			return ""
		}
		a := accounts[row]
		switch col {
		case 1:
			return a.Address
		case 2:
			if lab := mailcore.ProtocolLabel(a); lab != "" {
				return lab
			}
			if a.Transport == "" {
				return st.Backend
			}
			return a.Transport
		case 3:
			return a.ID
		default:
			return a.Name
		}
	}, nil)
	table.Selected = 0
	health := st.Health
	if health == "" {
		health = "ok"
	}
	info := widgets.NewLabel(fmt.Sprintf(
		"Backend: %s · Health: %s · %d account(s)\nConfig: %s   ·   Passwords: 0600 plaintext or OAuth (docs/mail.md)",
		st.Backend, health, st.Accounts, mailcore.ConfigPath(),
	))
	add := widgets.NewButton("Add account…", func() {
		_, _ = OpenAddAccount(a, cli, func() {
			refresh()
			if onChange != nil {
				onChange()
			}
		})
	})
	remove := widgets.NewButton("Remove account…", func() {
		i := table.Selected
		if i < 0 || i >= len(accounts) {
			widgets.Warn(win.Content(), "Remove account", "Select an account first.", nil)
			return
		}
		acct := accounts[i]
		confirmRemoveAccount(win.Content(), acct, func() {
			if err := cli.DeleteAccount(acct.ID); err != nil {
				widgets.Warn(win.Content(), "Remove account", err.Error(), nil)
				return
			}
			refresh()
			if onChange != nil {
				onChange()
			}
		})
	})
	importBtn := widgets.NewButton("Import…", func() {
		startImport(a, win, cli, accounts, func() {
			refresh()
			if onChange != nil {
				onChange()
			}
		})
	})
	importBtn.Tip = "Bring accounts in from Thunderbird or KMail"
	return widgets.NewColumn(
		widgets.NewTitle("Accounts (stores / transports)"),
		info, table, widgets.NewRow(add, remove, importBtn).WithGap(8),
	).WithGap(8)
}

func prefsTags(a *app.Application, win *app.Window, cli *mailcore.Client, onChange func()) widget.Component {
	tags, _ := cli.Tags()
	var table *widgets.TableView
	var remove *widgets.Button
	selected := func() (mailcore.Tag, bool) {
		i := -1
		if table != nil {
			i = table.Selected
		}
		if i < 0 || i >= len(tags) {
			return mailcore.Tag{}, false
		}
		return tags[i], true
	}
	syncRemove := func() {
		if remove == nil {
			return
		}
		t, ok := selected()
		remove.SetEnabled(ok && !t.System && !mailcore.IsSystemTag(t.Name))
	}
	refresh := func() {
		tags, _ = cli.Tags()
		if table != nil {
			table.RowCount = len(tags)
			if table.Selected >= len(tags) {
				table.Selected = len(tags) - 1
			}
			if table.Selected < 0 && len(tags) > 0 {
				table.Selected = 0
			}
			table.Invalidate()
		}
		syncRemove()
		if onChange != nil {
			onChange()
		}
	}
	table = widgets.NewTableView([]widgets.TableColumn{
		{Title: "Tag", Sortable: true},
		{Title: "Color", Width: 100},
	}, len(tags), func(row, col int) string {
		if row < 0 || row >= len(tags) {
			return ""
		}
		if col == 1 {
			return tags[row].Color
		}
		return tags[row].Name
	}, func(int) { syncRemove() })
	if len(tags) > 0 {
		table.Selected = 0
	}
	add := widgets.NewButton("Add", func() {
		_, _ = OpenTagEditor(a, mailcore.Tag{Color: "#7f8c8d"}, false, func(t mailcore.Tag) {
			if _, err := cli.PutTag(t); err != nil {
				widgets.Warn(win.Content(), "Tags", err.Error(), nil)
				return
			}
			refresh()
		})
	})
	edit := widgets.NewButton("Edit", func() {
		t, ok := selected()
		if !ok {
			widgets.Warn(win.Content(), "Tags", "Select a tag first.", nil)
			return
		}
		locked := t.System || mailcore.IsSystemTag(t.Name)
		_, _ = OpenTagEditor(a, t, locked, func(next mailcore.Tag) {
			if locked {
				next.Name = t.Name
				next.System = true
			} else if !strings.EqualFold(next.Name, t.Name) {
				next.Previous = t.Name
			}
			if _, err := cli.PutTag(next); err != nil {
				widgets.Warn(win.Content(), "Tags", err.Error(), nil)
				return
			}
			refresh()
		})
	})
	remove = widgets.NewButton("Remove", func() {
		t, ok := selected()
		if !ok {
			widgets.Warn(win.Content(), "Tags", "Select a tag first.", nil)
			return
		}
		if t.System || mailcore.IsSystemTag(t.Name) {
			widgets.Warn(win.Content(), "Tags", t.Name+" is a system tag and cannot be removed.", nil)
			return
		}
		widgets.Confirm(win.Content(), "Remove tag", "Remove "+t.Name+"?", func(yes bool) {
			if !yes {
				return
			}
			if err := cli.DeleteTag(t.Name); err != nil {
				widgets.Warn(win.Content(), "Tags", err.Error(), nil)
				return
			}
			refresh()
		})
	})
	syncRemove()
	col := widgets.NewColumn(
		widgets.NewTitle("Tags"),
		widgets.NewLabel("The sidebar Tags group is this list: locked Unread / Starred / Attachment plus keywords you add. Message › Tag toggles keywords."),
		table,
		widgets.NewRow(add, edit, remove).WithGap(8),
	).WithGap(8)
	col.AddFlex(table, 1)
	return col
}

// OpenTagEditor is Add / Edit for one tag (name + #rrggbb color).
func OpenTagEditor(a *app.Application, initial mailcore.Tag, nameLocked bool, onSave func(mailcore.Tag)) (*app.Window, error) {
	title := "Add tag"
	if strings.TrimSpace(initial.Name) != "" {
		title = "Edit tag"
	}
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 420, Height: 240, MinWidth: 360, MinHeight: 200,
	})
	if err != nil {
		return nil, err
	}
	name := widgets.NewTextField(initial.Name, "Name", nil)
	if nameLocked {
		name.SetEnabled(false)
	}
	color := widgets.NewTextField(initial.Color, "#rrggbb", nil)
	save := widgets.NewButton("Save", func() {
		t := mailcore.Tag{
			Name:   strings.TrimSpace(name.Text),
			Color:  strings.TrimSpace(color.Text),
			System: initial.System || nameLocked,
		}
		if t.Name == "" {
			widgets.Warn(win.Content(), title, "Name is required.", nil)
			return
		}
		if t.Color == "" {
			t.Color = "#7f8c8d"
		}
		if onSave != nil {
			onSave(t)
		}
		win.Close()
	})
	cancel := widgets.NewButton("Cancel", func() { win.Close() })
	hint := "Color is #rrggbb. Unread, Starred, and Attachment cannot be renamed or removed."
	if nameLocked {
		hint = initial.Name + " is a system tag. You can change its color."
	}
	// The fields scroll in a short window; Save and Cancel stay in view.
	fields := widgets.NewScrollView(widgets.NewPad(12, widgets.NewColumn(
		widgets.NewLabel("Name"),
		name,
		widgets.NewLabel("Color"),
		color,
		wrapLabel(hint),
	).WithGap(8)))
	root := widgets.NewColumn(
		widgets.NewTitleBar(title, "v"+uitoolkit.Version),
		fields,
		widgets.NewPad(12, widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept)),
	).WithGap(0)
	root.AddFlex(fields, 1)
	win.SetContent(root)
	return win, nil
}

// prefsSignatures edits the signature of each From the writer can pick.
// Write puts it under the message, above any quote.
func prefsSignatures(win *app.Window, cli *mailcore.Client) widget.Component {
	idents := sendIdentities(cli)
	names := make([]string, len(idents))
	for i, id := range idents {
		names[i] = id.DisplayFrom()
	}
	if len(names) == 0 {
		return widgets.NewLabel("Add an account first; each address you send from gets its own signature.")
	}
	sig := widgets.NewTextArea(idents[0].Signature, "Signature (plain text)", nil)
	sig.MinRows = 6
	status := widgets.NewLabel("")
	pick := widgets.NewComboBox(names, 0, func(i int) {
		if i >= 0 && i < len(idents) {
			sig.SetText(idents[i].Signature)
			status.SetText("")
		}
	})
	save := widgets.NewButton("Save signature", func() {
		i := pick.Selected
		if i < 0 || i >= len(idents) {
			return
		}
		next := idents[i]
		next.Signature = strings.TrimRight(sig.Text, "\n")
		saved, err := cli.PutIdentity(next)
		if err != nil {
			widgets.Warn(win.Content(), "Signature", err.Error(), nil)
			return
		}
		idents[i] = saved
		status.SetText("Saved. New messages from " + saved.DisplayFrom() + " start with it.")
	})
	save.Primary = true
	col := widgets.NewColumn(
		widgets.NewTitle("Signatures"),
		widgets.NewLabel("From"), pick,
		sig,
		widgets.NewRow(save, status).WithGap(8),
	).WithGap(8)
	col.AddFlex(sig, 1)
	return col
}

// prefsPrivacy lists the senders whose remote images load without asking
// (given with Always in the reading pane), and takes them back.
func prefsPrivacy(a *app.Application, win *app.Window, cli *mailcore.Client, onChange func()) widget.Component {
	senders, _ := cli.RemoteImageSenders()
	var table *widgets.TableView
	var remove *widgets.Button
	refresh := func() {
		senders, _ = cli.RemoteImageSenders()
		table.RowCount = len(senders)
		if table.Selected >= len(senders) {
			table.Selected = len(senders) - 1
		}
		if table.Selected < 0 && len(senders) > 0 {
			table.Selected = 0
		}
		remove.SetEnabled(table.Selected >= 0 && len(senders) > 0)
		table.Invalidate()
	}
	table = widgets.NewTableView([]widgets.TableColumn{
		{Title: "Sender", Sortable: true},
	}, len(senders), func(row, _ int) string {
		if row < 0 || row >= len(senders) {
			return ""
		}
		return senders[row]
	}, func(int) { remove.SetEnabled(table.Selected >= 0 && table.Selected < len(senders)) })
	remove = widgets.NewButton("Remove", func() {
		i := table.Selected
		if i < 0 || i >= len(senders) {
			return
		}
		if err := cli.AllowRemoteImages(senders[i], false); err != nil {
			widgets.Warn(win.Content(), "Privacy", err.Error(), nil)
			return
		}
		refresh()
		if onChange != nil {
			onChange()
		}
	})
	if len(senders) > 0 {
		table.Selected = 0
	}
	remove.SetEnabled(len(senders) > 0)
	col := widgets.NewColumn()
	if pass := passphraseSection(a, cli); pass != nil {
		col.Add(pass)
		col.Add(widgets.NewSeparator())
	}
	for _, c := range []widget.Component{
		widgets.NewTitle("Remote images"),
		wrapLabel("Images on the web are not loaded unless you ask: loading one tells the sender you opened the message, and from where. These senders' images load without asking (Always, in the reading pane). Remove one to be asked again."),
	} {
		col.Add(c)
	}
	col.AddFlex(table, 1)
	col.Add(widgets.NewRow(remove).WithGap(8))
	return col.WithGap(8)
}

// passphraseSection is Privacy's say on where the saved passwords and
// sign-ins are kept: the store in use, moving them to another, and the
// encrypted file's passphrase. None for a store that keeps no secrets
// (the demo).
func passphraseSection(a *app.Application, cli *mailcore.Client) widget.Component {
	st, err := cli.SecretsStatus()
	if err != nil || !st.Supported {
		return nil
	}
	note := wrapLabel("")
	var move, change *widgets.Button
	show := func(st mailcore.SecretsStatus) {
		text := "Your saved passwords and sign-ins are kept in " + mailcore.StoreLabel(st.Store) + "."
		switch st.Store {
		case "":
			switch {
			case len(st.PlainAccounts) > 0:
				text = "Your mail passwords are saved as readable text in mail.json, where any program running as you can read them. Choose a safer place for them."
			case st.PlainTokens:
				text = "Your Google or Microsoft sign-ins are saved in files whose key is kept beside them. Choose a safer place for them."
			default:
				text = "No passwords are saved yet. Choose where comms-mail should keep them."
			}
		case mailcore.StoreEncrypted:
			text += " comms-mail asks for its passphrase once each time it starts."
		case mailcore.StoreKeyring:
			text += " The desktop unlocks it when you log in."
		case mailcore.StorePlain:
			text += " Any program running as you can read them."
		}
		if st.Locked {
			text += " It is locked now."
		} else if st.Problem != "" {
			text += " It cannot be read now: " + st.Problem
		}
		note.SetText(text)
		move.Text = "Change where…"
		if st.Store == "" {
			move.Text = "Choose where…"
		}
		move.Invalidate()
		change.SetVisible(st.Store == mailcore.StoreEncrypted)
	}
	refresh := func() {
		if cur, err := cli.SecretsStatus(); err == nil {
			show(cur)
		}
	}
	move = widgets.NewButton("", func() {
		cur, err := cli.SecretsStatus()
		if err != nil {
			return
		}
		intro := switchIntro
		if cur.Store == "" {
			intro = plainIntro(cur)
		}
		openStoreChooser(a, cli, intro, cur, refresh)
	})
	change = widgets.NewButton("Change passphrase…", func() { openPassphrase(a, cli, passChange, refresh) })
	show(st)
	return widgets.NewColumn(widgets.NewTitle("Passwords"), note, widgets.NewRow(move, change).WithGap(8)).WithGap(8)
}
