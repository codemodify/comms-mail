package mailcore

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Local mail — a Thunderbird Local Folders mbox, a KMail or Evolution
// maildir, a Claws Mail MH folder, an Apple Mail mailbox, a directory of
// .eml files — lives only on disk: no server holds it, so an IMAP account
// that re-syncs does not bring it back. These readers walk such a store and
// hand over each message's raw RFC 822 bytes.

// The kinds of on-disk store the importer reads.
const (
	StoreMbox    = "mbox"    // one file, messages separated by "From " lines
	StoreMaildir = "maildir" // cur/ + new/, one file per message
	StoreMH      = "mh"      // one numbered file per message (Claws Mail, nmh)
	StoreEML     = "eml"     // a .eml file, or a directory of them
	StoreEMLX    = "emlx"    // Apple Mail: a .emlx file, or a .mbox directory of them
)

// LocalMailStore is one on-disk mail folder found by a scan.
type LocalMailStore struct {
	Source string `json:"source"` // "Thunderbird", "Evolution", "Folder · Mail", …
	Name   string `json:"name"`   // folder name, e.g. "Inbox" or "Archives/2023"
	Path   string `json:"path"`   // the file or directory holding it
	Kind   string `json:"kind"`   // one of the Store* kinds
}

// validate refuses a store the importer cannot or should not read.
func (s LocalMailStore) validate() error {
	switch s.Kind {
	case StoreMbox, StoreMaildir, StoreMH, StoreEML, StoreEMLX:
	default:
		return fmt.Errorf("mail: unknown mail store kind %q", s.Kind)
	}
	if !filepath.IsAbs(s.Path) {
		return fmt.Errorf("mail: mail store path %q is not absolute", s.Path)
	}
	if _, err := os.Stat(s.Path); err != nil {
		return fmt.Errorf("mail: %s: %w", s.Path, err)
	}
	return nil
}

// each calls fn with every message in the store, in order.
func (s LocalMailStore) each(fn func(raw []byte)) {
	switch s.Kind {
	case StoreMbox:
		f, err := os.Open(s.Path)
		if err != nil {
			return
		}
		defer f.Close()
		eachMboxRaw(f, fn)
	case StoreMaildir:
		eachMaildirRaw(s.Path, fn)
	case StoreMH:
		eachMHRaw(s.Path, fn)
	case StoreEML:
		eachEMLRaw(s.Path, fn)
	case StoreEMLX:
		eachEMLXRaw(s.Path, fn)
	}
}

// eachMboxRaw splits an mbox stream. A line beginning "From " at the start
// of the file or after a blank line separates messages; mboxrd's ">From "
// escaping in bodies is undone.
func eachMboxRaw(r io.Reader, fn func(raw []byte)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var cur bytes.Buffer
	started, prevBlank := false, true
	flush := func() {
		body := bytes.TrimRight(cur.Bytes(), "\n")
		if len(bytes.TrimSpace(body)) > 0 {
			fn(append(append([]byte(nil), body...), '\n'))
		}
		cur.Reset()
	}
	for sc.Scan() {
		line := sc.Bytes()
		if prevBlank && bytes.HasPrefix(line, []byte("From ")) {
			if started {
				flush()
			}
			started, prevBlank = true, false
			continue
		}
		started = true
		if len(line) > 0 && line[0] == '>' && bytes.HasPrefix(bytes.TrimLeft(line, ">"), []byte("From ")) {
			line = line[1:]
		}
		cur.Write(line)
		cur.WriteByte('\n')
		prevBlank = len(bytes.TrimSpace(line)) == 0
	}
	flush()
}

// eachMaildirRaw reads a maildir's cur/ and new/.
func eachMaildirRaw(dir string, fn func(raw []byte)) {
	for _, sub := range []string{"cur", "new"} {
		for _, name := range sortedFiles(filepath.Join(dir, sub), nil) {
			if raw, err := os.ReadFile(filepath.Join(dir, sub, name)); err == nil {
				fn(raw)
			}
		}
	}
}

// eachMHRaw reads an MH folder: its all-digit file names, in numeric order.
func eachMHRaw(dir string, fn func(raw []byte)) {
	names := sortedFiles(dir, isAllDigits)
	sort.Slice(names, func(i, j int) bool {
		a, _ := strconv.Atoi(names[i])
		b, _ := strconv.Atoi(names[j])
		return a < b
	})
	for _, name := range names {
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			fn(raw)
		}
	}
}

// eachEMLRaw reads one .eml file, or every .eml in a directory.
func eachEMLRaw(p string, fn func(raw []byte)) {
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		if raw, err := os.ReadFile(p); err == nil {
			fn(raw)
		}
		return
	}
	for _, name := range sortedFiles(p, func(n string) bool { return hasSuffixFold(n, ".eml") }) {
		if raw, err := os.ReadFile(filepath.Join(p, name)); err == nil {
			fn(raw)
		}
	}
}

// eachEMLXRaw reads one .emlx file, or every .emlx beneath an Apple Mail
// .mbox directory — skipping mailboxes nested inside it, which are folders
// of their own.
func eachEMLXRaw(p string, fn func(raw []byte)) {
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		if b, err := os.ReadFile(p); err == nil {
			if raw := decodeEMLX(b); raw != nil {
				fn(raw)
			}
		}
		return
	}
	var files []string
	_ = filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && q != p && hasSuffixFold(d.Name(), ".mbox") {
			return filepath.SkipDir
		}
		if !d.IsDir() && hasSuffixFold(d.Name(), ".emlx") {
			files = append(files, q)
		}
		return nil
	})
	sort.Strings(files)
	for _, q := range files {
		if b, err := os.ReadFile(q); err == nil {
			if raw := decodeEMLX(b); raw != nil {
				fn(raw)
			}
		}
	}
}

// decodeEMLX takes the message out of an Apple Mail .emlx: a line with the
// message's length in bytes, the message, then an XML property list.
func decodeEMLX(b []byte) []byte {
	nl := bytes.IndexByte(b, '\n')
	if nl < 0 {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b[:nl])))
	if err != nil || n <= 0 {
		return nil
	}
	body := b[nl+1:]
	if n < len(body) {
		body = body[:n]
	}
	return body
}

// sortedFiles lists dir's regular files that keep says yes to, sorted.
func sortedFiles(dir string, keep func(string) bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if keep == nil || keep(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
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

// isMHDir reports whether dir holds MH messages: numbered files that start
// with a mail header.
func isMHDir(dir string) bool {
	for _, name := range sortedFiles(dir, isAllDigits) {
		return looksLikeMessage(filepath.Join(dir, name))
	}
	return false
}

// hasEMLFiles reports whether dir directly holds .eml files.
func hasEMLFiles(dir string) bool {
	return len(sortedFiles(dir, func(n string) bool { return hasSuffixFold(n, ".eml") })) > 0
}

// containsEMLX reports whether an Apple Mail .mbox directory holds any .emlx
// (they sit a few levels down, under Messages/).
func containsEMLX(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(q string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if d.IsDir() && q != dir && hasSuffixFold(d.Name(), ".mbox") {
			return filepath.SkipDir
		}
		if !d.IsDir() && hasSuffixFold(d.Name(), ".emlx") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// looksLikeMbox reports whether path is a non-empty mbox: its first line is
// an mbox "From " separator.
func looksLikeMbox(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 5)
	n, _ := io.ReadFull(f, buf)
	return n == 5 && string(buf) == "From "
}

// looksLikeMessage reports whether path starts like an RFC 822 message: a
// header line "Name: value".
func looksLikeMessage(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	name, _, ok := strings.Cut(line, ":")
	if !ok || name == "" || strings.ContainsAny(name, " \t") {
		return false
	}
	return true
}

// trimMailExt strips the folder-container suffixes clients add to a name:
// Thunderbird's .sbd and Apple Mail's .mbox.
func trimMailExt(name string) string {
	for _, ext := range []string{".sbd", ".mbox"} {
		if hasSuffixFold(name, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

func joinName(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "/" + child
}

// scanAnyTree finds every mail store at or under p, whatever the client
// that wrote it: an mbox file, a maildir (with Maildir++ ".Sub" folders and
// KMail's ".sub.directory" children), an MH folder, a directory of .eml
// files, an Apple Mail .mbox. name is this node's folder name ("" at the
// root, where rootLabel names a store found there); depth bounds the walk.
func scanAnyTree(source, p, name, rootLabel string, depth int) []LocalMailStore {
	fi, err := os.Stat(p)
	if err != nil {
		return nil
	}
	label := name
	if label == "" {
		label = rootLabel
	}
	store := func(kind string) LocalMailStore {
		return LocalMailStore{Source: source, Name: label, Path: p, Kind: kind}
	}
	if !fi.IsDir() {
		switch {
		case hasSuffixFold(p, ".emlx"):
			return []LocalMailStore{store(StoreEMLX)}
		case hasSuffixFold(p, ".eml"):
			return []LocalMailStore{store(StoreEML)}
		case looksLikeMbox(p):
			return []LocalMailStore{store(StoreMbox)}
		}
		return nil
	}

	var out []LocalMailStore
	appleMbox := hasSuffixFold(p, ".mbox") && containsEMLX(p)
	maildir := isMaildir(p)
	switch {
	case appleMbox:
		out = append(out, store(StoreEMLX))
	case maildir:
		out = append(out, store(StoreMaildir))
	case isMHDir(p):
		out = append(out, store(StoreMH))
	case hasEMLFiles(p):
		out = append(out, store(StoreEML))
	}
	if depth <= 0 {
		return out
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return out
	}
	for _, e := range entries {
		n := e.Name()
		child := filepath.Join(p, n)
		if maildir && (n == "cur" || n == "new" || n == "tmp") {
			continue
		}
		if strings.HasPrefix(n, ".") {
			switch {
			case maildir && e.IsDir() && isMaildir(child):
				// Maildir++: ".Sent", ".Archive.2023" beside the root inbox.
				sub := strings.ReplaceAll(strings.TrimPrefix(n, "."), ".", "/")
				parent := path.Dir(name)
				if parent == "." {
					parent = ""
				}
				out = append(out, LocalMailStore{Source: source, Name: joinName(parent, sub), Path: child, Kind: StoreMaildir})
			case e.IsDir() && strings.HasSuffix(n, ".directory"):
				// KMail: ".inbox.directory" holds inbox's children.
				base := strings.TrimSuffix(strings.TrimPrefix(n, "."), ".directory")
				out = append(out, scanAnyTree(source, child, joinName(name, base), rootLabel, depth-1)...)
			}
			continue
		}
		if e.IsDir() {
			if appleMbox && !hasSuffixFold(n, ".mbox") {
				continue // Messages/, Data/: this mailbox's own files
			}
			out = append(out, scanAnyTree(source, child, joinName(name, trimMailExt(n)), rootLabel, depth-1)...)
			continue
		}
		// A lone mailbox file beside other things (Thunderbird's Local
		// Folders, mutt's ~/Mail). .eml files are already covered above.
		if !maildir && !appleMbox && !hasSuffixFold(n, ".eml") && looksLikeMbox(child) {
			out = append(out, LocalMailStore{Source: source, Name: joinName(name, n), Path: child, Kind: StoreMbox})
		}
	}
	return out
}
