package mailcore

import (
	"os"
	"sort"
	"time"
)

// Sync fetches headers only, so every new message used to be a download on
// first click. The prefetcher fills the on-disk cache with the bodies a
// person is likely to open next, in the background, on the shared session:
// the foreground session stays free for the click that beats it there.
const (
	prefetchMax     = 50 // bodies per pass
	prefetchAge     = 14 * 24 * time.Hour
	prefetchMaxSize = 2 << 20 // bytes; larger messages wait for a click
)

// prefetchBodies starts a pass for every account unless one is running.
func (s *LocalStore) prefetchBodies() {
	if !s.prefetching.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.prefetching.Store(false)
		s.mu.Lock()
		accts := append([]Account(nil), s.accounts...)
		s.mu.Unlock()
		for _, a := range accts {
			if s.feat != nil && !s.feat.Online() {
				return
			}
			s.prefetchAccount(a.ID)
		}
	}()
}

// prefetchCandidates is what to download for accountID, newest first: recent,
// not too large, not already cached, and not in Junk or Trash.
func (s *LocalStore) prefetchCandidates(accountID string, now time.Time) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-prefetchAge)
	var out []Message
	for _, m := range s.Messages {
		if m.AccountID != accountID || m.UID == 0 || m.Body != "" || m.HTML != "" {
			continue
		}
		if m.Date.Before(cutoff) || m.Size > prefetchMaxSize {
			continue
		}
		f, ok := s.folderLocked(m.Folder)
		if !ok || f.Virtual || f.Kind == FolderJunk || f.Kind == FolderTrash {
			continue
		}
		if p, err := s.rawPath(m); err != nil {
			continue
		} else if _, err := os.Stat(p); err == nil {
			continue
		}
		out = append(out, m.Clone())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	if len(out) > prefetchMax {
		out = out[:prefetchMax]
	}
	return out
}

func (s *LocalStore) prefetchAccount(accountID string) {
	if cfg, ok := s.accountCfg(accountID); !ok || cfg.IsPOP3() {
		return
	}
	want := s.prefetchCandidates(accountID, time.Now())
	if len(want) == 0 {
		return
	}
	cli, err := s.client(accountID)
	if err != nil {
		return
	}
	// One SELECT per folder rather than one per message.
	byFolder := map[FolderID][]Message{}
	var order []FolderID
	for _, m := range want {
		if _, seen := byFolder[m.Folder]; !seen {
			order = append(order, m.Folder)
		}
		byFolder[m.Folder] = append(byFolder[m.Folder], m)
	}
	for _, fid := range order {
		s.mu.Lock()
		f, ok := s.folderLocked(fid)
		s.mu.Unlock()
		if !ok {
			continue
		}
		err := cli.inBox(func() error {
			if err := selectFor(cli, f, true); err != nil {
				return err
			}
			for _, m := range byFolder[fid] {
				raw, err := cli.uidFetchRFC822(m.UID)
				if err != nil {
					return err
				}
				if len(raw) == 0 {
					continue // gone from the server; the next sync drops it
				}
				s.mu.Lock()
				if _, ok := s.indexLocked(m.ID); ok {
					s.WriteRawLocked(m, raw)
				}
				s.mu.Unlock()
			}
			return nil
		})
		if err != nil {
			if isNetworkError(err) {
				s.noteNetwork(accountID, err)
				return
			}
		}
	}
}
