package mailcore

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// replayServer is a fake IMAP server with INBOX, Archive and Trash that
// logs every UID MOVE / STORE / EXPUNGE with the mailbox it ran in.
type replayServer struct {
	*scriptIMAP
	mu       sync.Mutex
	uids     map[string][]uint32
	validity map[string]int
	log      []string
}

func newReplayServer(t *testing.T) *replayServer {
	rs := &replayServer{
		uids:     map[string][]uint32{"INBOX": {7}, "Archive": {}, "Trash": {3}},
		validity: map[string]int{"INBOX": 1, "Archive": 1, "Trash": 1},
	}
	rs.scriptIMAP = newScriptIMAP(t, "IMAP4rev1 UIDPLUS MOVE", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		rs.mu.Lock()
		defer rs.mu.Unlock()
		switch {
		case cmd == "LIST":
			s.send(`* LIST (\HasNoChildren) "/" "INBOX"`)
			s.send(`* LIST (\HasNoChildren \Archive) "/" "Archive"`)
			s.send(`* LIST (\HasNoChildren \Trash) "/" "Trash"`)
			s.send("%s OK list", tag)
		case cmd == "LSUB":
			s.send("%s OK lsub", tag)
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.box = strings.Trim(strings.Fields(line)[2], `"`)
			s.send("* %d EXISTS", len(rs.uids[s.box]))
			s.send("* OK [UIDVALIDITY %d]", rs.validity[s.box])
			s.send("* OK [UIDNEXT 99]")
			s.send("%s OK selected", tag)
		case strings.Contains(up, "UID MOVE"), strings.Contains(up, "UID STORE"), strings.Contains(up, "UID EXPUNGE"):
			rest := strings.TrimPrefix(line, tag+" ")
			rs.log = append(rs.log, s.box+": "+rest)
			if strings.Contains(up, "UID MOVE") {
				s.send("%s OK [COPYUID 1 7 70] moved", tag)
			} else {
				s.send("%s OK done", tag)
			}
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "(UID)"):
			for i, u := range rs.uids[s.box] {
				s.send("* %d FETCH (UID %d)", i+1, u)
			}
			s.send("%s OK fetch", tag)
		case strings.Contains(up, "UID FETCH"):
			for i, u := range rs.uids[s.box] {
				s.send(`* %d FETCH (UID %d FLAGS () RFC822.SIZE 10 ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" "m%d" (("B" NIL "b" "ex.com")) NIL NIL (("A" NIL "a" "ex.com")) NIL NIL NIL "<%s%d@ex>"))`, i+1, u, u, s.box, u)
			}
			s.send("%s OK fetch", tag)
		default:
			return false
		}
		return true
	})
	return rs
}

func (rs *replayServer) takeLog() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := rs.log
	rs.log = nil
	return out
}

func newReplayStore(t *testing.T, rs *replayServer) *LocalStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: rs.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	rs.takeLog()
	return st
}

func wantLog(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("server saw\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// A move made offline is replayed from the folder the message was in, not
// the one the cache has already put it in, and the entry takes its new UID.
func TestOfflineMoveReplaysFromTheSourceFolder(t *testing.T) {
	rs := newReplayServer(t)
	st := newReplayStore(t, rs)
	st.SetOnline(false)
	if err := st.Move([]MessageID{"home/inbox:7"}, "home/archive"); err != nil {
		t.Fatal(err)
	}
	st.SetOnline(true)
	if _, err := st.FlushOutbox(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	wantLog(t, rs.takeLog(), `INBOX: UID MOVE 7 "Archive"`)
	if _, ok := st.CachedMessage("home/archive:70"); !ok {
		t.Fatal("the moved message was not re-keyed to its Archive UID")
	}
	if n := len(st.ListOutbox()); n != 0 {
		t.Fatalf("%d ops left after replay", n)
	}
}

// A delete made offline is a move to Trash on the server — never an
// expunge in Trash of whatever carries the Inbox UID there.
func TestOfflineDeleteReplaysAsAMoveToTrash(t *testing.T) {
	rs := newReplayServer(t)
	st := newReplayStore(t, rs)
	st.SetOnline(false)
	if err := st.Delete([]MessageID{"home/inbox:7"}); err != nil {
		t.Fatal(err)
	}
	st.SetOnline(true)
	if _, err := st.FlushOutbox(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	wantLog(t, rs.takeLog(), `INBOX: UID MOVE 7 "Trash"`)
}

// Deleting from Trash offline purges that message from Trash on replay,
// even though the cache already dropped it.
func TestOfflineDeleteFromTrashPurgesOnReplay(t *testing.T) {
	rs := newReplayServer(t)
	st := newReplayStore(t, rs)
	st.SetOnline(false)
	if err := st.Delete([]MessageID{"home/trash:3"}); err != nil {
		t.Fatal(err)
	}
	st.SetOnline(true)
	if _, err := st.FlushOutbox(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	wantLog(t, rs.takeLog(), `Trash: UID STORE 3 +FLAGS.SILENT (\Deleted)`, `Trash: UID EXPUNGE 3`)
	if n := len(st.ListOutbox()); n != 0 {
		t.Fatalf("%d ops left after replay", n)
	}
}

// When the source folder was renumbered while offline, the queued UID names
// nothing any more: the op is dropped and the server is not touched.
func TestOfflineMoveIsDroppedWhenUIDsChanged(t *testing.T) {
	rs := newReplayServer(t)
	st := newReplayStore(t, rs)
	st.SetOnline(false)
	if err := st.Move([]MessageID{"home/inbox:7"}, "home/archive"); err != nil {
		t.Fatal(err)
	}
	rs.mu.Lock()
	rs.validity["INBOX"] = 2
	rs.mu.Unlock()
	st.SetOnline(true)
	if _, err := st.FlushOutbox(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	wantLog(t, rs.takeLog())
	if n := len(st.ListOutbox()); n != 0 {
		t.Fatalf("%d ops left; a stale op should be dropped", n)
	}
}

// Taking a tag off clears its keyword on the server, online and replayed.
func TestRemovingATagClearsItsKeyword(t *testing.T) {
	rs := newReplayServer(t)
	st := newReplayStore(t, rs)
	work := []string{"Work"}
	if err := st.SetFlags("home/inbox:7", FlagPatch{Tags: &work}); err != nil {
		t.Fatal(err)
	}
	rs.takeLog()
	none := []string{}
	st.SetOnline(false)
	if err := st.SetFlags("home/inbox:7", FlagPatch{Tags: &none}); err != nil {
		t.Fatal(err)
	}
	st.SetOnline(true)
	if _, err := st.FlushOutbox(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	wantLog(t, rs.takeLog(), `INBOX: UID STORE 7 -FLAGS.SILENT (Work)`)
}

// A signature the writer took out of the body stays out: the compose window
// puts it in where it can be seen and edited, and says so.
func TestSignatureInBodyIsNotAppendedAgain(t *testing.T) {
	ident := Identity{Address: "ada@example.com", Signature: "Ada\nExample Corp"}
	raw, err := BuildRFC822Strict(Message{
		From: "ada@example.com", To: "bob@example.com", Subject: "hi",
		Body: "no signature here, on purpose", SignatureInBody: true,
	}, ident, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Example Corp") {
		t.Fatalf("the signature was appended to a body that opted out:\n%s", raw)
	}
	raw, err = BuildRFC822Strict(Message{From: "ada@example.com", To: "bob@example.com", Subject: "hi", Body: "old client"}, ident, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "-- \r\nAda") && !strings.Contains(string(raw), "-- \nAda") {
		t.Fatalf("a message without the flag lost its signature:\n%s", raw)
	}
}
