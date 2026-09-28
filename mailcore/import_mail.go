package mailcore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Imported local mail lives under one account, "On This Computer", that has
// no server and is never synced. It holds mail that exists only on disk —
// Thunderbird Local Folders, a KMail maildir — which an IMAP re-sync cannot
// bring back.
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

// LocalMailStore is one on-disk mail folder found in another client.
type LocalMailStore struct {
	Source string // "Thunderbird", "KMail"
	Name   string // folder name, e.g. "Inbox" or "Archives/2023"
	Path   string // mbox file or maildir directory
	Kind   string // "mbox" | "maildir"
}

// read returns the folder's messages, keyed to id/accountID.
func (s LocalMailStore) read(folder FolderID, accountID string) []Message {
	if s.Kind == "maildir" {
		return readMaildirMessages(s.Path, folder, accountID)
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return nil
	}
	defer f.Close()
	return readMboxMessages(f, folder, accountID)
}

// DiscoverLocalMail finds the local mail folders of the clients installed for
// this user.
func DiscoverLocalMail() []LocalMailStore {
	var out []LocalMailStore
	out = append(out, thunderbirdLocalMail()...)
	out = append(out, kmailLocalMail()...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func thunderbirdLocalMail() []LocalMailStore {
	var out []LocalMailStore
	seen := map[string]bool{}
	for _, prefs := range ThunderbirdProfiles() {
		root := filepath.Join(filepath.Dir(prefs), "Mail", "Local Folders")
		for _, st := range scanMboxTree("Thunderbird", root, "") {
			if !seen[st.Path] {
				seen[st.Path] = true
				out = append(out, st)
			}
		}
	}
	return out
}

// scanMboxTree walks a Thunderbird Local Folders tree: mbox files (no
// extension; .msf/.dat are indexes), .sbd subdirectories for nesting, and a
// maildir where a newer Thunderbird used one.
func scanMboxTree(source, dir, prefix string) []LocalMailStore {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []LocalMailStore
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)
		if e.IsDir() {
			switch {
			case strings.HasSuffix(name, ".sbd"):
				base := strings.TrimSuffix(name, ".sbd")
				out = append(out, scanMboxTree(source, full, prefix+base+"/")...)
			case isMaildir(full):
				out = append(out, LocalMailStore{Source: source, Name: prefix + name, Path: full, Kind: "maildir"})
			}
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		// Thunderbird keeps indexes (.msf), its filter log (filterlog.html),
		// filter rules and POP state beside the mailboxes. Only a file that
		// actually starts like an mbox is one.
		if looksLikeMbox(full) {
			out = append(out, LocalMailStore{Source: source, Name: prefix + name, Path: full, Kind: "mbox"})
		}
	}
	return out
}

func kmailLocalMail() []LocalMailStore {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	roots := []string{
		filepath.Join(home, ".local", "share", "local-mail"),
		filepath.Join(home, ".local", "share", "kmail2", "mail"),
		filepath.Join(home, ".kde", "share", "apps", "kmail", "mail"),
	}
	var out []LocalMailStore
	seen := map[string]bool{}
	for _, root := range roots {
		for _, st := range scanMaildirTree("KMail", root, "") {
			if !seen[st.Path] {
				seen[st.Path] = true
				out = append(out, st)
			}
		}
	}
	return out
}

// scanMaildirTree walks a KMail-style maildir tree, where a folder is a
// maildir and its children live in a sibling ".<folder>.directory".
func scanMaildirTree(source, dir, prefix string) []LocalMailStore {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []LocalMailStore
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		full := filepath.Join(dir, name)
		if strings.HasSuffix(name, ".directory") {
			base := strings.TrimSuffix(strings.TrimPrefix(name, "."), ".directory")
			out = append(out, scanMaildirTree(source, full, prefix+base+"/")...)
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		if isMaildir(full) {
			out = append(out, LocalMailStore{Source: source, Name: prefix + name, Path: full, Kind: "maildir"})
		}
	}
	return out
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
	n, _ := f.Read(buf)
	return n == 5 && string(buf) == "From "
}

// ImportResult reports what an import brought in.
type ImportResult struct {
	Folders  int `json:"folders"`
	Messages int `json:"messages"`
}

// ImportLocalMail reads every discovered local mail folder into the "On This
// Computer" account, one folder each, skipping messages already imported
// (by RFC Message-ID). It is idempotent: importing twice adds nothing new.
func (s *LocalStore) ImportLocalMail() (ImportResult, error) {
	stores := DiscoverLocalMail()
	if len(stores) == 0 {
		return ImportResult{}, fmt.Errorf("no local mail found in Thunderbird or KMail")
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
		msgs := st.read(fid, LocalAccountID)
		added := 0
		s.mu.Lock()
		if _, ok := s.folderLocked(fid); !ok {
			s.Folders = append(s.Folders, Folder{
				ID: fid, AccountID: LocalAccountID, Name: folderName, Kind: FolderCustom,
			})
		}
		for _, m := range msgs {
			rfc := strings.TrimSpace(m.RFCMessageID)
			if rfc != "" && have[rfc] {
				continue
			}
			if rfc != "" {
				have[rfc] = true
			}
			s.nextID++
			m.ID = MessageID(fmt.Sprintf("%s-m-%04d", LocalAccountID, s.nextID))
			m.Folder = fid
			m.AccountID = LocalAccountID
			m.Read = true // imported mail is old mail
			if m.Date.IsZero() {
				m.Date = time.Now()
			}
			m.ThreadID = ThreadIDOf(m)
			s.Messages = append(s.Messages, m)
			if s.feat != nil && s.feat.index != nil {
				s.feat.index.add(m)
			}
			added++
		}
		s.mu.Unlock()
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
