package mail

import (
	"sync"
	"testing"

	"github.com/codemodify/uitoolkit/platform"
)

type recNotifier struct {
	mu   sync.Mutex
	sent []platform.DesktopNotification
}

func (r *recNotifier) Send(n platform.DesktopNotification) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n)
	return n.ID, nil
}

func (r *recNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func (r *recNotifier) Close() {}

// New mail is a desktop notification of its own (the portal's or the
// notification server's), not the tray's toast: one "new-mail"
// notification, replaced by the next, with a button, whose click opens
// the window.
func TestNewMailGoesThroughTheNotifier(t *testing.T) {
	rec := &recNotifier{}
	s := &session{notes: rec}
	s.onDaemonEvent(Event{Method: EventNotify, Title: "Ada Lovelace", Body: "Notes on the engine"})
	s.onDaemonEvent(Event{Method: EventNotify, Body: "2 new message(s)"})
	if len(rec.sent) != 2 {
		t.Fatalf("sent %d", len(rec.sent))
	}
	n := rec.sent[0]
	if n.ID != "new-mail" || n.Title != "Ada Lovelace" || n.Body != "Notes on the engine" || n.IconName != "mail-unread" {
		t.Fatalf("notification %+v", n)
	}
	if len(n.Actions) != 1 || n.Actions[0].ID != "open" || n.OnActivate == nil {
		t.Fatalf("actions %+v", n.Actions)
	}
	if rec.sent[1].ID != "new-mail" || rec.sent[1].Title != "New mail" {
		t.Fatalf("the second one %+v", rec.sent[1])
	}
	opened := 0
	note := newMailNotification("t", "b", func() { opened++ })
	note.OnActivate("")
	note.OnActivate("open")
	if opened != 2 {
		t.Fatalf("a click opened the window %d times of 2", opened)
	}
	// The daemon's has nowhere to open.
	newMailNotification("t", "b", nil).OnActivate("")
}
