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

func TestReadMbox(t *testing.T) {
	msgs := readMboxMessages(strings.NewReader(sampleMbox), "local/inbox", "local")
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Subject != "First" || msgs[0].From != "Bob <bob@example.org>" {
		t.Fatalf("msg0 = %+v", msgs[0])
	}
	if !strings.Contains(msgs[0].Body, "From here on") {
		t.Fatalf("mboxrd un-escaping failed: %q", msgs[0].Body)
	}
	if strings.Contains(msgs[0].Body, ">From here") {
		t.Fatalf("still escaped: %q", msgs[0].Body)
	}
	if msgs[1].Subject != "Second" {
		t.Fatalf("msg1 = %+v", msgs[1])
	}
	if msgs[0].Folder != "local/inbox" || msgs[0].AccountID != "local" {
		t.Fatalf("folder/account not set: %+v", msgs[0])
	}
}

func TestReadMaildir(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(sub, name, body string) {
		if err := os.WriteFile(filepath.Join(dir, sub, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("cur", "1.eml:2,S", "From: a@x\nSubject: Read\nMessage-ID: <a@x>\n\nbody a\n")
	write("new", "2.eml", "From: b@x\nSubject: New\nMessage-ID: <b@x>\n\nbody b\n")
	if !isMaildir(dir) {
		t.Fatal("should be recognised as a maildir")
	}
	msgs := readMaildirMessages(dir, "local/x", "local")
	if len(msgs) != 2 {
		t.Fatalf("got %d, want 2", len(msgs))
	}
	got := map[string]bool{}
	for _, m := range msgs {
		got[m.Subject] = true
	}
	if !got["Read"] || !got["New"] {
		t.Fatalf("subjects = %v", got)
	}
}
