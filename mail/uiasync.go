package mail

import (
	"sync"
	"time"

	"github.com/codemodify/uitoolkit/app"
)

// Off-thread RPC plumbing for the Mail window.
//
// Every mailclientd call is a blocking round trip, and a few of them (sync,
// fetch, opening a large attachment) legitimately take minutes. Running them
// on the UI goroutine froze the window; results now come back through
// app.Application.Post, which is the toolkit's UI-thread queue.

// async runs work off the UI goroutine and delivers the result on it.
// done may be nil. When the session has no Application (headless tests) the
// work runs inline so behaviour stays synchronous and testable.
// async runs work off the UI goroutine and delivers the result on it.
//
// done may be nil. When there is no live run loop — headless rendering and
// tests, where app.Post already executes inline — the work runs inline too,
// so a caller still observes the effect as soon as it returns.
func (s *session) async(work func() (any, error), done func(any, error)) {
	if s == nil || work == nil {
		return
	}
	if !appLooping(s.app) {
		v, err := work()
		if done != nil {
			done(v, err)
		}
		return
	}
	s.busy.Add(1)
	go func() {
		defer s.busy.Done()
		v, err := work()
		if done == nil {
			return
		}
		s.app.Post(func() { done(v, err) })
	}()
}

// appLooping reports whether a is pumping its event loop.
func appLooping(a *app.Application) bool {
	return a.Looping()
}

// post runs fn on the UI goroutine (inline when there is no live loop).
// post runs fn on the UI goroutine.
//
// It is only safe to call from a background goroutine when the application
// is actually pumping: app.Post runs fn inline when the loop is not running,
// which would touch widgets from the caller's goroutine. Background callers
// must therefore use postLive.
func (s *session) post(fn func()) {
	postUI(s.app, fn)
}

// postLive queues fn on a live UI loop and reports whether it did. When
// there is no loop (headless, tests) nothing is run and the caller is
// expected to mark the work as pending instead.
func (s *session) postLive(fn func()) bool {
	if s == nil || s.app == nil || fn == nil {
		return false
	}
	if !appLooping(s.app) {
		return false
	}
	s.app.Post(fn)
	return true
}
func (s *session) waitIdle() { s.busy.Wait() }

// refreshCoalescer collapses a burst of daemon events into one refresh.
// A busy IDLE folder can emit many mail.changed notifications per second and
// each refresh re-queries the daemon for every folder's unread count.
type refreshCoalescer struct {
	mu      sync.Mutex
	pending bool
	timer   *time.Timer
	delay   time.Duration
	fire    func()
}

func newRefreshCoalescer(delay time.Duration, fire func()) *refreshCoalescer {
	return &refreshCoalescer{delay: delay, fire: fire}
}

func (r *refreshCoalescer) request() {
	if r == nil || r.fire == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending {
		return
	}
	r.pending = true
	r.timer = time.AfterFunc(r.delay, func() {
		r.mu.Lock()
		r.pending = false
		r.mu.Unlock()
		r.fire()
	})
}

func (r *refreshCoalescer) stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.pending = false
}

// postUI runs fn on the UI goroutine of a (possibly nil) Application.
func postUI(a *app.Application, fn func()) {
	if fn == nil {
		return
	}
	if a == nil {
		fn()
		return
	}
	a.Post(fn)
}

// runAsync is the session-free form of session.async, for windows (Compose,
// Add Account) that hold an Application but no session.
func runAsync(a *app.Application, work func() (any, error), done func(any, error)) {
	if work == nil {
		return
	}
	if !appLooping(a) {
		v, err := work()
		if done != nil {
			done(v, err)
		}
		return
	}
	go func() {
		v, err := work()
		if done == nil {
			return
		}
		a.Post(func() { done(v, err) })
	}()
}
