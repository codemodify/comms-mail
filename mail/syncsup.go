package mail

import (
	"context"
	"strings"
	"time"
)

// StartPush runs the multi-folder IDLE supervisor (Inbox + Sent) and a
// CONDSTORE/QRESYNC poll of the other mailboxes. Safe to call once per
// LocalStore; a second call is a no-op.
//
// Everything here emits StoreEvents, so the daemon broadcasts mail.changed
// and a connected UI refreshes without polling.
func (s *LocalStore) StartPush(ctx context.Context) {
	s.mu.Lock()
	if s.pushCancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.pushCancel = cancel
	s.mu.Unlock()
	go s.pushLoop(ctx)
}

// StopPush cancels the supervisor (used by tests and on shutdown).
func (s *LocalStore) StopPush() {
	s.mu.Lock()
	cancel := s.pushCancel
	s.pushCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// pushLoop supervises the IDLE watchers and the periodic poll. The watcher
// set is re-evaluated on every tick so an account added through the
// first-run wizard starts receiving push without a daemon restart.
func (s *LocalStore) pushLoop(ctx context.Context) {
	watched := map[string]context.CancelFunc{}
	defer func() {
		for _, cancel := range watched {
			cancel()
		}
	}()
	s.rewatch(ctx, watched)

	tick := time.NewTicker(2 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if s.feat != nil && !s.feat.Online() {
				continue
			}
			s.rewatch(ctx, watched)
			res, _ := s.Sync("")
			if res.New > 0 {
				s.emit(StoreEvent{Reason: "poll", Count: res.New})
			}
		}
	}
}

// rewatch starts a watcher for every account that does not have one and
// stops watchers for accounts that went away.
func (s *LocalStore) rewatch(ctx context.Context, watched map[string]context.CancelFunc) {
	s.mu.Lock()
	accts := append([]Account(nil), s.accounts...)
	s.mu.Unlock()

	live := map[string]bool{}
	for _, a := range accts {
		live[a.ID] = true
		if _, ok := watched[a.ID]; ok {
			continue
		}
		actx, cancel := context.WithCancel(ctx)
		watched[a.ID] = cancel
		go s.idleAccount(actx, a.ID)
	}
	for id, cancel := range watched {
		if !live[id] {
			cancel()
			delete(watched, id)
		}
	}
}

func (s *LocalStore) idleAccount(ctx context.Context, accountID string) {
	cfg, ok := s.accountCfg(accountID)
	if !ok || cfg.IsPOP3() || cfg.IMAP.Host == "" {
		return
	}
	cfg.IMAP.tokenKey = accountID

	started := map[FolderID]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.mu.Lock()
		var watch []Folder
		for _, f := range s.folders {
			if f.AccountID == accountID && (f.Kind == FolderInbox || f.Kind == FolderSent) {
				watch = append(watch, f)
			}
		}
		s.mu.Unlock()
		for _, f := range watch {
			if started[f.ID] {
				continue
			}
			started[f.ID] = true
			go s.idleFolder(ctx, cfg, f)
		}
		// Folders appear after the first LIST, so keep looking for a while
		// instead of sampling once at startup.
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func (s *LocalStore) idleFolder(ctx context.Context, cfg AccountConfig, f Folder) {
	cli := newIMAPClient(cfg.IMAP, cfg.Address)
	defer cli.close()
	// Close the session as soon as the context ends so a blocked IDLE read
	// does not keep the goroutine (and the socket) alive after shutdown.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			cli.close()
		case <-done:
		}
	}()

	backoff := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if s.feat != nil && !s.feat.Online() {
			if !sleepCtx(ctx, 5*time.Second) {
				return
			}
			continue
		}
		if err := cli.connect(); err != nil {
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		if _, err := cli.selectBox(remoteName(f), true); err != nil {
			cli.close()
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = 5 * time.Second
		if !cli.has("IDLE") {
			if n, err := s.syncFolder(cli, f); err == nil && n > 0 {
				s.afterPush(f, n)
			}
			if !sleepCtx(ctx, 2*time.Minute) {
				return
			}
			continue
		}
		woke, err := cli.idleOnce(25 * time.Minute)
		if err != nil {
			cli.close()
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		if woke {
			n, err := s.syncFolder(cli, f)
			if err != nil {
				cli.close()
				continue
			}
			s.mu.Lock()
			s.saveLocked()
			s.mu.Unlock()
			s.afterPush(f, n)
		}
	}
}

// afterPush announces an IDLE-driven change: mail.changed always (flags and
// deletions matter too) and a new-mail notification when messages arrived.
func (s *LocalStore) afterPush(f Folder, n int) {
	s.emit(StoreEvent{Reason: "push", AccountID: f.AccountID, FolderID: f.ID, Count: n})
	if n > 0 {
		s.notifyNew(f.AccountID, n)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

// notifyNew is the daemon-side fallback toast. The RPC broadcast is driven
// by the StoreEvent above, so a connected UI shows its own notification.
func (s *LocalStore) notifyNew(accountID string, n int) {
	if s.feat == nil || n <= 0 {
		return
	}
	p := s.feat.NotifyPrefs()
	if !p.Enabled {
		return
	}
	title := "New mail"
	body := itoa(n) + " new message(s)"
	if p.VIPOnly {
		title = "VIP mail"
	}
	if p.Desktop {
		notifyDesktop(title, body)
	}
}

func syncMode(cli *imapClient) string {
	if cli == nil {
		return "poll"
	}
	var parts []string
	if cli.has("QRESYNC") {
		parts = append(parts, "QRESYNC")
	}
	if cli.has("CONDSTORE") {
		parts = append(parts, "CONDSTORE")
	}
	if cli.has("IDLE") {
		parts = append(parts, "IDLE")
	}
	if len(parts) == 0 {
		return "FLAGS FETCH"
	}
	return strings.Join(parts, "+")
}
