package mailcore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Starting comms-maild. The window needs the daemon; rather than tell the
// user to start it by hand, it starts it — through the systemd user unit
// when `comms-maild install` put one in place, else the comms-maild beside
// the window's own program or on PATH. `comms-maild install` also starts it
// at every login, so mail syncs and notifies with no window open.

// ErrDaemonRunning is the error of a second comms-maild on the same socket.
var ErrDaemonRunning = errors.New("a comms-maild is already running")

// ServiceName is the systemd user unit comms-maild installs as.
const ServiceName = "comms-maild.service"

// EnsureDaemon connects to the daemon on sock, starting it first when
// nothing answers there.
func EnsureDaemon(sock string) (*Client, error) {
	if cli, err := DialWait(sock, 300*time.Millisecond); err == nil {
		return cli, nil
	}
	if err := startDaemon(sock); err != nil {
		return nil, fmt.Errorf("comms-maild is not running and could not be started: %w", err)
	}
	return DialWait(sock, 10*time.Second)
}

func startDaemon(sock string) error {
	if sock == DefaultSocket() && ServiceInstalled() {
		if _, err := systemctl("--user", "start", ServiceName); err == nil {
			return nil
		}
	}
	bin := daemonBinary()
	if bin == "" {
		return errors.New("no comms-maild beside this program or on PATH")
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), EnvSock+"="+sock)
	detach(cmd) // its own session: it outlives the window
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// daemonBinary is comms-maild beside this program, else on PATH.
func daemonBinary() string {
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			p := filepath.Join(filepath.Dir(exe), "comms-maild")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return p
			}
		}
	}
	if p, err := exec.LookPath("comms-maild"); err == nil {
		return p
	}
	return ""
}

// ---- start at login ---------------------------------------------------------

func systemdUserDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "systemd", "user")
}

func autostartFile() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "autostart", "comms-maild.desktop")
}

// ServiceInstalled reports whether the systemd user unit is in place.
func ServiceInstalled() bool {
	_, err := os.Stat(filepath.Join(systemdUserDir(), ServiceName))
	return err == nil
}

// systemctl runs systemctl with args (tests stand in for it: they must
// never touch the real user manager).
var systemctl = func(args ...string) ([]byte, error) {
	return exec.Command("systemctl", args...).CombinedOutput()
}

// hasUserSystemd reports whether a systemd user manager runs this session.
var hasUserSystemd = func() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	_, err := systemctl("--user", "show-environment")
	return err == nil
}

// serviceUnit is the systemd user unit that runs exe.
func serviceUnit(exe string) string {
	return `[Unit]
Description=comms-mail daemon — mail accounts, sync and sending
Documentation=https://github.com/codemodify/comms-mail/blob/dev/docs/mail.md

[Service]
ExecStart=` + systemdQuote(exe) + `
Restart=on-failure
RestartSec=5
# A second daemon on the same socket exits 3: restarting it would only
# clash again.
RestartPreventExitStatus=3

[Install]
WantedBy=default.target
`
}

// autostartEntry is the XDG autostart entry that runs exe at login, for a
// desktop without a systemd user session.
func autostartEntry(exe string) string {
	return `[Desktop Entry]
Type=Application
Name=comms-mail daemon
Comment=Mail accounts, sync and sending for comms-mail
Exec=` + desktopQuote(exe) + `
NoDisplay=true
X-GNOME-Autostart-enabled=true
`
}

func systemdQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func desktopQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\`$") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(s) + `"`
}

// InstallService makes comms-maild (the program at exe) start at every
// login: a systemd user unit, enabled — started now too, unless a daemon
// already answers on the socket — or, with no systemd user session, an
// XDG autostart entry. It says what it did.
func InstallService(exe string) (string, error) {
	if !filepath.IsAbs(exe) {
		return "", fmt.Errorf("comms-maild: %q is not an absolute path", exe)
	}
	running := false
	if cli, err := DialWait(DefaultSocket(), 300*time.Millisecond); err == nil {
		running = true
		cli.Close()
	}
	if hasUserSystemd() {
		dir := systemdUserDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		unit := filepath.Join(dir, ServiceName)
		if err := WriteFileAtomic(unit, []byte(serviceUnit(exe)), 0o644); err != nil {
			return "", err
		}
		if out, err := systemctl("--user", "daemon-reload"); err != nil {
			return "", fmt.Errorf("systemctl daemon-reload: %v: %s", err, out)
		}
		args := []string{"--user", "enable", ServiceName}
		if !running {
			args = []string{"--user", "enable", "--now", ServiceName}
		}
		if out, err := systemctl(args...); err != nil {
			return "", fmt.Errorf("systemctl enable: %v: %s", err, out)
		}
		msg := "Installed " + unit + "; comms-maild starts at every login."
		if running {
			msg += " A comms-maild is running now; the service takes over at the next login (or stop that one and run: systemctl --user start comms-maild)."
		} else {
			msg += " Started it now."
		}
		return msg, nil
	}
	p := autostartFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := WriteFileAtomic(p, []byte(autostartEntry(exe)), 0o644); err != nil {
		return "", err
	}
	return "Installed " + p + "; comms-maild starts at every login (no systemd user session here, so an autostart entry).", nil
}

// UninstallService undoes InstallService (a running daemon keeps running).
func UninstallService() (string, error) {
	var done []string
	unit := filepath.Join(systemdUserDir(), ServiceName)
	if _, err := os.Stat(unit); err == nil {
		_, _ = systemctl("--user", "disable", ServiceName)
		if err := os.Remove(unit); err != nil {
			return "", err
		}
		_, _ = systemctl("--user", "daemon-reload")
		done = append(done, "removed "+unit)
	}
	if _, err := os.Stat(autostartFile()); err == nil {
		if err := os.Remove(autostartFile()); err != nil {
			return "", err
		}
		done = append(done, "removed "+autostartFile())
	}
	if len(done) == 0 {
		return "comms-maild was not set to start at login.", nil
	}
	return strings.Join(done, "; ") + ". comms-maild no longer starts at login.", nil
}
