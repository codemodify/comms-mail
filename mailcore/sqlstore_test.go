package mailcore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func newTestLocalStore(t *testing.T, dir string) *LocalStore {
	t.Helper()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func seedCache(st *LocalStore, n int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Folders = append(st.Folders, Folder{ID: "a/inbox", AccountID: "a", Name: "Inbox", Kind: FolderInbox, Remote: "INBOX"})
	for i := 1; i <= n; i++ {
		st.Messages = append(st.Messages, Message{
			ID: MessageID("a/inbox:" + itoa(i)), Folder: "a/inbox", AccountID: "a", UID: uint32(i),
			From: "bob@example.org", Subject: "m" + itoa(i), Date: time.Unix(int64(1_700_000_000+i), 0).UTC(),
		})
	}
	st.saveLocked()
}

// What the cache held is what a restart finds: messages in order, flags,
// bodies, and how far each folder had synced.
func TestCacheSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st := newTestLocalStore(t, dir)
	seedCache(st, 3)
	if err := st.SetFlags("a/inbox:2", FlagPatch{Read: BoolPtr(true), Starred: BoolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.Messages[2].Body = "hello body"
	st.saveFolderMeta("a/inbox", folderMeta{UIDValidity: 9, UIDNext: 4, HighestMod: 77, Remote: "INBOX"})
	st.saveLocked()
	st.mu.Unlock()

	again := newTestLocalStore(t, dir)
	got := again.ListMessages("a/inbox")
	if len(got) != 3 || got[0].ID != "a/inbox:1" || got[2].ID != "a/inbox:3" {
		t.Fatalf("after restart: %v", got)
	}
	if !got[1].Read || !got[1].Starred {
		t.Fatal("flags did not survive the restart")
	}
	if got[2].Body != "hello body" {
		t.Fatalf("body after restart = %q", got[2].Body)
	}
	again.mu.Lock()
	meta := again.loadFolderMeta("a/inbox")
	again.mu.Unlock()
	if meta != (folderMeta{UIDValidity: 9, UIDNext: 4, HighestMod: 77, Remote: "INBOX"}) {
		t.Fatalf("folder meta after restart = %+v", meta)
	}
	if _, err := os.Stat(filepath.Join(dir, "messages.json")); err == nil {
		t.Fatal("the cache wrote messages.json")
	}
}

// Marking one message read writes one row, not the whole cache.
func TestSaveWritesOnlyWhatChanged(t *testing.T) {
	st := newTestLocalStore(t, t.TempDir())
	seedCache(st, 200)
	// Local-only messages (no server UID): a flag change is saved and not
	// pushed, so nothing else — a queued retry — is written beside it.
	st.mu.Lock()
	for i := range st.Messages {
		st.Messages[i].UID = 0
	}
	st.saveLocked()
	st.mu.Unlock()
	changes := func() int {
		var n int
		if err := st.sqlc.db.QueryRow("SELECT total_changes()").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := changes()
	if err := st.SetFlags("a/inbox:150", FlagPatch{Read: BoolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if n := changes() - before; n != 1 {
		t.Fatalf("marking one message read changed %d rows, want 1", n)
	}
	before = changes()
	st.mu.Lock()
	st.saveLocked()
	st.mu.Unlock()
	if n := changes() - before; n != 0 {
		t.Fatalf("a save with nothing changed wrote %d rows", n)
	}
	if err := st.Delete([]MessageID{"a/inbox:7"}); err != nil {
		t.Fatal(err)
	}
	var rows int
	_ = st.sqlc.db.QueryRow("SELECT count(*) FROM messages").Scan(&rows)
	if rows != 199 {
		t.Fatalf("after a delete the table has %d rows, want 199", rows)
	}
}

// A field added to Message must be in messageStamp, or changes to it would
// never be saved.
func TestMessageStampCoversEveryField(t *testing.T) {
	base := messageStamp(&Message{})
	typ := reflect.TypeOf(Message{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		var m Message
		v := reflect.ValueOf(&m).Elem().Field(i)
		switch v.Kind() {
		case reflect.String:
			v.SetString("x")
		case reflect.Bool:
			v.SetBool(true)
		case reflect.Int, reflect.Int64:
			v.SetInt(1)
		case reflect.Uint32, reflect.Uint64:
			v.SetUint(1)
		case reflect.Slice:
			if v.Type().Elem().Kind() == reflect.String {
				v.Set(reflect.ValueOf([]string{"x"}))
			} else {
				v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			}
		case reflect.Map:
			v.Set(reflect.ValueOf(map[string]string{"List-Id": "x"}))
		case reflect.Struct:
			if v.Type() == reflect.TypeOf(time.Time{}) {
				v.Set(reflect.ValueOf(time.Unix(1, 0)))
			} else {
				t.Fatalf("field %s: no rule for %v", f.Name, v.Type())
			}
		default:
			t.Fatalf("field %s: no rule for kind %v", f.Name, v.Kind())
		}
		if messageStamp(&m) == base {
			t.Errorf("messageStamp ignores Message.%s: a change to it would not be saved", f.Name)
		}
	}
}

// A mail.db that will not open is set aside, not overwritten, and the store
// starts over with a working one.
func TestCorruptDatabaseIsSetAside(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dbFileName), []byte("this is not a database, it is a crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := newTestLocalStore(t, dir)
	if _, err := os.Stat(filepath.Join(dir, dbFileName+".corrupt")); err != nil {
		t.Fatalf("the corrupt database was not set aside: %v", err)
	}
	if st.Health() == nil {
		t.Fatal("Health should say so, so the user re-syncs")
	}
	seedCache(st, 2)
	if n := len(newTestLocalStore(t, dir).ListMessages("a/inbox")); n != 2 {
		t.Fatalf("the fresh database holds %d messages, want 2", n)
	}
}
