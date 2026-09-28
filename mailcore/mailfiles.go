package mailcore

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
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

// mailFlags is what a store records about a message's state, when it
// records anything.
type mailFlags struct {
	known    bool // the store says; else the importer decides
	read     bool
	starred  bool
	answered bool
	deleted  bool // marked for deletion and not yet purged: not imported
}

// each calls fn with every message in the store, in order, and what the
// store says about its state. mbox and .eml carry theirs in the message's
// own headers (headerFlags), read by the importer.
func (s LocalMailStore) each(fn func(raw []byte, fl mailFlags)) {
	switch s.Kind {
	case StoreMbox:
		f, err := os.Open(s.Path)
		if err != nil {
			return
		}
		defer f.Close()
		eachMboxRaw(f, func(raw []byte) { fn(raw, mailFlags{}) })
	case StoreMaildir:
		eachMaildirRaw(s.Path, fn)
	case StoreMH:
		eachMHRaw(s.Path, fn)
	case StoreEML:
		eachEMLRaw(s.Path, func(raw []byte) { fn(raw, mailFlags{}) })
	case StoreEMLX:
		eachEMLXRaw(s.Path, fn)
	}
}

// headerFlags reads the state a mail client wrote into a message's headers:
// Thunderbird's X-Mozilla-Status (0x1 read, 0x4 starred, 0x8 expunged), or
// the mbox convention mutt and Dovecot use — Status: R read (O alone is
// old but unread), X-Status: F flagged, D deleted.
func headerFlags(raw []byte) mailFlags {
	end := bytes.Index(raw, []byte("\n\n"))
	if e := bytes.Index(raw, []byte("\r\n\r\n")); e >= 0 && (end < 0 || e < end) {
		end = e
	}
	if end < 0 {
		end = len(raw)
	}
	var fl mailFlags
	var status, xstatus string
	moz := int64(-1)
	for _, line := range strings.Split(string(raw[:end]), "\n") {
		name, val, ok := strings.Cut(strings.TrimRight(line, "\r"), ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "x-mozilla-status":
			if n, err := strconv.ParseInt(val, 16, 64); err == nil {
				moz = n
			}
		case "status":
			status = val
		case "x-status":
			xstatus = val
		}
	}
	switch {
	case moz >= 0:
		fl = mailFlags{known: true, read: moz&0x1 != 0, answered: moz&0x2 != 0, starred: moz&0x4 != 0, deleted: moz&0x8 != 0}
	case status != "" || xstatus != "":
		fl = mailFlags{known: true, read: strings.Contains(status, "R"),
			answered: strings.Contains(xstatus, "A") || strings.Contains(status, "r"),
			starred:  strings.Contains(xstatus, "F"), deleted: strings.Contains(xstatus, "D")}
	}
	return fl
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

// eachMaildirRaw reads a maildir's cur/ and new/. A message's state is in
// its file name — "…:2,FRS" (or "!2," as Evolution may write it): S seen,
// F flagged, T trashed; one in new/ has not been seen.
func eachMaildirRaw(dir string, fn func(raw []byte, fl mailFlags)) {
	for _, sub := range []string{"cur", "new"} {
		for _, name := range sortedFiles(filepath.Join(dir, sub), nil) {
			raw, err := os.ReadFile(filepath.Join(dir, sub, name))
			if err != nil {
				continue
			}
			fl := mailFlags{known: true}
			if sub == "cur" {
				info := ""
				if i := strings.LastIndex(name, ":2,"); i >= 0 {
					info = name[i+3:]
				} else if i := strings.LastIndex(name, "!2,"); i >= 0 {
					info = name[i+3:]
				}
				fl.read = strings.Contains(info, "S")
				fl.starred = strings.Contains(info, "F")
				fl.answered = strings.Contains(info, "R")
				fl.deleted = strings.Contains(info, "T")
			}
			fn(raw, fl)
		}
	}
}

// eachMHRaw reads an MH folder: its all-digit file names, in numeric order.
// State comes from Claws Mail's .claws_mark when the folder has one, else
// from the MH sequences (.mh_sequences: unseen, flagged).
func eachMHRaw(dir string, fn func(raw []byte, fl mailFlags)) {
	names := sortedFiles(dir, isAllDigits)
	sort.Slice(names, func(i, j int) bool {
		a, _ := strconv.Atoi(names[i])
		b, _ := strconv.Atoi(names[j])
		return a < b
	})
	flagsOf := mhFlags(dir)
	for _, name := range names {
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			n, _ := strconv.Atoi(name)
			fn(raw, flagsOf(n))
		}
	}
}

// mhFlags returns the state of message n of an MH folder, from the
// folder's .claws_mark (or .sylpheed_mark), else its .mh_sequences.
func mhFlags(dir string) func(n int) mailFlags {
	for _, mark := range []string{".claws_mark", ".sylpheed_mark"} {
		if b, err := os.ReadFile(filepath.Join(dir, mark)); err == nil {
			if marks, ok := parseClawsMark(b); ok {
				return func(n int) mailFlags {
					f, ok := marks[uint32(n)]
					if !ok {
						f = clawsNew | clawsUnread // no record: new
					}
					return mailFlags{known: true, read: f&(clawsNew|clawsUnread) == 0,
						starred: f&clawsMarked != 0, answered: f&clawsReplied != 0, deleted: f&clawsDeleted != 0}
				}
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, ".mh_sequences")); err == nil {
		seqs := parseMHSequences(string(b))
		unseen, flagged, replied := seqs["unseen"], seqs["flagged"], seqs["replied"]
		return func(n int) mailFlags {
			return mailFlags{known: true, read: !unseen[n], starred: flagged[n], answered: replied[n]}
		}
	}
	return func(int) mailFlags { return mailFlags{} }
}

// Claws Mail's permanent message flags (procmsg.h).
const (
	clawsNew     = 1 << 0
	clawsUnread  = 1 << 1
	clawsMarked  = 1 << 2
	clawsDeleted = 1 << 3
	clawsReplied = 1 << 4
)

// parseClawsMark reads a .claws_mark: a 4-byte version (2), then 8-byte
// records of message number and flags, little-endian (a byte-swapped
// version marks a file written big-endian).
func parseClawsMark(b []byte) (map[uint32]uint32, bool) {
	if len(b) < 4 {
		return nil, false
	}
	order := binary.ByteOrder(binary.LittleEndian)
	switch {
	case binary.LittleEndian.Uint32(b) == 2:
	case binary.BigEndian.Uint32(b) == 2:
		order = binary.BigEndian
	default:
		return nil, false
	}
	out := map[uint32]uint32{}
	for i := 4; i+8 <= len(b); i += 8 {
		out[order.Uint32(b[i:])] = order.Uint32(b[i+4:])
	}
	return out, true
}

// parseMHSequences reads .mh_sequences lines like "unseen: 1-3 7".
func parseMHSequences(text string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	for _, line := range strings.Split(text, "\n") {
		name, list, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		set := map[int]bool{}
		for _, part := range strings.Fields(list) {
			lo, hi, isRange := strings.Cut(part, "-")
			a, err1 := strconv.Atoi(lo)
			if err1 != nil {
				continue
			}
			b := a
			if isRange {
				var err2 error
				if b, err2 = strconv.Atoi(hi); err2 != nil || b < a || b-a > 1_000_000 {
					continue
				}
			}
			for n := a; n <= b; n++ {
				set[n] = true
			}
		}
		out[strings.ToLower(strings.TrimSpace(name))] = set
	}
	return out
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
func eachEMLXRaw(p string, fn func(raw []byte, fl mailFlags)) {
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		if b, err := os.ReadFile(p); err == nil {
			if raw, fl := decodeEMLX(b); raw != nil {
				fn(raw, fl)
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
			if raw, fl := decodeEMLX(b); raw != nil {
				fn(raw, fl)
			}
		}
	}
}

// decodeEMLX takes the message out of an Apple Mail .emlx — a line with the
// message's length in bytes, the message, then an XML property list — and
// its state from the list's "flags" (bit 0 read, 1 deleted, 4 flagged).
func decodeEMLX(b []byte) ([]byte, mailFlags) {
	nl := bytes.IndexByte(b, '\n')
	if nl < 0 {
		return nil, mailFlags{}
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b[:nl])))
	if err != nil || n <= 0 {
		return nil, mailFlags{}
	}
	body := b[nl+1:]
	var fl mailFlags
	if n < len(body) {
		if m := emlxFlagsRe.FindSubmatch(body[n:]); m != nil {
			if v, err := strconv.ParseInt(string(m[1]), 10, 64); err == nil {
				fl = mailFlags{known: true, read: v&1 != 0, deleted: v&2 != 0, answered: v&4 != 0, starred: v&16 != 0}
			}
		}
		body = body[:n]
	}
	return body, fl
}

var emlxFlagsRe = regexp.MustCompile(`<key>flags</key>\s*<(?:integer|real)>\s*(-?\d+)`)

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
