package mailcore

import (
	"os"
	"os/exec"
	"runtime"
)

// openCachedFile launches the platform opener (xdg-open on Linux) after
// comms-maild writes an attachment cache file, and reports whether it did.
// It does not without a display — a daemon started at login by systemd may
// have none, and then the window, which always has one, opens the file
// (Client.OpenPart) — or when UITK_MAIL_NO_OPEN is set (tests / headless).
func openCachedFile(path string) bool {
	if path == "" || os.Getenv("UITK_MAIL_NO_OPEN") != "" {
		return false
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && runtime.GOOS == "linux" {
		return false
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return false
		}
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _ = cmd.Wait() }() // no zombie in a long-running daemon
	return true
}

// OpenWithDesktop opens path with the desktop's handler for it (a browser
// for .html), reporting whether it could.
func OpenWithDesktop(path string) bool { return openCachedFile(path) }
