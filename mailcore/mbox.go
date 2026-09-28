package mailcore

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Local mail — a Thunderbird Local Folders mbox, a KMail or Evolution
// maildir — lives only on disk: no server holds it, so an IMAP account that
// re-syncs does not bring it back. These readers turn such a store into
// messages the cache can hold.

// readMboxMessages parses an mbox stream (Thunderbird's Local Folders, and
// the classic Unix mailbox). Messages are separated by a line beginning
// "From "; mboxrd's ">From " escaping in bodies is undone.
func readMboxMessages(r io.Reader, folder FolderID, accountID string) []Message {
	var out []Message
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var cur bytes.Buffer
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		if m, err := ParseRFC822(cur.Bytes(), folder, accountID); err == nil && messageHasContent(m) {
			out = append(out, m)
		}
		cur.Reset()
	}
	first := true
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "From ") {
			// A "From " at the very start of a message is its separator.
			if !first {
				flush()
			}
			first = false
			continue
		}
		first = false
		// mboxrd un-escaping: ">From ", ">>From " → drop one '>'.
		if strings.HasPrefix(line, ">") {
			if trimmed := strings.TrimLeft(line, ">"); strings.HasPrefix(trimmed, "From ") {
				line = line[1:]
			}
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	flush()
	return out
}

// readMaildirMessages reads a maildir (KMail, Evolution, Dovecot): one RFC822
// file per message in its cur/ and new/ subdirectories.
func readMaildirMessages(dir string, folder FolderID, accountID string) []Message {
	var out []Message
	for _, sub := range []string{"cur", "new"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			raw, err := os.ReadFile(filepath.Join(dir, sub, name))
			if err != nil {
				continue
			}
			if m, err := ParseRFC822(raw, folder, accountID); err == nil && messageHasContent(m) {
				out = append(out, m)
			}
		}
	}
	return out
}

// isMaildir reports whether dir is a maildir (has a cur/ or new/).
func isMaildir(dir string) bool {
	for _, sub := range []string{"cur", "new"} {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// messageHasContent reports whether a parsed message is worth keeping: it has
// at least a subject, a sender, or a body, so blank separators do not become
// empty messages.
func messageHasContent(m Message) bool {
	return strings.TrimSpace(m.Subject) != "" ||
		strings.TrimSpace(m.From) != "" ||
		strings.TrimSpace(m.Body) != "" ||
		strings.TrimSpace(m.HTML) != ""
}
