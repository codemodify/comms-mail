package mailcore

import (
	"path/filepath"
	"strings"
	"testing"
)

func newImportStore(t *testing.T) *LocalStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestImportLocalMailFromThunderbird(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prof := filepath.Join(home, ".thunderbird", "p.default")
	lf := filepath.Join(prof, "Mail", "Local Folders")
	mustWrite(t, filepath.Join(prof, "prefs.js"), "")
	mustWrite(t, filepath.Join(lf, "Inbox"), sampleMbox)
	mustWrite(t, filepath.Join(lf, "Inbox.msf"), "index, must be ignored")
	mustWrite(t, filepath.Join(lf, "Archives.sbd", "2023"), "From x Mon Jan 1 00:00:00 2024\n"+msg("arch@ex", "Arch"))

	stores := thunderbirdLocalMail()
	if len(stores) != 2 {
		t.Fatalf("found %d local folders, want 2: %+v", len(stores), stores)
	}
	st := newImportStore(t)
	res, err := st.ImportLocalMail(stores)
	if err != nil {
		t.Fatal(err)
	}
	if res.Folders != 2 || res.Messages != 3 {
		t.Fatalf("result %+v, want 2 folders / 3 messages", res)
	}
	fs := st.ListFolders(LocalAccountID)
	if len(fs) != 2 {
		t.Fatalf("local account has %d folders, want 2", len(fs))
	}
	for _, f := range fs {
		for _, m := range st.ListMessages(f.ID) {
			if !m.Read {
				t.Errorf("%s should be marked read", m.ID)
			}
			// The original bytes are kept, so the source opens.
			raw, err := st.GetRaw(m.ID)
			if err != nil || !strings.Contains(string(raw), "Subject:") {
				t.Errorf("%s: raw source not kept (%v)", m.ID, err)
			}
		}
	}
	if n, _ := st.syncAccount(LocalAccountID); n != 0 {
		t.Fatal("the local account must not sync")
	}
	// Idempotent.
	if res2, err := st.ImportLocalMail(stores); err != nil || res2.Messages != 0 {
		t.Fatalf("second import: %+v %v, want nothing new", res2, err)
	}
}

// A message without a Message-ID is still imported once, not twice.
func TestImportDedupWithoutMessageID(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "box"), "From a Mon Jan 1 00:00:00 2024\nFrom: a@ex\nSubject: no id\n\nhello\n")
	st := newImportStore(t)
	stores := []LocalMailStore{{Source: "Folder · box", Name: "box", Path: filepath.Join(dir, "box"), Kind: StoreMbox}}
	for i, want := range []int{1, 0} {
		res, err := st.ImportLocalMail(stores)
		if err != nil || res.Messages != want {
			t.Fatalf("import %d: %+v %v, want %d", i+1, res, err, want)
		}
	}
}

func TestImportRejectsBadStores(t *testing.T) {
	st := newImportStore(t)
	for _, bad := range []LocalMailStore{
		{Source: "x", Name: "x", Path: "relative/path", Kind: StoreMbox},
		{Source: "x", Name: "x", Path: "/nonexistent/for/sure", Kind: StoreMbox},
		{Source: "x", Name: "x", Path: "/tmp", Kind: "pst"},
	} {
		if _, err := st.ImportLocalMail([]LocalMailStore{bad}); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestScanPath(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Maildir", "cur", "1"), msg("p@ex", "p"))
	src, err := ScanPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Mail) != 1 || src.Mail[0].Kind != StoreMaildir || !strings.HasPrefix(src.Source, "Folder · ") {
		t.Fatalf("ScanPath = %+v", src)
	}
	empty := t.TempDir()
	src, _ = ScanPath(empty)
	if len(src.Mail) != 0 || src.Note == "" {
		t.Fatalf("an empty folder should come back with a note: %+v", src)
	}
	if _, err := ScanPath("relative"); err == nil {
		t.Fatal("a relative path should be refused")
	}
}
