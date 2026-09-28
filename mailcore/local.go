package mailcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LocalStore is the production backend: on-disk cache + IMAP or POP3 sync + SMTP.
type LocalStore struct {
	mu         sync.Mutex
	dir        string
	cfg        MailConfig
	accounts   []Account
	identities []Identity
	// Folders and Messages are the cached mailbox tree and its messages.
	// They are exported so the UI package's tests can seed a store
	// directly; callers outside a test should go through the Store
	// methods, which take the lock.
	Folders  []Folder
	Messages []Message
	tags     []Tag
	rules    []FilterRule
	clients  map[string]*imapClient
	// fgClients is a second session per account for what a person is
	// waiting on — a message body, an attachment — so a click never
	// queues behind a background sync on the shared session.
	fgClients map[string]*imapClient
	// downSince is when each account's server last failed at the network
	// level with no success since. Foreground fetches fail fast inside
	// unreachableFor of it instead of waiting out a connect timeout per click.
	downSince map[string]time.Time
	// rekeyed maps a message's old id to the one it has since the server
	// gave it a UID, for a window still holding the old one (a draft
	// being written when its queued APPEND went out).
	rekeyed map[MessageID]MessageID
	// flushMu lets one Outbox replay run at a time: two at once could send
	// the same message twice.
	flushMu sync.Mutex
	// focus is the folder the window shows (watched like Inbox); a change
	// closes focusChanged to wake the watchers.
	focus        FolderID
	focusChanged chan struct{}
	nextID       int
	health       error
	now          time.Time
	feat         *featureHost
	pushCancel   func()
	pushCtx      context.Context
	// vault is the encrypted file (vault.go), DataDir()/secrets/vault.json;
	// kind is the secret store in use (secrets.go), moveMu one move of
	// the secrets at a time.
	vault  *Vault
	kind   atomic.Value
	moveMu sync.Mutex

	// sqlc is mail.db (sqlstore.go); nil only when not even a fresh one
	// could be created, and the store then runs from memory alone.
	sqlc *sqlCache

	// prefetching is set while a body prefetch pass runs (prefetch.go).
	prefetching atomic.Bool

	// syncMu serialises whole-account syncs. Network I/O happens with mu
	// released (snapshot → I/O → re-lock and apply), so an unresponsive
	// server can no longer block every unrelated RPC.
	syncMu sync.Mutex

	changeMu sync.Mutex
	onChange func(StoreEvent)
}

// StoreEvent is emitted by background work (push/IDLE, periodic sync) so the
// daemon can broadcast mail.changed and the UI can refresh without polling.
type StoreEvent struct {
	Reason    string
	AccountID string
	FolderID  FolderID
	Count     int
	Title     string // a "progress" event's words
}

// SetOnChange registers the daemon's broadcast hook. fn is always called
// with no store lock held.
func (s *LocalStore) SetOnChange(fn func(StoreEvent)) {
	s.changeMu.Lock()
	s.onChange = fn
	s.changeMu.Unlock()
}

// Emit delivers ev to the store's watchers.
func (s *LocalStore) Emit(ev StoreEvent) {
	s.changeMu.Lock()
	fn := s.onChange
	s.changeMu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

type folderMeta struct {
	UIDValidity uint32 `json:"uidValidity"`
	UIDNext     uint32 `json:"uidNext"`
	HighestMod  uint64 `json:"highestModseq,omitempty"`
	Remote      string `json:"remote"`
}

// NewLocalStore opens (or creates) the disk cache for cfg.
func NewLocalStore(cfg MailConfig) (*LocalStore, error) {
	return NewLocalStoreDir(cfg, DataDir())
}

func NewLocalStoreDir(cfg MailConfig, dir string) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)
	s := &LocalStore{
		dir: dir, cfg: cfg, clients: map[string]*imapClient{}, fgClients: map[string]*imapClient{}, downSince: map[string]time.Time{},
		tags: DefaultTags(), nextID: 1, now: time.Now(), feat: newFeatureHost(),
		vault: OpenVault(filepath.Join(dir, "secrets", "vault.json")),
	}
	s.initSecretKind()
	s.loadLocked()
	for _, a := range cfg.Accounts {
		s.ensureAccount(a)
	}
	if len(s.accounts) == 0 {
		s.health = fmt.Errorf("mail: no accounts in %s", ConfigPath())
	}
	s.saveLocked()
	return s, nil
}

func (s *LocalStore) Backend() string { return "imap" }

// SuggestContacts completes a recipient from the address book built out of
// the cached messages.
func (s *LocalStore) SuggestContacts(query string, limit int) []Contact {
	return s.feat.suggest(query, limit)
}

func (s *LocalStore) Health() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.health
}

func (s *LocalStore) ensureAccount(a AccountConfig) {
	id := a.ID
	if id == "" {
		id = slug(a.Address)
	}
	found := false
	for i, x := range s.accounts {
		if x.ID == id {
			a.ID = id
			s.accounts[i] = accountFromConfig(a, "")
			found = true
			break
		}
	}
	if !found {
		a.ID = id
		s.accounts = append(s.accounts, accountFromConfig(a, ""))
	}
	if len(a.Identities) > 0 {
		for _, idn := range a.Identities {
			idn.AccountID = id
			s.identities = upsertIdentity(s.identities, idn)
		}
	} else if !hasIdentityFor(s.identities, id) {
		s.identities = append(s.identities, Identity{
			ID: id + "-default", AccountID: id, Name: a.Name, Address: a.Address, Default: true,
		})
	}
}

// slug is the filesystem-safe form of an id. It is deliberately strict:
// account and folder ids derived from untrusted input become path segments.
func slug(s string) string {
	return safeID(s)
}
func hasIdentityFor(ids []Identity, accountID string) bool {
	for _, id := range ids {
		if id.AccountID == accountID {
			return true
		}
	}
	return false
}

func upsertIdentity(list []Identity, id Identity) []Identity {
	if id.ID == "" {
		id.ID = slug(id.Address)
	}
	for i, x := range list {
		if x.ID == id.ID {
			list[i] = id
			return list
		}
	}
	return append(list, id)
}

func (s *LocalStore) PutAccount(in AccountConfig) (Account, error) {
	a, err := SanitizeAccountConfig(in)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, _ := LoadConfig()
	// The saved passwords are in the file for the plain store (and before
	// a choice), else in the store in use.
	existing := file.Accounts
	kind := s.secretKind()
	if !keepsPlainPasswords(kind) {
		if err := s.storeFor(kind).Ready(); err != nil {
			return Account{}, err
		}
		existing = nil
		for _, x := range s.cfg.Accounts {
			existing = append(existing, s.withSecrets(accountKeyID(x), x))
		}
	}
	a = KeepExistingSecrets(a, existing)
	if left := LeftBehind(a, existing); len(left) > 0 {
		return Account{}, fmt.Errorf("the server changed, so its saved password was not sent there — enter the password for %s", strings.Join(left, " and "))
	}
	if !keepsPlainPasswords(kind) {
		id := accountKeyID(a)
		if err := s.storeFor(kind).Update(map[string]string{
			passSecret(id, "imap"): a.IMAP.Pass,
			passSecret(id, "pop"):  a.POP.Pass,
			passSecret(id, "smtp"): a.SMTP.Pass,
		}); err != nil {
			return Account{}, err
		}
		a.IMAP.Pass, a.POP.Pass, a.SMTP.Pass = "", "", ""
	}
	file.Accounts = upsertAccountConfig(file.Accounts, a)
	if err := s.saveConfig(file); err != nil {
		return Account{}, err
	}
	s.cfg.Accounts = upsertAccountConfig(s.cfg.Accounts, a)
	s.ensureAccount(a)
	s.health = nil
	s.saveLocked()
	for _, x := range s.accounts {
		if x.ID == a.ID {
			return x, nil
		}
	}
	return accountFromConfig(a, ""), nil
}

func (s *LocalStore) DeleteAccount(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("mail: account id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	addr := ""
	found := false
	for _, a := range s.accounts {
		if a.ID == id {
			found, addr = true, a.Address
			break
		}
	}
	if !found {
		for _, a := range s.cfg.Accounts {
			aid := a.ID
			if aid == "" {
				aid = slug(a.Address)
			}
			if aid == id || slug(a.Address) == id {
				found, id, addr = true, aid, a.Address
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("mail: no account %s", id)
	}
	for _, pool := range []map[string]*imapClient{s.clients, s.fgClients} {
		if c := pool[id]; c != nil {
			c.close()
			delete(pool, id)
		}
	}
	file, _ := LoadConfig()
	file.Accounts = dropAccountConfig(file.Accounts, id)
	if err := s.saveConfig(file); err != nil {
		return err
	}
	s.cfg.Accounts = dropAccountConfig(s.cfg.Accounts, id)
	s.dropAccountLocked(id)
	_ = DefaultTokenStore().Delete(id)
	if addr != "" {
		_ = DefaultTokenStore().Delete(addr)
	}
	if kind := s.secretKind(); !keepsPlainPasswords(kind) {
		_ = s.storeFor(kind).Update(nil, passSecret(id, "imap"), passSecret(id, "pop"), passSecret(id, "smtp"))
	}
	if len(s.accounts) == 0 {
		s.health = fmt.Errorf("mail: no accounts in %s", ConfigPath())
	}
	s.saveLocked()
	return nil
}

func (s *LocalStore) dropAccountLocked(id string) {
	accts := s.accounts[:0]
	for _, a := range s.accounts {
		if a.ID != id {
			accts = append(accts, a)
		}
	}
	s.accounts = accts
	idents := s.identities[:0]
	for _, idn := range s.identities {
		if idn.AccountID != id {
			idents = append(idents, idn)
		}
	}
	s.identities = idents
	var drop []FolderID
	folders := s.Folders[:0]
	for _, f := range s.Folders {
		if f.AccountID == id {
			drop = append(drop, f.ID)
			continue
		}
		folders = append(folders, f)
	}
	s.Folders = folders
	msgs := s.Messages[:0]
	for _, m := range s.Messages {
		if m.AccountID == id {
			continue
		}
		msgs = append(msgs, m)
	}
	s.Messages = msgs
	if s.feat != nil {
		ops := s.feat.outbox[:0]
		for _, op := range s.feat.outbox {
			if op.AccountID == id {
				continue
			}
			ops = append(ops, op)
		}
		s.feat.outbox = ops
	}
	if p, err := underRoot(s.dir, "raw", safeID(id)); err == nil {
		_ = os.RemoveAll(p)
	}
	for _, fid := range drop {
		s.dropFolderMetaLocked(fid)
	}
}

func (s *LocalStore) Accounts() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Account, len(s.accounts))
	copy(out, s.accounts)
	return out
}

func (s *LocalStore) ListFolders(accountID string) []Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Folder
	for _, f := range s.Folders {
		if f.AccountID == accountID && !f.Virtual {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		oi, oj := folderRank(out[i].Kind), folderRank(out[j].Kind)
		if oi != oj {
			return oi < oj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *LocalStore) GetFolder(id FolderID) (Folder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sid := SmartFolderID(id); sid != "" && s.feat != nil {
		for _, sf := range s.feat.ListSmartFolders() {
			if sf.ID == sid {
				return Folder{ID: id, AccountID: AccountSmart, Name: sf.Name, Kind: FolderCustom, Virtual: true}, true
			}
		}
	}
	if f, ok := virtualFolderByID(id); ok {
		return f, true
	}
	return s.folderLocked(id)
}

func (s *LocalStore) folderLocked(id FolderID) (Folder, bool) {
	for _, f := range s.Folders {
		if f.ID == id {
			return f, true
		}
	}
	return Folder{}, false
}

func (s *LocalStore) CreateFolder(accountID, name string, parent FolderID) (Folder, error) {
	name = strings.TrimSpace(name)
	if accountID == "" || name == "" {
		return Folder{}, fmt.Errorf("mail: account and folder name required")
	}
	if !validMailboxName(name) {
		return Folder{}, fmt.Errorf("mail: illegal folder name")
	}
	s.mu.Lock()
	remote, delim := name, s.accountDelimLocked(accountID)
	if parent != "" {
		if pf, ok := s.folderLocked(parent); ok && pf.Remote != "" {
			if pf.Delim != "" {
				delim = pf.Delim
			}
			remote = pf.Remote + delim + name
		}
	}
	if strings.Contains(name, delim) {
		s.mu.Unlock()
		return Folder{}, fmt.Errorf("mail: a folder name cannot contain %q", delim)
	}
	s.mu.Unlock()

	// CREATE is network I/O: no store lock held.
	if cli, err := s.client(accountID); err == nil {
		if err := cli.createMailbox(remote); err != nil {
			return Folder{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// A renamed folder keeps the id its old name gave it, so a new folder
	// under that old name needs another.
	id := s.uniqueFolderIDLocked(FolderID(safeID(accountID) + "/" + safeID(name)))
	f := Folder{ID: id, AccountID: accountID, Name: name, Kind: FolderCustom, Parent: parent, Remote: remote, Delim: delim}
	s.Folders = append(s.Folders, f)
	s.saveLocked()
	return f, nil
}

// DeleteFolder removes a user-created folder from the server and the cache.
// System folders (Inbox, Sent, …) and virtual folders are refused.
func (s *LocalStore) DeleteFolder(id FolderID) error {
	s.mu.Lock()
	f, ok := s.folderLocked(id)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("mail: no folder %s", id)
	}
	if f.Virtual || f.Kind != FolderCustom {
		s.mu.Unlock()
		return fmt.Errorf("mail: %q is a system folder and cannot be deleted", f.Name)
	}
	accountID, remote := f.AccountID, remoteName(f)
	s.mu.Unlock()

	// DELETE on the server first; if that fails the local folder stays.
	if cli, err := s.client(accountID); err == nil {
		if err := cli.deleteMailbox(remote); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Folder != id {
			continue
		}
		s.removeRawLocked(s.Messages[i])
		s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
	}
	out := s.Folders[:0]
	for _, x := range s.Folders {
		if x.ID != id {
			out = append(out, x)
		}
	}
	s.Folders = out
	s.dropFolderMetaLocked(id)
	s.saveLocked()
	return nil
}

// MarkFolderRead marks every message in a real folder read, in the cache and
// on the server. A virtual folder is left alone.
func (s *LocalStore) MarkFolderRead(id FolderID) error {
	s.mu.Lock()
	f, ok := s.folderLocked(id)
	if !ok || f.Virtual {
		s.mu.Unlock()
		return nil
	}
	changed := 0
	var uids []uint32
	for i := range s.Messages {
		if s.Messages[i].Folder == id && !s.Messages[i].Read {
			s.Messages[i].Read = true
			syncSystemTagsFromFlags(&s.Messages[i])
			changed++
			if s.Messages[i].UID != 0 {
				uids = append(uids, s.Messages[i].UID)
			}
		}
	}
	accountID, remote := f.AccountID, remoteName(f)
	offline := s.feat != nil && !s.feat.Online()
	// Only the messages the user saw unread are marked on the server — not
	// "1:*", which also marked mail that arrived since the last sync.
	op := OutboxOp{Kind: "read", AccountID: accountID, Src: id, UIDs: uids, UIDVal: s.loadFolderMeta(id).UIDValidity}
	if offline && len(uids) > 0 && s.feat != nil {
		s.feat.mu.Lock()
		s.feat.enqueueLocked(op)
		s.feat.mu.Unlock()
	}
	s.saveLocked()
	s.mu.Unlock()
	if changed > 0 {
		s.Emit(StoreEvent{Reason: "flags", AccountID: accountID, FolderID: id, Count: changed})
	}
	if len(uids) == 0 || offline || remote == "" || accountID == LocalAccountID {
		return nil
	}
	err := s.markReadOnServer(f, uids, op.UIDVal)
	s.noteNetwork(accountID, err)
	if err != nil && s.feat != nil {
		// The cache has it; the server gets it when it answers.
		Logf("mark %s read: %v — queued", id, err)
		op.Error = err.Error()
		s.mu.Lock()
		s.feat.mu.Lock()
		s.feat.enqueueLocked(op)
		s.feat.mu.Unlock()
		s.saveLocked()
		s.mu.Unlock()
		return nil
	}
	return err
}

// markReadOnServer sets \Seen on uids in f, refusing when the folder was
// renumbered since they were read (uidVal).
func (s *LocalStore) markReadOnServer(f Folder, uids []uint32, uidVal uint32) error {
	cli, err := s.client(f.AccountID)
	if err != nil {
		return err
	}
	return cli.inBox(func() error {
		if err := selectForReplay(cli, f, OutboxOp{UIDVal: uidVal}); err != nil {
			return err
		}
		return cli.uidStoreSet(uids, []string{`\Seen`})
	})
}
func (s *LocalStore) ListMessages(folder FolderID) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(folder)
}

// ListMatching is ListMessages narrowed to the messages f passes, the text
// looked up in the search index.
func (s *LocalStore) ListMatching(folder FolderID, f Filter) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	inText := s.textMatcherLocked(f.Query)
	var out []Message
	for _, m := range s.listLocked(folder) {
		if f.MatchText(m, inText) {
			out = append(out, m)
		}
	}
	return out
}

func (s *LocalStore) listLocked(folder FolderID) []Message {
	var out []Message
	for _, m := range s.Messages {
		f, ok := s.folderLocked(m.Folder)
		kind := FolderCustom
		if ok {
			kind = f.Kind
		}
		if IsVirtual(folder) {
			if matchVirtual(folder, m, f, kind, s.feat.snap()) {
				out = append(out, m.Clone())
			}
			continue
		}
		if m.Folder == folder {
			out = append(out, m.Clone())
		}
	}
	return out
}

// CachedMessage is the message as the cache holds it — headers and flags
// always, the body only if it has been downloaded. It never touches the
// network.
func (s *LocalStore) CachedMessage(id MessageID) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.indexLocked(id)
	if !ok {
		return Message{}, false
	}
	return s.Messages[i].Clone(), true
}

func (s *LocalStore) GetMessage(id MessageID) (Message, bool) {
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return Message{}, false
	}
	m := s.Messages[i].Clone()
	if m.Body != "" || m.HTML != "" {
		s.mu.Unlock()
		return m, true
	}
	if raw := s.readRawLocked(m); len(raw) > 0 {
		out := s.applyRawLocked(i, raw)
		s.mu.Unlock()
		return out, true
	}
	s.mu.Unlock()
	if raw, err := s.fetchRaw(m); err == nil && len(raw) > 0 {
		s.mu.Lock()
		defer s.mu.Unlock()
		if j, ok := s.indexLocked(id); ok {
			return s.applyRawLocked(j, raw), true
		}
	}
	return m, true
}

// applyRawLocked parses raw into the cached message at index i, preserving
// the locally-owned fields (flags, tags, ids).
func (s *LocalStore) applyRawLocked(i int, raw []byte) Message {
	m := s.Messages[i]
	parsed, err := ParseRFC822(raw, m.Folder, m.AccountID)
	if err != nil {
		return m.Clone()
	}
	parsed.ID = m.ID
	parsed.UID = m.UID
	parsed.Read = m.Read
	parsed.Starred = m.Starred
	parsed.Tags = m.Tags
	if parsed.ThreadID == "" {
		parsed.ThreadID = m.ThreadID
	}
	s.Messages[i] = parsed
	return parsed.Clone()
}

// fetchRaw downloads the full message. No store lock is held while it runs.
func (s *LocalStore) fetchRaw(m Message) ([]byte, error) {
	if m.UID == 0 {
		return nil, fmt.Errorf("mail: %s has no server UID", m.ID)
	}
	s.mu.Lock()
	f, ok := s.folderLocked(m.Folder)
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("mail: no folder for %s", m.ID)
	}
	cli, err := s.fgClient(m.AccountID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = cli.inBox(func() error {
		if err := selectFor(cli, f, true); err != nil {
			return err
		}
		var err error
		raw, err = cli.uidFetchRFC822(m.UID)
		return err
	})
	s.noteNetwork(m.AccountID, err)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("mail: empty body for %s", m.ID)
	}
	s.mu.Lock()
	s.WriteRawLocked(m, raw)
	s.mu.Unlock()
	return raw, nil
}
func (s *LocalStore) GetRaw(id MessageID) ([]byte, error) {
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("mail: no message %s", id)
	}
	m := s.Messages[i].Clone()
	if raw := s.readRawLocked(m); len(raw) > 0 {
		s.mu.Unlock()
		return append([]byte(nil), raw...), nil
	}
	s.mu.Unlock()
	raw, err := s.fetchRaw(m)
	if err != nil {
		return nil, fmt.Errorf("mail: no raw source for %s: %w", id, err)
	}
	s.mu.Lock()
	if j, ok := s.indexLocked(id); ok {
		s.applyRawLocked(j, raw)
	}
	s.mu.Unlock()
	return append([]byte(nil), raw...), nil
}

// hydrateFromDiskLocked fills m from the cached blob. Disk only — the
// network path is fetchRaw, which runs without the store lock.
func (s *LocalStore) hydrateFromDiskLocked(m *Message) bool {
	if m == nil {
		return false
	}
	raw := s.readRawLocked(*m)
	if len(raw) == 0 {
		return false
	}
	parsed, err := ParseRFC822(raw, m.Folder, m.AccountID)
	if err != nil {
		return false
	}
	parsed.ID = m.ID
	parsed.UID = m.UID
	parsed.Read = m.Read
	parsed.Starred = m.Starred
	parsed.Tags = m.Tags
	*m = parsed
	return true
}
func (s *LocalStore) Search(q SearchQuery) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.feat.snap()
	hits := searchMessages(s.Messages, SearchQuery{AccountID: q.AccountID, Filter: q.Filter}, s.textMatcherLocked(q.Filter.Query))
	if q.Folder == "" {
		return hits
	}
	var out []Message
	for _, m := range hits {
		f, ok := s.folderLocked(m.Folder)
		kind := FolderCustom
		if ok {
			kind = f.Kind
		}
		if IsVirtual(q.Folder) {
			if matchVirtual(q.Folder, m, f, kind, snap) {
				out = append(out, m)
			}
			continue
		}
		if m.Folder == q.Folder {
			out = append(out, m)
		}
	}
	return out
}

func (s *LocalStore) SetFlags(id MessageID, patch FlagPatch) error {
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("mail: no message %s", id)
	}
	m := &s.Messages[i]
	var add, rem []string
	if patch.Read != nil {
		m.Read = *patch.Read
		if *patch.Read {
			add = append(add, `\Seen`)
		} else {
			rem = append(rem, `\Seen`)
		}
	}
	if patch.Answered != nil {
		m.Answered = *patch.Answered
		if *patch.Answered {
			add = append(add, `\Answered`)
		} else {
			rem = append(rem, `\Answered`)
		}
	}
	if patch.Forwarded != nil {
		m.Forwarded = *patch.Forwarded
		if *patch.Forwarded {
			add = append(add, "$Forwarded")
		} else {
			rem = append(rem, "$Forwarded")
		}
	}
	if patch.Starred != nil {
		m.Starred = *patch.Starred
		if *patch.Starred {
			add = append(add, `\Flagged`)
		} else {
			rem = append(rem, `\Flagged`)
		}
	}
	if patch.Tags != nil {
		next := map[string]bool{}
		for _, t := range *patch.Tags {
			next[t] = true
		}
		// A tag taken off is a keyword to clear on the server; without this
		// the next sync brought it back. Unread, Starred and Attachment are
		// derived here from flags and parts, not stored as keywords.
		for _, t := range m.Tags {
			if !next[t] && !IsSystemTag(t) {
				// Both spellings: it may be on the server either way.
				rem = append(rem, tagKeyword(t))
				if kw := imapSafeKeyword(t); kw != tagKeyword(t) {
					rem = append(rem, kw)
				}
			}
		}
		m.Tags = append([]string(nil), (*patch.Tags)...)
		for _, t := range m.Tags {
			if !IsSystemTag(t) {
				add = append(add, tagKeyword(t))
			}
		}
	}
	syncSystemTagsFromFlags(m)
	snapshot := m.Clone()
	offline := s.feat != nil && !s.feat.Online()
	if offline {
		s.queueFlagsLocked(snapshot, patch, add, rem, "")
	}
	folder, hasFolder := s.folderLocked(snapshot.Folder)
	s.saveLocked()
	s.mu.Unlock()

	if offline || !hasFolder || snapshot.UID == 0 {
		return nil
	}
	if err := s.pushFlags(snapshot, folder, add, rem); err != nil && s.feat != nil {
		// The cache already has the change. Queue it for the server rather
		// than let a network blip lose it; the background tick retries.
		s.mu.Lock()
		s.queueFlagsLocked(snapshot, patch, add, rem, err.Error())
		s.saveLocked()
		s.mu.Unlock()
	}
	return nil
}

// queueFlagsLocked queues a flag change for the server, addressed to where
// the server has the message: its folder and UID now or, when a move or
// delete made offline has not reached the server yet, where it was before
// that — and ahead of it in the queue, so the move carries the flags along.
// Addressing the cache's current folder instead sent the old UID to the new
// folder (flagging whatever message had that UID there), and after the
// move re-keyed the message the change found nothing and was dropped.
// The caller holds s.mu.
func (s *LocalStore) queueFlagsLocked(m Message, patch FlagPatch, add, rem []string, cause string) {
	if s.feat == nil {
		return
	}
	op := OutboxOp{
		Kind: "flag", MessageID: m.ID, Patch: patch, AccountID: m.AccountID,
		UID: m.UID, Src: m.Folder, Add: add, Rem: rem, Error: cause,
	}
	if m.UID != 0 {
		op.UIDVal = s.loadFolderMeta(m.Folder).UIDValidity
	}
	s.feat.mu.Lock()
	defer s.feat.mu.Unlock()
	for i, q := range s.feat.outbox {
		if q.MessageID != m.ID || (q.Kind != "move" && q.Kind != "delete") {
			continue
		}
		op.Src, op.UID, op.UIDVal = q.Src, q.UID, q.UIDVal
		op = s.feat.enqueueLocked(op)
		// enqueueLocked appended it; move it in front of the pending move.
		last := len(s.feat.outbox) - 1
		copy(s.feat.outbox[i+1:], s.feat.outbox[i:last])
		s.feat.outbox[i] = op
		return
	}
	s.feat.enqueueLocked(op)
}
func imapSafeKeyword(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// pushFlags mirrors a flag change to the server. Network I/O: never called
// with the store lock held.
func (s *LocalStore) pushFlags(m Message, f Folder, add, rem []string) error {
	if m.UID == 0 || (len(add) == 0 && len(rem) == 0) {
		return nil
	}
	cli, err := s.client(m.AccountID)
	if err != nil {
		return err
	}
	return cli.inBox(func() error {
		if err := selectFor(cli, f, false); err != nil {
			return err
		}
		return cli.uidStore(m.UID, add, rem)
	})
}

// Move relocates messages. When the server reports the destination UID
// (UIDPLUS COPYUID, or the MOVE response) the cache entry is re-keyed to it;
// when it does not, the stale entry is dropped so a later UID STORE / MOVE
// can never address an unrelated message in the destination mailbox.
func (s *LocalStore) Move(ids []MessageID, dest FolderID) error {
	s.mu.Lock()
	df, ok := s.folderLocked(dest)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("mail: no folder %s", dest)
	}
	offline := s.feat != nil && !s.feat.Online()
	type job struct {
		id  MessageID
		msg Message
		src Folder
	}
	var jobs []job
	for _, id := range ids {
		i, ok := s.indexLocked(id)
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("mail: no message %s", id)
		}
		m := s.Messages[i].Clone()
		src, _ := s.folderLocked(m.Folder)
		if offline {
			if m.UID != 0 {
				s.feat.mu.Lock()
				s.feat.enqueueLocked(s.serverMoveOpLocked(m, dest))
				s.feat.mu.Unlock()
			}
			s.Messages[i].Folder = dest
			continue
		}
		if m.UID == 0 {
			s.Messages[i].Folder = dest
			continue
		}
		jobs = append(jobs, job{id: id, msg: m, src: src})
	}
	if len(jobs) == 0 {
		s.saveLocked()
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	mj := make([]moveJob, len(jobs))
	for k, j := range jobs {
		mj[k] = moveJob{id: j.id, msg: j.msg, src: j.src}
	}
	s.moveOnServer(mj, df, "Moving to "+df.Name)
	return nil
}

// moveJob is one message a server move takes: where it is, as it was.
type moveJob struct {
	id  MessageID
	msg Message
	src Folder
}

// moveOnServer moves the jobs' messages to dest on the server — a folder at
// a time and up to moveBatch messages a command: one SELECT and one UID
// MOVE for many, where it used to be both for each (a thousand round trips
// for five hundred messages) — and re-keys the cache to the UIDs the server
// gives them. What fails waits in the Outbox. A long move reports its
// progress (verb: "Moving to Archive", "Deleting").
func (s *LocalStore) moveOnServer(jobs []moveJob, dest Folder, verb string) {
	type group struct {
		src  Folder
		acct string
		jobs []moveJob
	}
	var groups []*group
	byFolder := map[FolderID]*group{}
	for _, j := range jobs {
		g := byFolder[j.src.ID]
		if g == nil {
			g = &group{src: j.src, acct: j.msg.AccountID}
			byFolder[j.src.ID] = g
			groups = append(groups, g)
		}
		g.jobs = append(g.jobs, j)
	}
	dremote := remoteName(dest)
	done, total := 0, len(jobs)
	for _, g := range groups {
		for start := 0; start < len(g.jobs); start += moveBatch {
			chunk := g.jobs[start:min(start+moveBatch, len(g.jobs))]
			uids := make([]uint32, len(chunk))
			for k, j := range chunk {
				uids[k] = j.msg.UID
			}
			var moved map[uint32]uint32
			cli, moveErr := s.client(g.acct)
			if moveErr == nil {
				moveErr = cli.inBox(func() error {
					if err := selectFor(cli, g.src, false); err != nil {
						return err
					}
					var err error
					moved, err = cli.uidMoveSet(sortedUIDs(uids), dremote)
					return err
				})
			}
			s.mu.Lock()
			for _, j := range chunk {
				i, ok := s.indexLocked(j.id)
				if !ok {
					continue
				}
				if moveErr != nil {
					// The cache has it; the server gets it when it answers.
					if s.feat != nil {
						s.feat.mu.Lock()
						op := s.serverMoveOpLocked(j.msg, dest.ID)
						op.Error = moveErr.Error()
						s.feat.enqueueLocked(op)
						s.feat.mu.Unlock()
					}
					s.Messages[i].Folder = dest.ID
					continue
				}
				s.rekeyMovedLocked(i, dest.ID, moved[j.msg.UID])
			}
			s.saveLocked()
			s.mu.Unlock()
			done += len(chunk)
			if total >= progressFrom {
				s.Emit(StoreEvent{Reason: "progress", AccountID: g.acct, Count: done,
					Title: fmt.Sprintf("%s… %d of %d", verb, done, total)})
			}
		}
	}
}

// moveBatch is how many messages one UID MOVE carries; progressFrom is how
// many a move must hold before it reports its progress.
const (
	moveBatch    = 200
	progressFrom = 50
)

// rekeyMovedLocked applies a completed server-side move to the cache entry
// at index i. newUID == 0 means the server gave us no UID: the entry is
// removed rather than kept under a UID that names something else.
func (s *LocalStore) rekeyMovedLocked(i int, dest FolderID, newUID uint32) {
	old := s.Messages[i]
	if newUID == 0 {
		s.removeRawLocked(old)
		s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
		return
	}
	raw := s.readRawLocked(old)
	s.removeRawLocked(old)
	m := old
	m.Folder = dest
	m.UID = newUID
	m.ID = MessageID(fmt.Sprintf("%s:%d", dest, newUID))
	if s.rekeyed == nil || len(s.rekeyed) > 1000 {
		s.rekeyed = map[MessageID]MessageID{}
	}
	s.rekeyed[old.ID] = m.ID
	s.Messages[i] = m
	if len(raw) > 0 {
		s.WriteRawLocked(m, raw)
	}
}
func (s *LocalStore) Delete(ids []MessageID) error {
	s.mu.Lock()
	ids = append([]MessageID(nil), ids...)
	for k := range ids {
		ids[k] = s.resolveLocked(ids[k]) // a draft re-keyed since the window last saw it
	}
	offline := s.feat != nil && !s.feat.Online()
	type toTrash struct {
		id    MessageID
		msg   Message
		src   Folder
		trash Folder
	}
	var purges []struct {
		msg Message
		src Folder
	}
	var moves []toTrash
	for _, id := range ids {
		i, ok := s.indexLocked(id)
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("mail: no message %s", id)
		}
		cur, ok := s.folderLocked(s.Messages[i].Folder)
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("mail: no folder for %s", id)
		}
		m := s.Messages[i].Clone()
		trash, hasTrash := s.specialLocked(cur.AccountID, FolderTrash)
		if offline && m.UID != 0 {
			// What the server must do later: a move to Trash, or — for a
			// message already in Trash — a purge from where it is.
			op := s.serverMoveOpLocked(m, trash.ID)
			if cur.Kind == FolderTrash || !hasTrash {
				op.Kind, op.Dest = "delete", ""
			}
			s.feat.mu.Lock()
			s.feat.enqueueLocked(op)
			s.feat.mu.Unlock()
		}
		if cur.Kind == FolderTrash || !hasTrash {
			if !offline {
				purges = append(purges, struct {
					msg Message
					src Folder
				}{m, cur})
			}
			s.removeRawLocked(m)
			s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
			continue
		}
		s.Messages[i].Folder = trash.ID
		if !offline && m.UID != 0 {
			moves = append(moves, toTrash{id: id, msg: m, src: cur, trash: trash})
		}
	}
	s.saveLocked()
	s.mu.Unlock()

	s.purgeOnServer(purges)
	// To each account's Trash, batched as moves are.
	byTrash := map[FolderID][]moveJob{}
	trashes := map[FolderID]Folder{}
	var order []FolderID
	for _, mv := range moves {
		if _, ok := trashes[mv.trash.ID]; !ok {
			trashes[mv.trash.ID] = mv.trash
			order = append(order, mv.trash.ID)
		}
		byTrash[mv.trash.ID] = append(byTrash[mv.trash.ID], moveJob{id: mv.id, msg: mv.msg, src: mv.src})
	}
	for _, id := range order {
		s.moveOnServer(byTrash[id], trashes[id], "Deleting")
	}
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return nil
}

// purgeOnServer permanently removes messages that were already in Trash
// (or in an account with none): a folder at a time, \Deleted then UID
// EXPUNGE on exactly those UIDs. What fails waits in the Outbox.
func (s *LocalStore) purgeOnServer(purges []struct {
	msg Message
	src Folder
}) {
	type group struct {
		src  Folder
		msgs []Message
	}
	var groups []*group
	by := map[FolderID]*group{}
	for _, p := range purges {
		if p.msg.UID == 0 {
			continue
		}
		g := by[p.src.ID]
		if g == nil {
			g = &group{src: p.src}
			by[p.src.ID] = g
			groups = append(groups, g)
		}
		g.msgs = append(g.msgs, p.msg)
	}
	for _, g := range groups {
		for start := 0; start < len(g.msgs); start += moveBatch {
			chunk := g.msgs[start:min(start+moveBatch, len(g.msgs))]
			uids := make([]uint32, len(chunk))
			for k, m := range chunk {
				uids[k] = m.UID
			}
			cli, err := s.client(chunk[0].AccountID)
			if err == nil {
				err = cli.inBox(func() error {
					if err := selectFor(cli, g.src, false); err != nil {
						return err
					}
					return cli.purgeSet(sortedUIDs(uids))
				})
			}
			if err != nil {
				for _, m := range chunk {
					s.queueFailed(m, "delete", "", err)
				}
			}
		}
	}
}

// expungeOne permanently removes one message from the server. UID EXPUNGE is
// used when UIDPLUS is advertised so other \Deleted messages in the mailbox
// (possibly flagged by another client) survive.
func (s *LocalStore) expungeOne(m Message, f Folder) error {
	if m.UID == 0 {
		return nil
	}
	cli, err := s.client(m.AccountID)
	if err != nil {
		return err
	}
	return cli.inBox(func() error {
		if err := selectFor(cli, f, false); err != nil {
			return err
		}
		if err := cli.uidStore(m.UID, []string{`\Deleted`}, nil); err != nil {
			return err
		}
		return cli.expungeUID(m.UID)
	})
}
func (s *LocalStore) Append(folder FolderID, msg Message) (MessageID, error) {
	s.mu.Lock()
	f, ok := s.folderLocked(folder)
	if !ok {
		s.mu.Unlock()
		return "", fmt.Errorf("mail: no folder %s", folder)
	}
	if msg.Date.IsZero() {
		msg.Date = time.Now()
	}
	s.nextID++
	msg.ID = MessageID(fmt.Sprintf("%s-m-%04d", safeID(f.AccountID), s.nextID))
	msg.Folder = folder
	msg.AccountID = f.AccountID
	if msg.ThreadID == "" {
		msg.ThreadID = ThreadIDOf(msg)
	}
	if msg.Category == "" && s.feat != nil {
		msg.Category = messageCategory(msg, s.feat.snap())
	}
	if msg.Size <= 0 {
		msg.Size = len(msg.Subject) + len(msg.Body) + 80
	}
	applyAutomaticTags(&msg)
	ident := s.defaultIdentLocked(f.AccountID)
	if strings.TrimSpace(msg.RFCMessageID) == "" {
		// Kept on the cached copy too, so a sync can tell the server's copy
		// of this message is this one.
		msg.RFCMessageID = newMessageID(ident.Address)
	}
	raw, buildErr := BuildRFC822Strict(msg, ident, nil)
	if buildErr != nil {
		s.mu.Unlock()
		return "", buildErr
	}
	s.WriteRawLocked(msg, raw)
	s.Messages = append(s.Messages, msg)
	s.saveLocked()
	accountID := f.AccountID
	s.mu.Unlock()

	// APPEND is network I/O.
	return s.appendToServer(f, accountID, msg.ID, raw), nil
}

// appendToServer stores raw in f on the server and, when the server says
// which UID it gave it, re-keys the cached copy id to that UID — so a sync
// finds the message it already has instead of adding a second copy. It
// returns the message's id afterwards.
func (s *LocalStore) appendToServer(f Folder, accountID string, id MessageID, raw []byte) MessageID {
	if f.Virtual || f.Remote == "" || accountID == LocalAccountID {
		return id
	}
	if s.feat != nil && !s.feat.Online() {
		s.queueAppend(accountID, id, f.ID, "")
		return id
	}
	newID, err := s.appendNow(f, accountID, id, raw)
	if err != nil {
		// The cache has it; the server gets it when it answers.
		Logf("append to %s: %v — queued", f.ID, err)
		s.queueAppend(accountID, id, f.ID, err.Error())
	}
	return newID
}

// appendNow is the APPEND itself, re-keying the cached copy to the UID the
// server gives it.
func (s *LocalStore) appendNow(f Folder, accountID string, id MessageID, raw []byte) (MessageID, error) {
	cli, err := s.client(accountID)
	if err != nil {
		return id, err
	}
	uid, err := cli.appendRaw(remoteName(f), raw, `\Seen`)
	if err != nil || uid == 0 {
		return id, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.indexLocked(id)
	if !ok {
		return id, nil
	}
	s.rekeyMovedLocked(i, f.ID, uid)
	s.saveLocked()
	return s.Messages[i].ID, nil
}

// queueAppend queues a message written here (a draft saved offline) for
// the server's copy of its folder, once.
func (s *LocalStore) queueAppend(accountID string, id MessageID, folder FolderID, cause string) {
	if s.feat == nil {
		return
	}
	s.feat.mu.Lock()
	for _, op := range s.feat.outbox {
		if op.Kind == "append" && op.MessageID == id {
			s.feat.mu.Unlock()
			return // one APPEND sends whatever the message holds by then
		}
	}
	s.feat.enqueueLocked(OutboxOp{Kind: "append", AccountID: accountID, MessageID: id, Dest: folder, Error: cause})
	s.feat.mu.Unlock()
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
}

// replayAppend sends a message written offline to the server, as it is
// now — a draft saved again since has its latest text.
func (s *LocalStore) replayAppend(op OutboxOp) error {
	s.mu.Lock()
	i, ok := s.indexLocked(op.MessageID)
	if !ok || s.Messages[i].UID != 0 {
		s.mu.Unlock()
		return nil // gone (sent, deleted), or on the server already
	}
	m := s.Messages[i].Clone()
	f, hasFolder := s.folderLocked(m.Folder)
	raw := s.readRawLocked(m)
	s.mu.Unlock()
	if !hasFolder || len(raw) == 0 {
		return nil
	}
	_, err := s.appendNow(f, m.AccountID, m.ID, raw)
	return err
}

// maxAutoTries is how often the background tick retries a queued op before
// leaving it for a flush by hand.
const maxAutoTries = 10

// resolveLocked follows a message's re-keys to its id now.
func (s *LocalStore) resolveLocked(id MessageID) MessageID {
	for n := 0; n < 4; n++ {
		if _, ok := s.indexLocked(id); ok {
			return id
		}
		next, ok := s.rekeyed[id]
		if !ok {
			return id
		}
		id = next
	}
	return id
}

// newMessageID makes a Message-ID for a message written here.
// newMessageID is a Message-ID for mail written here, made the way other
// clients make theirs: random, at the sender's domain. It names neither
// the software nor the sender's address, and does not say when it was
// written.
func newMessageID(addr string) string {
	host := "localhost"
	a := ExtractAddr(addr)
	if at := strings.LastIndexByte(a, '@'); at >= 0 && messageIDHost(a[at+1:]) {
		host = strings.ToLower(a[at+1:])
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + host + ">"
}

// messageIDHost reports whether h can stand as a Message-ID's domain as
// it is: ASCII letters, digits, dots and hyphens.
func messageIDHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, c := range h {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

// Update replaces a message's content — a draft saved again. The cached
// copy and its stored source change at once; a copy on the server is
// replaced (the new version appended, the old one removed), so the server's
// Drafts does not keep the first version, or gain one per save.
func (s *LocalStore) Update(id MessageID, msg Message) error {
	_, err := s.update(id, msg)
	return err
}

// update is Update, returning the message's id afterwards: replacing the
// server copy gives it a new UID.
func (s *LocalStore) update(id MessageID, msg Message) (MessageID, error) {
	s.mu.Lock()
	id = s.resolveLocked(id)
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return "", fmt.Errorf("mail: no message %s", id)
	}
	keep := s.Messages[i]
	msg.ID = keep.ID
	if msg.Folder == "" {
		msg.Folder = keep.Folder
	}
	if msg.AccountID == "" {
		msg.AccountID = keep.AccountID
	}
	if msg.Date.IsZero() {
		msg.Date = keep.Date
	}
	if strings.TrimSpace(msg.RFCMessageID) == "" {
		msg.RFCMessageID = keep.RFCMessageID
	}
	if msg.ThreadID == "" {
		msg.ThreadID = keep.ThreadID
	}
	msg.UID = keep.UID
	applyAutomaticTags(&msg)
	ident := s.defaultIdentLocked(msg.AccountID)
	raw, buildErr := BuildRFC822Strict(msg, ident, nil)
	if buildErr != nil {
		s.mu.Unlock()
		return "", buildErr
	}
	s.Messages[i] = msg
	s.WriteRawLocked(msg, raw)
	f, hasFolder := s.folderLocked(msg.Folder)
	s.saveLocked()
	s.mu.Unlock()

	if !hasFolder || keep.UID == 0 {
		return msg.ID, nil
	}
	// Network I/O: the new version goes in before the old one goes, so a
	// failure half way leaves a copy rather than none.
	newID := s.appendToServer(f, msg.AccountID, msg.ID, raw)
	if newID == msg.ID {
		return msg.ID, nil // not replaced (offline, or no UID back); kept locally
	}
	if cli, err := s.client(msg.AccountID); err == nil {
		_ = cli.inBox(func() error {
			if err := selectFor(cli, f, false); err != nil {
				return err
			}
			if err := cli.uidStore(keep.UID, []string{`\Deleted`}, nil); err != nil {
				return err
			}
			return cli.expungeUID(keep.UID)
		})
	}
	return newID, nil
}

func (s *LocalStore) Fetch(accountID string) (int, error) {
	res, err := s.Sync(accountID)
	return res.New, err
}

// Sync runs a full pass. Only one sync runs at a time (syncMu); the store
// lock is taken only to snapshot inputs and apply results, never across
// network I/O, and progress is announced with StoreEvents so the UI can
// refresh while a long first sync is still running.
func (s *LocalStore) Sync(accountID string) (SyncResult, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	res := SyncResult{AccountID: accountID}
	s.mu.Lock()
	accts := append([]Account(nil), s.accounts...)
	s.mu.Unlock()
	if accountID != "" {
		var only []Account
		for _, a := range accts {
			if a.ID == accountID {
				only = append(only, a)
			}
		}
		accts = only
	}
	for _, a := range accts {
		n, err := s.syncAccount(a.ID)
		res.New += n
		s.mu.Lock()
		nf := 0
		for _, f := range s.Folders {
			if f.AccountID == a.ID && !f.Virtual {
				nf++
			}
		}
		s.mu.Unlock()
		res.Folders += nf
		if err != nil {
			s.setHealth(err)
			res.Error = err.Error()
		}
		if n > 0 {
			s.Emit(StoreEvent{Reason: "sync", AccountID: a.ID, Count: n})
		}
	}
	s.mu.Lock()
	s.saveLocked()
	s.feat.setContacts(buildContacts(s.Messages))
	health := s.health
	s.mu.Unlock()
	// What rules did to new mail (moves, marks, tags) reaches the server
	// now, not at the next tick.
	if s.feat != nil && s.feat.Online() {
		_, _ = s.flushOutbox(func(op OutboxOp) bool { return op.Kind == "flag" || op.Kind == "move" })
	}
	s.prefetchBodies()
	return res, health
}
func (s *LocalStore) syncAccount(accountID string) (int, error) {
	if accountID == LocalAccountID {
		return 0, nil // imported local mail has no server to sync
	}
	if cfg, ok := s.accountCfg(accountID); ok && cfg.IsLocal() {
		return 0, nil
	}
	if cfg, ok := s.accountCfg(accountID); ok && cfg.IsPOP3() {
		return s.syncPOP3(accountID)
	}
	cli, err := s.client(accountID)
	if err != nil {
		return 0, err
	}
	boxes, err := cli.list()
	if err != nil {
		s.noteNetwork(accountID, err)
		return 0, err
	}
	added := 0
	var netErr error
	listed := map[FolderID]bool{}
	for _, b := range boxes {
		if b.Name == "" {
			continue
		}
		display := b.Display
		if display == "" {
			display = DecodeIMAPUTF7(b.Name)
		}
		kind := folderKindFromIMAP(display, b.Attrs)
		s.mu.Lock()
		// A folder is the server mailbox it names, whatever its id: a folder
		// renamed here keeps its id (and its messages theirs) under its new
		// name. Remote used to hold the modified-UTF-7 wire name, which
		// every command encoded a second time; it is the decoded name now.
		f, ok := s.folderByRemoteLocked(accountID, display, b.Name)
		if !ok {
			id := FolderID(safeID(accountID) + "/" + safeID(display))
			if kind == FolderInbox {
				id = FolderID(safeID(accountID) + "/inbox")
			}
			if x, taken := s.folderLocked(id); taken && x.Remote != "" && x.Remote != display && x.Remote != b.Name {
				// The id is a renamed folder's; this mailbox needs its own.
				id = s.uniqueFolderIDLocked(id)
			}
			if f, ok = s.folderLocked(id); !ok {
				f = Folder{ID: id, AccountID: accountID, Name: displayIMAPName(display, b.Delim), Kind: kind}
			}
		}
		noSelect := hasAttrFold(b.Attrs, `\Noselect`) || hasAttrFold(b.Attrs, `\NonExistent`)
		if !ok {
			f.Remote, f.Delim, f.NoSelect = display, b.Delim, noSelect
			s.Folders = append(s.Folders, f)
		} else {
			f.Remote, f.Delim, f.NoSelect = display, b.Delim, noSelect
			f.Kind = kind
			s.replaceFolderLocked(f)
		}
		listed[f.ID] = true
		s.mu.Unlock()
		if noSelect {
			continue // a parent only: nothing to open
		}

		n, err := s.syncFolder(cli, f)
		if err != nil {
			Logf("sync %s: %v", f.ID, err)
			s.setHealth(err)
			if isNetworkError(err) {
				netErr = err
			}
			continue
		}
		added += n
		if n > 0 {
			s.Emit(StoreEvent{Reason: "fetch", AccountID: accountID, FolderID: f.ID, Count: n})
		}
	}
	s.noteNetwork(accountID, netErr)
	s.dropUnlistedFolders(accountID, listed)
	s.mu.Lock()
	if s.nestFoldersLocked(accountID) {
		s.saveLocked()
	}
	s.mu.Unlock()
	return added, nil
}

func hasAttrFold(attrs []string, want string) bool {
	for _, a := range attrs {
		if strings.EqualFold(a, want) {
			return true
		}
	}
	return false
}

// nestFoldersLocked sets each IMAP folder's parent from its server name —
// "Archives/2023" goes under "Archives" and is called "2023". A mailbox
// whose parent the server does not list keeps its whole path as its name.
// When every other mailbox sits under "INBOX." (a server whose namespace is
// INBOX), that prefix is a namespace, not a parent, and is left out. It
// reports whether anything changed.
func (s *LocalStore) nestFoldersLocked(accountID string) bool {
	var idx []int
	byRemote := map[string]int{}
	inboxPrefix := ""
	for i, f := range s.Folders {
		if f.AccountID != accountID || f.Virtual || f.Remote == "" || f.Delim == "" {
			continue
		}
		idx = append(idx, i)
		byRemote[f.Remote] = i
		if f.Kind == FolderInbox {
			inboxPrefix = f.Remote + f.Delim
		}
	}
	namespaced, others := inboxPrefix != "", 0
	for _, i := range idx {
		f := s.Folders[i]
		if f.Kind == FolderInbox {
			continue
		}
		others++
		if !strings.HasPrefix(strings.ToUpper(f.Remote), strings.ToUpper(inboxPrefix)) {
			namespaced = false
		}
	}
	if others == 0 {
		namespaced = false
	}
	changed := false
	for _, i := range idx {
		f := s.Folders[i]
		if f.Kind == FolderInbox {
			continue
		}
		rel := f.Remote
		if namespaced {
			rel = rel[len(inboxPrefix):]
		}
		parent, name := FolderID(""), strings.ReplaceAll(rel, f.Delim, "/")
		if cut := strings.LastIndex(rel, f.Delim); cut >= 0 {
			parentRemote := f.Remote[:len(f.Remote)-len(rel)+cut]
			if j, ok := byRemote[parentRemote]; ok && j != i {
				parent, name = s.Folders[j].ID, rel[cut+len(f.Delim):]
			}
		}
		if f.Parent != parent || f.Name != name {
			s.Folders[i].Parent, s.Folders[i].Name = parent, name
			changed = true
		}
	}
	return changed
}

// accountDelimLocked is the hierarchy delimiter the account's server uses
// ("/" until a sync has said).
func (s *LocalStore) accountDelimLocked(accountID string) string {
	for _, f := range s.Folders {
		if f.AccountID == accountID && f.Delim != "" {
			return f.Delim
		}
	}
	return "/"
}

// folderByRemoteLocked is the account's folder for a server mailbox, by
// its decoded name (or, from caches written before names were decoded,
// its wire name).
func (s *LocalStore) folderByRemoteLocked(accountID, display, wire string) (Folder, bool) {
	for _, f := range s.Folders {
		if f.AccountID == accountID && !f.Virtual && f.Remote != "" && (f.Remote == display || f.Remote == wire) {
			return f, true
		}
	}
	return Folder{}, false
}

// uniqueFolderIDLocked is id, or id with a number after it, whichever no
// folder has.
func (s *LocalStore) uniqueFolderIDLocked(id FolderID) FolderID {
	if _, taken := s.folderLocked(id); !taken {
		return id
	}
	for n := 2; ; n++ {
		next := FolderID(fmt.Sprintf("%s-%d", id, n))
		if _, taken := s.folderLocked(next); !taken {
			return next
		}
	}
}

// dropUnlistedFolders forgets the folders the server no longer has —
// deleted, or renamed in another client (the new name arrived as a folder
// of its own). Only folders that were synced from the server before go: one
// made here while offline has not reached it yet. A folder that queued
// changes still point at is kept until they are replayed.
func (s *LocalStore) dropUnlistedFolders(accountID string, listed map[FolderID]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var gone []FolderID
	for _, f := range s.Folders {
		if f.AccountID != accountID || f.Virtual || listed[f.ID] || f.Remote == "" {
			continue
		}
		if !f.NoSelect && s.loadFolderMeta(f.ID).UIDValidity == 0 {
			continue // made here and not on the server yet
		}
		if s.feat != nil && s.feat.touchesFolder(f.ID) {
			continue
		}
		gone = append(gone, f.ID)
	}
	if len(gone) == 0 {
		return
	}
	drop := map[FolderID]bool{}
	for _, id := range gone {
		drop[id] = true
		s.dropFolderMessagesLocked(id)
		s.dropFolderMetaLocked(id)
	}
	out := s.Folders[:0]
	for _, f := range s.Folders {
		if !drop[f.ID] {
			out = append(out, f)
		}
	}
	s.Folders = out
	s.saveLocked()
}

// RenameFolder renames a user-created folder: RENAME on the server, then
// the cache. The folder keeps its id and its messages keep theirs; folders
// under it follow it (the server renames them with it).
func (s *LocalStore) RenameFolder(id FolderID, name string) (Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Folder{}, fmt.Errorf("mail: a folder needs a name")
	}
	if !validMailboxName(name) {
		return Folder{}, fmt.Errorf("mail: illegal folder name")
	}
	s.mu.Lock()
	f, err := s.movableFolderLocked(id)
	if err != nil {
		s.mu.Unlock()
		return Folder{}, err
	}
	delim := s.folderDelimLocked(f)
	if strings.Contains(name, delim) {
		s.mu.Unlock()
		return Folder{}, fmt.Errorf("mail: a folder name cannot contain %q", delim)
	}
	oldRemote := remoteName(f)
	newRemote := name
	if i := strings.LastIndex(oldRemote, delim); i >= 0 {
		newRemote = oldRemote[:i+len(delim)] + name
	} else if ns := s.namespacePrefixLocked(f.AccountID); ns != "" {
		newRemote = ns + name
	}
	s.mu.Unlock()
	return s.moveMailbox(f, newRemote, name, f.Parent)
}

// MoveFolder puts a user-created folder under parent (or at the top level
// when parent is empty): RENAME on the server to its new path, then the
// cache. Its messages, and the folders under it, go with it.
func (s *LocalStore) MoveFolder(id, parent FolderID) (Folder, error) {
	s.mu.Lock()
	f, err := s.movableFolderLocked(id)
	if err != nil {
		s.mu.Unlock()
		return Folder{}, err
	}
	if parent == f.Parent {
		s.mu.Unlock()
		return f, nil
	}
	delim := s.folderDelimLocked(f)
	leaf := f.Name
	if i := strings.LastIndex(remoteName(f), delim); i >= 0 {
		leaf = remoteName(f)[i+len(delim):]
	}
	newRemote := s.namespacePrefixLocked(f.AccountID) + leaf
	if parent != "" {
		p, ok := s.folderLocked(parent)
		switch {
		case !ok || p.AccountID != f.AccountID || p.Virtual:
			s.mu.Unlock()
			return Folder{}, fmt.Errorf("mail: cannot move a folder there")
		case p.ID == id || strings.HasPrefix(remoteName(p), remoteName(f)+delim):
			s.mu.Unlock()
			return Folder{}, fmt.Errorf("mail: a folder cannot go inside itself")
		}
		newRemote = remoteName(p) + delim + leaf
	}
	s.mu.Unlock()
	return s.moveMailbox(f, newRemote, leaf, parent)
}

// movableFolderLocked is folder id when the user may rename or move it.
func (s *LocalStore) movableFolderLocked(id FolderID) (Folder, error) {
	f, ok := s.folderLocked(id)
	if !ok {
		return Folder{}, fmt.Errorf("mail: no folder %s", id)
	}
	if f.Virtual || f.Kind != FolderCustom {
		return Folder{}, fmt.Errorf("mail: %q is a system folder and cannot be renamed or moved", f.Name)
	}
	return f, nil
}

func (s *LocalStore) folderDelimLocked(f Folder) string {
	if f.Delim != "" {
		return f.Delim
	}
	return s.accountDelimLocked(f.AccountID)
}

// namespacePrefixLocked is "INBOX." (or the like) for a server whose
// folders all live under INBOX, else "".
func (s *LocalStore) namespacePrefixLocked(accountID string) string {
	prefix, others := "", 0
	for _, f := range s.Folders {
		if f.AccountID == accountID && f.Kind == FolderInbox && f.Delim != "" && f.Remote != "" {
			prefix = f.Remote + f.Delim
		}
	}
	if prefix == "" {
		return ""
	}
	for _, f := range s.Folders {
		if f.AccountID != accountID || f.Virtual || f.Remote == "" || f.Kind == FolderInbox {
			continue
		}
		others++
		if !strings.HasPrefix(strings.ToUpper(f.Remote), strings.ToUpper(prefix)) {
			return ""
		}
	}
	if others == 0 {
		return ""
	}
	return prefix
}

// moveMailbox renames f's mailbox to newRemote on the server and in the
// cache, where it is called name and sits under parent; the folders under
// it follow.
func (s *LocalStore) moveMailbox(f Folder, newRemote, name string, parent FolderID) (Folder, error) {
	s.mu.Lock()
	for _, x := range s.Folders {
		if x.AccountID == f.AccountID && x.ID != f.ID && !x.Virtual && strings.EqualFold(remoteName(x), newRemote) {
			s.mu.Unlock()
			return Folder{}, fmt.Errorf("mail: there is already a folder called %q there", name)
		}
	}
	delim := s.folderDelimLocked(f)
	oldRemote := remoteName(f)
	onServer := f.Remote != "" && f.AccountID != LocalAccountID
	s.mu.Unlock()

	if onServer {
		cli, err := s.client(f.AccountID)
		if err != nil {
			return Folder{}, err
		}
		if err := cli.renameMailbox(oldRemote, newRemote); err != nil {
			return Folder{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.Folders {
		if x.AccountID != f.AccountID || x.Virtual {
			continue
		}
		switch {
		case x.ID == f.ID:
			x.Name, x.Parent = name, parent
			if onServer {
				x.Remote = newRemote
			}
		case onServer && strings.HasPrefix(x.Remote, oldRemote+delim):
			x.Remote = newRemote + strings.TrimPrefix(x.Remote, oldRemote)
		default:
			continue
		}
		s.Folders[i] = x
		if meta := s.loadFolderMeta(x.ID); meta.UIDValidity != 0 {
			meta.Remote = x.Remote
			s.saveFolderMeta(x.ID, meta)
		}
	}
	if onServer {
		s.nestFoldersLocked(f.AccountID)
	}
	s.saveLocked()
	out, _ := s.folderLocked(f.ID)
	return out, nil
}

// CompactFolder removes from the server what is marked deleted in a folder
// (EXPUNGE) — mail another client deleted by marking it — and drops it here.
func (s *LocalStore) CompactFolder(id FolderID) error {
	s.mu.Lock()
	f, ok := s.folderLocked(id)
	s.mu.Unlock()
	if !ok || f.Virtual || f.NoSelect || f.Remote == "" || f.AccountID == LocalAccountID {
		return fmt.Errorf("mail: this folder cannot be compacted")
	}
	cli, err := s.client(f.AccountID)
	if err != nil {
		return err
	}
	err = cli.inBox(func() error {
		if err := selectFor(cli, f, false); err != nil {
			return err
		}
		return cli.expunge()
	})
	if err != nil {
		return err
	}
	_, err = s.syncFolder(cli, f) // the diff drops what is gone
	return err
}

func (s *LocalStore) syncPOP3(accountID string) (int, error) {
	cfg, ok := s.accountCfg(accountID)
	if !ok {
		return 0, fmt.Errorf("mail: no account %s", accountID)
	}
	in := cfg.Incoming()
	if in.Host == "" {
		return 0, fmt.Errorf("mail: no POP3 host for %s", accountID)
	}
	if in.locked {
		return 0, ErrLocked
	}
	in.tokenKey = accountID

	s.mu.Lock()
	s.ensureLocalSpecialsLocked(accountID)
	inbox, ok := s.specialLocked(accountID, FolderInbox)
	haveRFC := map[string]bool{}
	for _, m := range s.Messages {
		if m.AccountID == accountID && m.RFCMessageID != "" {
			haveRFC[strings.TrimSpace(m.RFCMessageID)] = true
		}
	}
	s.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("mail: no inbox for %s", accountID)
	}

	cli := newPOP3Client(in, cfg.Address)
	if err := cli.connect(); err != nil {
		return 0, err
	}
	defer cli.close()

	uidls, err := cli.uidl()
	if err != nil {
		n, sterr := cli.stat()
		if sterr != nil {
			return 0, err
		}
		for i := 1; i <= n; i++ {
			uidls = append(uidls, popUIDL{N: i, UIDL: fmt.Sprintf("n%d", i)})
		}
	}

	added := 0
	for _, u := range uidls {
		id := popMessageID(accountID, u.UIDL)
		s.mu.Lock()
		_, exists := s.indexLocked(id)
		s.mu.Unlock()
		if exists {
			continue
		}
		raw, err := cli.retr(u.N)
		if err != nil {
			s.setHealth(err)
			continue
		}
		msg, err := ParseRFC822(raw, inbox.ID, accountID)
		if err != nil {
			msg = Message{Folder: inbox.ID, AccountID: accountID, Body: string(raw), Size: len(raw)}
		}
		if mid := strings.TrimSpace(msg.RFCMessageID); mid != "" && haveRFC[mid] {
			continue
		}
		msg.ID = id
		msg.Folder = inbox.ID
		msg.AccountID = accountID
		if msg.Date.IsZero() {
			msg.Date = time.Now()
		}
		if msg.ThreadID == "" {
			msg.ThreadID = ThreadIDOf(msg)
		}
		s.mu.Lock()
		if msg.Category == "" && s.feat != nil {
			msg.Category = messageCategory(msg, s.feat.snap())
		}
		applyAutomaticTags(&msg)
		s.WriteRawLocked(msg, raw)
		s.Messages = append(s.Messages, msg)
		s.mu.Unlock()
		if msg.RFCMessageID != "" {
			haveRFC[strings.TrimSpace(msg.RFCMessageID)] = true
		}
		added++
	}
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return added, nil
}
func (s *LocalStore) ensureLocalSpecialsLocked(accountID string) {
	for _, spec := range defaultSpecials() {
		id := FolderID(accountID + "/" + strings.ToLower(spec.Name))
		if spec.Kind == FolderInbox {
			id = FolderID(accountID + "/inbox")
		}
		if _, ok := s.folderLocked(id); ok {
			continue
		}
		s.Folders = append(s.Folders, Folder{
			ID: id, AccountID: accountID, Name: spec.Name, Kind: spec.Kind, Remote: spec.Remote,
		})
	}
}

func displayIMAPName(name, delim string) string {
	if strings.EqualFold(name, "INBOX") {
		return "Inbox"
	}
	if delim != "" && strings.Contains(name, delim) {
		parts := strings.Split(name, delim)
		return parts[len(parts)-1]
	}
	return name
}

func (s *LocalStore) replaceFolderLocked(f Folder) {
	for i, x := range s.Folders {
		if x.ID == f.ID {
			s.Folders[i] = f
			return
		}
	}
	s.Folders = append(s.Folders, f)
}

// syncFolder is one mailbox pass: SELECT (+QRESYNC), incremental UID FETCH,
// flag refresh, and — for servers that never send VANISHED — a UID-set diff
// so messages deleted elsewhere actually leave the cache.
//
// All of it runs with the store lock released; results are applied at the
// end under the lock.
func (s *LocalStore) syncFolder(cli *imapClient, f Folder) (int, error) {
	remote := remoteName(f)

	s.mu.Lock()
	meta := s.loadFolderMeta(f.ID)
	s.mu.Unlock()

	// Everything from the SELECT to the UID list reads the one mailbox, so
	// it holds the session's mailbox lock throughout.
	var (
		st         imapSelect
		vanished   []uint32
		from       = uint32(1)
		list       []imapMeta
		flags      []imapMeta
		flagErr    error
		serverUIDs []uint32
		haveUIDSet bool
	)
	err := cli.inBox(func() error {
		var err error
		st, vanished, err = cli.selectSync(remote, true, meta)
		if err != nil {
			return err
		}
		if meta.UIDValidity != 0 && st.UIDValidity != 0 && meta.UIDValidity != st.UIDValidity {
			s.mu.Lock()
			s.dropFolderMessagesLocked(f.ID)
			s.mu.Unlock()
			meta = folderMeta{}
		}
		if meta.UIDNext > 1 {
			from = meta.UIDNext
		}
		list, err = cli.uidFetchMeta(from)
		if err != nil {
			return err
		}
		flags, flagErr = cli.uidFetchFlags(1, meta.HighestMod)
		// A message the flags name that the cache lacks — undeleted in
		// another client, or missed — is fetched now, older than it is.
		if flagErr == nil {
			var missing []uint32
			s.mu.Lock()
			for _, im := range flags {
				if im.UID != 0 && im.UID < from && !imapFlagDeleted(im.Flags) && !s.feat.pendingUID(f.ID, im.UID) {
					if _, ok := s.indexLocked(MessageID(fmt.Sprintf("%s:%d", f.ID, im.UID))); !ok {
						missing = append(missing, im.UID)
					}
				}
			}
			s.mu.Unlock()
			if len(missing) > 0 && len(missing) <= 500 {
				if more, err := cli.uidFetchMetaSet(uidSetString(sortedUIDs(missing))); err == nil {
					list = append(list, more...)
				}
			}
		}

		// Deletion reconciliation. QRESYNC servers tell us what vanished; the
		// rest need an explicit UID-set diff, or messages deleted from another
		// client stay in the cache forever (and later UID commands address a
		// message that no longer exists). VANISHED only covers what changed
		// since the saved modseq, so a deletion missed once — a sync that died
		// after saving the watermark, a cache from before QRESYNC — is never
		// reported again. When the cache and the server disagree on how many
		// messages the folder holds, diff anyway.
		if !cli.has("QRESYNC") || s.cachedAfterSync(f.ID, from, vanished, len(list)) != st.Exists {
			if uids, err := cli.uidList(); err == nil {
				serverUIDs = uids
				haveUIDSet = true
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	added := 0
	maxUID := meta.UIDNext
	for _, im := range list {
		if im.UID == 0 {
			continue
		}
		if im.UID >= maxUID {
			maxUID = im.UID + 1
		}
		id := MessageID(fmt.Sprintf("%s:%d", f.ID, im.UID))
		if _, ok := s.indexLocked(id); ok {
			continue
		}
		if imapFlagDeleted(im.Flags) {
			continue // marked deleted by another client: gone, as it shows there
		}
		if j, ok := s.localCopyLocked(f.ID, im.RFCMessageID); ok {
			// A message written here (a draft, a sent copy) that the server
			// did not give a UID for when it was appended: this is it.
			s.rekeyMovedLocked(j, f.ID, im.UID)
			continue
		}
		m := Message{
			ID: id, Folder: f.ID, AccountID: f.AccountID,
			From: im.From, To: im.To, Cc: im.Cc, Subject: im.Subject,
			Date: im.Date, Size: im.Size, UID: im.UID,
			Read: imapFlagSeen(im.Flags), Starred: imapFlagStar(im.Flags),
			Answered: imapFlagAnswered(im.Flags), Forwarded: imapFlagForwarded(im.Flags),
			Tags: keywordTags(im.Flags, s.tags), Parts: im.Parts,
			RFCMessageID: im.RFCMessageID, InReplyTo: im.InReplyTo,
		}
		m.ThreadID = ThreadIDOf(m)
		m.Keywords = append([]string(nil), m.Tags...)
		if s.feat != nil {
			m.Category = messageCategory(m, s.feat.snap())
		}
		for _, p := range im.Parts {
			if p.Filename != "" {
				m.HasAttach = true
				m.Attachments = append(m.Attachments, p.Filename)
			}
		}
		applyAutomaticTags(&m)
		s.Messages = append(s.Messages, m)
		s.applyRulesOnLocked(&s.Messages[len(s.Messages)-1])
		added++
	}

	for _, uid := range vanished {
		s.dropUIDLocked(f.ID, uid)
	}
	if haveUIDSet {
		live := make(map[uint32]bool, len(serverUIDs))
		for _, u := range serverUIDs {
			live[u] = true
		}
		for i := len(s.Messages) - 1; i >= 0; i-- {
			m := s.Messages[i]
			if m.Folder != f.ID || m.UID == 0 {
				continue
			}
			if live[m.UID] {
				continue
			}
			if m.UID >= maxUID {
				// Arrived after the listing; keep it.
				continue
			}
			if s.feat != nil && s.feat.pendingFor(m.ID) {
				continue
			}
			s.removeRawLocked(m)
			s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
		}
	}
	if flagErr == nil {
		for _, im := range flags {
			id := MessageID(fmt.Sprintf("%s:%d", f.ID, im.UID))
			if s.feat != nil && (s.feat.pendingFor(id) || s.feat.pendingRead(f.ID, im.UID)) {
				continue
			}
			if imapFlagDeleted(im.Flags) {
				// Marked deleted elsewhere: it leaves the lists now, and
				// comes back if it is undeleted before it is expunged.
				if i, ok := s.indexLocked(id); ok {
					m := s.Messages[i]
					s.removeRawLocked(m)
					s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
				}
				continue
			}
			if i, ok := s.indexLocked(id); ok {
				s.Messages[i].Read = imapFlagSeen(im.Flags)
				s.Messages[i].Starred = imapFlagStar(im.Flags)
				s.Messages[i].Answered = imapFlagAnswered(im.Flags)
				s.Messages[i].Forwarded = imapFlagForwarded(im.Flags)
				mergeServerTags(&s.Messages[i], keywordTags(im.Flags, s.tags))
			}
		}
	}
	if added > 0 {
		// Re-derive conversation roots across the whole cache: a reply that
		// arrives before its parent (or a reply to a reply) resolves to the
		// wrong root when it is threaded in isolation.
		assignThreadIDs(s.Messages)
	}
	s.saveFolderMeta(f.ID, folderMeta{
		UIDValidity: st.UIDValidity, UIDNext: maxUID, HighestMod: st.HighestMod, Remote: remote,
	})
	return added, nil
}

// localCopyLocked finds a message in folder that was written here and has
// no server UID yet, by its Message-ID.
func (s *LocalStore) localCopyLocked(folder FolderID, rfcID string) (int, bool) {
	rfcID = strings.TrimSpace(rfcID)
	if rfcID == "" {
		return 0, false
	}
	for i, m := range s.Messages {
		if m.Folder == folder && m.UID == 0 && strings.TrimSpace(m.RFCMessageID) == rfcID {
			return i, true
		}
	}
	return 0, false
}

// cachedAfterSync is how many messages the folder will hold once a sync
// applies vanished and adds the listed messages from UID from onwards: the
// number to hold against the server's EXISTS.
func (s *LocalStore) cachedAfterSync(folder FolderID, from uint32, vanished []uint32, listed int) int {
	gone := make(map[uint32]bool, len(vanished))
	for _, u := range vanished {
		gone[u] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := listed
	for _, m := range s.Messages {
		if m.Folder == folder && m.UID != 0 && m.UID < from && !gone[m.UID] {
			n++
		}
	}
	return n
}

// dropUIDLocked removes one cached message by folder+UID unless a local
// mutation for it is still queued.
func (s *LocalStore) dropUIDLocked(folder FolderID, uid uint32) {
	id := MessageID(fmt.Sprintf("%s:%d", folder, uid))
	if s.feat != nil && s.feat.pendingFor(id) {
		return
	}
	i, ok := s.indexLocked(id)
	if !ok {
		return
	}
	s.removeRawLocked(s.Messages[i])
	s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
}
func (s *LocalStore) dropFolderMessagesLocked(id FolderID) {
	out := s.Messages[:0]
	for _, m := range s.Messages {
		if m.Folder != id {
			out = append(out, m)
		}
	}
	s.Messages = out
}

func (s *LocalStore) Unread(folder FolderID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.Messages {
		if m.Read {
			continue
		}
		f, ok := s.folderLocked(m.Folder)
		kind := FolderCustom
		if ok {
			kind = f.Kind
		}
		if IsVirtual(folder) {
			if matchVirtual(folder, m, f, kind, s.feat.snap()) {
				n++
			}
			continue
		}
		if m.Folder == folder {
			n++
		}
	}
	return n
}

func (s *LocalStore) UnreadTotal() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.Messages {
		if !m.Read {
			n++
		}
	}
	return n
}

func (s *LocalStore) MessageCount(folder FolderID) int {
	return len(s.ListMessages(folder))
}

func (s *LocalStore) Identities(accountID string) []Identity {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Identity
	for _, id := range s.identities {
		if accountID == "" || id.AccountID == accountID {
			out = append(out, id)
		}
	}
	return out
}

func (s *LocalStore) PutIdentity(id Identity) (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id.ID == "" {
		id.ID = slug(id.Address)
	}
	s.identities = upsertIdentity(s.identities, id)
	s.saveLocked()
	return id, nil
}

func (s *LocalStore) DeleteIdentity(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.identities[:0]
	for _, x := range s.identities {
		if x.ID != id {
			out = append(out, x)
		}
	}
	s.identities = out
	s.saveLocked()
	return nil
}

func (s *LocalStore) ListTags() []Tag {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags = mergeTagStore(s.tags)
	return cloneTags(s.tags)
}

func (s *LocalStore) PutTag(t Tag) (Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags = mergeTagStore(s.tags)
	prev := strings.TrimSpace(t.Previous)
	t.Previous = ""
	if prev != "" && !strings.EqualFold(prev, t.Name) {
		next, err := renameTag(s.tags, prev, t)
		if err != nil {
			return Tag{}, err
		}
		s.tags = next
		for i := range s.Messages {
			s.Messages[i].Tags = replaceTagName(s.Messages[i].Tags, prev, t.Name)
		}
		s.saveLocked()
		return t, nil
	}
	s.tags = upsertTag(s.tags, t)
	if got, ok := TagByName(s.tags, t.Name); ok {
		t = got
	}
	s.saveLocked()
	return t, nil
}

func (s *LocalStore) DeleteTag(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags = mergeTagStore(s.tags)
	next, err := removeTag(s.tags, name)
	if err != nil {
		return err
	}
	s.tags = next
	for i := range s.Messages {
		s.Messages[i].Tags = dropTag(s.Messages[i].Tags, name)
	}
	s.saveLocked()
	return nil
}

func (s *LocalStore) VirtualFolders() []Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	base := []Folder{
		{ID: FolderUnifiedInbox, AccountID: AccountUnified, Name: "Unified Inbox", Kind: FolderInbox, Virtual: true, MatchKind: FolderInbox},
		{ID: FolderUnifiedUnread, AccountID: AccountUnified, Name: "Unread", Kind: FolderCustom, Virtual: true},
		{ID: FolderUnifiedStarred, AccountID: AccountUnified, Name: "Starred", Kind: FolderCustom, Virtual: true},
	}
	out := append(base, extraVirtualFolders(s.feat.snap())...)
	for _, t := range s.tags {
		out = append(out, Folder{
			ID: TagFolderID(t.Name), AccountID: AccountTags, Name: t.Name,
			Kind: FolderCustom, Virtual: true, Tag: t.Name,
		})
	}
	return out
}

func (s *LocalStore) ListRules() []FilterRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRules(s.rules)
}

func (s *LocalStore) PutRule(r FilterRule) (FilterRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == "" {
		r.ID = nextRuleID(s.rules)
	}
	found := false
	for i, x := range s.rules {
		if x.ID == r.ID {
			s.rules[i] = r
			found = true
			break
		}
	}
	if !found {
		s.rules = append(s.rules, r)
	}
	s.saveLocked()
	return r, nil
}

func (s *LocalStore) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.rules[:0]
	for _, r := range s.rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	s.rules = out
	s.saveLocked()
	return nil
}

func (s *LocalStore) ApplyRules(folder FolderID) (int, error) {
	s.mu.Lock()
	n := 0
	for i := range s.Messages {
		if folder != "" && s.Messages[i].Folder != folder && !IsVirtual(folder) {
			continue
		}
		if s.applyRulesOnLocked(&s.Messages[i]) {
			n++
		}
	}
	s.saveLocked()
	s.mu.Unlock()
	if n > 0 && s.feat != nil && s.feat.Online() {
		_, _ = s.flushOutbox(func(op OutboxOp) bool { return op.Kind == "flag" || op.Kind == "move" })
	}
	return n, nil
}

// applyRulesOnLocked runs the rules on m. What they change goes to the
// server too — a move as a server move, flags and tags as a flag change —
// queued, since this runs under the store lock in the middle of a sync;
// the queue is replayed when the sync ends. (A rule's move and marks used
// to stay in the cache: the server kept the message in the Inbox, and a
// later delete addressed its UID in the wrong folder.)
func (s *LocalStore) applyRulesOnLocked(m *Message) bool {
	before := m.Clone()
	changed := false
	for _, r := range s.rules {
		if !r.match(*m) {
			continue
		}
		stop, err := applyRuleActions(s, m, r.Actions)
		if err == nil {
			changed = true
		}
		if stop || r.Stop {
			break
		}
	}
	if !changed {
		return false
	}
	applyAutomaticTags(m)
	if m.UID != 0 && m.AccountID != LocalAccountID {
		if patch, add, rem := flagChanges(before, *m); len(add)+len(rem) > 0 {
			s.queueFlagsLocked(*m, patch, add, rem, "")
		}
	}
	return true
}

// flagChanges is the flag change from a to b: the patch and the IMAP flags
// and keywords it sets and clears.
func flagChanges(a, b Message) (FlagPatch, []string, []string) {
	var patch FlagPatch
	var add, rem []string
	set := func(was, now bool, flag string, p **bool) {
		if was == now {
			return
		}
		*p = BoolPtr(now)
		if now {
			add = append(add, flag)
		} else {
			rem = append(rem, flag)
		}
	}
	set(a.Read, b.Read, `\Seen`, &patch.Read)
	set(a.Starred, b.Starred, `\Flagged`, &patch.Starred)
	var tagsChanged bool
	for _, t := range b.Tags {
		if !IsSystemTag(t) && !HasTag(a.Tags, t) {
			add = append(add, tagKeyword(t))
			tagsChanged = true
		}
	}
	for _, t := range a.Tags {
		if !IsSystemTag(t) && !HasTag(b.Tags, t) {
			rem = append(rem, tagKeyword(t))
			tagsChanged = true
		}
	}
	if tagsChanged {
		tags := append([]string(nil), b.Tags...)
		patch.Tags = &tags
	}
	return patch, add, rem
}

func (s *LocalStore) deleteOne(id MessageID) error {
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("mail: no message %s", id)
	}
	m := s.Messages[i].Clone()
	f, _ := s.folderLocked(m.Folder)
	s.removeRawLocked(m)
	s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
	s.mu.Unlock()
	if err := s.expungeOne(m, f); err != nil {
		s.queueFailed(m, "delete", "", err)
	}
	return nil
}

// moveOne is a rule's move: the cache now, the server by a queued move
// (replayed from where the server has the message, which re-keys it).
// The caller holds s.mu.
func (s *LocalStore) moveOne(id MessageID, dest FolderID) error {
	i, ok := s.indexLocked(id)
	if !ok {
		return fmt.Errorf("mail: no message %s", id)
	}
	m := s.Messages[i]
	df, ok := s.folderLocked(dest)
	if !ok || df.Virtual {
		return fmt.Errorf("mail: no folder %s", dest)
	}
	if df.AccountID != m.AccountID {
		return fmt.Errorf("mail: a rule cannot move mail to another account's folder")
	}
	if m.UID != 0 && s.feat != nil && m.AccountID != LocalAccountID {
		s.feat.mu.Lock()
		s.feat.enqueueLocked(s.serverMoveOpLocked(m, dest))
		s.feat.mu.Unlock()
	}
	s.Messages[i].Folder = dest
	return nil
}

func (s *LocalStore) indexOf(id MessageID) (int, bool) { return s.indexLocked(id) }

// folderByPath is ruleHost's: the caller holds s.mu.
func (s *LocalStore) folderByPath(account, path string) (FolderID, bool) {
	return folderByPath(s.Folders, account, path)
}
func (s *LocalStore) messageAt(i int) *Message { return &s.Messages[i] }

// GetPart returns one decoded MIME section. The cached .eml is re-walked so
// attachments come back as their actual bytes; previously a non-text part
// resolved to an empty PartData (and "Save As" wrote the text body under the
// attachment's name).
func (s *LocalStore) GetPart(id MessageID, partID string) (PartData, error) {
	if !ValidPartID(partID) {
		return PartData{}, fmt.Errorf("mail: illegal part id %q", truncate(partID, 32))
	}
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return PartData{}, fmt.Errorf("mail: no message %s", id)
	}
	m := s.Messages[i].Clone()
	raw := s.readRawLocked(m)
	s.mu.Unlock()

	if len(raw) == 0 {
		fetched, err := s.fetchRaw(m)
		if err == nil && len(fetched) > 0 {
			raw = fetched
			s.mu.Lock()
			if j, ok := s.indexLocked(id); ok {
				s.applyRawLocked(j, raw)
			}
			s.mu.Unlock()
		}
	}
	if len(raw) > 0 {
		if p, ok := PartFromRaw(raw, partID); ok {
			return p, nil
		}
	}
	// No cached blob: ask the server for just this section.
	if m.UID != 0 {
		s.mu.Lock()
		f, hasFolder := s.folderLocked(m.Folder)
		s.mu.Unlock()
		if hasFolder {
			if cli, err := s.fgClient(m.AccountID); err == nil {
				var b []byte
				err := cli.inBox(func() error {
					if err := selectFor(cli, f, true); err != nil {
						return err
					}
					var err error
					b, err = cli.uidFetchSection(m.UID, partID)
					return err
				})
				s.noteNetwork(m.AccountID, err)
				if len(b) > 0 {
					p := partByID(m.Parts, partID)
					p.Data = b
					return p, nil
				}
			}
		}
	}
	if len(raw) == 0 {
		return PartData{}, fmt.Errorf("mail: no body for %s", id)
	}
	return PartData{}, fmt.Errorf("mail: no part %s", partID)
}
func partByID(parts []Part, id string) PartData {
	for _, p := range parts {
		if p.ID == id {
			return PartData{Part: p}
		}
	}
	return PartData{Part: Part{ID: id}}
}

// OpenPart writes the part to the cache dir and hands it to the desktop
// opener. The filename is attacker-controlled, so it is reduced to a base
// name, kept inside the cache directory, written 0600, and refused outright
// for types the desktop would execute.
func (s *LocalStore) OpenPart(id MessageID, partID string) (PartData, error) {
	p, err := s.GetPart(id, partID)
	if err != nil {
		return p, err
	}
	name := AttachFileName(p.Filename)
	if name == "attachment" {
		// An unnamed part (an invite's calendar object) is named for what
		// it is, so the desktop knows what opens it.
		name = AttachFileName(safeID(string(id)) + "-" + safeID(partID) + extForMIME(p.MIMEType))
	}
	if unsafeAttachmentName(name) {
		return p, fmt.Errorf("mail: refusing to open %q — save it and inspect it instead", name)
	}
	path, err := underRoot(s.dir, "open", name)
	if err != nil {
		return p, err
	}
	RemoveOld(filepath.Join(filepath.Dir(path), "*"), OpenedKeep)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return p, err
	}
	if len(p.Data) > 0 {
		if err := WriteFileAtomic(path, p.Data, 0o600); err != nil {
			return p, err
		}
	} else if _, statErr := os.Stat(path); statErr != nil {
		return p, fmt.Errorf("mail: part %s has no data to open", partID)
	}
	p.Path = path
	p.Opened = openCachedFile(path)
	return p, nil
}

// extForMIME is the file extension for an unnamed part of a known type.
func extForMIME(mt string) string {
	switch strings.ToLower(mt) {
	case "text/calendar", "application/ics":
		return ".ics"
	case "text/plain":
		return ".txt"
	case "text/vcard", "text/x-vcard":
		return ".vcf"
	case "application/pdf":
		return ".pdf"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "message/rfc822":
		return ".eml"
	}
	return ""
}

// unsafeAttachmentName flags extensions a desktop handler would run.
func unsafeAttachmentName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".desktop", ".exe", ".com", ".bat", ".cmd", ".scr", ".pif", ".msi",
		".sh", ".bash", ".zsh", ".run", ".appimage", ".jar", ".js", ".jse",
		".vbs", ".vbe", ".wsf", ".wsh", ".ps1", ".psm1", ".hta", ".lnk",
		".reg", ".scf", ".url", ".html", ".htm", ".xhtml", ".svg", ".mhtml":
		return true
	}
	return false
}
func (s *LocalStore) UnreadAll() int { return s.UnreadTotal() }

func (s *LocalStore) indexLocked(id MessageID) (int, bool) {
	for i, m := range s.Messages {
		if m.ID == id {
			return i, true
		}
	}
	return -1, false
}

func (s *LocalStore) specialLocked(accountID string, kind FolderKind) (Folder, bool) {
	for _, f := range s.Folders {
		if f.AccountID == accountID && f.Kind == kind && !f.Virtual {
			return f, true
		}
	}
	return Folder{}, false
}

func (s *LocalStore) defaultIdentLocked(accountID string) Identity {
	var first Identity
	for _, id := range s.identities {
		if id.AccountID != accountID {
			continue
		}
		if first.ID == "" {
			first = id
		}
		if id.Default {
			return id
		}
	}
	return first
}

// client returns a connected IMAP session for accountID.
//
// It must be called WITHOUT s.mu held: connecting (and the TLS handshake,
// and LOGIN) is network I/O, and holding the store lock across it used to
// freeze every unrelated RPC behind one slow server.
func (s *LocalStore) client(accountID string) (*imapClient, error) {
	return s.clientIn(s.clients, accountID, false)
}

// fgClient is client's counterpart for foreground fetches: its own session,
// so fetching a body never waits for a sync to finish with the shared one.
//
// Its timeouts are shorter than a sync's: someone is looking at "Loading
// message…", and a dead network should say so in seconds, not minutes.
func (s *LocalStore) fgClient(accountID string) (*imapClient, error) {
	if err := s.reachable(accountID); err != nil {
		return nil, err
	}
	return s.clientIn(s.fgClients, accountID, true)
}

const (
	fgIMAPDialTimeout = 10 * time.Second
	fgIMAPCmdTimeout  = 45 * time.Second
	// unreachableFor is how long after a network failure foreground fetches
	// fail at once. Background sync keeps trying and clears it on success.
	unreachableFor = 15 * time.Second
)

// UnreachableError is a fetch refused because the account's server failed
// at the network level moments ago.
type UnreachableError struct {
	AccountID string
	Retry     time.Duration
	Cause     string
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("can't reach the mail server (%s); trying again in %ds",
		e.Cause, int((e.Retry+time.Second-1)/time.Second))
}

func (s *LocalStore) reachable(accountID string) error {
	s.mu.Lock()
	since, down := s.downSince[accountID]
	cause := "network error"
	if s.health != nil {
		cause = truncate(s.health.Error(), 80)
	}
	s.mu.Unlock()
	if !down {
		return nil
	}
	left := unreachableFor - time.Since(since)
	if left <= 0 {
		return nil
	}
	return &UnreachableError{AccountID: accountID, Retry: left, Cause: cause}
}

// noteNetwork records how a network operation for accountID went: a
// network-level failure opens the fail-fast window, a success closes it,
// and an answer from the server (NO, BAD) leaves it as it was.
func (s *LocalStore) noteNetwork(accountID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		if since, was := s.downSince[accountID]; was {
			Logf("%s: connected again (down %s)", accountID, time.Since(since).Round(time.Second))
		}
		delete(s.downSince, accountID)
		return
	}
	if isNetworkError(err) {
		if _, was := s.downSince[accountID]; !was {
			Logf("%s: connection lost: %v", accountID, err)
			s.downSince[accountID] = time.Now()
		}
		s.health = err
	}
}

// isNetworkError is a failure of the connection rather than an answer from
// the server: a dial, a TLS handshake, a timeout, a session lost mid-reply.
func isNetworkError(err error) bool {
	var lost *imapConnError
	if errors.As(err, &lost) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

func (s *LocalStore) clientIn(pool map[string]*imapClient, accountID string, fg bool) (*imapClient, error) {
	s.mu.Lock()
	existing := pool[accountID]
	cfg, ok := s.accountCfgLocked(accountID)
	s.mu.Unlock()
	if existing != nil {
		if err := existing.connect(); err != nil {
			s.noteNetwork(accountID, err)
			return nil, err
		}
		return existing, nil
	}
	if !ok {
		return nil, fmt.Errorf("mail: no account %s", accountID)
	}
	if cfg.IsPOP3() {
		return nil, fmt.Errorf("mail: account %s is POP3 (no IMAP session)", accountID)
	}
	if cfg.IMAP.Host == "" {
		return nil, fmt.Errorf("mail: no IMAP host for %s", accountID)
	}
	if cfg.IMAP.locked {
		return nil, ErrLocked
	}
	cfg.IMAP.tokenKey = accountID
	c := newIMAPClient(cfg.IMAP, cfg.Address)
	if fg {
		c.dialTimeout, c.cmdTimeout = fgIMAPDialTimeout, fgIMAPCmdTimeout
	}
	if err := c.connect(); err != nil {
		s.setHealth(err)
		s.noteNetwork(accountID, err)
		return nil, err
	}
	s.mu.Lock()
	if prev := pool[accountID]; prev != nil {
		// Another goroutine won the race; keep one session per account.
		s.mu.Unlock()
		c.close()
		return prev, nil
	}
	pool[accountID] = c
	s.mu.Unlock()
	return c, nil
}

func (s *LocalStore) setHealth(err error) {
	s.mu.Lock()
	s.health = err
	s.mu.Unlock()
}

// selectFor selects the remote mailbox backing f (network I/O; no store lock).
func selectFor(cli *imapClient, f Folder, readOnly bool) error {
	remote := f.Remote
	if remote == "" {
		remote = f.Name
	}
	_, err := cli.selectBox(remote, readOnly)
	return err
}

func remoteName(f Folder) string {
	if f.Remote != "" {
		return f.Remote
	}
	return f.Name
}
func (s *LocalStore) accountCfg(accountID string) (AccountConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accountCfgLocked(accountID)
}

func (s *LocalStore) accountCfgLocked(accountID string) (AccountConfig, bool) {
	for _, a := range s.cfg.Accounts {
		id := a.ID
		if id == "" {
			id = slug(a.Address)
		}
		if id == accountID {
			return s.withSecrets(id, a), true
		}
	}
	return AccountConfig{}, false
}
func (s *LocalStore) loadLocked() {
	if s.feat == nil {
		s.feat = newFeatureHost()
	}
	c, err := openSQLCache(s.dir)
	if err != nil {
		// A database that will not open is set aside and a fresh one takes
		// its place: the server has the mail, and the next sync refills it.
		for _, ext := range []string{"", "-wal", "-shm"} {
			quarantine(filepath.Join(s.dir, dbFileName+ext))
		}
		s.health = fmt.Errorf("mail: the cache database could not be opened (%v); moved it to %s.corrupt, re-sync to refill", err, dbFileName)
		if c, err = openSQLCache(s.dir); err != nil {
			s.health = fmt.Errorf("mail: cache database: %w; running without a cache", err)
			return
		}
	}
	s.sqlc = c
	if err := s.loadSQL(); err != nil {
		s.health = fmt.Errorf("mail: reading the cache database: %w; re-sync to refill", err)
	}
	for i := range s.Messages {
		if c.text[s.Messages[i].ID] != textStamp(&s.Messages[i]) {
			// Text the search index does not have yet (a cache from before
			// it, or one saved without it): indexed now, once.
			s.saveLocked()
			break
		}
	}
	// Fold the write-ahead log into the database and cut it back, so a log
	// grown by a large write before (a first index, a big sync) does not
	// keep its size on disk until SQLite happens to reset it.
	_, _ = c.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	s.tags = mergeTagStore(s.tags)
	RemoveOld(filepath.Join(s.dir, "open", "*"), OpenedKeep)
	assignThreadIDs(s.Messages)
	s.feat.setContacts(buildContacts(s.Messages))
	for _, m := range s.Messages {
		if n := idSeq(m.ID); n >= s.nextID {
			s.nextID = n + 1
		}
	}
}

// saveLocked persists what changed in the cache to mail.db, in one
// transaction (sqlstore.go).
func (s *LocalStore) saveLocked() {
	if s.sqlc == nil {
		return
	}
	if err := s.saveSQL(); err != nil {
		s.health = fmt.Errorf("mail: saving the cache: %w", err)
	}
}

// rawPath is the .eml blob for m. Both the account id and the message id are
// folded to safe segments and the result is checked against the data root,
// so a hostile accounts.put / message id cannot write outside the cache.
func (s *LocalStore) rawPath(m Message) (string, error) {
	acct := safeID(string(m.AccountID))
	name := safeID(string(m.ID))
	if name == "acct" {
		name = "msg"
	}
	return underRoot(s.dir, "raw", acct, name+".eml")
}

// WriteRawLocked writes a message's raw RFC 5322 bytes into the cache.
// The caller holds the store lock.
func (s *LocalStore) WriteRawLocked(m Message, raw []byte) {
	p, err := s.rawPath(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = WriteFileAtomic(p, raw, 0o600)
}
func (s *LocalStore) readRawLocked(m Message) []byte {
	p, err := s.rawPath(m)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return b
}

// removeRawLocked drops the cached blob (used when a message is re-keyed
// after a server-side MOVE).
func (s *LocalStore) removeRawLocked(m Message) {
	if p, err := s.rawPath(m); err == nil {
		_ = os.Remove(p)
	}
}

// loadFolderMeta is how far folder id has synced. The caller holds s.mu.
func (s *LocalStore) loadFolderMeta(id FolderID) folderMeta {
	var m folderMeta
	if s.sqlc == nil {
		return m
	}
	var raw []byte
	if err := s.sqlc.db.QueryRow("SELECT data FROM folder_meta WHERE folder = ?", string(id)).Scan(&raw); err != nil {
		return m
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return folderMeta{}
	}
	return m
}

// saveFolderMeta records how far folder id has synced. The caller holds s.mu.
func (s *LocalStore) saveFolderMeta(id FolderID, m folderMeta) {
	if s.sqlc == nil {
		return
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	_, _ = s.sqlc.db.Exec("INSERT INTO folder_meta(folder, data) VALUES(?, ?) ON CONFLICT(folder) DO UPDATE SET data = excluded.data", string(id), raw)
}

// dropFolderMetaLocked forgets how far folder id had synced.
func (s *LocalStore) dropFolderMetaLocked(id FolderID) {
	if s.sqlc != nil {
		_, _ = s.sqlc.db.Exec("DELETE FROM folder_meta WHERE folder = ?", string(id))
	}
}
func idSeq(id MessageID) int {
	s := string(id)
	i := strings.LastIndex(s, "-")
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range s[i+1:] {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// SendViaSMTP builds RFC822 and submits, then APPENDs to Sent.
// SendViaSMTP builds the RFC822 message and submits it. When the account is
// offline the message (with its attachments) is queued instead; a transport
// failure queues it once and reports the error — it no longer both queues a
// copy and errors, which used to send the message twice after a user retry.
func (s *LocalStore) SendViaSMTP(accountID, identityID string, msg Message, files []AttachedFile) (MessageID, error) {
	return s.sendViaSMTP(accountID, identityID, msg, files, false)
}

// sendViaSMTP is SendViaSMTP. From the Outbox (retry) a failure is only
// reported: the op being retried stays queued, and queueing the message
// again sent it twice once the network came back.
func (s *LocalStore) sendViaSMTP(accountID, identityID string, msg Message, files []AttachedFile, retry bool) (MessageID, error) {
	s.mu.Lock()
	cfg, ok := s.accountCfgLocked(accountID)
	ident := s.defaultIdentLocked(accountID)
	for _, id := range s.identities {
		if id.ID == identityID {
			ident = id
			if id.AccountID != "" {
				accountID = id.AccountID
				if c, ok2 := s.accountCfgLocked(accountID); ok2 {
					cfg = c
					ok = true
				}
			}
		}
	}
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("mail: no SMTP account %s", accountID)
	}
	if msg.From == "" {
		msg.From = ident.DisplayFrom()
	}
	raw, err := BuildRFC822Strict(msg, ident, files)
	if err != nil {
		return "", err
	}
	rcpts := append(splitAddrs(msg.To), splitAddrs(msg.Cc)...)
	rcpts = append(rcpts, splitAddrs(msg.Bcc)...)
	if len(rcpts) == 0 {
		return "", fmt.Errorf("mail: no recipient")
	}
	cfg.SMTP.tokenKey = accountID

	queue := func(id MessageID, sendErr string) {
		if s.feat == nil {
			return
		}
		cp := msg.Clone()
		s.feat.mu.Lock()
		s.feat.enqueueLocked(OutboxOp{
			Kind: "send", AccountID: accountID, IdentityID: ident.ID,
			MessageID: id, Message: &cp, Attachments: files, Error: sendErr,
		})
		s.feat.mu.Unlock()
		s.mu.Lock()
		s.saveLocked()
		s.mu.Unlock()
	}

	if s.feat != nil && !s.feat.Online() && !retry {
		id, err := s.Append(FolderOutbox, msg)
		if err != nil {
			s.mu.Lock()
			s.nextID++
			id = MessageID(fmt.Sprintf("%s-out-%04d", safeID(accountID), s.nextID))
			msg.ID = id
			msg.AccountID = accountID
			s.Messages = append(s.Messages, msg)
			s.mu.Unlock()
		}
		queue(id, "")
		return id, nil
	}
	if cfg.SMTP.locked {
		err = ErrLocked // it waits in the Outbox until comms-mail is unlocked
	} else {
		err = SendSMTP(cfg.SMTP, ExtractAddr(msg.From), rcpts, raw)
	}
	if err != nil {
		if !transientSendError(err) || retry {
			Logf("send from %s: %v", accountID, err)
			return "", err // retrying would fail the same way, or it is a retry
		}
		Logf("send from %s: %v — queued in the Outbox", accountID, err)
		queue("", err.Error())
		return "", &QueuedError{Reason: err.Error()}
	}
	sent, ok := specialFolder(s, accountID, FolderSent)
	if !ok {
		return "", nil
	}
	return s.Append(sent.ID, msg)
}

// hasConfiguredAccounts reports whether any account has real server config.
func (s *LocalStore) hasConfiguredAccounts() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cfg.Accounts) > 0
}

func (s *LocalStore) extras() *featureHost {
	if s.feat == nil {
		s.feat = newFeatureHost()
	}
	return s.feat
}

func (s *LocalStore) SetOnline(v bool)       { s.extras().SetOnline(v) }
func (s *LocalStore) Online() bool           { return s.extras().Online() }
func (s *LocalStore) ListOutbox() []OutboxOp { return s.extras().ListOutbox() }
func (s *LocalStore) ListSmartFolders() []SmartFolder {
	return s.extras().ListSmartFolders()
}
func (s *LocalStore) PutSmartFolder(sf SmartFolder) (SmartFolder, error) {
	out, err := s.extras().PutSmartFolder(sf)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return out, err
}
func (s *LocalStore) DeleteSmartFolder(id string) error {
	err := s.extras().DeleteSmartFolder(id)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return err
}
func (s *LocalStore) MuteThread(id string, muted bool) error {
	err := s.extras().MuteThread(id, muted)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return err
}
func (s *LocalStore) MutedThreads() []string         { return s.extras().MutedThreads() }
func (s *LocalStore) InviteAnswer(key string) string { return s.extras().InviteAnswer(key) }

// ImportContacts adds contacts to the address book, kept across restarts.
func (s *LocalStore) ImportContacts(cs []Contact) int {
	n := s.extras().ImportContacts(cs)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return n
}
func (s *LocalStore) RemoteImageSenders() []string { return s.extras().RemoteImageSenders() }
func (s *LocalStore) AllowRemoteImages(address string, allow bool) error {
	err := s.extras().AllowRemoteImages(address, allow)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return err
}
func (s *LocalStore) SetInviteAnswer(key, partstat string) error {
	err := s.extras().SetInviteAnswer(key, partstat)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return err
}
func (s *LocalStore) ListVIPs() []VIP { return s.extras().ListVIPs() }
func (s *LocalStore) PutVIP(v VIP) (VIP, error) {
	out, err := s.extras().PutVIP(v)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return out, err
}
func (s *LocalStore) DeleteVIP(address string) error {
	err := s.extras().DeleteVIP(address)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return err
}
func (s *LocalStore) NotifyPrefs() NotifyPrefs { return s.extras().NotifyPrefs() }
func (s *LocalStore) PutNotifyPrefs(p NotifyPrefs) NotifyPrefs {
	out := s.extras().PutNotifyPrefs(p)
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return out
}
func (s *LocalStore) SetSenderCategory(address, category string) error {
	err := s.extras().SetSenderCategory(address, category)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Messages {
		if canonAddr(s.Messages[i].From) == canonAddr(address) {
			s.Messages[i].Category = category
		}
	}
	s.saveLocked()
	return nil
}
func (s *LocalStore) ListSenderCategories() []SenderCat {
	return s.extras().ListSenderCategories()
}

func (s *LocalStore) FlushOutbox() (int, error) {
	return s.flushOutbox(nil)
}

// flushOutbox replays the queued ops only accepts, or all of them when
// only is nil.
func (s *LocalStore) flushOutbox(only func(OutboxOp) bool) (int, error) {
	if s.feat == nil {
		return 0, nil
	}
	if !s.flushMu.TryLock() {
		return 0, nil // a replay is already running; it takes these too
	}
	defer s.flushMu.Unlock()
	s.feat.mu.Lock()
	var ops []OutboxOp
	for _, op := range s.feat.outbox {
		if only == nil || only(op) {
			ops = append(ops, op)
		}
	}
	s.feat.mu.Unlock()
	flushed := 0
	var last error
	for _, op := range ops {
		if err := s.flushOne(op); err != nil {
			Logf("outbox %s %s (try %d): %v", op.Kind, op.ID, op.Tries+1, err)
			last = err
			s.feat.mu.Lock()
			for i := range s.feat.outbox {
				if s.feat.outbox[i].ID == op.ID {
					s.feat.outbox[i].Error = err.Error()
					s.feat.outbox[i].Tries++
				}
			}
			s.feat.mu.Unlock()
			continue
		}
		s.feat.mu.Lock()
		s.feat.dropOutboxLocked(op.ID)
		s.feat.mu.Unlock()
		flushed++
	}
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return flushed, last
}

func (s *LocalStore) flushOne(op OutboxOp) error {
	switch op.Kind {
	case "flag":
		if op.Src != "" && op.UID != 0 {
			return s.replayFlags(op)
		}
		// Queued before flag ops recorded where the message was.
		s.mu.Lock()
		i, ok := s.indexLocked(op.MessageID)
		if !ok {
			s.mu.Unlock()
			return nil // vanished; drop
		}
		m := s.Messages[i].Clone()
		f, hasFolder := s.folderLocked(m.Folder)
		s.mu.Unlock()
		if !hasFolder {
			return nil
		}
		var add, rem []string
		if op.Patch.Read != nil {
			if *op.Patch.Read {
				add = append(add, `\Seen`)
			} else {
				rem = append(rem, `\Seen`)
			}
		}
		if op.Patch.Starred != nil {
			if *op.Patch.Starred {
				add = append(add, `\Flagged`)
			} else {
				rem = append(rem, `\Flagged`)
			}
		}
		if len(op.Add) > 0 || len(op.Rem) > 0 {
			add, rem = op.Add, op.Rem
		}
		return s.pushFlags(m, f, add, rem)
	case "append":
		return s.replayAppend(op)
	case "read":
		src, _, ok := s.replayFolders(OutboxOp{Src: op.Src, UID: 1})
		if !ok || len(op.UIDs) == 0 {
			return nil
		}
		err := s.markReadOnServer(src, op.UIDs, op.UIDVal)
		if errors.Is(err, errStaleUIDs) {
			return nil // renumbered: those UIDs name other messages now
		}
		return err
	case "move":
		return s.replayMove(op)
	case "delete":
		return s.replayPurge(op)
	case "send":
		if op.Message == nil {
			return nil
		}
		// Attachments are carried in the queued op, so a message composed
		// offline still goes out with its files.
		_, err := s.sendViaSMTP(op.AccountID, op.IdentityID, *op.Message, op.Attachments, true)
		return err
	default:
		return nil
	}
}

func sortedUIDs(u []uint32) []uint32 {
	out := append([]uint32(nil), u...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
