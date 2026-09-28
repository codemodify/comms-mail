package mailcore

import (
	"path/filepath"
	"strings"
	"testing"
)

const tbFilterFile = `version="9"
logging="no"
name="Newsletters"
enabled="yes"
type="17"
action="Move to folder"
actionValue="imap://alice%40example.com@imap.example.com/Lists/News%20letters"
action="Mark read"
condition="AND (from,contains,news@example.org) AND (subject,begins with,\"Weekly, \")"
name="Boss"
enabled="no"
type="17"
action="AddTag"
actionValue="$label1"
action="Stop execution"
condition="OR (from,is,boss@example.com) OR (to or cc,contains,board)"
name="Old stuff"
enabled="yes"
type="17"
action="Delete"
condition="AND (age in days,is greater than,365)"
name="Forwarder"
enabled="yes"
type="17"
action="Forward"
actionValue="me@example.net"
condition="ALL"
`

func TestParseThunderbirdFilters(t *testing.T) {
	fs := parseThunderbirdFilters(tbFilterFile)
	if len(fs) != 4 {
		t.Fatalf("filters %d", len(fs))
	}
	f := fs[0]
	if f.Name != "Newsletters" || !f.Enabled || f.Match != "AND" || len(f.Conds) != 2 || f.Conds[1] != [3]string{"subject", "begins with", "Weekly, "} {
		t.Fatalf("first %+v", f)
	}
	if fs[1].Match != "OR" || fs[1].Enabled || fs[1].Actions[0] != [2]string{"AddTag", "$label1"} {
		t.Fatalf("second %+v", fs[1])
	}
	if tbFolderPath(f.Actions[0][1]) != "Lists/News letters" {
		t.Fatalf("path %q", tbFolderPath(f.Actions[0][1]))
	}
}

// Filters become rules scoped to their account and Inbox; one that tests
// or does something rules cannot is left out with the reason.
func TestImportFiltersAndRunThem(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}, "Lists/News letters": nil, "Lists": nil})
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "alice@example.com",
		IMAP: ServerConfig{Host: fs.addr(), User: "alice@example.com", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.Split(fs.addr(), ":")[0]
	file := strings.ReplaceAll(tbFilterFile, "imap.example.com", host) // the folder URIs name the server
	res := st.ImportFilters([]FilterSet{{Host: host, User: "alice@example.com", Filters: parseThunderbirdFilters(file)},
		{Host: "elsewhere.example", Filters: parseThunderbirdFilters(file)[:1]}})
	if res.Added != 2 || len(res.Skipped) != 3 {
		t.Fatalf("import %+v", res)
	}
	joined := strings.Join(res.Skipped, "|")
	for _, want := range []string{`"Old stuff" tests "age in days"`, `"Forwarder" does "Forward"`, `its account (elsewhere.example)`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("skipped %q lacks %q", joined, want)
		}
	}
	if again := st.ImportFilters([]FilterSet{{Host: host, Filters: parseThunderbirdFilters(file)}}); again.Added != 0 {
		t.Fatalf("imported twice: %+v", again)
	}

	// Mail from the list that arrives in the Inbox is moved and read.
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	m := Message{ID: "home/inbox:9", Folder: "home/inbox", AccountID: "home", From: "news@example.org", Subject: "Weekly, issue 12"}
	st.mu.Lock()
	st.Messages = append(st.Messages, m)
	st.applyRulesOnLocked(&st.Messages[len(st.Messages)-1])
	got := st.Messages[len(st.Messages)-1]
	st.mu.Unlock()
	lists, _ := folderByRemote(st, "Lists/News letters")
	if got.Folder != lists.ID || !got.Read {
		t.Fatalf("after the rule: folder %s read %v (want %s)", got.Folder, got.Read, lists.ID)
	}
	// The same mail in another folder is left alone.
	other := Message{ID: "home/lists:5", Folder: "home/lists", AccountID: "home", From: "news@example.org", Subject: "Weekly, issue 13"}
	st.mu.Lock()
	st.Messages = append(st.Messages, other)
	st.applyRulesOnLocked(&st.Messages[len(st.Messages)-1])
	moved := st.Messages[len(st.Messages)-1].Folder
	st.mu.Unlock()
	if moved != "home/lists" {
		t.Fatalf("a rule for the Inbox moved mail in another folder to %s", moved)
	}
}

func TestRuleMatchingOps(t *testing.T) {
	m := Message{From: "Boss <boss@example.com>", Subject: "Weekly report", AccountID: "a", Folder: "a/inbox"}
	for _, c := range []struct {
		cond RuleCondition
		want bool
	}{
		{RuleCondition{Field: "subject", Op: "begins", Value: "weekly"}, true},
		{RuleCondition{Field: "subject", Op: "ends", Value: "REPORT"}, true},
		{RuleCondition{Field: "subject", Op: "notcontains", Value: "invoice"}, true},
		{RuleCondition{Field: "subject", Op: "isnot", Value: "Weekly report"}, false},
		{RuleCondition{Field: "account", Value: "a"}, true},
		{RuleCondition{Field: "inbox"}, true},
	} {
		if got := c.cond.match(m); got != c.want {
			t.Fatalf("%+v: %v", c.cond, got)
		}
	}
	any := FilterRule{Enabled: true, Any: true, Conditions: []RuleCondition{
		{Field: "account", Value: "b"}, {Field: "subject", Op: "contains", Value: "weekly"}}}
	if any.match(m) {
		t.Fatal("an OR rule ignored its account scope")
	}
	any.Conditions[0].Value = "a"
	if !any.match(m) {
		t.Fatal("an OR rule did not match on one condition")
	}
}

func TestNextRuleIDSkipsTaken(t *testing.T) {
	if id := nextRuleID([]FilterRule{{ID: "rule-01"}, {ID: "rule-03"}}); id == "rule-01" || id == "rule-03" {
		t.Fatalf("id %s is taken", id)
	}
}
