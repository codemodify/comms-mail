package mail

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LocalStore is the production backend: on-disk cache + IMAP or POP3 sync + SMTP.
type LocalStore struct {
	mu         sync.Mutex
	dir        string
	cfg        MailConfig
	accounts   []Account
	identities []Identity
	folders    []Folder
	messages   []Message
	tags       []Tag
	rules      []FilterRule
	clients    map[string]*imapClient
	nextID     int
	health     error
	now        time.Time
	feat       *featureHost
	pushCancel func()

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
}

// SetOnChange registers the daemon's broadcast hook. fn is always called
// with no store lock held.
func (s *LocalStore) SetOnChange(fn func(StoreEvent)) {
	s.changeMu.Lock()
	s.onChange = fn
	s.changeMu.Unlock()
}

func (s *LocalStore) emit(ev StoreEvent) {
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
		dir: dir, cfg: cfg, clients: map[string]*imapClient{},
		tags: DefaultTags(), nextID: 1, now: time.Now(), feat: newFeatureHost(),
	}
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
	a = keepExistingSecrets(a, file.Accounts)
	file.Accounts = upsertAccountConfig(file.Accounts, a)
	if err := SaveConfig(file); err != nil {
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
	if c := s.clients[id]; c != nil {
		c.close()
		delete(s.clients, id)
	}
	file, _ := LoadConfig()
	file.Accounts = dropAccountConfig(file.Accounts, id)
	if err := SaveConfig(file); err != nil {
		return err
	}
	s.cfg.Accounts = dropAccountConfig(s.cfg.Accounts, id)
	s.dropAccountLocked(id)
	_ = DefaultTokenStore().Delete(id)
	if addr != "" {
		_ = DefaultTokenStore().Delete(addr)
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
	folders := s.folders[:0]
	for _, f := range s.folders {
		if f.AccountID == id {
			drop = append(drop, f.ID)
			continue
		}
		folders = append(folders, f)
	}
	s.folders = folders
	msgs := s.messages[:0]
	for _, m := range s.messages {
		if m.AccountID == id {
			if s.feat != nil && s.feat.index != nil {
				s.feat.index.remove(m.ID)
			}
			continue
		}
		msgs = append(msgs, m)
	}
	s.messages = msgs
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
		if p, err := s.folderMetaPath(fid); err == nil {
			_ = os.Remove(p)
		}
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
	for _, f := range s.folders {
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
	for _, f := range s.folders {
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
	remote := name
	if parent != "" {
		if pf, ok := s.folderLocked(parent); ok && pf.Remote != "" {
			remote = pf.Remote + "/" + name
		}
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
	id := FolderID(safeID(accountID) + "/" + safeID(name))
	f := Folder{ID: id, AccountID: accountID, Name: name, Kind: FolderCustom, Parent: parent, Remote: remote}
	s.folders = append(s.folders, f)
	s.saveLocked()
	return f, nil
}
func (s *LocalStore) ListMessages(folder FolderID) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(folder)
}

func (s *LocalStore) listLocked(folder FolderID) []Message {
	var out []Message
	for _, m := range s.messages {
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

func (s *LocalStore) GetMessage(id MessageID) (Message, bool) {
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return Message{}, false
	}
	m := s.messages[i].Clone()
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
	m := s.messages[i]
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
	s.messages[i] = parsed
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
	cli, err := s.client(m.AccountID)
	if err != nil {
		return nil, err
	}
	if err := selectFor(cli, f, true); err != nil {
		return nil, err
	}
	raw, err := cli.uidFetchRFC822(m.UID)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("mail: empty body for %s", m.ID)
	}
	s.mu.Lock()
	s.writeRawLocked(m, raw)
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
	m := s.messages[i].Clone()
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
	var idx *searchIndex
	if s.feat != nil {
		idx = s.feat.index
	}
	hits := searchMessages(s.messages, SearchQuery{AccountID: q.AccountID, Filter: q.Filter}, idx)
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
	m := &s.messages[i]
	var add, rem []string
	if patch.Read != nil {
		m.Read = *patch.Read
		if *patch.Read {
			add = append(add, `\Seen`)
		} else {
			rem = append(rem, `\Seen`)
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
		m.Tags = append([]string(nil), (*patch.Tags)...)
		for _, t := range m.Tags {
			add = append(add, imapSafeKeyword(t))
		}
	}
	syncSystemTagsFromFlags(m)
	snapshot := m.Clone()
	offline := s.feat != nil && !s.feat.Online()
	if offline {
		s.feat.mu.Lock()
		s.feat.enqueueLocked(OutboxOp{Kind: "flag", MessageID: id, Patch: patch, AccountID: m.AccountID, UID: m.UID})
		s.feat.mu.Unlock()
	}
	folder, hasFolder := s.folderLocked(snapshot.Folder)
	s.saveLocked()
	s.mu.Unlock()

	if offline || !hasFolder || snapshot.UID == 0 {
		return nil
	}
	s.pushFlags(snapshot, folder, add, rem)
	return nil
}
func imapSafeKeyword(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// pushFlags mirrors a flag change to the server. Network I/O: never called
// with the store lock held.
func (s *LocalStore) pushFlags(m Message, f Folder, add, rem []string) {
	if m.UID == 0 || (len(add) == 0 && len(rem) == 0) {
		return
	}
	cli, err := s.client(m.AccountID)
	if err != nil {
		return
	}
	if err := selectFor(cli, f, false); err != nil {
		return
	}
	_ = cli.uidStore(m.UID, add, rem)
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
		m := s.messages[i].Clone()
		src, _ := s.folderLocked(m.Folder)
		if offline {
			s.feat.mu.Lock()
			s.feat.enqueueLocked(OutboxOp{Kind: "move", MessageID: id, Dest: dest, AccountID: m.AccountID, UID: m.UID})
			s.feat.mu.Unlock()
			s.messages[i].Folder = dest
			continue
		}
		if m.UID == 0 {
			s.messages[i].Folder = dest
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

	dremote := remoteName(df)
	for _, j := range jobs {
		var newUID uint32
		var moveErr error
		cli, err := s.client(j.msg.AccountID)
		if err == nil {
			if err := selectFor(cli, j.src, false); err == nil {
				newUID, moveErr = cli.uidMove(j.msg.UID, dremote)
			} else {
				moveErr = err
			}
		} else {
			moveErr = err
		}
		s.mu.Lock()
		i, ok := s.indexLocked(j.id)
		if !ok {
			s.mu.Unlock()
			continue
		}
		if moveErr != nil {
			if s.feat != nil {
				s.feat.mu.Lock()
				s.feat.enqueueLocked(OutboxOp{Kind: "move", MessageID: j.id, Dest: dest, AccountID: j.msg.AccountID, UID: j.msg.UID, Error: moveErr.Error()})
				s.feat.mu.Unlock()
			}
			s.messages[i].Folder = dest
			s.mu.Unlock()
			continue
		}
		s.rekeyMovedLocked(i, dest, newUID)
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return nil
}

// rekeyMovedLocked applies a completed server-side move to the cache entry
// at index i. newUID == 0 means the server gave us no UID: the entry is
// removed rather than kept under a UID that names something else.
func (s *LocalStore) rekeyMovedLocked(i int, dest FolderID, newUID uint32) {
	old := s.messages[i]
	if newUID == 0 {
		s.removeRawLocked(old)
		if s.feat != nil && s.feat.index != nil {
			s.feat.index.remove(old.ID)
		}
		s.messages = append(s.messages[:i], s.messages[i+1:]...)
		return
	}
	raw := s.readRawLocked(old)
	s.removeRawLocked(old)
	m := old
	m.Folder = dest
	m.UID = newUID
	m.ID = MessageID(fmt.Sprintf("%s:%d", dest, newUID))
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.remove(old.ID)
		s.feat.index.add(m)
	}
	s.messages[i] = m
	if len(raw) > 0 {
		s.writeRawLocked(m, raw)
	}
}
func (s *LocalStore) Delete(ids []MessageID) error {
	s.mu.Lock()
	offline := s.feat != nil && !s.feat.Online()
	type purge struct {
		msg Message
		src Folder
	}
	type toTrash struct {
		id    MessageID
		msg   Message
		src   Folder
		trash Folder
	}
	var purges []purge
	var moves []toTrash
	for _, id := range ids {
		i, ok := s.indexLocked(id)
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("mail: no message %s", id)
		}
		cur, ok := s.folderLocked(s.messages[i].Folder)
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("mail: no folder for %s", id)
		}
		m := s.messages[i].Clone()
		if offline {
			s.feat.mu.Lock()
			s.feat.enqueueLocked(OutboxOp{Kind: "delete", MessageID: id, AccountID: m.AccountID, UID: m.UID})
			s.feat.mu.Unlock()
		}
		trash, hasTrash := s.specialLocked(cur.AccountID, FolderTrash)
		if cur.Kind == FolderTrash || !hasTrash {
			if !offline {
				purges = append(purges, purge{msg: m, src: cur})
			}
			s.removeRawLocked(m)
			if s.feat != nil && s.feat.index != nil {
				s.feat.index.remove(m.ID)
			}
			s.messages = append(s.messages[:i], s.messages[i+1:]...)
			continue
		}
		s.messages[i].Folder = trash.ID
		if !offline && m.UID != 0 {
			moves = append(moves, toTrash{id: id, msg: m, src: cur, trash: trash})
		}
	}
	s.saveLocked()
	s.mu.Unlock()

	for _, p := range purges {
		s.expungeOne(p.msg, p.src)
	}
	for _, mv := range moves {
		var newUID uint32
		cli, err := s.client(mv.msg.AccountID)
		if err != nil {
			continue
		}
		if err := selectFor(cli, mv.src, false); err != nil {
			continue
		}
		newUID, err = cli.uidMove(mv.msg.UID, remoteName(mv.trash))
		if err != nil {
			continue
		}
		s.mu.Lock()
		if i, ok := s.indexLocked(mv.id); ok {
			s.rekeyMovedLocked(i, mv.trash.ID, newUID)
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	return nil
}

// expungeOne permanently removes one message from the server. UID EXPUNGE is
// used when UIDPLUS is advertised so other \Deleted messages in the mailbox
// (possibly flagged by another client) survive.
func (s *LocalStore) expungeOne(m Message, f Folder) {
	if m.UID == 0 {
		return
	}
	cli, err := s.client(m.AccountID)
	if err != nil {
		return
	}
	if err := selectFor(cli, f, false); err != nil {
		return
	}
	if err := cli.uidStore(m.UID, []string{`\Deleted`}, nil); err != nil {
		return
	}
	_ = cli.expungeUID(m.UID)
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
	raw, buildErr := BuildRFC822Strict(msg, ident, nil)
	if buildErr != nil {
		s.mu.Unlock()
		return "", buildErr
	}
	s.writeRawLocked(msg, raw)
	s.messages = append(s.messages, msg)
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.add(msg)
	}
	s.saveLocked()
	accountID := f.AccountID
	s.mu.Unlock()

	// APPEND is network I/O.
	if cli, err := s.client(accountID); err == nil {
		_ = cli.appendRaw(remoteName(f), raw, `\Seen`)
	}
	return msg.ID, nil
}
func (s *LocalStore) Update(id MessageID, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.indexLocked(id)
	if !ok {
		return fmt.Errorf("mail: no message %s", id)
	}
	keep := s.messages[i]
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
	msg.UID = keep.UID
	s.messages[i] = msg
	s.saveLocked()
	return nil
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
		for _, f := range s.folders {
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
			s.emit(StoreEvent{Reason: "sync", AccountID: a.ID, Count: n})
		}
	}
	s.mu.Lock()
	s.saveLocked()
	health := s.health
	s.mu.Unlock()
	return res, health
}
func (s *LocalStore) syncAccount(accountID string) (int, error) {
	if cfg, ok := s.accountCfg(accountID); ok && cfg.IsPOP3() {
		return s.syncPOP3(accountID)
	}
	cli, err := s.client(accountID)
	if err != nil {
		return 0, err
	}
	boxes, err := cli.list()
	if err != nil {
		return 0, err
	}
	added := 0
	for _, b := range boxes {
		if b.Name == "" {
			continue
		}
		display := b.Display
		if display == "" {
			display = decodeIMAPUTF7(b.Name)
		}
		kind := folderKindFromIMAP(display, b.Attrs)
		id := FolderID(safeID(accountID) + "/" + safeID(display))
		if kind == FolderInbox {
			id = FolderID(safeID(accountID) + "/inbox")
		}
		s.mu.Lock()
		f, ok := s.folderLocked(id)
		if !ok {
			f = Folder{ID: id, AccountID: accountID, Name: displayIMAPName(display, b.Delim), Kind: kind, Remote: b.Name}
			s.folders = append(s.folders, f)
		} else {
			f.Remote = b.Name
			f.Kind = kind
			s.replaceFolderLocked(f)
		}
		s.mu.Unlock()

		n, err := s.syncFolder(cli, f)
		if err != nil {
			s.setHealth(err)
			continue
		}
		added += n
		if n > 0 {
			s.emit(StoreEvent{Reason: "fetch", AccountID: accountID, FolderID: f.ID, Count: n})
		}
	}
	return added, nil
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
	in.tokenKey = accountID

	s.mu.Lock()
	s.ensureLocalSpecialsLocked(accountID)
	inbox, ok := s.specialLocked(accountID, FolderInbox)
	haveRFC := map[string]bool{}
	for _, m := range s.messages {
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
		s.writeRawLocked(msg, raw)
		s.messages = append(s.messages, msg)
		if s.feat != nil && s.feat.index != nil {
			s.feat.index.add(msg)
		}
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
		s.folders = append(s.folders, Folder{
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
	for i, x := range s.folders {
		if x.ID == f.ID {
			s.folders[i] = f
			return
		}
	}
	s.folders = append(s.folders, f)
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

	st, vanished, err := cli.selectSync(remote, true, meta)
	if err != nil {
		return 0, err
	}
	if meta.UIDValidity != 0 && st.UIDValidity != 0 && meta.UIDValidity != st.UIDValidity {
		s.mu.Lock()
		s.dropFolderMessagesLocked(f.ID)
		s.mu.Unlock()
		meta = folderMeta{}
	}
	from := uint32(1)
	if meta.UIDNext > 1 {
		from = meta.UIDNext
	}
	list, err := cli.uidFetchMeta(from)
	if err != nil {
		return 0, err
	}
	flags, flagErr := cli.uidFetchFlags(1, meta.HighestMod)

	// Deletion reconciliation. QRESYNC servers tell us what vanished; the
	// rest need an explicit UID-set diff, or messages deleted from another
	// client stay in the cache forever (and later UID commands address a
	// message that no longer exists).
	var serverUIDs []uint32
	haveUIDSet := false
	if !cli.has("QRESYNC") {
		if uids, err := cli.uidList(); err == nil {
			serverUIDs = uids
			haveUIDSet = true
		}
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
		m := Message{
			ID: id, Folder: f.ID, AccountID: f.AccountID,
			From: im.From, To: im.To, Cc: im.Cc, Subject: im.Subject,
			Date: im.Date, Size: im.Size, UID: im.UID,
			Read: imapFlagSeen(im.Flags), Starred: imapFlagStar(im.Flags),
			Tags: imapKeywords(im.Flags), Parts: im.Parts,
			RFCMessageID: im.RFCMessageID, InReplyTo: im.InReplyTo,
		}
		m.ThreadID = ThreadIDOf(m)
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
		s.messages = append(s.messages, m)
		if s.feat != nil && s.feat.index != nil {
			s.feat.index.add(m)
		}
		s.applyRulesOnLocked(&s.messages[len(s.messages)-1])
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
		for i := len(s.messages) - 1; i >= 0; i-- {
			m := s.messages[i]
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
			if s.feat != nil && s.feat.index != nil {
				s.feat.index.remove(m.ID)
			}
			s.messages = append(s.messages[:i], s.messages[i+1:]...)
		}
	}
	if flagErr == nil {
		for _, im := range flags {
			id := MessageID(fmt.Sprintf("%s:%d", f.ID, im.UID))
			if s.feat != nil && s.feat.pendingFor(id) {
				continue
			}
			if i, ok := s.indexLocked(id); ok {
				s.messages[i].Read = imapFlagSeen(im.Flags)
				s.messages[i].Starred = imapFlagStar(im.Flags)
			}
		}
	}
	if added > 0 {
		// Re-derive conversation roots across the whole cache: a reply that
		// arrives before its parent (or a reply to a reply) resolves to the
		// wrong root when it is threaded in isolation.
		assignThreadIDs(s.messages)
	}
	s.saveFolderMeta(f.ID, folderMeta{
		UIDValidity: st.UIDValidity, UIDNext: maxUID, HighestMod: st.HighestMod, Remote: remote,
	})
	return added, nil
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
	s.removeRawLocked(s.messages[i])
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.remove(id)
	}
	s.messages = append(s.messages[:i], s.messages[i+1:]...)
}
func (s *LocalStore) dropFolderMessagesLocked(id FolderID) {
	out := s.messages[:0]
	for _, m := range s.messages {
		if m.Folder != id {
			out = append(out, m)
		}
	}
	s.messages = out
}

func (s *LocalStore) Unread(folder FolderID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
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
	for _, m := range s.messages {
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
		for i := range s.messages {
			s.messages[i].Tags = replaceTagName(s.messages[i].Tags, prev, t.Name)
		}
		s.saveLocked()
		return t, nil
	}
	s.tags = upsertTag(s.tags, t)
	if got, ok := tagByName(s.tags, t.Name); ok {
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
	for i := range s.messages {
		s.messages[i].Tags = dropTag(s.messages[i].Tags, name)
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
	defer s.mu.Unlock()
	n := 0
	for i := range s.messages {
		if folder != "" && s.messages[i].Folder != folder && !IsVirtual(folder) {
			continue
		}
		if s.applyRulesOnLocked(&s.messages[i]) {
			n++
		}
	}
	s.saveLocked()
	return n, nil
}

func (s *LocalStore) applyRulesOnLocked(m *Message) bool {
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
	if changed {
		applyAutomaticTags(m)
	}
	return changed
}

func (s *LocalStore) deleteOne(id MessageID) error {
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("mail: no message %s", id)
	}
	m := s.messages[i].Clone()
	f, _ := s.folderLocked(m.Folder)
	s.removeRawLocked(m)
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.remove(id)
	}
	s.messages = append(s.messages[:i], s.messages[i+1:]...)
	s.mu.Unlock()
	s.expungeOne(m, f)
	return nil
}
func (s *LocalStore) moveOne(id MessageID, dest FolderID) error {
	i, ok := s.indexLocked(id)
	if !ok {
		return fmt.Errorf("mail: no message %s", id)
	}
	s.messages[i].Folder = dest
	return nil
}

func (s *LocalStore) indexOf(id MessageID) (int, bool) { return s.indexLocked(id) }
func (s *LocalStore) messageAt(i int) *Message         { return &s.messages[i] }

// GetPart returns one decoded MIME section. The cached .eml is re-walked so
// attachments come back as their actual bytes; previously a non-text part
// resolved to an empty PartData (and "Save As" wrote the text body under the
// attachment's name).
func (s *LocalStore) GetPart(id MessageID, partID string) (PartData, error) {
	if !validPartID(partID) {
		return PartData{}, fmt.Errorf("mail: illegal part id %q", truncate(partID, 32))
	}
	s.mu.Lock()
	i, ok := s.indexLocked(id)
	if !ok {
		s.mu.Unlock()
		return PartData{}, fmt.Errorf("mail: no message %s", id)
	}
	m := s.messages[i].Clone()
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
			if cli, err := s.client(m.AccountID); err == nil {
				if err := selectFor(cli, f, true); err == nil {
					if b, err := cli.uidFetchSection(m.UID, partID); err == nil && len(b) > 0 {
						p := partByID(m.Parts, partID)
						p.Data = b
						return p, nil
					}
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
	name := attachFileName(p.Filename)
	if name == "attachment" {
		name = attachFileName(safeID(string(id)) + "-" + safeID(partID))
	}
	if unsafeAttachmentName(name) {
		return p, fmt.Errorf("mail: refusing to open %q — save it and inspect it instead", name)
	}
	path, err := underRoot(s.dir, "open", name)
	if err != nil {
		return p, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return p, err
	}
	if len(p.Data) > 0 {
		if err := writeFileAtomic(path, p.Data, 0o600); err != nil {
			return p, err
		}
	} else if _, statErr := os.Stat(path); statErr != nil {
		return p, fmt.Errorf("mail: part %s has no data to open", partID)
	}
	p.Path = path
	openCachedFile(path)
	return p, nil
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
	for i, m := range s.messages {
		if m.ID == id {
			return i, true
		}
	}
	return -1, false
}

func (s *LocalStore) specialLocked(accountID string, kind FolderKind) (Folder, bool) {
	for _, f := range s.folders {
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
	s.mu.Lock()
	existing := s.clients[accountID]
	cfg, ok := s.accountCfgLocked(accountID)
	s.mu.Unlock()
	if existing != nil {
		if err := existing.connect(); err != nil {
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
	cfg.IMAP.tokenKey = accountID
	c := newIMAPClient(cfg.IMAP, cfg.Address)
	if err := c.connect(); err != nil {
		s.setHealth(err)
		return nil, err
	}
	s.mu.Lock()
	if prev := s.clients[accountID]; prev != nil {
		// Another goroutine won the race; keep one session per account.
		s.mu.Unlock()
		c.close()
		return prev, nil
	}
	s.clients[accountID] = c
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
			return a, true
		}
	}
	return AccountConfig{}, false
}
func (s *LocalStore) loadLocked() {
	if s.feat == nil {
		s.feat = newFeatureHost()
	}
	type slot struct {
		name string
		dest any
	}
	slots := []slot{
		{"accounts.json", &s.accounts},
		{"identities.json", &s.identities},
		{"folders.json", &s.folders},
		{"messages.json", &s.messages},
		{"tags.json", &s.tags},
		{"rules.json", &s.rules},
		{"smart.json", &s.feat.smart},
		{"vip.json", &s.feat.vips},
		{"muted.json", &s.feat.muted},
		{"categories.json", &s.feat.cats},
		{"notify.json", &s.feat.notify},
		{"outbox.json", &s.feat.outbox},
	}
	var bad []string
	for _, sl := range slots {
		path := filepath.Join(s.dir, sl.name)
		if err := readJSONFileStrict(path, sl.dest); err != nil {
			quarantine(path)
			bad = append(bad, sl.name)
		}
	}
	if len(bad) > 0 {
		// Rebuild what we can: messages.json is reconstructible from the
		// raw/*.eml blobs the next sync re-reads.
		s.health = fmt.Errorf("mail: recovered from corrupt cache file(s) %s (moved to *.corrupt); re-sync to refill",
			strings.Join(bad, ", "))
	}
	s.tags = mergeTagStore(s.tags)
	assignThreadIDs(s.messages)
	if s.feat.index != nil {
		s.feat.index.rebuild(s.messages)
	}
	for _, m := range s.messages {
		if n := idSeq(m.ID); n >= s.nextID {
			s.nextID = n + 1
		}
	}
}

// saveLocked persists the cache with tmp+fsync+rename so a crash mid-write
// leaves the previous good file in place.
func (s *LocalStore) saveLocked() {
	_ = writeJSONFileAtomic(filepath.Join(s.dir, "accounts.json"), s.accounts)
	_ = writeJSONFileAtomic(filepath.Join(s.dir, "identities.json"), s.identities)
	_ = writeJSONFileAtomic(filepath.Join(s.dir, "folders.json"), s.folders)
	_ = writeJSONFileAtomic(filepath.Join(s.dir, "messages.json"), s.messages)
	_ = writeJSONFileAtomic(filepath.Join(s.dir, "tags.json"), s.tags)
	_ = writeJSONFileAtomic(filepath.Join(s.dir, "rules.json"), s.rules)
	if s.feat != nil {
		_ = writeJSONFileAtomic(filepath.Join(s.dir, "smart.json"), s.feat.smart)
		_ = writeJSONFileAtomic(filepath.Join(s.dir, "vip.json"), s.feat.vips)
		_ = writeJSONFileAtomic(filepath.Join(s.dir, "muted.json"), s.feat.muted)
		_ = writeJSONFileAtomic(filepath.Join(s.dir, "categories.json"), s.feat.cats)
		_ = writeJSONFileAtomic(filepath.Join(s.dir, "notify.json"), s.feat.notify)
		_ = writeJSONFileAtomic(filepath.Join(s.dir, "outbox.json"), s.feat.outbox)
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
func (s *LocalStore) writeRawLocked(m Message, raw []byte) {
	p, err := s.rawPath(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = writeFileAtomic(p, raw, 0o600)
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
func (s *LocalStore) folderMetaPath(id FolderID) (string, error) {
	return underRoot(s.dir, "meta", safeID(string(id))+".json")
}

func (s *LocalStore) loadFolderMeta(id FolderID) folderMeta {
	var m folderMeta
	p, err := s.folderMetaPath(id)
	if err != nil {
		return m
	}
	if err := readJSONFileStrict(p, &m); err != nil {
		quarantine(p)
		return folderMeta{}
	}
	return m
}
func (s *LocalStore) saveFolderMeta(id FolderID, m folderMeta) {
	p, err := s.folderMetaPath(id)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = writeJSONFileAtomic(p, m)
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

	if s.feat != nil && !s.feat.Online() {
		id, err := s.Append(FolderOutbox, msg)
		if err != nil {
			s.mu.Lock()
			s.nextID++
			id = MessageID(fmt.Sprintf("%s-out-%04d", safeID(accountID), s.nextID))
			msg.ID = id
			msg.AccountID = accountID
			s.messages = append(s.messages, msg)
			s.mu.Unlock()
		}
		queue(id, "")
		return id, nil
	}
	if err := SendSMTP(cfg.SMTP, extractAddr(msg.From), rcpts, raw); err != nil {
		queue("", err.Error())
		return "", err
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
func (s *LocalStore) MutedThreads() []string { return s.extras().MutedThreads() }
func (s *LocalStore) ListVIPs() []VIP        { return s.extras().ListVIPs() }
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
	for i := range s.messages {
		if canonAddr(s.messages[i].From) == canonAddr(address) {
			s.messages[i].Category = category
		}
	}
	s.saveLocked()
	return nil
}
func (s *LocalStore) ListSenderCategories() []SenderCat {
	return s.extras().ListSenderCategories()
}

func (s *LocalStore) FlushOutbox() (int, error) {
	if s.feat == nil {
		return 0, nil
	}
	s.feat.mu.Lock()
	ops := append([]OutboxOp(nil), s.feat.outbox...)
	s.feat.mu.Unlock()
	flushed := 0
	var last error
	for _, op := range ops {
		if err := s.flushOne(op); err != nil {
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
		s.mu.Lock()
		i, ok := s.indexLocked(op.MessageID)
		if !ok {
			s.mu.Unlock()
			return nil // vanished; drop
		}
		m := s.messages[i].Clone()
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
		s.pushFlags(m, f, add, rem)
		return nil
	case "move":
		return s.Move([]MessageID{op.MessageID}, op.Dest)
	case "delete":
		return s.Delete([]MessageID{op.MessageID})
	case "send":
		if op.Message == nil {
			return nil
		}
		// Attachments are carried in the queued op, so a message composed
		// offline still goes out with its files.
		_, err := s.SendViaSMTP(op.AccountID, op.IdentityID, *op.Message, op.Attachments)
		return err
	default:
		return nil
	}
}
