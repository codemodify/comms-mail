package mailui

import (
	"reflect"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// OpenPrefs opens Settings: Accounts / Signatures / Tags.
func OpenPrefs(a *app.Application, cli *mailcore.Client, onChange func()) (*app.Window, error) {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Settings", Width: 760, Height: 860, MinWidth: 710, MinHeight: 440,
	})
	if err != nil {
		return nil, err
	}
	setContent(win, PrefsApp(a, win, cli, onChange))
	return win, nil
}

// OpenFilters opens Settings (whose Filters tab lists the rules).
func OpenFilters(a *app.Application, cli *mailcore.Client) (*app.Window, error) {
	return OpenPrefs(a, cli, nil)
}

// PrefsApp is tabbed: Accounts and Tags (sidebar Tags / filter pins share this model).
func PrefsApp(a *app.Application, win *app.Window, cli *mailcore.Client, onChange func()) widget.Component {
	// The status bar names the config file in use, and nothing else.
	status := widgets.NewStatusBar(mailcore.ConfigPath())

	accountsTab := prefsAccounts(a, win, cli, onChange)
	tagsTab := prefsTags(a, win, cli, onChange)
	sigTab := prefsSignatures(a, win, cli)
	securityTab, securityShown := prefsSecurity(a, win, cli, onChange)
	filtersTab := prefsFilters(a, win, cli)
	appearanceTab := prefsAppearance(a)

	tabs := widgets.NewTabView(
		widgets.Tab{Title: "Accounts", Content: widgets.NewPad(10, accountsTab)},
		widgets.Tab{Title: "Signatures", Content: widgets.NewPad(10, sigTab)},
		widgets.Tab{Title: "Tags", Content: widgets.NewPad(10, tagsTab)},
		widgets.Tab{Title: "Filters", Content: widgets.NewPad(10, filtersTab)},
		widgets.Tab{Title: "Security", Content: widgets.NewPad(10, securityTab)},
		widgets.Tab{Title: "Appearance", Content: widgets.NewPad(10, appearanceTab)},
	)
	securityIndex := len(tabs.Bar().Titles) - 2
	tabs.OnChange = func(i int) {
		if i == securityIndex {
			securityShown()
		}
	}
	// The tabs start at the top; Close is at the right under them.
	closeBtn := newButton("Close", func() { win.Close() })
	gap := widgets.NewSpacer()
	tools := widgets.NewRow(gap, closeBtn)
	tools.AddFlex(gap, 1)
	root := widgets.NewColumn(tabs, widgets.NewPad(8, tools), status).WithGap(0)
	root.AddFlex(tabs, 1)
	return root
}

func prefsAccounts(a *app.Application, win *app.Window, cli *mailcore.Client, onChange func()) widget.Component {
	st, _ := cli.Status() // the backend, for an account that names no protocol
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
	saved := func() {
		refresh()
		if onChange != nil {
			onChange()
		}
	}
	add := newButton("Add", func() { _, _ = OpenAddAccount(a, cli, saved) })
	add.Tip = "Set up an account"
	editRow := func(i int) {
		if i < 0 || i >= len(accounts) {
			widgets.Warn(win.Content(), "Edit account", "Select an account first.", nil)
			return
		}
		runAsync(a, func() (any, error) { return cli.AccountConfig(accounts[i].ID) }, func(v any, err error) {
			if err != nil {
				widgets.Warn(win.Content(), "Edit account", err.Error(), nil)
				return
			}
			_, _ = OpenEditAccount(a, cli, v.(mailcore.AccountConfig), saved)
		})
	}
	edit := newButton("Edit", func() { editRow(table.Selected) })
	edit.Tip = "Change the selected account's settings"
	table.OnActivate = editRow
	remove := newButton("Remove", func() {
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
			saved()
		})
	})
	remove.Tip = "Remove the selected account"
	importBtn := newButton("Import", func() { startImport(a, win, cli, accounts, saved) })
	importBtn.Tip = "Bring accounts in from Thunderbird or KMail"
	// The table gives up height (it scrolls) so the buttons keep theirs
	// when a narrow window folds them onto a second line.
	col := widgets.NewColumn(table, foldRow(add, edit, remove, importBtn)).WithGap(8)
	col.AddFlex(table, 1)
	return col
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
	add := newButton("Add", func() {
		_, _ = OpenTagEditor(a, mailcore.Tag{Color: "#7f8c8d"}, false, func(t mailcore.Tag) {
			if _, err := cli.PutTag(t); err != nil {
				widgets.Warn(win.Content(), "Tags", err.Error(), nil)
				return
			}
			refresh()
		})
	})
	edit := newButton("Edit", func() {
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
	remove = newButton("Remove", func() {
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
	col := widgets.NewColumn(table, foldRow(add, edit, remove)).WithGap(8)
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
	save := newButton("Save", func() {
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
	cancel := newButton("Cancel", func() { win.Close() })
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
	setContent(win, root)
	return win, nil
}

// prefsSignatures is each From the writer can pick, on the left, and the
// one picked's signature on the right. A signature is saved as it is
// typed — a moment after the typing stops, and at once when another From
// is picked — with no button to press. Write puts it under the message,
// above any quote.
func prefsSignatures(a *app.Application, win *app.Window, cli *mailcore.Client) widget.Component {
	idents := sendIdentities(cli)
	if len(idents) == 0 {
		return wrapLabel("Add an account first; each address you send from gets its own signature.")
	}
	cur := 0
	sig := widgets.NewTextArea(idents[0].Signature, "Signature (plain text)", nil)
	sig.MinRows = 6
	sig.Wrap = true

	// save stores what is typed for identity i, if it changed.
	save := func(i int, text string) {
		if i < 0 || i >= len(idents) {
			return
		}
		next := idents[i]
		next.Signature = strings.TrimRight(text, "\n")
		if next.Signature == idents[i].Signature {
			return
		}
		idents[i].Signature = next.Signature // what the next save compares with
		runAsync(a, func() (any, error) { return cli.PutIdentity(next) }, func(v any, err error) {
			if err != nil {
				widgets.Warn(win.Content(), "Signature", err.Error(), nil)
				return
			}
			idents[i] = v.(mailcore.Identity)
		})
	}
	var timer *time.Timer
	flush := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		save(cur, sig.Text)
	}
	sig.OnInput = func(string) {
		if timer != nil {
			timer.Stop()
		}
		i := cur
		timer = time.AfterFunc(signatureSaveDelay, func() {
			a.Post(func() {
				if i == cur {
					flush()
				}
			})
		})
	}

	list := widgets.NewTableView([]widgets.TableColumn{{Title: "From"}}, len(idents), func(row, _ int) string {
		if row < 0 || row >= len(idents) {
			return ""
		}
		return idents[row].DisplayFrom()
	}, func(row int) {
		if row < 0 || row >= len(idents) || row == cur {
			return
		}
		flush()
		cur = row
		sig.SetText(idents[row].Signature)
	})
	list.Selected = 0
	split := widgets.NewSplitter(widgets.SplitColumns, list, sig)
	split.Ratio = 0.38
	return split
}

// signatureSaveDelay is how long after the typing stops a signature is
// saved.
const signatureSaveDelay = 600 * time.Millisecond

// securityTopic is one of Security's topics: its name in the list, its
// page, and what asks again each time the page shows (nil: nothing).
type securityTopic struct {
	name  string
	page  widget.Component
	shown func()
}

// prefsSecurity is Settings' Security tab: its topics on the left —
// Passwords, Keys, Remote images, Educate — and the one picked on the
// right. shown
// is for the tab showing: the page on show asks again, since the store may
// have locked or a key been made in secretvault itself meanwhile.
func prefsSecurity(a *app.Application, win *app.Window, cli *mailcore.Client, onChange func()) (tab widget.Component, shown func()) {
	var topics []securityTopic
	if pass, refresh := passwordsSection(a, win, cli); pass != nil {
		topics = append(topics, securityTopic{"Passwords", pass, refresh})
	}
	keys, refreshKeys := keysSection(a, cli)
	topics = append(topics,
		securityTopic{"Keys", widgets.NewScrollView(keys), refreshKeys},
		securityTopic{"Remote images", remoteImagesSection(win, cli, onChange), nil},
		securityTopic{"Educate", educateSection(), nil})

	pages := widgets.NewStack()
	for _, t := range topics {
		pages.Add(t.page)
	}
	cur := 0
	show := func(i int) {
		cur = i
		for j, t := range topics {
			t.page.SetVisible(j == i)
		}
		pages.RequestLayout()
		pages.Invalidate()
	}
	shown = func() {
		if f := topics[cur].shown; f != nil {
			f()
		}
	}
	list := widgets.NewListView(len(topics), func(i int) string {
		if i < 0 || i >= len(topics) {
			return ""
		}
		return topics[i].name
	}, func(i int) {
		if i < 0 || i >= len(topics) || i == cur {
			return
		}
		show(i)
		shown()
	})
	list.Sidebar = true
	list.Selected = 0
	show(0) // each page asked once as it was made
	split := widgets.NewSplitter(widgets.SplitColumns, list, widgets.NewPad(4, pages))
	split.Ratio = 0.25
	return split, shown
}

// remoteImagesSection lists the senders whose remote images load without
// asking (given with Always in the reading pane), and takes them back.
func remoteImagesSection(win *app.Window, cli *mailcore.Client, onChange func()) widget.Component {
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
	remove = newButton("Remove", func() {
		i := table.Selected
		if i < 0 || i >= len(senders) {
			return
		}
		if err := cli.AllowRemoteImages(senders[i], false); err != nil {
			widgets.Warn(win.Content(), "Remote images", err.Error(), nil)
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
	col := widgets.NewColumn(
		wrapLabel("Images on the web are not loaded unless you ask: loading one tells the sender you opened the message, and from where. These senders' images load without asking (Always, in the reading pane). Remove one to be asked again."),
		table, widgets.NewRow(remove).WithGap(8)).WithGap(8)
	col.AddFlex(table, 1)
	return col
}

// passwordsSection is Settings › Security › Passwords: where the saved
// passwords and sign-ins are kept, as the choice of where to keep them —
// each place with what it means, the one in use picked — and Apply, which
// moves them all to the place picked. Under the place in use: that it is
// locked, with Unlock…, and Change passphrase… for the encrypted file.
// None for a store that keeps no secrets (the demo). refresh asks the
// daemon again and redraws the page when anything changed.
func passwordsSection(a *app.Application, win *app.Window, cli *mailcore.Client) (section widget.Component, refresh func()) {
	st, err := cli.SecretsStatus()
	if err != nil || !st.Supported {
		return nil, nil
	}
	page := widgets.NewColumn().WithGap(10)
	var shown mailcore.SecretsStatus
	var show func(mailcore.SecretsStatus)
	reload := func(always bool) {
		if cur, err := cli.SecretsStatus(); err == nil && (always || !reflect.DeepEqual(cur, shown)) {
			show(cur)
		}
	}
	refresh = func() { reload(false) }
	show = func(st mailcore.SecretsStatus) {
		shown = st
		page.ClearChildren()
		var inUse []widget.Component
		if st.Store != "" && !st.Ready {
			text := "Locked now."
			icon := style.IconLock
			if !st.Locked {
				text, icon = "It cannot be read now: "+st.Problem, style.IconWarning
			}
			l := iconLine(icon, text)
			unlockText := "Unlock…"
			if st.Store == mailcore.StoreSecretVault {
				// secretvault shows its own prompt; comms-mail never sees
				// the passphrase.
				unlockText = "Unlock secretvault…"
			}
			unlock := newButton(unlockText, func() { unlockStore(a, cli, st, win, refresh) })
			inUse = append(inUse, l, foldRow(unlock))
		}
		if st.Store == mailcore.StoreEncrypted {
			inUse = append(inUse, foldRow(newButton("Change passphrase…", func() { openPassphrase(a, cli, passChange, refresh) })))
		}
		under := map[string]widget.Component{}
		if len(inUse) > 0 {
			under[st.Store] = widgets.NewColumn(inUse...).WithGap(6)
		}
		c := newStoreChoices(st, "passwords", true, under)
		apply := newButton("Apply", nil)
		apply.Tip = "Move your passwords and sign-ins to the place picked"
		apply.SetEnabled(false)
		apply.OnClick = func() { c.move(a, cli, page, apply, func() { reload(true) }) }
		c.onPick = func() { apply.SetEnabled(c.moving()) }
		c.first.OnSubmit = func(string) { apply.OnClick() }
		c.again.OnSubmit = func(string) { apply.OnClick() }
		// The places scroll; the passphrase fields and Apply stay in view.
		scroll := widgets.NewScrollView(c.list)
		page.AddFlex(scroll, 1)
		page.Add(c.passBox)
		page.Add(foldRow(apply))
		page.RequestLayout()
		page.Invalidate()
	}
	show(st)
	return page, refresh
}
