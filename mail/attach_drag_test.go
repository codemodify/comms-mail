package mail

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// A drag of an attachment offers the file it is going to write, and the
// bytes come when the target asks for them.
//
// The fetch used to run at the press: a mailclientd round trip for a part
// the store had not cached froze the window between the press and the
// drag taking hold. The path is all the drag needs to start, so it starts
// with that and the fetch runs behind it.
func TestAttachmentDragPromisesItsFile(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	name := selectMessageWithAttachment(t, s)

	d := s.dragAttachment(0)
	if d == nil {
		t.Fatal("no drag for the first attachment")
	}
	if !d.Offers("text/uri-list") {
		t.Fatalf("the drag offers %q, want a uri-list", d.Types)
	}
	data, ok := d.Read("text/uri-list")
	if !ok {
		t.Fatal("the target asked for the uri-list and was refused")
	}
	uris := string(data)
	if !strings.HasPrefix(uris, "file://") || !strings.Contains(uris, attachFileName(name)) {
		t.Fatalf("the drag offers %q, want a file:// URI naming %q", uris, name)
	}
	// The promised file is really there, under the scratch directory and
	// nowhere near the user's own.
	e := widget.NewDropEvent(paintengine2d.Point{}, "text/uri-list", data)
	if len(e.Paths) != 1 {
		t.Fatalf("the uri-list decodes to %q, want one path", e.Paths)
	}
	path := e.Paths[0]
	if dir := attachScratchDir(); dir == "" || !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		t.Fatalf("the attachment was written to %q, outside the scratch directory %q", path, dir)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the file the drag promised is not there: %v", err)
	}
	if st.Size() == 0 {
		t.Fatal("the file the drag promised is empty")
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("the attachment is %v, want 0600: it is somebody's mail", st.Mode().Perm())
	}
}

// A fetch that never lands, or lands with an error, refuses the type
// rather than handing over a path to a file that was never written: a
// target given a path to nothing shows the user an empty drop.
func TestAttachmentDragRefusesAFileThatNeverArrived(t *testing.T) {
	old := attachDragWait
	attachDragWait = 30 * time.Millisecond
	defer func() { attachDragWait = old }()

	arrived := &attachPromise{path: "/tmp/x", done: make(chan struct{})}
	close(arrived.done)
	if !arrived.ready() {
		t.Fatal("a fetch that landed is ready")
	}

	failed := &attachPromise{path: "/tmp/x", done: make(chan struct{}), err: errors.New("no route to the store")}
	close(failed.done)
	if failed.ready() {
		t.Fatal("a fetch that failed is not ready, whatever it left behind")
	}

	stuck := &attachPromise{path: "/tmp/x", done: make(chan struct{})}
	start := time.Now()
	if stuck.ready() {
		t.Fatal("a fetch still running is not ready")
	}
	if waited := time.Since(start); waited < attachDragWait {
		t.Fatalf("gave up after %v, before the wait was out", waited)
	}
	// It comes good the moment the fetch does.
	close(stuck.done)
	if !stuck.ready() {
		t.Fatal("the fetch landed and the promise is still not ready")
	}
}

// selectMessageWithAttachment puts the reading pane on a message that has
// one, and reports the first attachment's name.
func selectMessageWithAttachment(t *testing.T, s *session) string {
	t.Helper()
	for _, m := range s.rows {
		if len(m.Attachments) == 0 {
			continue
		}
		s.selected = []MessageID{m.ID}
		s.loadPreview()
		if len(s.attNames) == 0 {
			continue
		}
		return s.attNames[0]
	}
	t.Skip("no message in the demo store has an attachment")
	return ""
}
