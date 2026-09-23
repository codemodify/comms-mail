//go:build linux

package mail

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// Socket hardening for mailclientd.
//
// The daemon exposes every account, every message and the send path over
// this socket with no authentication of its own, so the socket itself is
// the authorisation boundary:
//
//   - the parent directory is created 0700 and re-chmodded on every start
//   - the socket file is chmodded 0600 immediately after bind
//   - a lock file keeps a second daemon from stealing the path from a
//     running one (the old code unconditionally removed the socket)
//   - every accepted connection must come from our own uid (SO_PEERCRED)

// socketLock is an advisory lock held for the lifetime of a listener.
type socketLock struct {
	f *os.File
}

func (l *socketLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

// lockSocket takes the per-socket lock file. An error means another
// mailclientd is already serving that path.
func lockSocket(socket string) (*socketLock, error) {
	path := socket + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("mailclientd: another daemon is already serving %s (%w)", socket, err)
	}
	if err := f.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	}
	return &socketLock{f: f}, nil
}

// prepareSocketDir makes sure the directory holding the socket is ours and
// owner-only. A world-writable directory (for example /tmp when
// XDG_RUNTIME_DIR is unset) would let another user pre-create the path.
func prepareSocketDir(socket string) error {
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if ok && int(sys.Uid) != os.Getuid() {
		return fmt.Errorf("mailclientd: socket directory %s is owned by uid %d, not %d", dir, sys.Uid, os.Getuid())
	}
	// Only tighten a directory we own and that is not a shared root such as
	// /tmp or $XDG_RUNTIME_DIR itself.
	if ok && st.Mode().Perm()&0o077 != 0 && isPrivateSocketDir(dir) {
		_ = os.Chmod(dir, 0o700)
	}
	return nil
}

// isPrivateSocketDir is true for directories this package creates for its
// own socket (never for /tmp or an XDG runtime root shared with other apps).
// isPrivateSocketDir is true for a directory this package owns exclusively.
// Shared roots (/tmp, $XDG_RUNTIME_DIR itself) are never re-chmodded.
func isPrivateSocketDir(dir string) bool {
	clean := filepath.Clean(dir)
	for _, shared := range []string{filepath.Clean(os.TempDir()), "/", "/run", "/var/run"} {
		if clean == shared {
			return false
		}
	}
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" && clean == filepath.Clean(rt) {
		return false
	}
	return true
}
func listenSocket(socket string) (net.Listener, *socketLock, error) {
	if err := prepareSocketDir(socket); err != nil {
		return nil, nil, err
	}
	lock, err := lockSocket(socket)
	if err != nil {
		return nil, nil, err
	}
	// The lock is ours, so any socket file left behind is stale.
	if err := os.RemoveAll(socket); err != nil {
		lock.release()
		return nil, nil, err
	}
	// Bind with a restrictive umask so there is no window in which the
	// socket is connectable by another user.
	old := syscall.Umask(0o177)
	ln, err := net.Listen("unix", socket)
	syscall.Umask(old)
	if err != nil {
		lock.release()
		return nil, nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = ln.Close()
		lock.release()
		return nil, nil, err
	}
	return ln, lock, nil
}

// peerAllowed reports whether conn's peer is the same uid as this process.
// Root is allowed too (it can read the files anyway).
func peerAllowed(conn net.Conn) (bool, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		// Not a unix socket (tests may use a pipe): nothing to check.
		return true, nil
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return false, err
	}
	var cred *syscall.Ucred
	var credErr error
	err = raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return false, err
	}
	if credErr != nil {
		return false, credErr
	}
	if cred == nil {
		return false, fmt.Errorf("mailclientd: no peer credentials")
	}
	me := os.Getuid()
	return int(cred.Uid) == me || cred.Uid == 0, nil
}
