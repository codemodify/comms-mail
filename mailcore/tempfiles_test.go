package mailcore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Old temporary copies go; recent ones, and anything a link points to,
// stay.
func TestRemoveOldTempCopies(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(t.TempDir(), "outside.txt")
	write := func(p string, age time.Duration) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-age)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "print-old.html"), 2*time.Hour)
	write(filepath.Join(dir, "print-new.html"), time.Minute)
	write(keep, 48*time.Hour)
	link := filepath.Join(dir, "print-link.html")
	if err := os.Symlink(keep, link); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(link, old, old) // the link's own time is its target's; it counts as old either way here
	write(filepath.Join(dir, "drag-old", "a.pdf"), 48*time.Hour)
	oldDir := filepath.Join(dir, "drag-old")
	_ = os.Chtimes(oldDir, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))

	RemoveOld(filepath.Join(dir, "print-*"), time.Hour)
	RemoveOld(filepath.Join(dir, "drag-*"), 24*time.Hour)
	for p, want := range map[string]bool{
		filepath.Join(dir, "print-old.html"): false,
		filepath.Join(dir, "print-new.html"): true,
		oldDir:                               false,
		keep:                                 true,
	} {
		_, err := os.Stat(p)
		if got := err == nil; got != want {
			t.Errorf("%s: present %v, want %v", filepath.Base(p), got, want)
		}
	}
}

// Opening an attachment clears out the ones opened long ago.
func TestOpenPartClearsOldOpenedCopies(t *testing.T) {
	dir := t.TempDir()
	st := newTestLocalStore(t, dir)
	seedCache(st, 1)
	old := filepath.Join(dir, "open", "last-week.pdf")
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	week := time.Now().Add(-7 * 24 * time.Hour)
	_ = os.Chtimes(old, week, week)
	st.mu.Lock()
	st.WriteRawLocked(st.Messages[0], []byte("From: a@example.com\r\nSubject: s\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nhi\r\n--b\r\nContent-Type: text/plain; name=\"notes.txt\"\r\nContent-Disposition: attachment; filename=\"notes.txt\"\r\n\r\nnotes\r\n--b--\r\n"))
	st.mu.Unlock()
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	if _, err := st.OpenPart(st.Messages[0].ID, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatal("a week-old opened copy is still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "open", "notes.txt")); err != nil {
		t.Fatalf("the new copy: %v", err)
	}
}
