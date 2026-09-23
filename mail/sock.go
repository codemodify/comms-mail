package mail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Env vars for mailclientd / mailclientui.
const (
	EnvMail = "UITK_MAIL"      // unset: empty LocalStore | memory | imap
	EnvSock = "UITK_MAIL_SOCK" // Unix socket path
	EnvHost = "UITK_MAIL_HOST"
	EnvUser = "UITK_MAIL_USER"
	EnvPass = "UITK_MAIL_PASS"
	EnvTLS  = "UITK_MAIL_TLS"
	EnvName = "UITK_MAIL_NAME"
)

// DefaultSocket is the mailclientd listen path.
// UITK_MAIL_SOCK overrides; else $XDG_RUNTIME_DIR/mailclientd.sock;
// else /tmp/mailclientd-<uid>.sock.
func DefaultSocket() string {
	if p := os.Getenv(EnvSock); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "mailclientd.sock")
	}
	return filepath.Join(os.TempDir(), "mailclientd-"+strconv.Itoa(os.Getuid())+".sock")
}

// OpenStore selects the disk+IMAP/POP3 LocalStore, or MemoryStore when asked.
//
//	UITK_MAIL=memory|demo|mem  — seeded in-memory dogfood (explicit only)
//	UITK_MAIL=imap             — LocalStore from env and/or mail.json
//	unset + config file        — LocalStore (real accounts)
//	unset + no config          — empty LocalStore (no demo accounts)
func OpenStore() (Store, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMail))) {
	case "memory", "demo", "mem":
		return NewDemoStore(), nil
	case "imap":
		cfg, err := ConfigFromEnv()
		if err != nil {
			if file, ferr := LoadConfig(); ferr == nil && len(file.Accounts) > 0 {
				cfg = file
				err = nil
			}
		}
		if err != nil {
			s := NewIMAPStoreFromEnv()
			return s, err
		}
		if file, ferr := LoadConfig(); ferr == nil && len(file.Accounts) > 0 {
			cfg.Accounts = append(file.Accounts, cfg.Accounts...)
		}
		return NewLocalStore(cfg)
	default:
		if cfg, err := LoadConfig(); err == nil && len(cfg.Accounts) > 0 {
			return NewLocalStore(cfg)
		}
		if os.Getenv(EnvMail) != "" {
			return nil, fmt.Errorf("UITK_MAIL=%s: want memory or imap", os.Getenv(EnvMail))
		}
		return NewLocalStore(MailConfig{})
	}
}

// StartDemo runs mailclientd in-process on a temp socket with the seeded
// MemoryStore (screenshots / UITK_MAIL=memory dogfood).
func StartDemo(ctx context.Context) (socket string, stop func(), err error) {
	return startStore(ctx, NewDemoStore())
}

// StartEmpty runs mailclientd with an empty MemoryStore (no demo accounts).
func StartEmpty(ctx context.Context) (socket string, stop func(), err error) {
	return startStore(ctx, NewMemoryStore(time.Time{}))
}

func startStore(ctx context.Context, store Store) (socket string, stop func(), err error) {
	dir, err := os.MkdirTemp("", "mailclientd-")
	if err != nil {
		return "", nil, err
	}
	socket = filepath.Join(dir, "mailclientd.sock")
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- ListenAndServe(ctx, socket, store)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			stop = func() {
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
				}
				_ = os.RemoveAll(dir)
			}
			return socket, stop, nil
		}
		select {
		case err := <-done:
			_ = os.RemoveAll(dir)
			if err == nil {
				err = fmt.Errorf("mailclientd exited before listen")
			}
			cancel()
			return "", nil, err
		case <-time.After(15 * time.Millisecond):
		}
	}
	cancel()
	_ = os.RemoveAll(dir)
	return "", nil, fmt.Errorf("mailclientd: socket not ready")
}
