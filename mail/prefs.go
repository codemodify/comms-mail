package mail

import (
	"fmt"
	"strings"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// OpenPrefs opens Accounts / Tags.
func OpenPrefs(a *app.Application, cli *Client, onChange func()) (*app.Window, error) {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Preferences", Width: 640, Height: 480, MinWidth: 440, MinHeight: 320,
	})
	if err != nil {
		return nil, err
	}
	win.SetContent(PrefsApp(a, win, cli, onChange))
	return win, nil
}

// OpenFilters is the former Tools → Message Filters entry (same window, Tags tab).
func OpenFilters(a *app.Application, cli *Client) (*app.Window, error) {
	return OpenPrefs(a, cli, nil)
}

// PrefsApp is tabbed: Accounts and Tags (sidebar Tags / filter pins share this model).
func PrefsApp(a *app.Application, win *app.Window, cli *Client, onChange func()) widget.Component {
	st, _ := cli.Status()
	status := widgets.NewStatusBar("mailclientd settings.", st.Backend, "v"+uitoolkit.Version)

	accountsTab := prefsAccounts(a, win, cli, st, onChange)
	tagsTab := prefsTags(a, win, cli, onChange)

	tabs := widgets.NewTabView(
		widgets.Tab{Title: "Accounts", Content: widgets.NewPad(10, accountsTab)},
		widgets.Tab{Title: "Tags", Content: widgets.NewPad(10, tagsTab)},
	)
	closeBtn := widgets.NewButton("Close", func() { win.Close() })
	tools := widgets.NewRow(widgets.NewSpacer(), closeBtn).WithGap(8)
	chrome := widgets.NewTitleBar("Preferences", "accounts · tags · v"+uitoolkit.Version)
	root := widgets.NewColumn(chrome, tabs, tools, status).WithGap(0)
	root.AddFlex(tabs, 1)
	return root
}

func prefsAccounts(a *app.Application, win *app.Window, cli *Client, st DaemonStatus, onChange func()) widget.Component {
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
			if lab := ProtocolLabel(a); lab != "" {
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
		"Backend: %s   Socket: %s\nHealth: %s   Accounts: %d\nConfig: %s\nPasswords: mail.json (mode 0600, temporary plaintext) or passEnv / OAuth. See docs/mail.md.",
		st.Backend, cli.Socket, health, st.Accounts, ConfigPath(),
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
	return widgets.NewColumn(
		widgets.NewTitle("Accounts (stores / transports)"),
		info, table, widgets.NewRow(add, remove).WithGap(8),
	).WithGap(8)
}

func prefsTags(a *app.Application, win *app.Window, cli *Client, onChange func()) widget.Component {
	tags, _ := cli.Tags()
	var table *widgets.TableView
	var remove *widgets.Button
	selected := func() (Tag, bool) {
		i := -1
		if table != nil {
			i = table.Selected
		}
		if i < 0 || i >= len(tags) {
			return Tag{}, false
		}
		return tags[i], true
	}
	syncRemove := func() {
		if remove == nil {
			return
		}
		t, ok := selected()
		remove.SetEnabled(ok && !t.System && !IsSystemTag(t.Name))
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
		_, _ = OpenTagEditor(a, Tag{Color: "#7f8c8d"}, false, func(t Tag) {
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
		locked := t.System || IsSystemTag(t.Name)
		_, _ = OpenTagEditor(a, t, locked, func(next Tag) {
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
		if t.System || IsSystemTag(t.Name) {
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
		widgets.NewLabel("The sidebar Tags group is this list: locked Unread / Starred / Attachment plus keywords you add. Message → Tag toggles keywords."),
		table,
		widgets.NewRow(add, edit, remove).WithGap(8),
	).WithGap(8)
	col.AddFlex(table, 1)
	return col
}

// OpenTagEditor is Add / Edit for one tag (name + #rrggbb color).
func OpenTagEditor(a *app.Application, initial Tag, nameLocked bool, onSave func(Tag)) (*app.Window, error) {
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
		t := Tag{
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
	root := widgets.NewColumn(
		widgets.NewTitleBar(title, "v"+uitoolkit.Version),
		widgets.NewPad(12, widgets.NewColumn(
			widgets.NewLabel("Name"),
			name,
			widgets.NewLabel("Color"),
			color,
			widgets.NewLabel(hint),
			widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept),
		).WithGap(8)),
	).WithGap(0)
	win.SetContent(root)
	return win, nil
}
