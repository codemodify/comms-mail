package mailui

import (
	"os"
	"path/filepath"

	"github.com/codemodify/uitoolkit/style"
)

// UseShippedArt tells uitoolkit where the icon packs comms-mail ships are
// (<dir>/icons/<set>/*.png, which `make` copies from the uitoolkit it was
// built with), so an icon the toolkit added since the user installed a
// pack — or a pack they never installed — draws rather than showing the
// missing-icon mark. The user's own ~/.config/uitoolkit/icons still comes
// first, file by file; comms-mail never writes there. Call it before the
// first window.
func UseShippedArt() {
	for _, d := range shareDirs() {
		if fi, err := os.Stat(filepath.Join(d, "icons")); err == nil && fi.IsDir() {
			style.AddSearchPath(d)
		}
	}
}

// shareDirs are where comms-mail's art may be, first found first used:
// beside the binary (bin/../share/comms-mail, where `make build` and
// `make install` put it), then $XDG_DATA_HOME/comms-mail, then each of
// $XDG_DATA_DIRS for a system-wide install.
func shareDirs() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		out = append(out, filepath.Join(filepath.Dir(exe), "..", "share", "comms-mail"))
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		if home, err := os.UserHomeDir(); err == nil {
			data = filepath.Join(home, ".local", "share")
		}
	}
	if data != "" {
		out = append(out, filepath.Join(data, "comms-mail"))
	}
	system := os.Getenv("XDG_DATA_DIRS")
	if system == "" {
		system = "/usr/local/share:/usr/share"
	}
	for _, d := range filepath.SplitList(system) {
		if d != "" {
			out = append(out, filepath.Join(d, "comms-mail"))
		}
	}
	return out
}
