package mailcore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func folderByRemote(st *LocalStore, remote string) (Folder, bool) {
	for _, f := range st.ListFolders("home") {
		if f.Remote == remote {
			return f, true
		}
	}
	return Folder{}, false
}

// Folders nest as the server names them; one whose parent is not listed
// keeps its whole path; a \Noselect mailbox is a parent that is never
// opened.
func TestFoldersNestFromServerNames(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{
		"INBOX": {1}, "Archives": {2}, "Archives/2023": {3}, "Lone/Child": {4},
		"[Gmail]": nil, "[Gmail]/Sent Mail": {5},
	})
	fs.attrs = map[string]string{"[Gmail]": `\Noselect \HasChildren`, "[Gmail]/Sent Mail": `\Sent \HasNoChildren`}
	st := newFolderStore(t, fs)
	arch, _ := folderByRemote(st, "Archives")
	y, _ := folderByRemote(st, "Archives/2023")
	if y.Parent != arch.ID || y.Name != "2023" {
		t.Fatalf("Archives/2023: %+v", y)
	}
	if lone, _ := folderByRemote(st, "Lone/Child"); lone.Parent != "" || lone.Name != "Lone/Child" {
		t.Fatalf("an orphan keeps its path: %+v", lone)
	}
	gm, _ := folderByRemote(st, "[Gmail]")
	sent, _ := folderByRemote(st, "[Gmail]/Sent Mail")
	if !gm.NoSelect || sent.Parent != gm.ID || sent.Kind != FolderSent {
		t.Fatalf("gmail: %+v / %+v", gm, sent)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	for _, l := range fs.takeLog() {
		if strings.Contains(l, "[Gmail]") && !strings.Contains(l, "Sent Mail") {
			t.Fatalf("a \\Noselect mailbox was opened: %s", l)
		}
	}
}

// A server whose folders all live under "INBOX." (its namespace) does not
// show them all inside Inbox, and new folders get its delimiter.
func TestINBOXNamespaceIsNotAParent(t *testing.T) {
	fs := newFolderServer(t, ".", map[string][]uint32{
		"INBOX": {1}, "INBOX.Sent": {2}, "INBOX.Projects": {3}, "INBOX.Projects.Acme": {4},
	})
	st := newFolderStore(t, fs)
	sent, _ := folderByRemote(st, "INBOX.Sent")
	proj, _ := folderByRemote(st, "INBOX.Projects")
	acme, _ := folderByRemote(st, "INBOX.Projects.Acme")
	if sent.Parent != "" || sent.Name != "Sent" || proj.Parent != "" || acme.Parent != proj.ID || acme.Name != "Acme" {
		t.Fatalf("sent %+v projects %+v acme %+v", sent, proj, acme)
	}
	f, err := st.CreateFolder("home", "Beta", proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Remote != "INBOX.Projects.Beta" {
		t.Fatalf("new subfolder remote %q", f.Remote)
	}
	// Moved to the top level, it stays in the namespace.
	moved, err := st.MoveFolder(acme.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Remote != "INBOX.Acme" || moved.Parent != "" {
		t.Fatalf("moved %+v", moved)
	}
}

// Moving a folder renames its mailbox to the new path; what is inside it
// goes along, and it cannot go into itself.
func TestMoveFolder(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}, "Projects": {4}, "Projects/Acme": {5}, "Archives": {6}})
	st := newFolderStore(t, fs)
	proj, _ := folderByRemote(st, "Projects")
	arch, _ := folderByRemote(st, "Archives")
	acme, _ := folderByRemote(st, "Projects/Acme")
	msgs := st.ListMessages(proj.ID)

	got, err := st.MoveFolder(proj.ID, arch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if l := fs.takeLog(); len(l) != 1 || l[0] != "RENAME Projects Archives/Projects" {
		t.Fatalf("server saw %q", l)
	}
	if got.Parent != arch.ID || got.Remote != "Archives/Projects" || got.ID != proj.ID {
		t.Fatalf("moved %+v", got)
	}
	if a, _ := st.GetFolder(acme.ID); a.Remote != "Archives/Projects/Acme" || a.Parent != proj.ID {
		t.Fatalf("child %+v", a)
	}
	if n := len(st.ListMessages(proj.ID)); n != len(msgs) {
		t.Fatalf("messages %d, want %d", n, len(msgs))
	}
	if _, err := st.MoveFolder(proj.ID, acme.ID); err == nil {
		t.Fatal("moved a folder into its own subfolder")
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.GetFolder(proj.ID); again.Parent != arch.ID || again.Name != "Projects" {
		t.Fatalf("after sync %+v", again)
	}
}

// Compact removes from the server what another client marked deleted, and
// the cache follows.
func TestCompactFolder(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1, 2, 3}})
	st := newFolderStore(t, fs)
	fs.mu.Lock()
	fs.expunge = map[string][]uint32{"INBOX": {2}}
	fs.mu.Unlock()
	if err := st.CompactFolder("home/inbox"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fs.takeLog(), "|"), "INBOX: EXPUNGE") {
		t.Fatal("no EXPUNGE")
	}
	if _, ok := st.CachedMessage("home/inbox:2"); ok {
		t.Fatal("the expunged message is still here")
	}
	if _, ok := st.CachedMessage("home/inbox:1"); !ok {
		t.Fatal("a kept message went")
	}
	_ = filepath.Join
}

// The folder the window shows is watched (and caught up at once) along
// with Inbox and Sent.
func TestFocusedFolderIsWatched(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}, "Projects": {4}})
	st := newFolderStore(t, fs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go st.idleAccount(ctx, "home")
	proj, _ := folderByRemote(st, "Projects")
	st.Focus(proj.ID)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range fs.takeLog() {
			if strings.HasSuffix(l, " Projects") && (strings.HasPrefix(l, "EXAMINE") || strings.HasPrefix(l, "SELECT")) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the focused folder was never opened")
}
