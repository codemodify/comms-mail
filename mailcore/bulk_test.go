package mailcore

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestCopyUIDMapAndSets(t *testing.T) {
	m := copyUIDMap([]string{"* 3 EXPUNGE", "OK [COPYUID 38505 304,319:320 3956:3958] Done"})
	if len(m) != 3 || m[304] != 3956 || m[319] != 3957 || m[320] != 3958 {
		t.Fatalf("map %v", m)
	}
	if got := expandUIDSet("9:7,2"); fmt.Sprint(got) != "[9 8 7 2]" {
		t.Fatalf("set %v", got)
	}
	if len(copyUIDMap([]string{"OK moved"})) != 0 {
		t.Fatal("no COPYUID, no map")
	}
}

// A large move is one SELECT and one UID MOVE per 200 messages — not both
// per message — re-keys every message, and reports its progress.
func TestBulkMoveIsBatched(t *testing.T) {
	var uids []uint32
	for u := uint32(1); u <= 250; u++ {
		uids = append(uids, u)
	}
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": uids, "Archive": nil})
	st := newFolderStore(t, fs)
	var mu sync.Mutex
	var progress []string
	st.SetOnChange(func(ev StoreEvent) {
		if ev.Reason == "progress" {
			mu.Lock()
			progress = append(progress, ev.Title)
			mu.Unlock()
		}
	})
	var ids []MessageID
	for _, m := range st.ListMessages("home/inbox") {
		ids = append(ids, m.ID)
	}
	if len(ids) != 250 {
		t.Fatalf("inbox %d", len(ids))
	}
	if err := st.Move(ids, "home/archive"); err != nil {
		t.Fatal(err)
	}
	var moves, selects int
	for _, l := range fs.takeLog() {
		switch {
		case strings.Contains(l, "UID MOVE"):
			moves++
		case strings.HasPrefix(l, "SELECT") || strings.HasPrefix(l, "EXAMINE"):
			selects++
		}
	}
	if moves != 2 || selects > 2 {
		t.Fatalf("%d UID MOVE and %d SELECT for 250 messages", moves, selects)
	}
	if n := len(st.ListMessages("home/archive")); n != 250 {
		t.Fatalf("archive %d", n)
	}
	if _, ok := st.CachedMessage("home/archive:1000"); !ok {
		t.Fatal("not re-keyed to the new UIDs")
	}
	if len(st.ListMessages("home/inbox")) != 0 || len(st.ListOutbox()) != 0 {
		t.Fatal("left behind or queued")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(progress) != 2 || progress[1] != "Moving to Archive… 250 of 250" {
		t.Fatalf("progress %q", progress)
	}
}

// Emptying many messages out of Trash is \Deleted and UID EXPUNGE on the
// whole set, not one message at a time.
func TestBulkPurgeIsBatched(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": nil, "Trash": {3, 4, 5, 9}})
	fs.attrs = map[string]string{"Trash": `\Trash \HasNoChildren`}
	st := newFolderStore(t, fs)
	var ids []MessageID
	for _, m := range st.ListMessages("home/trash") {
		ids = append(ids, m.ID)
	}
	if err := st.Delete(ids); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range fs.takeLog() {
		if strings.Contains(l, "UID STORE") || strings.Contains(l, "UID EXPUNGE") {
			got = append(got, l)
		}
	}
	if strings.Join(got, "|") != `Trash: UID STORE 3:5,9 +FLAGS.SILENT (\Deleted)|Trash: UID EXPUNGE 3:5,9` {
		t.Fatalf("server saw %q", got)
	}
}
