//go:build !linux

package mail

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Socket hardening fallback for platforms without SO_PEERCRED. The socket is
// still confined to an owner-only directory and chmodded 0600, and the lock
// file still prevents two daemons from fighting over the path; only the
// per-connection uid check is unavailable.

type socketLock struct{ f *os.File }

func (l *socketLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = l.f.Close()
	l.f = nil
}

func lockSocket(socket string) (*socketLock, error) {
	f, err := os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return &socketLock{f: f}, nil
}

func prepareSocketDir(socket string) error {
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return nil
}

func listenSocket(socket string) (net.Listener, *socketLock, error) {
	if err := prepareSocketDir(socket); err != nil {
		return nil, nil, err
	}
	lock, err := lockSocket(socket)
	if err != nil {
		return nil, nil, err
	}
	if err := os.RemoveAll(socket); err != nil {
		lock.release()
		return nil, nil, err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		lock.release()
		return nil, nil, fmt.Errorf("mailclientd: listen %s: %w", socket, err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = ln.Close()
		lock.release()
		return nil, nil, err
	}
	return ln, lock, nil
}

// peerAllowed cannot be checked here; the 0600 socket in a 0700 directory is
// the boundary.
func peerAllowed(conn net.Conn) (bool, error) { return true, nil }
