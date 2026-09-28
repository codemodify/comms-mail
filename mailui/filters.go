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

// Filters: the daemon's rules, which run on new mail — moving, tagging,
// marking it. The tab lists them, turns them on and off, deletes them, runs
// them now over each Inbox, and adds or edits a simple one (one test, one
// action). A rule with more (one imported from Thunderbird) can be turned
// on and off and deleted here.

var ruleFields = []struct{ key, label string }{
	{"from", "From"}, {"to", "To or Cc"}, {"subject", "Subject"}, {"body", "Body"},
}

var ruleOps = []struct{ key, label string }{
	{"contains", "contains"}, {"notcontains", "doesn't contain"}, {"is", "is"},
	{"isnot", "isn't"}, {"begins", "begins with"}, {"ends", "ends with"},
}

var ruleActions = []struct{ key, label string }{
	{"move", "Move to folder"}, {"tag", "Tag"}, {"markRead", "Mark read"}, {"star", "Star"}, {"delete", "Delete"},
}

func labelOf(list []struct{ key, label string }, key string) string {
	for _, x := range list {
		if strings.EqualFold(x.key, key) {
			return x.label
		}
	}
	return key
}

// ruleSummary says what a rule does in a line.
func ruleSummary(r mailcore.FilterRule, folderName func(mailcore.FolderID) string) string {
	var tests []string
	where := ""
	for _, c := range r.Conditions {
		switch strings.ToLower(c.Field) {
		case "account":
			where = " (" + c.Value
		case "inbox":
			if where == "" {
				where = " (Inbox"
			} else {
				where += " Inbox"
			}
		case "attachment":
			tests = append(tests, "has an attachment")
		case "unread":
			tests = append(tests, "is unread")
		case "tag":
			tests = append(tests, "is tagged "+c.Value)
		default:
			tests = append(tests, fmt.Sprintf("%s %s “%s”", labelOf(ruleFields, c.Field), labelOf(ruleOps, orDefault(c.Op, "contains")), c.Value))
		}
	}
	if where != "" {
		where += ")"
	}
	join := " and "
	if r.Any {
		join = " or "
	}
	var acts []string
	for _, a := range r.Actions {
		switch strings.ToLower(a.Type) {
		case "move":
			dest := a.Path
			if a.Folder != "" {
				dest = folderName(a.Folder)
			}
			acts = append(acts, "move to "+dest)
		case "tag":
			acts = append(acts, "tag "+a.Tag)
		case "stop":
			acts = append(acts, "stop")
		default:
			acts = append(acts, strings.ToLower(labelOf(ruleActions, a.Type)))
		}
	}
	return "When " + strings.Join(tests, join) + where + " → " + strings.Join(acts, ", ")
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// simpleRule reports whether the editor can show r whole: one test (and
// the Inbox scope), one action.
func simpleRule(r mailcore.FilterRule) bool {
	tests := 0
	for _, c := range r.Conditions {
		switch strings.ToLower(c.Field) {
		case "inbox":
		case "from", "to", "subject", "body":
			tests++
		default:
			return false
		}
	}
	return tests == 1 && len(r.Actions) == 1 && strings.ToLower(r.Actions[0].Type) != "stop" && r.Actions[0].Path == ""
}

// moveTargets is every folder a rule can move mail to.
func moveTargets(cli *mailcore.Client) ([]mailcore.Folder, []string) {
	var folders []mailcore.Folder
	var labels []string
	accts, _ := cli.Accounts()
	for _, a := range accts {
		fs, _ := cli.ListFolders(a.ID)
		for _, f := range fs {
			if f.Virtual || f.NoSelect {
				continue
			}
			folders = append(folders, f)
			labels = append(labels, a.Address+" · "+f.Name)
		}
	}
	return folders, labels
}

func prefsFilters(a *app.Application, win *app.Window, cli *mailcore.Client) widget.Component {
	rules, _ := cli.Rules()
	names := map[mailcore.FolderID]string{}
	folders, labels := moveTargets(cli)
	for i, f := range folders {
		names[f.ID] = labels[i]
	}
	folderName := func(id mailcore.FolderID) string {
		if n := names[id]; n != "" {
			return n
		}
		return string(id)
	}
	var table *widgets.TableView
	var toggle, edit, del *widgets.Button
	selected := func() (mailcore.FilterRule, bool) {
		if i := table.Selected; i >= 0 && i < len(rules) {
			return rules[i], true
		}
		return mailcore.FilterRule{}, false
	}
	sync := func() {
		r, ok := selected()
		toggle.SetEnabled(ok)
		del.SetEnabled(ok)
		edit.SetEnabled(ok && simpleRule(r))
		toggle.Text = "Turn Off"
		if ok && !r.Enabled {
			toggle.Text = "Turn On"
		}
		toggle.RequestLayout()
	}
	refresh := func() {
		rules, _ = cli.Rules()
		table.RowCount = len(rules)
		if table.Selected >= len(rules) {
			table.Selected = len(rules) - 1
		}
		if table.Selected < 0 && len(rules) > 0 {
			table.Selected = 0
		}
		sync()
		table.Invalidate()
	}
	table = widgets.NewTableView([]widgets.TableColumn{
		{Title: "Rule", Width: 170, Sortable: true},
		{Title: "On", Width: 40},
		{Title: "What it does"},
	}, len(rules), func(row, col int) string {
		if row < 0 || row >= len(rules) {
			return ""
		}
		r := rules[row]
		switch col {
		case 1:
			if r.Enabled {
				return "yes"
			}
			return "no"
		case 2:
			return ruleSummary(r, folderName)
		}
		return r.Name
	}, func(int) { sync() })
	if len(rules) > 0 {
		table.Selected = 0
	}
	warn := func(err error) { widgets.Warn(win.Content(), "Filters", err.Error(), nil) }
	add := widgets.NewButton("Add…", func() {
		openRuleEditor(a, cli, mailcore.FilterRule{Enabled: true, Conditions: []mailcore.RuleCondition{{Field: "inbox"}}}, refresh)
	})
	edit = widgets.NewButton("Edit…", func() {
		if r, ok := selected(); ok {
			openRuleEditor(a, cli, r, refresh)
		}
	})
	edit.Tip = "Rules with one test and one action; others can be turned on or off, or deleted"
	toggle = widgets.NewButton("Turn Off", func() {
		r, ok := selected()
		if !ok {
			return
		}
		r.Enabled = !r.Enabled
		if _, err := cli.PutRule(r); err != nil {
			warn(err)
			return
		}
		refresh()
	})
	del = widgets.NewButton("Delete", func() {
		r, ok := selected()
		if !ok {
			return
		}
		widgets.Confirm(win.Content(), "Delete rule", "Delete “"+r.Name+"”?", func(yes bool) {
			if !yes {
				return
			}
			if err := cli.DeleteRule(r.ID); err != nil {
				warn(err)
				return
			}
			refresh()
		})
	})
	run := widgets.NewButton("Run Now", func() {
		n := 0
		accts, _ := cli.Accounts()
		for _, acct := range accts {
			if inbox, ok := mailcore.SpecialFolderClient(cli, acct.ID, mailcore.FolderInbox); ok {
				c, err := cli.ApplyRules(inbox.ID)
				if err != nil {
					warn(err)
					return
				}
				n += c
			}
		}
		widgets.Info(win.Content(), "Filters", fmt.Sprintf("The rules acted on %d message(s) in your Inboxes.", n), nil)
	})
	run.Tip = "Run the rules over the mail already in each Inbox"
	sync()
	col := widgets.NewColumn(
		widgets.NewTitle("Filters"),
		wrapLabel("Rules run on new mail as it arrives, in order: the first that says stop ends the run."),
		table,
		widgets.NewRow(add, edit, toggle, del, widgets.NewSpacer(), run).WithGap(8),
	).WithGap(8)
	col.AddFlex(table, 1)
	return col
}

// openRuleEditor is Add / Edit for a simple rule: a name, one test, one
// action, whether it looks only at the Inbox, and whether it is on.
func openRuleEditor(a *app.Application, cli *mailcore.Client, r mailcore.FilterRule, onSave func()) {
	title := "New Rule"
	if r.ID != "" {
		title = "Edit Rule"
	}
	win, err := a.NewWindow(platform.WindowOptions{Title: title, Width: 520, Height: 400, MinWidth: 420, MinHeight: 340})
	if err != nil {
		return
	}
	var test mailcore.RuleCondition
	inboxOnly := false
	for _, c := range r.Conditions {
		if strings.EqualFold(c.Field, "inbox") {
			inboxOnly = true
		} else {
			test = c
		}
	}
	var act mailcore.RuleAction
	if len(r.Actions) > 0 {
		act = r.Actions[0]
	}
	index := func(list []struct{ key, label string }, key string) int {
		for i, x := range list {
			if strings.EqualFold(x.key, key) {
				return i
			}
		}
		return 0
	}
	labelsOf := func(list []struct{ key, label string }) []string {
		out := make([]string, len(list))
		for i, x := range list {
			out[i] = x.label
		}
		return out
	}
	name := widgets.NewTextField(r.Name, "Name", nil)
	field := widgets.NewComboBox(labelsOf(ruleFields), index(ruleFields, test.Field), nil)
	op := widgets.NewComboBox(labelsOf(ruleOps), index(ruleOps, orDefault(test.Op, "contains")), nil)
	value := widgets.NewTextField(test.Value, "text", nil)
	folders, flabels := moveTargets(cli)
	tags, _ := cli.Tags()
	var tagNames []string
	for _, t := range tags {
		if !mailcore.IsSystemTag(t.Name) {
			tagNames = append(tagNames, t.Name)
		}
	}
	target := widgets.NewComboBox(nil, 0, nil)
	action := widgets.NewComboBox(labelsOf(ruleActions), index(ruleActions, orDefault(act.Type, "move")), nil)
	fillTarget := func() {
		switch ruleActions[action.Selected].key {
		case "move":
			target.Items, target.Selected = flabels, 0
			for i, f := range folders {
				if f.ID == act.Folder {
					target.Selected = i
				}
			}
			target.SetVisible(len(flabels) > 0)
		case "tag":
			target.Items, target.Selected = tagNames, 0
			for i, t := range tagNames {
				if strings.EqualFold(t, act.Tag) {
					target.Selected = i
				}
			}
			target.SetVisible(len(tagNames) > 0)
		default:
			target.SetVisible(false)
		}
		target.RequestLayout()
	}
	action.OnChange = func(int) { fillTarget() }
	fillTarget()
	inbox := widgets.NewCheckbox("Only mail arriving in an Inbox", inboxOnly, nil)
	on := widgets.NewCheckbox("On", r.Enabled || r.ID == "", nil)
	save := widgets.NewButton("Save", func() {
		v := strings.TrimSpace(value.Text)
		if v == "" {
			widgets.Warn(win.Content(), title, "Say what the rule looks for.", nil)
			return
		}
		out := r
		out.Name = strings.TrimSpace(name.Text)
		if out.Name == "" {
			out.Name = fmt.Sprintf("%s %s %s", ruleFields[field.Selected].label, ruleOps[op.Selected].label, v)
		}
		out.Enabled, out.Any = on.Checked, false
		out.Conditions = []mailcore.RuleCondition{{Field: ruleFields[field.Selected].key, Op: ruleOps[op.Selected].key, Value: v}}
		if inbox.Checked {
			out.Conditions = append(out.Conditions, mailcore.RuleCondition{Field: "inbox"})
		}
		a := mailcore.RuleAction{Type: ruleActions[action.Selected].key}
		switch a.Type {
		case "move":
			if target.Selected < 0 || target.Selected >= len(folders) {
				widgets.Warn(win.Content(), title, "Choose the folder to move to.", nil)
				return
			}
			a.Folder = folders[target.Selected].ID
		case "tag":
			if target.Selected < 0 || target.Selected >= len(tagNames) {
				widgets.Warn(win.Content(), title, "Choose the tag.", nil)
				return
			}
			a.Tag = tagNames[target.Selected]
		}
		out.Actions = []mailcore.RuleAction{a}
		if _, err := cli.PutRule(out); err != nil {
			widgets.Warn(win.Content(), title, err.Error(), nil)
			return
		}
		if onSave != nil {
			onSave()
		}
		win.Close()
	})
	save.Primary = true
	cancel := widgets.NewButton("Cancel", func() { win.Close() })
	form := widgets.NewForm()
	form.RowGap = 6
	form.AddRow("Name", name)
	form.AddRow("When", field)
	form.AddRow("", op)
	form.AddRow("", value)
	form.AddRow("Then", action)
	form.AddRow("", target)
	fields := widgets.NewScrollView(widgets.NewColumn(form, inbox, on).WithGap(8))
	root := widgets.NewColumn(fields,
		widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept)).WithGap(8)
	root.AddFlex(fields, 1)
	win.SetContent(widgets.NewPad(12, root))
}
