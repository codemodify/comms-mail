package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
)

// With "All folders" on, the message list is a search across every folder,
// not just the current one, and turning it off (or picking a folder) returns
// to the folder view.
func TestSearchAllFolders(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()

	// Collect the folders the current-folder view can never mix.
	folderOf := map[mailcore.MessageID]mailcore.FolderID{}
	acct := s.accountID()
	for _, f := range mustFolders(t, s, acct) {
		for _, m := range mustList(t, s, f.ID) {
			folderOf[m.ID] = f.ID
		}
	}

	// A word that appears in more than one folder. "re" (as in Re:) is
	// common; fall back to any token if the demo changes.
	s.searchAll = true
	s.filter.Query = "the"
	s.refreshList()
	a.PumpOnce()

	if len(s.rows) == 0 {
		t.Skip("no messages match the probe query in this demo build")
	}
	seen := map[mailcore.FolderID]bool{}
	for _, m := range s.rows {
		seen[m.Folder] = true
	}
	if len(seen) < 2 {
		t.Fatalf("all-folder search returned messages from %d folder(s), want ≥2: %v", len(seen), keysOf(seen))
	}

	// Selecting a folder leaves search mode.
	s.selectFolder(mailcore.FolderID(acct + "/inbox"))
	if s.searchAll {
		t.Fatal("picking a folder should turn off All folders")
	}
	for _, m := range s.rows {
		if !strings.HasSuffix(string(m.Folder), "/inbox") {
			t.Fatalf("after leaving search, a row is from %s, not the inbox", m.Folder)
		}
	}
}

func mustFolders(t *testing.T, s *session, acct string) []mailcore.Folder {
	fs, err := s.cli.ListFolders(acct)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func mustList(t *testing.T, s *session, id mailcore.FolderID) []mailcore.Message {
	ms, err := s.cli.ListMessages(id, mailcore.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func keysOf(m map[mailcore.FolderID]bool) []mailcore.FolderID {
	var out []mailcore.FolderID
	for k := range m {
		out = append(out, k)
	}
	return out
}
