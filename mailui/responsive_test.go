package mailui

import (
	"context"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
)

// stuckBodyStore lists messages without their bodies and holds every body
// fetch until released: a first click on a message over a network that has
// stopped answering.
type stuckBodyStore struct {
	*mailcore.MemoryStore
	asked   chan mailcore.MessageID
	release chan struct{}
}

func (s *stuckBodyStore) ListMessages(folder mailcore.FolderID) []mailcore.Message {
	out := s.MemoryStore.ListMessages(folder)
	for i := range out {
		out[i].Body, out[i].HTML = "", ""
	}
	return out
}

func (s *stuckBodyStore) GetMessage(id mailcore.MessageID) (mailcore.Message, bool) {
	s.asked <- id
	<-s.release
	return s.MemoryStore.GetMessage(id)
}

// A click on a message whose body is not downloaded returns at once: the
// headers show, the body says it is loading, the row is marked read, and
// the window keeps answering while the daemon waits on the network. When
// bodies arrive out of order, the preview shows the last click.
func TestClickDoesNotWaitForTheBody(t *testing.T) {
	store := &stuckBodyStore{
		MemoryStore: mailcore.NewDemoStore(),
		asked:       make(chan mailcore.MessageID, 8),
		release:     make(chan struct{}),
	}
	sock, stop, err := mailcore.StartStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	// The first preview, of the row the window opens on, is built before
	// the loop runs and so fetches inline; let that one through.
	go func() {
		<-store.asked
		store.release <- struct{}{}
	}()
	s := newSession(a, w, cli, AppOptions{})
	w.SetContent(s.build())

	ran := make(chan error, 1)
	go func() { ran <- a.Run() }()
	defer func() {
		a.Quit()
		select {
		case <-ran:
		case <-time.After(3 * time.Second):
			// The loop is wedged in a handler; the failure above says where.
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !a.Looping() {
		if time.Now().After(deadline) {
			t.Fatal("the event loop never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// onUI runs fn on the UI goroutine and waits for it.
	onUI := func(fn func()) {
		t.Helper()
		done := make(chan struct{})
		a.Post(func() { fn(); close(done) })
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("the UI goroutine did not answer within 3s")
		}
	}

	var (
		took              time.Duration
		subj, placeholder string
		read              bool
		first, second     mailcore.MessageID
	)
	onUI(func() {
		unread := -1
		for i, m := range s.rows {
			if !m.Read && i > 0 {
				unread = i
				break
			}
		}
		if unread < 0 {
			t.Fatal("the demo inbox needs an unread message below the first row")
		}
		first = s.rows[unread].ID
		start := time.Now()
		s.clickRow(unread, false)
		took = time.Since(start)
		subj = s.hdrSubj.Text
		placeholder = s.preview.Placeholder
		read = s.rows[unread].Read
		if subj != s.rows[unread].Subject {
			t.Errorf("headers show %q, want the row's %q", subj, s.rows[unread].Subject)
		}
	})
	if took > 200*time.Millisecond {
		t.Fatalf("clickRow took %v with the body fetch stuck", took)
	}
	if placeholder != "Loading message…" {
		t.Fatalf("preview placeholder %q while loading", placeholder)
	}
	if !read {
		t.Fatal("the row was not marked read at once")
	}
	if got := <-store.asked; got != first {
		t.Fatalf("the daemon was asked for %s, want %s", got, first)
	}

	// A second click, on another message not yet downloaded, while the
	// first body is still stuck.
	onUI(func() {
		for i, m := range s.rows {
			if i > 0 && m.ID != first {
				second = m.ID
				s.clickRow(i, false)
				return
			}
		}
	})
	if got := <-store.asked; got != second {
		t.Fatalf("the daemon was asked for %s, want %s", got, second)
	}
	close(store.release)

	deadline = time.Now().Add(3 * time.Second)
	for {
		var shown mailcore.MessageID
		var ok bool
		var text string
		onUI(func() { shown, ok, text = s.shown.ID, s.shownOK, s.preview.Text })
		if ok {
			if shown != second {
				t.Fatalf("preview shows %s, want the last click %s", shown, second)
			}
			if text == "" {
				t.Fatal("the body arrived but the preview is empty")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the body never reached the preview")
		}
		time.Sleep(10 * time.Millisecond)
	}
	onUI(func() { w.Close() })
}
