package mailcore

import (
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// folderServer is a fake IMAP server whose mailboxes can be listed,
// renamed (with what is under them) and read; it logs RENAME and SELECT.
type folderServer struct {
	*scriptIMAP
	mu    sync.Mutex
	delim string
	boxes map[string][]uint32 // wire (modified UTF-7) name → UIDs
	log   []string
	// hits answers UID SEARCH in a mailbox (every UID there when nil).
	hits map[string][]uint32
	// flags are a message's flags by UID (\Seen when unset).
	flags map[uint32]string
}

func newFolderServer(t *testing.T, delim string, boxes map[string][]uint32) *folderServer {
	fs := &folderServer{delim: delim, boxes: boxes}
	fs.scriptIMAP = newScriptIMAP(t, "IMAP4rev1 UIDPLUS", func(s *imapSession, tag, cmd, line string) bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		switch {
		case cmd == "LIST":
			var names []string
			for n := range fs.boxes {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				s.send(`* LIST (\HasNoChildren) %q %q`, fs.delim, n)
			}
			s.send("%s OK list", tag)
		case cmd == "LSUB":
			s.send("%s OK lsub", tag)
		case cmd == "CREATE":
			fs.boxes[strings.Trim(strings.Fields(line)[2], `"`)] = nil
			s.send("%s OK created", tag)
		case cmd == "RENAME":
			f := strings.Fields(line)
			from, to := strings.Trim(f[2], `"`), strings.Trim(f[3], `"`)
			fs.log = append(fs.log, "RENAME "+from+" "+to)
			if _, ok := fs.boxes[from]; !ok {
				s.send("%s NO no such mailbox", tag)
				return true
			}
			for n, u := range fs.boxes {
				if n == from || strings.HasPrefix(n, from+fs.delim) {
					delete(fs.boxes, n)
					fs.boxes[to+strings.TrimPrefix(n, from)] = u
				}
			}
			s.send("%s OK renamed", tag)
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.box = strings.Trim(strings.Fields(line)[2], `"`)
			fs.log = append(fs.log, cmd+" "+s.box)
			u, ok := fs.boxes[s.box]
			if !ok {
				s.send("%s NO no such mailbox", tag)
				return true
			}
			s.send("* %d EXISTS", len(u))
			s.send("* OK [UIDVALIDITY 5]")
			s.send("* OK [UIDNEXT 99]")
			s.send("%s OK selected", tag)
		case strings.Contains(strings.ToUpper(line), "UID SEARCH"):
			rest := strings.TrimPrefix(line, tag+" ")
			if strings.HasSuffix(line, "}") {
				n, _ := strconv.Atoi(line[strings.LastIndexByte(line, '{')+1 : len(line)-1])
				s.send("+ go ahead")
				buf := make([]byte, n)
				_, _ = io.ReadFull(s.r, buf)
				_, _ = s.r.ReadString('\n')
				rest += " " + string(buf)
			}
			fs.log = append(fs.log, s.box+": "+rest)
			uids := fs.boxes[s.box]
			if fs.hits != nil {
				uids = fs.hits[s.box]
			}
			parts := []string{"* SEARCH"}
			for _, u := range uids {
				parts = append(parts, strconv.Itoa(int(u)))
			}
			s.send("%s", strings.Join(parts, " "))
			s.send("%s OK search", tag)
		case strings.Contains(strings.ToUpper(line), "UID FETCH") && strings.Contains(strings.ToUpper(line), "(UID)"):
			for i, u := range fs.boxes[s.box] {
				s.send("* %d FETCH (UID %d)", i+1, u)
			}
			s.send("%s OK fetch", tag)
		case strings.Contains(strings.ToUpper(line), "UID FETCH") && strings.Contains(strings.ToUpper(line), "ENVELOPE"):
			for i, u := range fs.boxes[s.box] {
				s.send(`* %d FETCH (UID %d FLAGS (%s) RFC822.SIZE 10 ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" "m%d" NIL NIL NIL NIL NIL NIL NIL "<%d.%s@ex>"))`, i+1, u, fs.flagsOf(u), u, u, strings.ReplaceAll(s.box, "&", "_"))
			}
			s.send("%s OK fetch", tag)
		case strings.Contains(strings.ToUpper(line), "UID FETCH") && strings.Contains(strings.ToUpper(line), "FLAGS)"):
			for i, u := range fs.boxes[s.box] {
				s.send(`* %d FETCH (UID %d FLAGS (%s))`, i+1, u, fs.flagsOf(u))
			}
			s.send("%s OK fetch", tag)
		case strings.Contains(strings.ToUpper(line), "UID FETCH"):
			s.send("%s OK fetch", tag)
		default:
			return false
		}
		return true
	})
	return fs
}

// flagsOf is uid's flags; the caller holds fs.mu.
func (fs *folderServer) flagsOf(u uint32) string {
	if f, ok := fs.flags[u]; ok {
		return f
	}
	return `\Seen`
}

func (fs *folderServer) takeLog() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := fs.log
	fs.log = nil
	return out
}

func newFolderStore(t *testing.T, fs *folderServer) *LocalStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: fs.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	fs.takeLog()
	return st
}

func folderNamed(st *LocalStore, name string) (Folder, bool) {
	for _, f := range st.ListFolders("home") {
		if f.Name == name {
			return f, true
		}
	}
	return Folder{}, false
}

// Renaming a folder renames the server mailbox and the folders under it;
// the folder keeps its id and its messages theirs, and a sync after it
// finds nothing new to add.
func TestRenameFolderKeepsItsMessages(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}, "Projects": {4, 5}, "Projects/2026": {9}})
	st := newFolderStore(t, fs)
	before, ok := folderNamed(st, "Projects")
	if !ok {
		t.Fatalf("no Projects folder: %+v", st.ListFolders("home"))
	}
	msgs := st.ListMessages(before.ID)
	nFolders := len(st.ListFolders("home"))

	f, err := st.RenameFolder(before.ID, "Clients")
	if err != nil {
		t.Fatal(err)
	}
	if got := fs.takeLog(); len(got) != 1 || got[0] != "RENAME Projects Clients" {
		t.Fatalf("server saw %v", got)
	}
	if f.ID != before.ID || f.Name != "Clients" || f.Remote != "Clients" {
		t.Fatalf("renamed folder %+v", f)
	}
	if child, ok := folderNamed(st, "2026"); !ok || child.Remote != "Clients/2026" {
		t.Fatalf("the folder under it: %+v", child)
	}
	after := st.ListMessages(f.ID)
	if len(after) != len(msgs) || after[0].ID != msgs[0].ID {
		t.Fatalf("messages after rename %v, before %v", after, msgs)
	}

	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	if n := len(st.ListFolders("home")); n != nFolders {
		t.Fatalf("sync after rename: %d folders, want %d", n, nFolders)
	}
	if n := len(st.ListMessages(f.ID)); n != len(msgs) {
		t.Fatalf("sync after rename: %d messages, want %d", n, len(msgs))
	}
	for _, l := range fs.takeLog() {
		if strings.Contains(l, "Projects") {
			t.Fatalf("the old name was used after the rename: %s", l)
		}
	}

	// A new folder under the old name gets an id of its own.
	nf, err := st.CreateFolder("home", "Projects", "")
	if err != nil {
		t.Fatal(err)
	}
	if nf.ID == f.ID {
		t.Fatal("a new folder took the renamed folder's id")
	}
}

// A folder renamed (or deleted) in another client is not left behind: the
// sync drops it and adds the new name.
func TestFolderRenamedElsewhereLeavesNoGhost(t *testing.T) {
	fs := newFolderServer(t, ".", map[string][]uint32{"INBOX": {1}, "Projects": {4, 5}})
	st := newFolderStore(t, fs)
	old, _ := folderNamed(st, "Projects")
	fs.mu.Lock()
	fs.boxes["Work"] = fs.boxes["Projects"]
	delete(fs.boxes, "Projects")
	fs.mu.Unlock()
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	if _, ok := folderNamed(st, "Projects"); ok {
		t.Fatal("the old folder is still there")
	}
	if n := len(st.ListMessages(old.ID)); n != 0 {
		t.Fatalf("%d messages left under the old folder", n)
	}
	work, ok := folderNamed(st, "Work")
	if !ok || len(st.ListMessages(work.ID)) != 2 {
		t.Fatalf("the renamed folder: %+v", work)
	}
}

// A folder with a non-ASCII name is addressed by its modified-UTF-7 name
// once — it used to be encoded twice ("Entw&-APw-rfe") and never opened.
func TestNonASCIIFolderIsSelectedByItsWireName(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}, "Entw&APw-rfe": {3}})
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: fs.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	f, ok := folderNamed(st, "Entwürfe")
	if !ok || f.Remote != "Entwürfe" {
		t.Fatalf("folder %+v", f)
	}
	if n := len(st.ListMessages(f.ID)); n != 1 {
		t.Fatalf("messages in Entwürfe: %d", n)
	}
	for _, l := range fs.takeLog() {
		if strings.Contains(l, "&-") {
			t.Fatalf("double-encoded mailbox name: %s", l)
		}
	}
}

func TestRenameFolderRefuses(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}, "Projects": {4}, "Clients": {}})
	st := newFolderStore(t, fs)
	p, _ := folderNamed(st, "Projects")
	inbox, _ := folderNamed(st, "Inbox")
	for _, c := range []struct {
		id   FolderID
		name string
	}{
		{inbox.ID, "Other"},  // system folder
		{p.ID, "Clients"},    // taken
		{p.ID, "a/b"},        // the delimiter
		{p.ID, "  "},         // empty
		{"home/nope", "New"}, // no such folder
	} {
		if _, err := st.RenameFolder(c.id, c.name); err == nil {
			t.Fatalf("renamed %s to %q", c.id, c.name)
		}
	}
	if got := fs.takeLog(); len(got) != 0 {
		t.Fatalf("a refused rename reached the server: %v", got)
	}
}
