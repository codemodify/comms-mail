package mailui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

func demoClient(t *testing.T) *mailcore.Client {
	t.Helper()
	sock, stop, err := mailcore.StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli
}

// composeFields builds a Write window headless and returns its fields by
// placeholder, plus the From box and the body.
func composeFields(t *testing.T, cli *mailcore.Client, opts ComposeOptions) (map[string]*widgets.TextField, *widgets.ComboBox, *widgets.TextArea) {
	t.Helper()
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Write", Width: 760, Height: 640, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	w.SetContent(ComposeApp(a, w, cli, opts))
	a.PumpOnce()
	fields := map[string]*widgets.TextField{}
	var from *widgets.ComboBox
	var body *widgets.TextArea
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			fields[v.Placeholder] = v
		case *widgets.ComboBox:
			if from == nil {
				from = v
			}
		case *widgets.TextArea:
			if !v.ReadOnly {
				body = v
			}
		}
	})
	if fields["To"] == nil || fields["Cc"] == nil || from == nil || body == nil {
		t.Fatalf("compose is missing a field: %v from=%v body=%v", fields, from, body)
	}
	return fields, from, body
}

func TestReplyAndReplyAllRecipients(t *testing.T) {
	cli := demoClient(t)
	m := mailcore.Message{
		From:    "Bob <bob@example.org>",
		To:      "ada@example.com, Carol <carol@example.org>",
		Cc:      "dave@example.org, ada.lovelace@example.com",
		Subject: "plans",
		Body:    "see you",
	}
	f, _, _ := composeFields(t, cli, ComposeOptions{ReplyTo: &m})
	if got := f["To"].Text; !strings.Contains(got, "bob@example.org") || strings.Contains(got, "carol") {
		t.Errorf("Reply To = %q, want only the sender", got)
	}
	if got := f["Cc"].Text; got != "" {
		t.Errorf("Reply Cc = %q, want empty", got)
	}

	f, _, _ = composeFields(t, cli, ComposeOptions{ReplyTo: &m, ReplyAll: true})
	if got, want := f["To"].Text, "Bob <bob@example.org>, Carol <carol@example.org>"; got != want {
		t.Errorf("Reply All To = %q, want %q", got, want)
	}
	if got, want := f["Cc"].Text, "dave@example.org"; got != want {
		t.Errorf("Reply All Cc = %q, want %q (both of Ada's addresses left out)", got, want)
	}
}

// The signature is in the body from the start, above the quote, and
// follows the From box.
func TestComposeSignatureFollowsFrom(t *testing.T) {
	cli := demoClient(t)
	m := mailcore.Message{From: "bob@example.org", To: "ada@example.com", Subject: "q", Body: "question"}
	_, from, body := composeFields(t, cli, ComposeOptions{ReplyTo: &m})
	sig := strings.Index(body.Text, "-- \nAda Lovelace\nMathematical scientist")
	quote := strings.Index(body.Text, "> question")
	if sig < 0 || quote < 0 || sig > quote {
		t.Fatalf("want the signature above the quote, got:\n%s", body.Text)
	}
	alias := -1
	for i, it := range from.Items {
		if strings.Contains(it, "ada.lovelace@example.com") {
			alias = i
		}
	}
	if alias < 0 {
		t.Fatalf("no alias identity in %v", from.Items)
	}
	from.Selected = alias
	from.OnChange(alias)
	if strings.Contains(body.Text, "Mathematical scientist") || !strings.Contains(body.Text, "-- \n— Ada") {
		t.Fatalf("switching From did not swap the signature:\n%s", body.Text)
	}
}

func TestMoveMenuAndDropMoveMessages(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	if len(s.rows) < 2 {
		t.Fatal("demo inbox too small")
	}
	id := s.rows[0].ID
	s.selected = []mailcore.MessageID{id}

	var junk *widgets.MenuItem
	for _, it := range s.moveMenu() {
		if strings.TrimSpace(it.Text) == "Junk" {
			junk = it
		}
		if it.Disabled != (strings.TrimSpace(it.Text) == "Inbox") {
			t.Errorf("menu item %q disabled=%v; only the current folder should be", it.Text, it.Disabled)
		}
	}
	if junk == nil {
		t.Fatal("Move to has no Junk")
	}
	junk.OnClick()
	a.PumpOnce()
	m, _, err := s.cli.GetMessage(id)
	if err != nil || !strings.HasSuffix(string(m.Folder), "/junk") {
		t.Fatalf("after Move to Junk the message is in %q (%v)", m.Folder, err)
	}

	// Drag the next message onto Archives in the sidebar.
	id = s.rows[0].ID
	drag := s.dragMessages([]int{0})
	if drag == nil || !drag.Local {
		t.Fatal("dragging a row should carry its messages inside the window")
	}
	var target, current *widgets.TreeNode
	var walk func([]*widgets.TreeNode)
	walk = func(ns []*widgets.TreeNode) {
		for _, n := range ns {
			if fid, ok := n.Data.(mailcore.FolderID); ok {
				if strings.HasSuffix(string(fid), "/archive") || strings.HasSuffix(string(fid), "/archives") {
					target = n
				}
				if fid == s.folder {
					current = n
				}
			}
			walk(n.Children)
		}
	}
	walk(s.tree.Roots)
	if target == nil || current == nil {
		t.Fatal("no Archives or current folder node in the sidebar")
	}
	if s.dropOnFolder(current, widget.DropEvent{Payload: drag.Payload}) {
		t.Error("a drop on the folder the messages are already in should be refused")
	}
	if !s.dropOnFolder(target, widget.DropEvent{Payload: drag.Payload}) {
		t.Fatal("the drop on Archives was refused")
	}
	a.PumpOnce()
	m, _, _ = s.cli.GetMessage(id)
	if m.Folder != target.Data.(mailcore.FolderID) {
		t.Fatalf("after the drop the message is in %q, want %q", m.Folder, target.Data)
	}
}

// liveLoop runs a's event loop on a goroutine and returns a function that
// runs fn on the UI goroutine and waits for it.
func liveLoop(t *testing.T, a *app.Application) func(func()) {
	t.Helper()
	ran := make(chan error, 1)
	go func() { ran <- a.Run() }()
	t.Cleanup(func() {
		a.Quit()
		select {
		case <-ran:
		case <-time.After(3 * time.Second):
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for !a.Looping() {
		if time.Now().After(deadline) {
			t.Fatal("the event loop never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return func(fn func()) {
		t.Helper()
		done := make(chan struct{})
		a.Post(func() { fn(); close(done) })
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("the UI goroutine did not answer within 3s")
		}
	}
}

// Archive takes the row away at once but tells the daemon only when the
// undo window closes; Undo inside it puts the row back and the daemon never
// hears of it.
func TestUndoHoldsTheMoveBack(t *testing.T) {
	old := undoWindow
	undoWindow = 300 * time.Millisecond
	defer func() { undoWindow = old }()

	cli := demoClient(t)
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(a, w, cli, AppOptions{})
	w.SetContent(s.build())
	onUI := liveLoop(t, a)
	defer onUI(func() { w.Close() })

	folderOf := func(id mailcore.MessageID) mailcore.FolderID {
		m, _, err := cli.GetMessage(id)
		if err != nil {
			t.Fatal(err)
		}
		return m.Folder
	}
	var id mailcore.MessageID
	var inbox mailcore.FolderID
	var shown, pending bool
	onUI(func() {
		id, inbox = s.rows[1].ID, s.folder
		s.selected = []mailcore.MessageID{id}
		s.archive()
		shown, pending = false, s.undo != nil
		for _, m := range s.rows {
			if m.ID == id {
				shown = true
			}
		}
	})
	if shown || !pending {
		t.Fatalf("after Archive: row shown=%v, undo pending=%v", shown, pending)
	}
	if f := folderOf(id); f != inbox {
		t.Fatalf("the daemon moved it to %q inside the undo window", f)
	}
	onUI(func() {
		s.undoLast()
		shown = false
		for _, m := range s.rows {
			if m.ID == id {
				shown = true
			}
		}
	})
	if !shown {
		t.Fatal("Undo did not put the row back")
	}
	time.Sleep(2 * undoWindow)
	if f := folderOf(id); f != inbox {
		t.Fatalf("an undone archive still reached the daemon: now in %q", f)
	}

	onUI(func() {
		s.selected = []mailcore.MessageID{id}
		s.archive()
	})
	deadline := time.Now().Add(3 * time.Second)
	for folderOf(id) == inbox {
		if time.Now().After(deadline) {
			t.Fatal("the archive never reached the daemon after the undo window")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A tag change can be taken back from the undo bar: every message gets the
// tags it had.
func TestUndoTagChange(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	a, b := s.rows[0], s.rows[1]
	s.selected = []mailcore.MessageID{a.ID, b.ID}
	s.toggleTag("Later")
	s.waitIdle()
	for _, id := range []mailcore.MessageID{a.ID, b.ID} {
		if m, _, _ := s.cli.GetMessage(id); !mailcore.HasTag(m.Tags, "Later") {
			t.Fatalf("%s not tagged: %q", id, m.Tags)
		}
	}
	if s.tagUndo == nil || !s.undoBar.Visible() || !strings.Contains(s.undoLabel.Text, "Tagged Later · 2 messages") {
		t.Fatalf("undo bar %v %q", s.undoBar.Visible(), s.undoLabel.Text)
	}
	s.undoLast()
	s.waitIdle()
	for _, m := range []mailcore.Message{a, b} {
		got, _, _ := s.cli.GetMessage(m.ID)
		if mailcore.HasTag(got.Tags, "Later") != mailcore.HasTag(m.Tags, "Later") {
			t.Fatalf("%s after undo: %q, before: %q", m.ID, got.Tags, m.Tags)
		}
	}
	if s.tagUndo != nil || s.undoBar.Visible() {
		t.Fatal("the undo bar stayed up")
	}
}
