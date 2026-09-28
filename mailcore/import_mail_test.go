package mailcore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportLocalMailFromThunderbird(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// A Thunderbird profile with Local Folders holding an Inbox mbox and a
	// nested Archives/2023 mbox.
	lf := filepath.Join(home, ".thunderbird", "p.default", "Mail", "Local Folders")
	if err := os.MkdirAll(filepath.Join(lf, "Archives.sbd"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A profile prefs.js so ThunderbirdProfiles finds it.
	os.WriteFile(filepath.Join(home, ".thunderbird", "p.default", "prefs.js"), []byte(""), 0o600)
	os.WriteFile(filepath.Join(lf, "Inbox"), []byte(sampleMbox), 0o600)
	os.WriteFile(filepath.Join(lf, "Inbox.msf"), []byte("index, must be ignored"), 0o600)
	os.WriteFile(filepath.Join(lf, "Archives.sbd", "2023"),
		[]byte("From x\nFrom: X <x@ex>\nSubject: Arch\nMessage-ID: <arch@ex>\n\nold mail\n"), 0o600)

	if got := len(DiscoverLocalMail()); got != 2 {
		t.Fatalf("discovered %d local folders, want 2 (Inbox + Archives/2023)", got)
	}

	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.ImportLocalMail()
	if err != nil {
		t.Fatal(err)
	}
	if res.Folders != 2 || res.Messages != 3 {
		t.Fatalf("import result = %+v, want 2 folders / 3 messages", res)
	}

	// The local account and its folders exist.
	local := false
	for _, a := range st.Accounts() {
		if a.ID == LocalAccountID {
			local = true
		}
	}
	if !local {
		t.Fatal("the On This Computer account was not created")
	}
	fs := st.ListFolders(LocalAccountID)
	if len(fs) != 2 {
		t.Fatalf("local account has %d folders, want 2: %v", len(fs), fs)
	}
	total := 0
	for _, f := range fs {
		for _, m := range st.ListMessages(f.ID) {
			if !m.Read {
				t.Errorf("imported message %s should be marked read", m.ID)
			}
			total++
		}
	}
	if total != 3 {
		t.Fatalf("imported %d messages into folders, want 3", total)
	}

	// A local account is skipped by sync (no server): syncing it adds
	// nothing and does not try to connect.
	if n, _ := st.syncAccount(LocalAccountID); n != 0 {
		t.Fatalf("syncAccount(local) returned %d, want 0 (skipped)", n)
	}

	// Idempotent: a second import adds nothing.
	res2, err := st.ImportLocalMail()
	if err != nil {
		t.Fatal(err)
	}
	if res2.Messages != 0 {
		t.Fatalf("second import added %d messages, want 0", res2.Messages)
	}
}

// Thunderbird's Local Folders holds more than mailboxes: indexes, the filter
// log, filter rules, POP state. Only real mboxes are offered.
func TestScanMboxTreeSkipsNonMail(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Inbox"), []byte(sampleMbox), 0o600)
	os.WriteFile(filepath.Join(dir, "Inbox.msf"), []byte("// <!-- <mdb:mork"), 0o600)
	os.WriteFile(filepath.Join(dir, "filterlog.html"), []byte("<html><body>log</body></html>"), 0o600)
	os.WriteFile(filepath.Join(dir, "msgFilterRules.dat"), []byte("version=\"9\""), 0o600)
	os.WriteFile(filepath.Join(dir, "Trash"), []byte(""), 0o600) // empty mailbox
	got := scanMboxTree("Thunderbird", dir, "")
	if len(got) != 1 || got[0].Name != "Inbox" {
		t.Fatalf("offered %v, want only Inbox", got)
	}
}
