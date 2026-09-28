package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

func TestRuleSummary(t *testing.T) {
	r := mailcore.FilterRule{Any: true, Conditions: []mailcore.RuleCondition{
		{Field: "account", Value: "home"}, {Field: "inbox"},
		{Field: "from", Op: "is", Value: "boss@example.com"}, {Field: "subject", Op: "begins", Value: "Weekly"},
	}, Actions: []mailcore.RuleAction{{Type: "move", Path: "Lists/News"}, {Type: "markRead"}, {Type: "stop"}}}
	got := ruleSummary(r, func(id mailcore.FolderID) string { return string(id) })
	want := "When From is “boss@example.com” or Subject begins with “Weekly” (home Inbox) → move to Lists/News, mark read, stop"
	if got != want {
		t.Fatalf("summary\n %q\nwant\n %q", got, want)
	}
	if !editableRule(r) {
		t.Fatal("a rule of tests and actions the editor offers should be editable")
	}
	if editableRule(mailcore.FilterRule{Conditions: []mailcore.RuleCondition{{Field: "size"}}}) {
		t.Fatal("a test the editor does not offer is not editable")
	}
}

// The editor makes a rule with several tests and actions, which the
// Filters tab lists, turns off, and edits again.
func TestFiltersTabAndEditor(t *testing.T) {
	cli := demoClient(t)
	before, _ := cli.Rules()
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	e := openRuleEditor(a, cli, mailcore.FilterRule{Enabled: true, Conditions: []mailcore.RuleCondition{{Field: "inbox"}}}, nil)
	if e == nil || len(e.condRows) != 1 || len(e.actRows) != 1 || !e.inbox.Checked {
		t.Fatalf("new editor %+v", e)
	}
	// Subject begins with "Invoice" OR From contains "billing@" → tag, mark read, stop.
	e.condRows[0].field.Selected = keyIndex(editorFields, "subject")
	e.condRows[0].field.OnChange(0)
	e.condRows[0].op.Selected = keyIndex(ruleOps, "begins")
	e.condRows[0].value.SetText("Invoice")
	e.addCond(mailcore.RuleCondition{Field: "from", Op: "contains", Value: "billing@"})
	e.match.Selected = 1
	e.actRows[0].action.Selected = keyIndex(editorActions, "tag")
	e.actRows[0].action.OnChange(0)
	e.addAct(mailcore.RuleAction{Type: "markRead"})
	e.stop.SetChecked(true)
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	after, _ := cli.Rules()
	if len(after) != len(before)+1 {
		t.Fatalf("rules %d → %d", len(before), len(after))
	}
	r := after[len(after)-1]
	if !r.Any || !r.Stop || len(r.Conditions) != 3 || len(r.Actions) != 3 || r.Actions[0].Type != "tag" || r.Actions[1].Type != "markRead" || r.Actions[2].Type != "stop" {
		t.Fatalf("rule %+v", r)
	}
	if r.Name != "Subject begins with Invoice" {
		t.Fatalf("name %q", r.Name)
	}

	// Editing it again shows it whole.
	e2 := openRuleEditor(a, cli, r, nil)
	if len(e2.condRows) != 2 || len(e2.actRows) != 2 || !e2.stop.Checked || e2.match.Selected != 1 {
		t.Fatalf("reopened: %d tests, %d actions, stop %v", len(e2.condRows), len(e2.actRows), e2.stop.Checked)
	}

	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 560, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	col := prefsFilters(a, w, cli)
	w.SetContent(col)
	a.PumpOnce()
	var table *widgets.TableView
	buttons := map[string]*widgets.Button{}
	widget.Walk(col, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TableView:
			table = v
		case *widgets.Button:
			buttons[v.Text] = v
		}
	})
	table.Selected = len(after) - 1
	table.OnSelect(table.Selected)
	if !buttons["Edit…"].Enabled() || !strings.Contains(table.CellText(table.Selected, 2), "Subject begins with “Invoice” or From contains “billing@”") {
		t.Fatalf("row %q edit %v", table.CellText(table.Selected, 2), buttons["Edit…"].Enabled())
	}
	buttons["Turn Off"].OnClick()
	if got, _ := cli.Rules(); got[len(got)-1].Enabled {
		t.Fatal("Turn Off left it on")
	}
}

// An imported rule — account scope, a move by server path — edits without
// losing either.
func TestEditorKeepsScopeAndServerPath(t *testing.T) {
	cli := demoClient(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	r := mailcore.FilterRule{ID: "rule-x", Name: "Thunderbird: News", Enabled: true,
		Conditions: []mailcore.RuleCondition{{Field: "account", Value: "ada"}, {Field: "inbox"}, {Field: "from", Op: "contains", Value: "news@"}},
		Actions:    []mailcore.RuleAction{{Type: "move", Account: "ada", Path: "Lists/News"}}}
	e := openRuleEditor(a, cli, r, nil)
	if len(e.scope) != 1 || !e.actRows[0].pathItem {
		t.Fatalf("scope %v path item %v", e.scope, e.actRows[0].pathItem)
	}
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	rules, _ := cli.Rules()
	got := rules[len(rules)-1]
	if got.Conditions[0].Field != "account" || got.Actions[0].Path != "Lists/News" || got.Actions[0].Folder != "" {
		t.Fatalf("saved %+v", got)
	}
}
