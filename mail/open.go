package mail

import (
	"os"
	"os/exec"
	"runtime"
)

// openCachedFile launches the platform opener (xdg-open on Linux) after
// mailclientd writes an attachment cache file. No-op without a display
// or when UITK_MAIL_NO_OPEN is set (tests / headless).
func openCachedFile(path string) {
	if path == "" || os.Getenv("UITK_MAIL_NO_OPEN") != "" {
		return
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && runtime.GOOS == "linux" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return
		}
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}
