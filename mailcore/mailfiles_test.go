package mailcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleMbox = `From bob@example.org Mon Jan 01 00:00:00 2024
From: Bob <bob@example.org>
To: ada@example.com
Subject: First
Message-ID: <1@ex>
Date: Mon, 1 Jan 2024 00:00:00 +0000

Hello Ada.
>From here on it is body text.

From carol@example.org Tue Jan 02 00:00:00 2024
From: Carol <carol@example.org>
Subject: Second
Message-ID: <2@ex>

Second body.
`

func collect(st LocalMailStore) []Message {
	var out []Message
	st.each(func(raw []byte, _ mailFlags) {
		if m, err := ParseRFC822(raw, "f", "a"); err == nil {
			out = append(out, m)
		}
	})
	return out
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func msg(id, subj string) string {
	return "From: x@ex\nSubject: " + subj + "\nMessage-ID: <" + id + ">\n\nbody " + subj + "\n"
}

func TestMboxReader(t *testing.T) {
	var got []Message
	eachMboxRaw(strings.NewReader(sampleMbox), func(raw []byte) {
		m, _ := ParseRFC822(raw, "f", "a")
		got = append(got, m)
	})
	if len(got) != 2 || got[0].Subject != "First" || got[1].Subject != "Second" {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(got[0].Body, "From here on") || strings.Contains(got[0].Body, ">From here") {
		t.Fatalf("mboxrd un-escaping: %q", got[0].Body)
	}
}

// One tree holding every kind of store, the way a chosen folder might.
func TestScanAnyTreeFindsEveryKind(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "old.mbox-file"), sampleMbox)                             // mbox (by content)
	mustWrite(t, filepath.Join(root, "notes.txt"), "not mail")                                 // ignored
	mustWrite(t, filepath.Join(root, "Maildir", "cur", "1:2,S"), msg("m1@ex", "md1"))          // maildir root
	mustWrite(t, filepath.Join(root, "Maildir", ".Sent", "cur", "2:2,S"), msg("m2@ex", "md2")) // Maildir++
	mustWrite(t, filepath.Join(root, "Maildir", "new", "3"), msg("m3@ex", "md3"))
	mustWrite(t, filepath.Join(root, "claws", "inbox", "1"), msg("h1@ex", "mh1")) // MH
	mustWrite(t, filepath.Join(root, "claws", "inbox", "2"), msg("h2@ex", "mh2"))
	mustWrite(t, filepath.Join(root, "claws", "inbox", ".mh_sequences"), "unseen: 2")
	mustWrite(t, filepath.Join(root, "exports", "a.eml"), msg("e1@ex", "eml1")) // .eml dir
	emlx := msg("x1@ex", "emlx1")
	mustWrite(t, filepath.Join(root, "Apple", "Receipts.mbox", "Data", "1", "Messages", "5.emlx"),
		strings.Repeat(" ", 2)+itoa(len(emlx))+"\n"+emlx+`<?xml version="1.0"?><plist/>`) // Apple
	mustWrite(t, filepath.Join(root, "Apple", "Receipts.mbox", "2024.mbox", "Messages", "6.emlx"),
		itoa(len(msg("x2@ex", "emlx2")))+"\n"+msg("x2@ex", "emlx2"))

	stores := scanAnyTree("Folder", root, "", "root", 8)
	byName := map[string]LocalMailStore{}
	for _, st := range stores {
		byName[st.Name] = st
	}
	want := map[string]string{
		"old.mbox-file":       StoreMbox,
		"Maildir":             StoreMaildir,
		"Sent":                StoreMaildir,
		"claws/inbox":         StoreMH,
		"exports":             StoreEML,
		"Apple/Receipts":      StoreEMLX,
		"Apple/Receipts/2024": StoreEMLX,
	}
	for name, kind := range want {
		st, ok := byName[name]
		if !ok || st.Kind != kind {
			t.Errorf("store %q: got %+v, want kind %s (all: %v)", name, st, kind, stores)
		}
	}
	if len(stores) != len(want) {
		t.Errorf("found %d stores, want %d: %+v", len(stores), len(want), stores)
	}

	counts := map[string]int{"old.mbox-file": 2, "Maildir": 2, "Sent": 1, "claws/inbox": 2, "exports": 1, "Apple/Receipts": 1, "Apple/Receipts/2024": 1}
	for name, n := range counts {
		if got := len(collect(byName[name])); got != n {
			t.Errorf("%s: read %d messages, want %d", name, got, n)
		}
	}
	// The Apple parent must not swallow its nested mailbox's message.
	for _, m := range collect(byName["Apple/Receipts"]) {
		if m.Subject == "emlx2" {
			t.Error("the parent .mbox read its child mailbox's message")
		}
	}
}

// Thunderbird's Local Folders holds more than mailboxes: indexes, the filter
// log, filter rules, POP state. Only real mboxes are offered, nested .sbd
// folders by their plain names.
func TestScanSkipsNonMail(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Inbox"), sampleMbox)
	mustWrite(t, filepath.Join(dir, "Inbox.msf"), "// <!-- <mdb:mork")
	mustWrite(t, filepath.Join(dir, "filterlog.html"), "<html><body>log</body></html>")
	mustWrite(t, filepath.Join(dir, "msgFilterRules.dat"), `version="9"`)
	mustWrite(t, filepath.Join(dir, "Trash"), "")
	mustWrite(t, filepath.Join(dir, "Archives.sbd", "2023"), "From a Mon Jan 1 00:00:00 2024\n"+msg("a@ex", "arch"))
	got := map[string]string{}
	for _, st := range scanAnyTree("Thunderbird", dir, "", "Local Folders", 8) {
		got[st.Name] = st.Kind
	}
	if len(got) != 2 || got["Inbox"] != StoreMbox || got["Archives/2023"] != StoreMbox {
		t.Fatalf("offered %v, want Inbox and Archives/2023 (mbox)", got)
	}
}

func TestDecodeEMLX(t *testing.T) {
	m := msg("z@ex", "z")
	raw, _ := decodeEMLX([]byte(itoa(len(m)) + "\n" + m + "<?xml?><plist></plist>"))
	if string(raw) != m {
		t.Fatalf("decodeEMLX = %q", raw)
	}
	if raw, _ := decodeEMLX([]byte("garbage")); raw != nil {
		t.Fatal("garbage should not decode")
	}
}
