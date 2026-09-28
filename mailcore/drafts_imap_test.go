package mailcore

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// draftServer is a fake IMAP server with Drafts and Sent that takes APPEND
// (answering APPENDUID when uidplus), keeps what it was given, and honours
// UID STORE \Deleted + UID EXPUNGE — enough to watch drafts being saved.
type draftServer struct {
	*scriptIMAP
	mu      sync.Mutex
	next    uint32
	boxes   map[string]map[uint32]string // box → uid → raw
	deleted map[string]map[uint32]bool
}

func newDraftServer(t *testing.T, uidplus bool) *draftServer {
	ds := &draftServer{next: 100, boxes: map[string]map[uint32]string{"INBOX": {}, "Drafts": {}, "Sent": {}},
		deleted: map[string]map[uint32]bool{"INBOX": {}, "Drafts": {}, "Sent": {}}}
	caps := "IMAP4rev1"
	if uidplus {
		caps += " UIDPLUS"
	}
	ds.scriptIMAP = newScriptIMAP(t, caps, func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case cmd == "LIST":
			s.send(`* LIST (\HasNoChildren) "/" "INBOX"`)
			s.send(`* LIST (\HasNoChildren \Drafts) "/" "Drafts"`)
			s.send(`* LIST (\HasNoChildren \Sent) "/" "Sent"`)
			s.send("%s OK list", tag)
		case cmd == "LSUB":
			s.send("%s OK lsub", tag)
		case cmd == "APPEND":
			f := strings.Fields(line)
			box := strings.Trim(f[2], `"`)
			last := f[len(f)-1]
			n, _ := strconv.Atoi(strings.Trim(last, "{}"))
			s.send("+ go ahead")
			buf := make([]byte, n)
			_, _ = io.ReadFull(s.r, buf)
			_, _ = s.r.ReadString('\n')
			ds.mu.Lock()
			ds.next++
			uid := ds.next
			ds.boxes[box][uid] = string(buf)
			ds.mu.Unlock()
			if uidplus {
				s.send("%s OK [APPENDUID 1 %d] appended", tag, uid)
			} else {
				s.send("%s OK appended", tag)
			}
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.box = strings.Trim(strings.Fields(line)[2], `"`)
			ds.mu.Lock()
			n := len(ds.boxes[s.box])
			ds.mu.Unlock()
			s.send("* %d EXISTS", n)
			s.send("* OK [UIDVALIDITY 1]")
			s.send("* OK [UIDNEXT %d]", ds.next+1)
			s.send("%s OK selected", tag)
		case strings.Contains(up, "UID STORE"):
			f := strings.Fields(line)
			uid, _ := strconv.Atoi(f[3])
			if strings.Contains(up, `\DELETED`) {
				ds.mu.Lock()
				ds.deleted[s.box][uint32(uid)] = true
				ds.mu.Unlock()
			}
			s.send("%s OK stored", tag)
		case strings.Contains(up, "UID EXPUNGE") || cmd == "EXPUNGE":
			ds.mu.Lock()
			for u := range ds.deleted[s.box] {
				delete(ds.boxes[s.box], u)
			}
			ds.deleted[s.box] = map[uint32]bool{}
			ds.mu.Unlock()
			s.send("%s OK expunged", tag)
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "(UID)"):
			i := 0
			for _, u := range ds.sorted(s.box) {
				i++
				s.send("* %d FETCH (UID %d)", i, u)
			}
			s.send("%s OK fetch", tag)
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "ENVELOPE"):
			i := 0
			for _, u := range ds.sorted(s.box) {
				i++
				ds.mu.Lock()
				m, _ := ParseRFC822([]byte(ds.boxes[s.box][u]), "", "")
				ds.mu.Unlock()
				s.send(`* %d FETCH (UID %d FLAGS (\Seen) RFC822.SIZE 10 ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" %q NIL NIL NIL NIL NIL NIL NIL %q))`, i, u, m.Subject, m.RFCMessageID)
			}
			s.send("%s OK fetch", tag)
		case strings.Contains(up, "UID FETCH"):
			s.send("%s OK fetch", tag)
		default:
			return false
		}
		return true
	})
	return ds
}

func (ds *draftServer) sorted(box string) []uint32 {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	var out []uint32
	for u := ds.next - uint32(len(ds.boxes[box])) - 50; u <= ds.next; u++ {
		if _, ok := ds.boxes[box][u]; ok {
			out = append(out, u)
		}
	}
	return out
}

func (ds *draftServer) count(box string) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return len(ds.boxes[box])
}

func (ds *draftServer) only(t *testing.T, box string) string {
	t.Helper()
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if len(ds.boxes[box]) != 1 {
		t.Fatalf("%s holds %d messages, want 1", box, len(ds.boxes[box]))
	}
	for _, raw := range ds.boxes[box] {
		return raw
	}
	return ""
}

func newDraftStore(t *testing.T, ds *draftServer) (*LocalStore, *Server) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: ds.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	return st, NewServer(st, filepath.Join(dir, "s.sock"))
}

func draftsIn(st *LocalStore) []Message {
	return st.ListMessages("home/drafts")
}

// A draft saved again replaces the server's copy — the server's Drafts
// holds one message, the latest — and a sync adds no second copy.
func TestDraftSavedAgainReplacesTheServerCopy(t *testing.T) {
	ds := newDraftServer(t, true)
	st, srv := newDraftStore(t, ds)

	r, err := srv.saveDraft(ComposeParams{AccountID: "home", Message: Message{Subject: "plan", To: "bob@example.org", Body: "v1\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(r.ID), "home/drafts:") {
		t.Fatalf("draft id %q: not keyed to its server UID", r.ID)
	}
	for i := 2; i <= 3; i++ {
		r, err = srv.saveDraft(ComposeParams{AccountID: "home", ID: r.ID, Message: Message{Subject: "plan", To: "bob@example.org", Body: fmt.Sprintf("v%d\n", i)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if raw := ds.only(t, "Drafts"); !strings.Contains(raw, "v3") {
		t.Fatalf("the server's draft is not the latest:\n%s", raw)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	d := draftsIn(st)
	if len(d) != 1 || d[0].ID != r.ID || d[0].Body != "v3\n" {
		t.Fatalf("drafts after sync: %+v (want %s)", d, r.ID)
	}
}

// Without UIDPLUS the server does not say which UID a draft got; the next
// sync recognises it by Message-ID instead of adding it twice.
func TestAppendedCopyAdoptedBySyncWithoutUIDPLUS(t *testing.T) {
	ds := newDraftServer(t, false)
	st, srv := newDraftStore(t, ds)
	r, err := srv.saveDraft(ComposeParams{AccountID: "home", Message: Message{Subject: "plan", Body: "v1\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if ds.count("Drafts") != 1 {
		t.Fatalf("server drafts %d", ds.count("Drafts"))
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	d := draftsIn(st)
	if len(d) != 1 || d[0].UID == 0 {
		t.Fatalf("drafts after sync: %d (uid %v) — the server copy was added again", len(d), d)
	}
	if d[0].ID == r.ID {
		t.Fatal("adopted draft kept its local id")
	}
	if d[0].Body != "v1\n" {
		t.Fatalf("adopted draft lost its body: %q", d[0].Body)
	}
}

// A draft saved offline (and saved again) reaches the server once, with its
// latest text, when back online; the window, still holding the id it had
// before, keeps saving to it and can remove it.
func TestDraftSavedOfflineReachesTheServer(t *testing.T) {
	ds := newDraftServer(t, true)
	st, srv := newDraftStore(t, ds)
	st.SetOnline(false)
	r, err := srv.saveDraft(ComposeParams{AccountID: "home", Message: Message{Subject: "plan", Body: "v1\n"}})
	if err != nil {
		t.Fatal(err)
	}
	old := r.ID
	if _, err := srv.saveDraft(ComposeParams{AccountID: "home", ID: old, Message: Message{Subject: "plan", Body: "v2\n"}}); err != nil {
		t.Fatal(err)
	}
	if n := ds.count("Drafts"); n != 0 {
		t.Fatalf("offline, the server got %d drafts", n)
	}
	if ops := st.ListOutbox(); len(ops) != 1 || ops[0].Kind != "append" {
		t.Fatalf("outbox %+v", ops)
	}
	st.SetOnline(true)
	if _, err := st.FlushOutbox(); err != nil {
		t.Fatal(err)
	}
	if raw := ds.only(t, "Drafts"); !strings.Contains(raw, "v2") {
		t.Fatalf("the server's draft:\n%s", raw)
	}
	// The window still says `old`.
	r, err = srv.saveDraft(ComposeParams{AccountID: "home", ID: old, Message: Message{Subject: "plan", Body: "v3\n"}})
	if err != nil {
		t.Fatalf("saving to the old id: %v", err)
	}
	if raw := ds.only(t, "Drafts"); !strings.Contains(raw, "v3") {
		t.Fatalf("after saving again:\n%s", raw)
	}
	if err := st.Delete([]MessageID{old}); err != nil {
		t.Fatalf("delete by the old id: %v", err)
	}
	if len(draftsIn(st)) != 0 {
		t.Fatal("the draft is still here")
	}
}
