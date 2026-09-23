package mail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeID is the character set allowed in any identifier that reaches the
// filesystem (account ids, folder ids, message ids). Anything else is
// folded to '-' so a hostile accounts.put cannot escape the data dir.
func safeID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if out == "" || out == "." || out == ".." {
		return "acct"
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// ValidAccountID reports whether id is already safe for use in a path.
func ValidAccountID(id string) bool {
	return id != "" && id == safeID(id)
}

// underRoot resolves rel against root and fails if the result escapes it.
// Every filesystem write derived from untrusted ids goes through this.
func underRoot(root string, rel ...string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("mail: empty data root")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(append([]string{absRoot}, rel...)...)
	clean := filepath.Clean(p)
	if clean != absRoot && !strings.HasPrefix(clean, absRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("mail: path %q escapes the data directory", filepath.Join(rel...))
	}
	return clean, nil
}

// writeFileAtomic writes b to path durably: a sibling temp file is written,
// fsynced, renamed over path, and the parent directory is fsynced so the
// rename itself survives a crash. A half-written cache file can therefore
// never be observed by the next start.
func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	syncDir(dir)
	return nil
}

func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// writeJSONFileAtomic marshals v and writes it durably with mode 0600.
func writeJSONFileAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o600)
}

// readJSONFileStrict reads and decodes path. A missing file is not an error
// (v is left alone); a corrupt file is reported so the caller can quarantine
// it instead of silently continuing with an empty store and overwriting it.
func readJSONFileStrict(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("mail: %s is corrupt: %w", path, err)
	}
	return nil
}

// quarantine renames a corrupt cache file out of the way so the next start
// is clean and the bad bytes are still available for a bug report.
func quarantine(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = os.Rename(path, path+".corrupt")
}
