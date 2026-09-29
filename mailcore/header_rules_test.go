package mailcore

import (
	"strings"
	"testing"
)

func TestHeaderConditionMatches(t *testing.T) {
	m := Message{Headers: map[string]string{"List-Id": "Dev list <dev.lists.example.com>", "X-Spam-Flag": ""}}
	for c, want := range map[RuleCondition]bool{
		{Field: "header", Header: "list-id", Op: "contains", Value: "dev.lists"}:    true,
		{Field: "header", Header: "List-Id", Op: "notcontains", Value: "dev.lists"}: false,
		{Field: "header", Header: "X-Spam-Flag", Op: "is", Value: "YES"}:            false,
		{Field: "header", Header: "X-Mailer", Op: "notcontains", Value: "Outlook"}:  true, // absent reads as empty
	} {
		if got := c.match(m); got != want {
			t.Errorf("%+v: %v, want %v", c, got, want)
		}
	}
	if err := checkRule(FilterRule{Conditions: []RuleCondition{{Field: "header", Header: "List Id"}}}); err == nil {
		t.Fatal("a header name with a space was taken")
	}
}

func TestHeaderFieldsResponseParses(t *testing.T) {
	block := "List-Id: =?utf-8?q?Caf=C3=A9_list?= <cafe.example>\r\nX-Spam-Flag: NO\r\n\r\n"
	ln := "* 3 FETCH (UID 42 BODY[HEADER.FIELDS (LIST-ID X-SPAM-FLAG)] {" + itoa(len(block)) + "}\r\n" + block + ")"
	got, rest := cutHeaderLiteral(ln)
	if got != block || !strings.Contains(rest, "UID 42") || strings.Contains(rest, "Caf") {
		t.Fatalf("block %q rest %q", got, rest)
	}
	h := headersOf([]byte(got), []string{"List-Id", "X-Spam-Flag", "Reply-To"})
	if h["List-Id"] != "Café list <cafe.example>" || h["X-Spam-Flag"] != "NO" {
		t.Fatalf("headers %v", h)
	}
	if v, ok := h["Reply-To"]; !ok || v != "" {
		t.Fatal("a header looked for and absent should be there, empty")
	}
}

// A Thunderbird filter on a custom header (quoted in its file) is a
// header test now, not left out.
func TestThunderbirdCustomHeaderFilter(t *testing.T) {
	fs := parseThunderbirdFilters(`name="Lists"
enabled="yes"
action="Mark read"
condition="AND (\"list-id\",contains,dev.lists)"
`)
	r, why := tbRule(fs[0], FilterSet{Host: "imap.example.com"}, "home", nil)
	if why != "" || len(r.Conditions) != 3 || r.Conditions[2] != (RuleCondition{Field: "header", Header: "list-id", Op: "contains", Value: "dev.lists"}) {
		t.Fatalf("rule %+v, why %q", r, why)
	}
}

// Run now fetches the headers the rules test for mail already here (once:
// not again on the next run), and new mail brings them as it arrives; a
// rule on List-Id tags what matches.
func TestHeaderRuleOnMailHereAndNew(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1, 2}})
	fs.hdrs = map[uint32]string{1: "List-Id: <dev.lists.example.com>", 2: "List-Id: <other.example>"}
	st := newFolderStore(t, fs) // synced before the rule exists
	if _, err := st.PutRule(FilterRule{Name: "Dev list", Enabled: true,
		Conditions: []RuleCondition{{Field: "header", Header: "List-Id", Op: "contains", Value: "dev.lists"}},
		Actions:    []RuleAction{{Type: "tag", Tag: "Work"}}}); err != nil {
		t.Fatal(err)
	}
	inbox, _ := folderByRemote(st, "INBOX")
	tagged := func() map[uint32]bool {
		out := map[uint32]bool{}
		for _, m := range st.ListMessages(inbox.ID) {
			out[m.UID] = HasTag(m.Tags, "Work")
		}
		return out
	}

	if _, err := st.ApplyRules(inbox.ID); err != nil {
		t.Fatal(err)
	}
	if got := tagged(); !got[1] || got[2] {
		t.Fatalf("after Run now: %v (header fetches %d)", got, fs.headerFetches)
	}
	before := fs.headerFetches
	if _, err := st.ApplyRules(inbox.ID); err != nil {
		t.Fatal(err)
	}
	if fs.headerFetches != before {
		t.Fatal("Run now fetched headers it already had")
	}

	// New mail, after the rule: its List-Id comes with it.
	fs.mu.Lock()
	fs.boxes["INBOX"] = append(fs.boxes["INBOX"], 3)
	fs.hdrs[3] = "List-Id: Dev <dev.lists.example.com>"
	fs.mu.Unlock()
	if _, err := st.Sync(""); err != nil {
		t.Fatal(err)
	}
	if got := tagged(); !got[3] {
		t.Fatalf("new mail: %v", got)
	}
}
