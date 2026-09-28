package mailcore

import (
	"testing"
	"time"
)

func TestBuildAndSuggestContacts(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs := []Message{
		{From: `Jane Doe <jane@example.com>`, Date: base},
		{From: `Jane Doe <jane@example.com>`, To: "bob@example.org", Date: base.AddDate(0, 0, 5)},
		{From: `"Smith, John" <john@work.example>`, Date: base.AddDate(0, 0, 1)},
		{To: `jane@example.com, Bob <bob@example.org>`, Date: base.AddDate(0, 0, 2)},
		{From: "noaddress", Date: base}, // ignored: no @
	}
	book := buildContacts(msgs)
	byAddr := map[string]Contact{}
	for _, c := range book {
		byAddr[c.Address] = c
	}
	if len(byAddr) != 3 {
		t.Fatalf("book has %d addresses, want 3: %+v", len(byAddr), book)
	}
	jane := byAddr["jane@example.com"]
	if jane.Name != "Jane Doe" || jane.Count != 3 {
		t.Fatalf("jane = %+v, want name Jane Doe count 3", jane)
	}
	if !jane.Last.Equal(base.AddDate(0, 0, 5)) {
		t.Fatalf("jane.Last = %v", jane.Last)
	}
	// Most-corresponded-with first.
	if book[0].Address != "jane@example.com" {
		t.Fatalf("book[0] = %s, want jane (highest count)", book[0].Address)
	}

	// "jo" matches John by address prefix and name word; ranks above a mid
	// substring. "doe" matches Jane by name word.
	got := suggestContacts(book, "jo", 5)
	if len(got) == 0 || got[0].Address != "john@work.example" {
		t.Fatalf(`suggest "jo" = %+v, want john first`, got)
	}
	if d := suggestContacts(book, "doe", 5); len(d) != 1 || d[0].Address != "jane@example.com" {
		t.Fatalf(`suggest "doe" = %+v, want jane`, d)
	}
	// Empty query returns the ranked book, capped.
	if e := suggestContacts(book, "", 2); len(e) != 2 || e[0].Address != "jane@example.com" {
		t.Fatalf(`suggest "" = %+v`, e)
	}
	if c := (Contact{Name: "Jane Doe", Address: "jane@example.com"}).Display(); c != "Jane Doe <jane@example.com>" {
		t.Fatalf("Display = %q", c)
	}
	if c := (Contact{Address: "x@y.z"}).Display(); c != "x@y.z" {
		t.Fatalf("bare Display = %q", c)
	}
}

func TestContactsSuggestRPC(t *testing.T) {
	sock, stop, err := StartDemo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	got, err := cli.SuggestContacts("", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("the demo store should yield some contacts")
	}
	// A prefix of a demo sender narrows it.
	one, err := cli.SuggestContacts(got[0].Address[:3], 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) == 0 {
		t.Fatalf("prefix %q matched nothing", got[0].Address[:3])
	}
}
