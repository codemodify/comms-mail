package mailui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/codemodify/uitoolkit/style"
)

// comms-mail registers the share directories that hold icon packs — its
// user's data directory and the system's — and no directory that has none.
func TestUseShippedArt(t *testing.T) {
	home, system := t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)
	t.Setenv("XDG_DATA_DIRS", filepath.Join(system, "empty")+string(os.PathListSeparator)+system)
	if err := os.MkdirAll(filepath.Join(home, "comms-mail", "icons", "heroicons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(system, "comms-mail", "icons", "lucide"), 0o755); err != nil {
		t.Fatal(err)
	}
	style.ResetSearchPathsForTest()
	defer style.ResetSearchPathsForTest()

	UseShippedArt()
	got := style.SearchPaths()
	want := []string{filepath.Join(home, "comms-mail"), filepath.Join(system, "comms-mail")}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Fatalf("search paths %q: %s missing", got, w)
		}
	}
	if slices.Contains(got, filepath.Join(system, "empty", "comms-mail")) {
		t.Fatalf("search paths %q: a directory with no icons was registered", got)
	}
	if i, j := slices.Index(got, want[0]), slices.Index(got, want[1]); i > j {
		t.Fatalf("search paths %q: the user's data directory should come before the system's", got)
	}
	// The user's own set still comes first.
	dirs := style.IconSetDirs("heroicons")
	if len(dirs) < 2 || dirs[0] != style.UserIconSetDir("heroicons") || !slices.Contains(dirs, filepath.Join(home, "comms-mail", "icons", "heroicons")) {
		t.Fatalf("heroicons dirs %q", dirs)
	}
}
