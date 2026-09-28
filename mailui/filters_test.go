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
	if simpleRule(r) {
		t.Fatal("a two-test rule is not simple")
	}
}

// The editor makes a rule the Filters tab lists; the tab turns it off and
// deletes it.
func TestFiltersTabAndEditor(t *testing.T) {
	cli := demoClient(t)
	before, _ := cli.Rules()
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	openRuleEditor(a, cli, mailcore.FilterRule{Enabled: true, Conditions: []mailcore.RuleCondition{{Field: "inbox"}}}, nil)
	a.PumpOnce()
	ws := a.Windows()
	ed := ws[len(ws)-1]
	var value *widgets.TextField
	var combos []*widgets.ComboBox
	var save *widgets.Button
	widget.Walk(ed.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			if v.Placeholder == "text" {
				value = v
			}
		case *widgets.ComboBox:
			combos = append(combos, v)
		case *widgets.Button:
			if v.Text == "Save" {
				save = v
			}
		}
	})
	if value == nil || len(combos) != 4 || save == nil {
		t.Fatalf("editor parts: value %v combos %d save %v", value, len(combos), save)
	}
	combos[0].Selected = 2 // Subject
	combos[1].Selected = 4 // begins with
	value.SetText("Invoice")
	combos[2].Selected = 1 // Tag
	combos[2].OnChange(1)
	save.OnClick()
	after, _ := cli.Rules()
	if len(after) != len(before)+1 {
		t.Fatalf("rules %d → %d", len(before), len(after))
	}
	r := after[len(after)-1]
	if r.Name != "Subject begins with Invoice" || r.Actions[0].Type != "tag" || r.Actions[0].Tag == "" || r.Conditions[1].Field != "inbox" {
		t.Fatalf("rule %+v", r)
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
	if !buttons["Edit…"].Enabled() || !strings.Contains(table.CellText(table.Selected, 2), "Subject begins with “Invoice”") {
		t.Fatalf("row %q edit %v", table.CellText(table.Selected, 2), buttons["Edit…"].Enabled())
	}
	buttons["Turn Off"].OnClick()
	if got, _ := cli.Rules(); got[len(got)-1].Enabled {
		t.Fatal("Turn Off left it on")
	}
}
