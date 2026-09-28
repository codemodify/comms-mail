package mailcore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Imported local mail lives under one account, "On This Computer", that has
// no server and is never synced. It holds mail that exists only on disk,
// which an IMAP re-sync cannot bring back.
const (
	LocalAccountID   = "on-this-computer"
	LocalAccountName = "On This Computer"
)

// IsLocal reports whether an account has no server: the imported-mail
// account, or one left with no incoming host.
func (a AccountConfig) IsLocal() bool {
	return strings.EqualFold(a.Protocol, "local") ||
		a.ID == LocalAccountID ||
		(strings.TrimSpace(a.IMAP.Host) == "" && strings.TrimSpace(a.POP.Host) == "")
}

// ImportSource is what a scan found for one client (or one folder the user
// pointed at): the accounts it has set up, and the mail it keeps on disk.
// The import window offers a checkbox for each half.
type ImportSource struct {
	Source   string            `json:"source"`
	Accounts []ImportedAccount `json:"accounts,omitempty"`
	Mail     []LocalMailStore  `json:"mail,omitempty"`
	// Contacts are the people in the client's address book.
	Contacts []Contact `json:"contacts,omitempty"`
	// Filters are its message filters, per incoming server.
	Filters []FilterSet `json:"filters,omitempty"`
	// Note says what the scan could not do for this client — a layout it
	// only reads best-effort, accounts it has no way to read.
	Note string `json:"note,omitempty"`
}

func (s ImportSource) empty() bool {
	return len(s.Accounts) == 0 && len(s.Mail) == 0 && len(s.Contacts) == 0 && len(s.Filters) == 0
}

// ScanImportSources looks for every mail client it knows for this user and
// returns what each one has. A client with nothing to offer is left out.
// Outlook (.pst) is not read.
func ScanImportSources() []ImportSource {
	var out []ImportSource
	add := func(src ImportSource) {
		if !src.empty() {
			out = append(out, src)
		}
	}
	tb, _ := ImportThunderbird()
	add(ImportSource{Source: "Thunderbird", Accounts: tb, Mail: thunderbirdLocalMail(), Contacts: readThunderbirdAddressBooks(),
		Filters: readThunderbirdFilters()})
	km, _ := ImportKMail()
	kmFilters, kmNote := readKMailFilters(KMailConfigDir())
	add(ImportSource{Source: "KMail", Accounts: km, Mail: kmailLocalMail(), Contacts: readVCardDirs(), Filters: kmFilters,
		Note: strings.TrimSpace("KMail's settings are read best-effort; check each account before you rely on it. " + kmNote)})
	add(ImportSource{Source: "Evolution", Accounts: importEvolution(), Mail: evolutionLocalMail(), Contacts: readEvolutionAddressBooks()})
	add(ImportSource{Source: "Claws Mail", Accounts: importClaws(), Mail: clawsLocalMail(), Contacts: readClawsAddressBook()})
	add(ImportSource{Source: "Geary", Accounts: importGeary(),
		Note: "Geary keeps only a cache of IMAP mail, which re-syncs from the server — there is no local mail to import."})
	for _, m := range importMuttSources() {
		add(m)
	}
	add(importAppleMail())
	return out
}

// ScanPath finds the mail in a file or folder the user chose: an mbox, a
// maildir, an MH folder, .eml / .emlx files, or a tree holding any of them.
func ScanPath(p string) (ImportSource, error) {
	p = filepath.Clean(expandHome(strings.TrimSpace(p)))
	if !filepath.IsAbs(p) {
		return ImportSource{}, fmt.Errorf("choose a file or folder by its full path")
	}
	if _, err := os.Stat(p); err != nil {
		return ImportSource{}, err
	}
	base := filepath.Base(p)
	src := ImportSource{Source: "Folder · " + base}
	src.Mail = scanAnyTree(src.Source, p, "", trimMailExt(base), 8)
	if len(src.Mail) == 0 {
		src.Note = "No mail found in " + p + " — it should hold an mbox, a maildir, an MH folder, or .eml / .emlx files."
	}
	return src, nil
}

func thunderbirdLocalMail() []LocalMailStore {
	var out []LocalMailStore
	seen := map[string]bool{}
	for _, prefs := range ThunderbirdProfiles() {
		root := filepath.Join(filepath.Dir(prefs), "Mail", "Local Folders")
		for _, st := range scanAnyTree("Thunderbird", root, "", "Local Folders", 8) {
			if !seen[st.Path] {
				seen[st.Path] = true
				out = append(out, st)
			}
		}
	}
	return out
}

func kmailLocalMail() []LocalMailStore {
	home := homeDir()
	if home == "" {
		return nil
	}
	var out []LocalMailStore
	seen := map[string]bool{}
	for _, root := range []string{
		filepath.Join(xdgDataHome(), "local-mail"),
		filepath.Join(xdgDataHome(), "kmail2", "mail"),
		filepath.Join(home, ".kde", "share", "apps", "kmail", "mail"),
	} {
		for _, st := range scanAnyTree("KMail", root, "", "Local Folders", 8) {
			if !seen[st.Path] {
				seen[st.Path] = true
				out = append(out, st)
			}
		}
	}
	return out
}

// ImportResult reports what an import brought in.
type ImportResult struct {
	Folders  int `json:"folders"`
	Messages int `json:"messages"`
}

// ImportLocalMail reads the given on-disk stores into the "On This Computer"
// account, a folder each, skipping messages it already has (by Message-ID,
// or a digest of the message where it has none) — so importing twice adds
// nothing. Each message's original bytes are kept, as for downloaded mail,
// so its source and attachments open like any other.
func (s *LocalStore) ImportLocalMail(stores []LocalMailStore) (ImportResult, error) {
	if len(stores) == 0 {
		return ImportResult{}, fmt.Errorf("nothing selected to import")
	}
	for _, st := range stores {
		if err := st.validate(); err != nil {
			return ImportResult{}, err
		}
	}
	s.ensureLocalAccount()

	s.mu.Lock()
	have := map[string]bool{}
	for _, m := range s.Messages {
		if m.AccountID == LocalAccountID && m.RFCMessageID != "" {
			have[strings.TrimSpace(m.RFCMessageID)] = true
		}
	}
	s.mu.Unlock()

	var res ImportResult
	for _, st := range stores {
		folderName := st.Source + " · " + st.Name
		fid := FolderID(LocalAccountID + "/" + safeID(folderName))
		s.mu.Lock()
		if _, ok := s.folderLocked(fid); !ok {
			s.Folders = append(s.Folders, Folder{ID: fid, AccountID: LocalAccountID, Name: folderName, Kind: FolderCustom})
		}
		s.mu.Unlock()

		added := 0
		st.each(func(raw []byte, fl mailFlags) {
			if !fl.known {
				fl = headerFlags(raw)
			}
			if fl.deleted {
				return // the client had it marked for deletion
			}
			m, err := ParseRFC822(raw, fid, LocalAccountID)
			if err != nil || !messageHasContent(m) {
				return
			}
			key := strings.TrimSpace(m.RFCMessageID)
			if key == "" {
				// No Message-ID: a digest of the bytes keeps a re-import
				// from duplicating it.
				sum := sha256.Sum256(raw)
				key = "<import-" + hex.EncodeToString(sum[:16]) + "@comms-mail.local>"
				m.RFCMessageID = key
			}
			s.mu.Lock()
			if have[key] {
				s.mu.Unlock()
				return
			}
			have[key] = true
			s.nextID++
			m.ID = MessageID(fmt.Sprintf("%s-m-%04d", LocalAccountID, s.nextID))
			m.Folder, m.AccountID = fid, LocalAccountID
			// The state the client kept; a store that keeps none is old
			// mail, read.
			m.Read, m.Starred = true, false
			if fl.known {
				m.Read, m.Starred, m.Answered, m.Forwarded = fl.read, fl.starred, fl.answered, fl.forwarded
			}
			applyAutomaticTags(&m)
			if m.Date.IsZero() {
				m.Date = time.Now()
			}
			m.ThreadID = ThreadIDOf(m)
			s.Messages = append(s.Messages, m)
			s.mu.Unlock()
			s.writeRaw(m, raw)
			added++
		})
		if added > 0 {
			res.Folders++
			res.Messages += added
		}
	}
	s.mu.Lock()
	assignThreadIDs(s.Messages)
	s.feat.setContacts(buildContacts(s.Messages))
	s.saveLocked()
	s.mu.Unlock()
	s.Emit(StoreEvent{Reason: "import", AccountID: LocalAccountID, Count: res.Messages})
	return res, nil
}

// writeRaw keeps a message's original bytes in the cache. It touches only
// the filesystem, so it runs without the store lock.
func (s *LocalStore) writeRaw(m Message, raw []byte) {
	p, err := s.rawPath(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = WriteFileAtomic(p, raw, 0o600)
}

// messageHasContent reports whether a parsed message is worth keeping: it has
// at least a subject, a sender, or a body.
func messageHasContent(m Message) bool {
	return strings.TrimSpace(m.Subject) != "" ||
		strings.TrimSpace(m.From) != "" ||
		strings.TrimSpace(m.Body) != "" ||
		strings.TrimSpace(m.HTML) != ""
}

// ensureLocalAccount adds the "On This Computer" account if it is not already
// present, and persists it.
func (s *LocalStore) ensureLocalAccount() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.accounts {
		if a.ID == LocalAccountID {
			return
		}
	}
	s.accounts = append(s.accounts, Account{
		ID: LocalAccountID, Name: LocalAccountName, Address: LocalAccountName, Protocol: "local", Transport: "local",
	})
	s.cfg.Accounts = append(s.cfg.Accounts, AccountConfig{ID: LocalAccountID, Name: LocalAccountName, Address: LocalAccountName, Protocol: "local"})
	s.saveLocked()
}

// homeDir, xdgConfigHome and xdgDataHome are where other clients keep their
// settings and mail.
func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func xdgConfigHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".config")
}

func xdgDataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".local", "share")
}

// expandHome turns a leading ~ or $HOME into the home directory.
func expandHome(p string) string {
	switch {
	case p == "~":
		return homeDir()
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(homeDir(), p[2:])
	case strings.HasPrefix(p, "$HOME/"):
		return filepath.Join(homeDir(), p[6:])
	}
	return p
}
