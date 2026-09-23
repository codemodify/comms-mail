package mail

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
)

// Dragging an attachment out of the reading pane, which is how a mail
// client hands a file to a file manager, an editor, or another mail
// client's compose window.
//
// The other side of a drag wants a path, not bytes, so the attachment is
// written out to a scratch file. Where that file will be is settled at
// the press — it is a name and a directory, and costs nothing — but the
// bytes are fetched off the UI goroutine while the drag is already
// running, because a mailclientd round trip for a part the store has not
// cached can take seconds, and doing it at the press froze the window
// between the press and the drag taking hold.
//
// The drag is therefore a promise: the path is offered at once, and the
// fetch has the whole of the drag to land. A target only asks for the
// data at the drop, so by then it nearly always has; what little wait is
// left is bounded (attachDragWait) and answered with a refusal rather
// than a path to a file that is not there.

// attachScratch is where dragged attachments are written, made once per
// process.
var attachScratch struct {
	sync.Once
	dir string
}

func attachScratchDir() string {
	attachScratch.Do(func() {
		if dir, err := os.MkdirTemp("", "uitk-mail-drag-"); err == nil {
			attachScratch.dir = dir
		}
	})
	return attachScratch.dir
}

// attachDragWait is how long a target asking for the data may be made to
// wait for the fetch the press started. It is generous: by the time
// anything asks, the drag has already been going for as long as the user
// took to move the pointer, so the wait is nearly always nothing at all,
// and giving up early would refuse a drop the user meant. A variable so
// a test need not sit through it.
var attachDragWait = 10 * time.Second

// attachPromise is the file an attachment drag promises: where it will
// be, and the fetch putting it there.
type attachPromise struct {
	path string
	done chan struct{}
	err  error // written before done is closed, read after
}

// ready waits for the fetch and reports whether the file is there.
func (p *attachPromise) ready() bool {
	select {
	case <-p.done:
	case <-time.After(attachDragWait):
		return false
	}
	return p.err == nil
}

// dragAttachment is the drag of attachment i: the file it will be written
// to, offered as a uri-list the way every file drag is, with the fetch
// running off the UI goroutine behind it.
func (s *session) dragAttachment(i int) *widget.Drag {
	m, ok := s.primary()
	if !ok || i < 0 || i >= len(s.attNames) {
		return nil
	}
	dir := attachScratchDir()
	if dir == "" {
		return nil
	}
	// Which part this is, and what it is called, are the window's to
	// answer and are settled here; only the bytes are fetched later.
	pid := s.attachPartID(m, i)
	if pid == "" {
		s.mark("Drag: no such attachment")
		return nil
	}
	name := attachFileName(s.attNames[i])
	// One directory per attachment: two messages with a scan.pdf each
	// must not write over one another mid-drag.
	sub := filepath.Join(dir, safeDirName(m.ID, i))
	if err := os.MkdirAll(sub, 0o700); err != nil {
		s.mark("Drag: " + err.Error())
		return nil
	}
	path := filepath.Join(sub, name)
	d := widget.DragFiles(path)
	if d == nil {
		return nil
	}
	pr := &attachPromise{path: path, done: make(chan struct{})}
	// No done callback: it would be delivered on the UI goroutine, which
	// is the very goroutine waiting on the promise, and the two would
	// hold each other. The worker closes the channel itself, and whoever
	// asks for the data reports what went wrong.
	s.async(func() (any, error) {
		defer close(pr.done)
		data, err := s.partBytes(m.ID, pid, name)
		if err == nil {
			err = writeFileAtomic(path, data, 0o600)
		}
		pr.err = err
		return nil, err
	}, nil)
	offer := d.Data
	d.Data = func(mime string) ([]byte, bool) {
		if !pr.ready() {
			// The target asked and there is nothing to give it. Refusing
			// the type is what makes the drop fail cleanly, instead of
			// handing over a path to a file that was never written.
			if pr.err != nil {
				s.mark("Drag: " + pr.err.Error())
			} else {
				s.mark("Drag: " + name + " is still being fetched")
			}
			return nil, false
		}
		return offer(mime)
	}
	// An attachment is a copy: the message keeps it whatever the target
	// does, so offering a move would promise something untrue.
	d.Actions, d.Preferred = platform.DragCopy, platform.DragCopy
	if s.win != nil {
		d.Image, d.Hotspot = widget.DragLabel(s.win.Look(), name, s.win.Scale())
	}
	d.Done = func(action platform.DragAction) {
		if action == platform.DragNone {
			s.mark("Drag cancelled")
			return
		}
		s.mark("Dragged " + name)
	}
	return d
}

// safeDirName is a directory name for one message's attachment, with
// nothing in it a path could be traversed with.
func safeDirName(id MessageID, i int) string {
	out := make([]rune, 0, len(id)+4)
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
		if len(out) >= 48 {
			break
		}
	}
	return string(out) + "-" + string(rune('a'+i%26))
}

// DragAt drags the attachment out of its row (widget.DragSource).
func (h *attachHit) DragAt(paintengine2d.Point) *widget.Drag {
	if h.Drag == nil {
		return nil
	}
	return h.Drag()
}
