package mailcore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogFileWritesAndRotates(t *testing.T) {
	dir := t.TempDir()
	p, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseLog()
	old := maxLogBytes
	maxLogBytes = 300
	defer func() { maxLogBytes = old }()
	Logf("sync %s: %v", "home/inbox", "timeout\nforged line")
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "sync home/inbox: timeout ⏎ forged line\n") || strings.Count(string(b), "\n") != 1 {
		t.Fatalf("log line %q", b)
	}
	for i := 0; i < 20; i++ {
		Logf("line %02d with some padding to fill the log up", i)
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatal("the log did not rotate")
	}
	b, _ = os.ReadFile(p)
	if len(b) > 300 || !strings.Contains(string(b), "line 19") {
		t.Fatalf("after rotation %d bytes: %q", len(b), b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v", fi.Mode().Perm())
	}
	CloseLog()
	Logf("after close") // no-op, no panic
}

// install writes and enables a systemd user unit (a stand-in systemctl
// records the calls — the real user manager is never touched); uninstall
// takes it away.
func TestInstallServiceSystemd(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv(EnvSock, filepath.Join(t.TempDir(), "none.sock")) // nothing running
	var calls []string
	oldCtl, oldHas := systemctl, hasUserSystemd
	systemctl = func(args ...string) ([]byte, error) { calls = append(calls, strings.Join(args, " ")); return nil, nil }
	hasUserSystemd = func() bool { return true }
	defer func() { systemctl, hasUserSystemd = oldCtl, oldHas }()

	msg, err := InstallService("/opt/comms mail/comms-maild")
	if err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(filepath.Join(cfg, "systemd", "user", ServiceName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), `ExecStart="/opt/comms mail/comms-maild"`) || !strings.Contains(string(unit), "WantedBy=default.target") ||
		!strings.Contains(string(unit), "RestartPreventExitStatus=3") {
		t.Fatalf("unit:\n%s", unit)
	}
	if strings.Join(calls, "|") != "--user daemon-reload|--user enable --now "+ServiceName || !strings.Contains(msg, "Started it now") {
		t.Fatalf("calls %q msg %q", calls, msg)
	}
	if !ServiceInstalled() {
		t.Fatal("ServiceInstalled after install")
	}
	calls = nil
	if _, err := UninstallService(); err != nil {
		t.Fatal(err)
	}
	if ServiceInstalled() || calls[0] != "--user disable "+ServiceName {
		t.Fatalf("after uninstall: installed=%v calls %q", ServiceInstalled(), calls)
	}
	if _, err := InstallService("relative/comms-maild"); err == nil {
		t.Fatal("installed a relative path")
	}
}

func TestInstallServiceAutostartWithoutSystemd(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv(EnvSock, filepath.Join(t.TempDir(), "none.sock"))
	old := hasUserSystemd
	hasUserSystemd = func() bool { return false }
	defer func() { hasUserSystemd = old }()
	if _, err := InstallService("/usr/bin/comms-maild"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cfg, "autostart", "comms-maild.desktop"))
	if err != nil || !strings.Contains(string(b), "Exec=/usr/bin/comms-maild\n") || !strings.Contains(string(b), "NoDisplay=true") {
		t.Fatalf("autostart %q %v", b, err)
	}
	if msg, _ := UninstallService(); !strings.Contains(msg, "no longer starts") {
		t.Fatalf("uninstall: %q", msg)
	}
}

// A running daemon is used as it is; with none and nothing to start, the
// error says so.
func TestEnsureDaemon(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	start := time.Now()
	cli, err := EnsureDaemon(sock)
	if err != nil {
		t.Fatal(err)
	}
	cli.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatal("slow to use a running daemon")
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := EnsureDaemon(filepath.Join(t.TempDir(), "none.sock")); err == nil || !strings.Contains(err.Error(), "could not be started") {
		t.Fatalf("no daemon: %v", err)
	}
}
