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
// them now over each Inbox, and adds or edits them: any number of tests
// (all or any of them) and of actions.

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
	return "When " + strings.Join(tests, join) + where + ": " + strings.Join(acts, ", ")
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// editableRule reports whether the editor can show r whole: every test
// and action is one it offers (an account or Inbox scope is kept as it is).
func editableRule(r mailcore.FilterRule) bool {
	for _, c := range r.Conditions {
		switch strings.ToLower(c.Field) {
		case "account", "inbox", "from", "to", "subject", "body", "tag", "attachment", "unread":
		default:
			return false
		}
	}
	for _, a := range r.Actions {
		switch strings.ToLower(a.Type) {
		case "move", "tag", "markread", "read", "markunread", "unread", "star", "flag", "delete", "stop":
		default:
			return false
		}
	}
	return true
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
		edit.SetEnabled(ok && editableRule(r))
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
	edit.Tip = "Change what the rule looks for and what it does"
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

// Tests and actions the editor offers, beyond ruleFields / ruleActions.
var editorFields = []struct{ key, label string }{
	{"from", "From"}, {"to", "To or Cc"}, {"subject", "Subject"}, {"body", "Body"},
	{"tag", "Tagged"}, {"attachment", "Has an attachment"}, {"unread", "Is unread"},
}

var editorActions = []struct{ key, label string }{
	{"move", "Move to folder"}, {"tag", "Tag"}, {"markRead", "Mark read"},
	{"markUnread", "Mark unread"}, {"star", "Star"}, {"delete", "Delete"},
}

func keyIndex(list []struct{ key, label string }, key string) int {
	for i, x := range list {
		if strings.EqualFold(x.key, key) {
			return i
		}
	}
	switch strings.ToLower(key) { // older spellings
	case "read":
		return keyIndex(list, "markRead")
	case "unread":
		return keyIndex(list, "markUnread")
	case "flag":
		return keyIndex(list, "star")
	}
	return 0
}

func labelsOf(list []struct{ key, label string }) []string {
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = x.label
	}
	return out
}

// ruleEditor is Add / Edit for a rule: tests (all or any), actions, whether
// it looks only at mail arriving in an Inbox, stop, on.
type ruleEditor struct {
	cli             *mailcore.Client
	win             *app.Window
	rule            mailcore.FilterRule
	scope           []mailcore.RuleCondition // account scope, kept as it was
	name            *widgets.TextField
	match           *widgets.ComboBox
	conds           *widgets.FlexBox
	acts            *widgets.FlexBox
	condRows        []*condRow
	actRows         []*actRow
	inbox, stop, on *widgets.Checkbox
	folders         []mailcore.Folder
	flabels         []string
	tags            []string
}

type condRow struct {
	field, op *widgets.ComboBox
	value     *widgets.TextField
	row       *widgets.FlexBox
}

type actRow struct {
	action, target *widgets.ComboBox
	path, account  string // a move to a folder not synced yet, by server path
	pathItem       bool   // target's first item is that path
	row            *widgets.FlexBox
}

// openRuleEditor opens the editor on r (a new rule when r.ID is empty).
func openRuleEditor(a *app.Application, cli *mailcore.Client, r mailcore.FilterRule, onSave func()) *ruleEditor {
	title := "New Rule"
	if r.ID != "" {
		title = "Edit Rule"
	}
	win, err := a.NewWindow(platform.WindowOptions{Title: title, Width: 640, Height: 520, MinWidth: 520, MinHeight: 420})
	if err != nil {
		return nil
	}
	e := &ruleEditor{cli: cli, win: win, rule: r}
	e.folders, e.flabels = moveTargets(cli)
	tags, _ := cli.Tags()
	for _, t := range tags {
		if !mailcore.IsSystemTag(t.Name) {
			e.tags = append(e.tags, t.Name)
		}
	}
	e.name = widgets.NewTextField(r.Name, "Name (optional)", nil)
	matchIdx := 0
	if r.Any {
		matchIdx = 1
	}
	e.match = widgets.NewComboBox([]string{"all of these", "any of these"}, matchIdx, nil)
	e.conds = widgets.NewColumn().WithGap(6)
	e.acts = widgets.NewColumn().WithGap(6)
	inboxOnly, stop := false, r.Stop
	for _, c := range r.Conditions {
		switch strings.ToLower(c.Field) {
		case "inbox":
			inboxOnly = true
		case "account":
			e.scope = append(e.scope, c)
		default:
			e.addCond(c)
		}
	}
	for _, act := range r.Actions {
		if strings.EqualFold(act.Type, "stop") {
			stop = true
			continue
		}
		e.addAct(act)
	}
	if len(e.condRows) == 0 {
		e.addCond(mailcore.RuleCondition{Field: "from", Op: "contains"})
	}
	if len(e.actRows) == 0 {
		e.addAct(mailcore.RuleAction{Type: "move"})
	}
	e.inbox = widgets.NewCheckbox("Only mail arriving in an Inbox", inboxOnly, nil)
	e.stop = widgets.NewCheckbox("Stop: no later rule runs on it", stop, nil)
	e.on = widgets.NewCheckbox("On", r.Enabled || r.ID == "", nil)

	matchRow := widgets.NewRow(widgets.NewLabel("When a message matches"), e.match).WithGap(8)
	addCond := widgets.NewButton("Add a test", func() { e.addCond(mailcore.RuleCondition{Field: "subject", Op: "contains"}); e.relayout() })
	addAct := widgets.NewButton("Add an action", func() { e.addAct(mailcore.RuleAction{Type: "tag"}); e.relayout() })
	body := widgets.NewColumn(
		widgets.NewLabel("Name"), e.name,
		matchRow, e.conds, widgets.NewRow(addCond).WithGap(8),
		widgets.NewLabel("Then"), e.acts, widgets.NewRow(addAct).WithGap(8),
		e.inbox, e.stop, e.on,
	).WithGap(8)
	if len(e.scope) > 0 {
		body.Add(wrapLabel("Only for account " + e.scope[0].Value + " (as it was imported)."))
	}
	fields := widgets.NewScrollView(body)
	save := widgets.NewButton("Save", func() {
		if err := e.save(); err != nil {
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
	root := widgets.NewColumn(fields, widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept)).WithGap(8)
	root.AddFlex(fields, 1)
	win.SetContent(widgets.NewPad(12, root))
	return e
}

func (e *ruleEditor) relayout() {
	e.conds.RequestLayout()
	e.acts.RequestLayout()
	e.conds.Invalidate()
	e.acts.Invalidate()
}

func (e *ruleEditor) addCond(c mailcore.RuleCondition) {
	cr := &condRow{}
	cr.field = widgets.NewComboBox(labelsOf(editorFields), keyIndex(editorFields, c.Field), nil)
	cr.op = widgets.NewComboBox(labelsOf(ruleOps), keyIndex(ruleOps, orDefault(c.Op, "contains")), nil)
	cr.value = widgets.NewTextField(c.Value, "text", nil)
	remove := widgets.NewButton("×", nil)
	remove.Tip = "Remove this test"
	cr.row = widgets.NewRow(cr.field, cr.op, cr.value, remove).WithGap(6)
	cr.row.AddFlex(cr.value, 1)
	sync := func() {
		switch editorFields[cr.field.Selected].key {
		case "attachment", "unread":
			cr.op.SetVisible(false)
			cr.value.SetVisible(false)
		case "tag":
			cr.op.SetVisible(false)
			cr.value.SetVisible(true)
		default:
			cr.op.SetVisible(true)
			cr.value.SetVisible(true)
		}
		cr.row.RequestLayout()
	}
	cr.field.OnChange = func(int) { sync() }
	sync()
	remove.OnClick = func() {
		if len(e.condRows) <= 1 {
			return // a rule needs a test
		}
		e.conds.Remove(cr.row)
		for i, x := range e.condRows {
			if x == cr {
				e.condRows = append(e.condRows[:i], e.condRows[i+1:]...)
				break
			}
		}
		e.relayout()
	}
	e.condRows = append(e.condRows, cr)
	e.conds.Add(cr.row)
}

func (e *ruleEditor) addAct(a mailcore.RuleAction) {
	ar := &actRow{path: a.Path, account: a.Account}
	ar.action = widgets.NewComboBox(labelsOf(editorActions), keyIndex(editorActions, a.Type), nil)
	ar.target = widgets.NewComboBox(nil, 0, nil)
	remove := widgets.NewButton("×", nil)
	remove.Tip = "Remove this action"
	gap := widgets.NewSpacer() // keeps × at the right when there is no target
	ar.row = widgets.NewRow(ar.action, ar.target, gap, remove).WithGap(6)
	ar.row.AddFlex(ar.target, 1)
	ar.row.AddFlex(gap, 1)
	fill := func() {
		switch editorActions[ar.action.Selected].key {
		case "move":
			items := append([]string(nil), e.flabels...)
			sel := 0
			for i, f := range e.folders {
				if f.ID == a.Folder {
					sel = i
				}
			}
			ar.pathItem = ar.path != "" && a.Folder == ""
			if ar.pathItem {
				// Not synced yet: kept by its server path until it is.
				items = append([]string{"(on the server) " + ar.path}, items...)
				sel = 0
			}
			ar.target.Items, ar.target.Selected = items, sel
			ar.target.SetVisible(len(items) > 0)
		case "tag":
			sel := 0
			for i, t := range e.tags {
				if strings.EqualFold(t, a.Tag) {
					sel = i
				}
			}
			ar.target.Items, ar.target.Selected = e.tags, sel
			ar.target.SetVisible(len(e.tags) > 0)
		default:
			ar.target.SetVisible(false)
		}
		gap.SetVisible(!ar.target.Visible())
		ar.row.RequestLayout()
	}
	ar.action.OnChange = func(int) { fill() }
	fill()
	remove.OnClick = func() {
		if len(e.actRows) <= 1 {
			return // a rule needs an action
		}
		e.acts.Remove(ar.row)
		for i, x := range e.actRows {
			if x == ar {
				e.actRows = append(e.actRows[:i], e.actRows[i+1:]...)
				break
			}
		}
		e.relayout()
	}
	e.actRows = append(e.actRows, ar)
	e.acts.Add(ar.row)
}

// save builds the rule from the editor and stores it.
func (e *ruleEditor) save() error {
	out := e.rule
	out.Any = e.match.Selected == 1
	out.Enabled, out.Stop = e.on.Checked, e.stop.Checked
	out.Conditions = append([]mailcore.RuleCondition(nil), e.scope...)
	if e.inbox.Checked {
		out.Conditions = append(out.Conditions, mailcore.RuleCondition{Field: "inbox"})
	}
	var first string
	for _, cr := range e.condRows {
		key := editorFields[cr.field.Selected].key
		c := mailcore.RuleCondition{Field: key}
		switch key {
		case "attachment", "unread":
		case "tag":
			c.Value = strings.TrimSpace(cr.value.Text)
			if c.Value == "" {
				return fmt.Errorf("say which tag")
			}
		default:
			c.Op, c.Value = ruleOps[cr.op.Selected].key, strings.TrimSpace(cr.value.Text)
			if c.Value == "" {
				return fmt.Errorf("say what %s should %s", strings.ToLower(editorFields[cr.field.Selected].label), ruleOps[cr.op.Selected].label)
			}
		}
		if first == "" {
			first = strings.TrimSpace(editorFields[cr.field.Selected].label + " " + labelOf(ruleOps, c.Op) + " " + c.Value)
		}
		out.Conditions = append(out.Conditions, c)
	}
	out.Actions = nil
	for _, ar := range e.actRows {
		a := mailcore.RuleAction{Type: editorActions[ar.action.Selected].key}
		switch a.Type {
		case "move":
			i := ar.target.Selected
			if ar.pathItem {
				i-- // the first item is the server path; -1 keeps it
			}
			switch {
			case i < 0 && ar.path != "":
				a.Path, a.Account = ar.path, ar.account
			case i >= 0 && i < len(e.folders):
				a.Folder = e.folders[i].ID
			default:
				return fmt.Errorf("choose the folder to move to")
			}
		case "tag":
			if ar.target.Selected < 0 || ar.target.Selected >= len(e.tags) {
				return fmt.Errorf("choose the tag")
			}
			a.Tag = e.tags[ar.target.Selected]
		}
		out.Actions = append(out.Actions, a)
	}
	if out.Stop {
		out.Actions = append(out.Actions, mailcore.RuleAction{Type: "stop"})
	}
	out.Name = strings.TrimSpace(e.name.Text)
	if out.Name == "" {
		out.Name = first
	}
	_, err := e.cli.PutRule(out)
	return err
}
