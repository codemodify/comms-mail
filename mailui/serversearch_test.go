package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
)

// With On server on, a query is sent to the server, and what the server
// finds beyond the local hits joins the list once, marked in the status —
// only while it answers the query showing, and only past the pins.
func TestServerSearchMergesHits(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{ShowStatusBar: true})
	defer done()
	s.setSearch("", false, true)
	if !s.srv.on {
		t.Fatal("the server search did not turn on")
	}
	s.filter.Query = "zzqx-no-local-hit"
	s.refreshList()
	if s.srv.asked == "" {
		t.Fatal("the query was not sent to the server")
	}
	if len(s.rows) != 0 {
		t.Fatalf("local rows for a nonsense query: %d", len(s.rows))
	}

	// The daemon's demo store has no server; stand in its answer.
	all, _ := s.cli.ListMessages(s.folder, mailcore.Filter{})
	_, _, key := s.serverScope()
	read, unread := all[0], all[0]
	for _, m := range all {
		if m.Read {
			read = m
		} else {
			unread = m
		}
	}
	s.srv.key, s.srv.hits = key, []mailcore.Message{read, unread, read}
	s.refreshList()
	if len(s.rows) != 2 || s.srvAdded != 2 {
		t.Fatalf("merged rows %d (added %d), want 2", len(s.rows), s.srvAdded)
	}
	if st := s.status.Parts()[1]; !strings.Contains(st, "(2 from the server)") {
		t.Fatalf("status %q", st)
	}
	s.filter.Unread = true
	s.refreshList()
	if len(s.rows) != 1 || s.rows[0].ID != unread.ID {
		t.Fatalf("the unread pin should keep only the unread hit: %d rows", len(s.rows))
	}
	s.filter.Unread = false

	// Another query: the old answer no longer applies.
	s.filter.Query = "another-nonsense-query"
	s.refreshList()
	if s.srvAdded != 0 || len(s.rows) != 0 {
		t.Fatalf("a stale server answer was merged: %d rows", len(s.rows))
	}
	s.setSearch("", false, false)
	if s.srv.on {
		t.Fatal("the server search did not turn off")
	}
}
