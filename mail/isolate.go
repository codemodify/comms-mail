package mail

import (
	"os"
	"path/filepath"
	"strings"
)

// IsolateTestEnv points mail config, cache, chrome prefs, and runtime
// sockets at dir and forces the in-memory backend.
//
// It never reads the user's ~/.config/uitoolkit/mail.json or connects to
// a live IMAP/POP3 account. Automated tests and uitest-driver MUST call
// this (or IsolateTestEnvTB) before constructing Mail UI.
func IsolateTestEnv(dir string) error {
	if dir == "" {
		return os.ErrInvalid
	}
	cfg := filepath.Join(dir, "config")
	data := filepath.Join(dir, "data")
	run := filepath.Join(dir, "run")
	for _, p := range []string{cfg, data, run} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return err
		}
	}
	_ = os.Setenv("XDG_CONFIG_HOME", cfg)
	_ = os.Setenv("XDG_DATA_HOME", data)
	_ = os.Setenv("XDG_RUNTIME_DIR", run)
	_ = os.Setenv(EnvConfig, filepath.Join(cfg, "no-user-mail.json"))
	_ = os.Setenv(EnvData, data)
	_ = os.Setenv(EnvMail, "memory")
	_ = os.Setenv("UITK_MAIL_NO_OPEN", "1")
	_ = os.Setenv("UITK_MAIL_NO_NOTIFY", "1")
	_ = os.Unsetenv(EnvSock)
	_ = os.Unsetenv(EnvHost)
	_ = os.Unsetenv(EnvUser)
	_ = os.Unsetenv(EnvPass)
	_ = os.Unsetenv(EnvSMTPHost)
	_ = os.Unsetenv(EnvXOAuth)
	return nil
}

// envTB is the testing.TB subset IsolateTestEnvTB needs.
type envTB interface {
	Helper()
	TempDir() string
	Setenv(key, value string)
	Fatal(args ...any)
}

// IsolateTestEnvTB isolates mail I/O for a Go test and restores env on cleanup.
func IsolateTestEnvTB(t envTB) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	data := filepath.Join(dir, "data")
	run := filepath.Join(dir, "run")
	for _, p := range []string{cfg, data, run} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv(EnvConfig, filepath.Join(cfg, "no-user-mail.json"))
	t.Setenv(EnvData, data)
	t.Setenv(EnvMail, "memory")
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	t.Setenv("UITK_MAIL_NO_NOTIFY", "1")
	t.Setenv(EnvSock, "")
	t.Setenv(EnvHost, "")
	t.Setenv(EnvUser, "")
	t.Setenv(EnvPass, "")
	t.Setenv(EnvSMTPHost, "")
	t.Setenv(EnvXOAuth, "")
}

// IsDisposableMailSocket reports whether socket was created by StartDemo /
// StartEmpty (temp dir named mailclientd-*). The user's default daemon
// socket (XDG_RUNTIME_DIR/mailclientd.sock or /tmp/mailclientd-<uid>.sock)
// is not disposable.
func IsDisposableMailSocket(socket string) bool {
	if socket == "" {
		return false
	}
	base := filepath.Base(filepath.Dir(socket))
	return strings.HasPrefix(base, "mailclientd-")
}

// AssertMemoryBackend refuses anything except the in-memory dogfood store.
func AssertMemoryBackend(backend string) error {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "memory", "demo", "mem":
		return nil
	default:
		return errNotMemory(backend)
	}
}

type errNotMemory string

func (e errNotMemory) Error() string {
	return "mail test safety: backend " + string(e) + " is not the in-memory fixture (refusing live IMAP/POP3)"
}
