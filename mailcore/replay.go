package mailcore

import "errors"

// A queued move or delete is replayed against the server only. The cache
// took the change when it was made, so replay must not apply it again: the
// old replay called Move and Delete, which by then found the message
// already in its destination, SELECTed that folder and sent a UID that
// belongs to the source — moving nothing, or in Trash expunging whatever
// message happened to carry that UID there.

// errStaleUIDs is a queued op whose folder was renumbered (UIDVALIDITY
// changed) since it was queued: its UID no longer names the message.
var errStaleUIDs = errors.New("mail: folder UIDs changed since the op was queued")

// serverMoveOpLocked is the queued op that moves m from where it is now to
// dest on the server. The caller holds s.mu and has not yet moved m in the
// cache.
func (s *LocalStore) serverMoveOpLocked(m Message, dest FolderID) OutboxOp {
	return OutboxOp{
		Kind:      "move",
		MessageID: m.ID,
		AccountID: m.AccountID,
		UID:       m.UID,
		Src:       m.Folder,
		Dest:      dest,
		UIDVal:    s.loadFolderMeta(m.Folder).UIDValidity,
	}
}

// queueFailed queues a server move (kind "move" to dest) or purge (kind
// "delete") that failed while online, so a network blip does not leave the
// server disagreeing with the cache for good.
func (s *LocalStore) queueFailed(m Message, kind string, dest FolderID, cause error) {
	if s.feat == nil || m.UID == 0 {
		return
	}
	s.mu.Lock()
	op := s.serverMoveOpLocked(m, dest)
	op.Kind = kind
	if kind == "delete" {
		op.Dest = ""
	}
	op.Error = cause.Error()
	s.feat.mu.Lock()
	s.feat.enqueueLocked(op)
	s.feat.mu.Unlock()
	s.saveLocked()
	s.mu.Unlock()
}

// replayFolders resolves a queued op's source (and destination, when it
// has one). ok is false for an op that cannot be replayed safely: one
// queued before ops recorded their source, or whose folders are gone.
func (s *LocalStore) replayFolders(op OutboxOp) (src, dst Folder, ok bool) {
	if op.Src == "" || op.UID == 0 {
		return Folder{}, Folder{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	src, okSrc := s.folderLocked(op.Src)
	if !okSrc {
		return Folder{}, Folder{}, false
	}
	if op.Dest != "" {
		var okDst bool
		if dst, okDst = s.folderLocked(op.Dest); !okDst {
			return Folder{}, Folder{}, false
		}
	}
	return src, dst, true
}

// selectForReplay SELECTs src read-write and refuses when its UIDs were
// renumbered since op was queued. The caller holds the mailbox lock.
func selectForReplay(cli *imapClient, src Folder, op OutboxOp) error {
	st, err := cli.selectBox(remoteName(src), false)
	if err != nil {
		return err
	}
	if op.UIDVal != 0 && st.UIDValidity != 0 && st.UIDValidity != op.UIDVal {
		return errStaleUIDs
	}
	return nil
}

// replayMove performs a queued move on the server and re-keys the cached
// entry to the UID the destination gave it.
func (s *LocalStore) replayMove(op OutboxOp) error {
	src, dst, ok := s.replayFolders(op)
	if !ok || op.Dest == "" {
		return nil // nothing safe to do; drop it and let sync show the truth
	}
	cli, err := s.client(op.AccountID)
	if err != nil {
		return err
	}
	var newUID uint32
	err = cli.inBox(func() error {
		if err := selectForReplay(cli, src, op); err != nil {
			return err
		}
		var err error
		newUID, err = cli.uidMove(op.UID, remoteName(dst))
		return err
	})
	if errors.Is(err, errStaleUIDs) {
		return nil
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	if i, ok := s.indexLocked(op.MessageID); ok {
		s.rekeyMovedLocked(i, dst.ID, newUID)
	}
	s.saveLocked()
	s.mu.Unlock()
	return nil
}

// replayPurge permanently deletes a queued message from the folder it was
// in when it was deleted.
func (s *LocalStore) replayPurge(op OutboxOp) error {
	src, _, ok := s.replayFolders(OutboxOp{Src: op.Src, UID: op.UID})
	if !ok {
		return nil
	}
	cli, err := s.client(op.AccountID)
	if err != nil {
		return err
	}
	err = cli.inBox(func() error {
		if err := selectForReplay(cli, src, op); err != nil {
			return err
		}
		if err := cli.uidStore(op.UID, []string{`\Deleted`}, nil); err != nil {
			return err
		}
		return cli.expungeUID(op.UID)
	})
	if errors.Is(err, errStaleUIDs) {
		return nil
	}
	return err
}
