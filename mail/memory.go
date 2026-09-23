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

// MemoryStore is an in-memory, maildir-ish Store. Safe for the UI thread
// plus tests. Not a network client.
type MemoryStore struct {
	mu         sync.Mutex
	accounts   []Account
	folders    []Folder
	messages   []Message
	identities []Identity
	tags       []Tag
	rules      []FilterRule
	nextID     int
	fetches    map[string]int
	now        time.Time
	feat       *featureHost
	raw        map[MessageID][]byte
}

// NewMemoryStore builds an empty store. now is used for Fetch timestamps
// and date formatting in tests; zero means time.Now.
func NewMemoryStore(now time.Time) *MemoryStore {
	if now.IsZero() {
		now = time.Now()
	}
	return &MemoryStore{
		nextID: 1, fetches: map[string]int{}, now: now,
		tags: DefaultTags(), feat: newFeatureHost(),
		raw: map[MessageID][]byte{},
	}
}

// NewDemoStore returns a seeded two-account mailbox (50–200 messages).
func NewDemoStore() *MemoryStore {
	s := NewMemoryStore(DemoNow)
	seedDemo(s)
	return s
}

func (s *MemoryStore) Backend() string { return "memory" }

func (s *MemoryStore) Health() error { return nil }

func (s *MemoryStore) Accounts() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Account, len(s.accounts))
	copy(out, s.accounts)
	return out
}

func (s *MemoryStore) ListFolders(accountID string) []Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Folder
	for _, f := range s.folders {
		if f.AccountID == accountID {
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

func (s *MemoryStore) GetFolder(id FolderID) (Folder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.liveVirtual(id); ok {
		return f, true
	}
	return s.folderLocked(id)
}

func (s *MemoryStore) liveVirtual(id FolderID) (Folder, bool) {
	if sid := SmartFolderID(id); sid != "" && s.feat != nil {
		for _, sf := range s.feat.ListSmartFolders() {
			if sf.ID == sid {
				return Folder{ID: id, AccountID: AccountSmart, Name: sf.Name, Kind: FolderCustom, Virtual: true}, true
			}
		}
	}
	return virtualFolderByID(id)
}

func (s *MemoryStore) folderLocked(id FolderID) (Folder, bool) {
	for _, f := range s.folders {
		if f.ID == id {
			return f, true
		}
	}
	return Folder{}, false
}

func (s *MemoryStore) ListMessages(folder FolderID) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(folder)
}

func (s *MemoryStore) listLocked(folder FolderID) []Message {
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

func (s *MemoryStore) GetMessage(id MessageID) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.messages {
		if m.ID == id {
			return m.Clone(), true
		}
	}
	return Message{}, false
}

func (s *MemoryStore) GetRaw(id MessageID) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.indexLocked(id)
	if !ok {
		return nil, fmt.Errorf("mail: no message %s", id)
	}
	s.storeRawLocked(s.messages[i])
	b := s.raw[id]
	out := make([]byte, len(b))
	copy(out, b)
	return out, nil
}

func (s *MemoryStore) storeRawLocked(m Message) {
	if s.raw == nil {
		s.raw = map[MessageID][]byte{}
	}
	if len(s.raw[m.ID]) > 0 || m.ID == "" {
		return
	}
	ident := Identity{Address: m.From, Name: m.From}
	for _, id := range s.identities {
		if id.AccountID == m.AccountID && (id.Default || ident.Address == m.From) {
			ident = id
			if id.Default {
				break
			}
		}
	}
	var files []AttachedFile
	for _, name := range m.Attachments {
		files = append(files, AttachedFile{
			Name: name, MIME: "application/octet-stream", Data: []byte(name),
		})
	}
	s.raw[m.ID] = BuildRFC822(m, ident, files)
}

func (s *MemoryStore) SetFlags(id MessageID, patch FlagPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.indexLocked(id)
	if !ok {
		return fmt.Errorf("mail: no message %s", id)
	}
	if patch.Read != nil {
		s.messages[i].Read = *patch.Read
	}
	if patch.Starred != nil {
		s.messages[i].Starred = *patch.Starred
	}
	if patch.Tags != nil {
		s.messages[i].Tags = append([]string(nil), (*patch.Tags)...)
	}
	syncSystemTagsFromFlags(&s.messages[i])
	if s.feat != nil && !s.feat.Online() {
		s.feat.mu.Lock()
		s.feat.enqueueLocked(OutboxOp{Kind: "flag", MessageID: id, Patch: patch, AccountID: s.messages[i].AccountID})
		s.feat.mu.Unlock()
	}
	return nil
}

func (s *MemoryStore) Move(ids []MessageID, dest FolderID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.folderLocked(dest); !ok {
		return fmt.Errorf("mail: no folder %s", dest)
	}
	for _, id := range ids {
		i, ok := s.indexLocked(id)
		if !ok {
			return fmt.Errorf("mail: no message %s", id)
		}
		s.messages[i].Folder = dest
	}
	return nil
}

func (s *MemoryStore) Delete(ids []MessageID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		i, ok := s.indexLocked(id)
		if !ok {
			return fmt.Errorf("mail: no message %s", id)
		}
		cur, ok := s.folderLocked(s.messages[i].Folder)
		if !ok {
			return fmt.Errorf("mail: no folder for %s", id)
		}
		if cur.Kind == FolderTrash {
			s.messages = append(s.messages[:i], s.messages[i+1:]...)
			continue
		}
		trash, ok := s.specialLocked(cur.AccountID, FolderTrash)
		if !ok {
			return fmt.Errorf("mail: no trash for %s", cur.AccountID)
		}
		s.messages[i].Folder = trash.ID
	}
	return nil
}

func (s *MemoryStore) Append(folder FolderID, msg Message) (MessageID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.folderLocked(folder)
	if !ok {
		return "", fmt.Errorf("mail: no folder %s", folder)
	}
	if msg.Date.IsZero() {
		msg.Date = s.now
	}
	if msg.Size <= 0 {
		msg.Size = len(msg.Subject) + len(msg.Body) + 80
	}
	s.nextID++
	msg.ID = MessageID(fmt.Sprintf("m-%04d", s.nextID))
	msg.Folder = folder
	msg.AccountID = f.AccountID
	if msg.ThreadID == "" {
		msg.ThreadID = ThreadIDOf(msg)
	}
	if msg.Category == "" && s.feat != nil {
		msg.Category = messageCategory(msg, s.feat.snap())
	}
	applyAutomaticTags(&msg)
	s.messages = append(s.messages, msg)
	s.storeRawLocked(msg)
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.add(msg)
	}
	return msg.ID, nil
}

func (s *MemoryStore) Update(id MessageID, msg Message) error {
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
	if msg.Size <= 0 {
		msg.Size = len(msg.Subject) + len(msg.Body) + 80
	}
	s.messages[i] = msg
	return nil
}

func (s *MemoryStore) Fetch(accountID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fetches == nil {
		s.fetches = map[string]int{}
	}
	n := s.fetches[accountID]
	if n >= 3 {
		return 0, nil
	}
	inbox, ok := s.specialLocked(accountID, FolderInbox)
	if !ok {
		return 0, fmt.Errorf("mail: no inbox for %s", accountID)
	}
	s.fetches[accountID] = n + 1
	s.nextID++
	id := MessageID(fmt.Sprintf("m-%04d", s.nextID))
	acct := accountID
	for _, a := range s.accounts {
		if a.ID == accountID {
			acct = a.Address
			break
		}
	}
	msg := Message{
		ID:        id,
		Folder:    inbox.ID,
		AccountID: accountID,
		From:      "Fetch Robot <fetch@demo.invalid>",
		To:        acct,
		Subject:   fmt.Sprintf("Get Messages demo #%d", n+1),
		Date:      s.now.Add(time.Duration(n) * time.Minute),
		Read:      false,
		Body:      "This arrival was injected by MemoryStore.Fetch.\nIMAP would FETCH unseen here.\n",
	}
	msg.Size = len(msg.Subject) + len(msg.Body) + 80
	applyAutomaticTags(&msg)
	s.applyRulesOnLocked(&msg)
	s.messages = append(s.messages, msg)
	s.storeRawLocked(msg)
	return 1, nil
}

func (s *MemoryStore) Unread(folder FolderID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.listLocked(folder) {
		if !m.Read {
			n++
		}
	}
	return n
}

func (s *MemoryStore) UnreadTotal() int {
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

func (s *MemoryStore) MessageCount(folder FolderID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.Folder == folder {
			n++
		}
	}
	return n
}

func (s *MemoryStore) CreateFolder(accountID, name string, parent FolderID) (Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if accountID == "" || name == "" {
		return Folder{}, fmt.Errorf("mail: account and folder name required")
	}
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	id := FolderID(accountID + "/" + slug)
	if parent != "" {
		id = parent + "/" + FolderID(slug)
	}
	for _, f := range s.folders {
		if f.ID == id {
			return Folder{}, fmt.Errorf("mail: folder %s exists", id)
		}
	}
	f := Folder{ID: id, AccountID: accountID, Name: name, Kind: FolderCustom, Parent: parent}
	s.folders = append(s.folders, f)
	return f, nil
}

func (s *MemoryStore) Search(q SearchQuery) []Message {
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

// Folders / Folder / List / Get are aliases for older call sites.
func (s *MemoryStore) Folders(accountID string) []Folder { return s.ListFolders(accountID) }
func (s *MemoryStore) Folder(id FolderID) (Folder, bool) { return s.GetFolder(id) }
func (s *MemoryStore) List(folder FolderID) []Message    { return s.ListMessages(folder) }
func (s *MemoryStore) Get(id MessageID) (Message, bool)  { return s.GetMessage(id) }

func (s *MemoryStore) indexLocked(id MessageID) (int, bool) {
	for i, m := range s.messages {
		if m.ID == id {
			return i, true
		}
	}
	return -1, false
}

func (s *MemoryStore) specialLocked(accountID string, kind FolderKind) (Folder, bool) {
	for _, f := range s.folders {
		if f.AccountID == accountID && f.Kind == kind {
			return f, true
		}
	}
	return Folder{}, false
}

func (s *MemoryStore) addAccount(a Account) { s.accounts = append(s.accounts, a) }

func (s *MemoryStore) addFolder(f Folder) { s.folders = append(s.folders, f) }

func (s *MemoryStore) addMessage(m Message) {
	s.nextID++
	if m.ID == "" {
		m.ID = MessageID(fmt.Sprintf("m-%04d", s.nextID))
	}
	if m.Size <= 0 {
		m.Size = len(m.Subject) + len(m.Body) + 80
		if m.HasAttach {
			m.Size += 24 * 1024
		}
	}
	if m.ThreadID == "" {
		m.ThreadID = ThreadIDOf(m)
	}
	if m.Category == "" && s.feat != nil {
		m.Category = messageCategory(m, s.feat.snap())
	}
	applyAutomaticTags(&m)
	s.messages = append(s.messages, m)
	s.storeRawLocked(m)
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.add(m)
	}
}

func folderRank(k FolderKind) int {
	switch k {
	case FolderInbox:
		return 0
	case FolderDrafts:
		return 1
	case FolderSent:
		return 2
	case FolderJunk:
		return 3
	case FolderTrash:
		return 4
	case FolderArchive:
		return 5
	default:
		return 10
	}
}

func (s *MemoryStore) Special(accountID string, kind FolderKind) (Folder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.specialLocked(accountID, kind)
}

func (s *MemoryStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *MemoryStore) PutAccount(in AccountConfig) (Account, error) {
	a, err := SanitizeAccountConfig(in)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acct := accountFromConfig(a, "memory")
	found := false
	for i, x := range s.accounts {
		if x.ID == a.ID {
			s.accounts[i] = acct
			found = true
			break
		}
	}
	if !found {
		s.accounts = append(s.accounts, acct)
	}
	for _, idn := range a.Identities {
		idn.AccountID = a.ID
		s.identities = upsertIdentity(s.identities, idn)
	}
	s.ensureSpecialsLocked(a.ID)
	return acct, nil
}

func (s *MemoryStore) DeleteAccount(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("mail: account id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	accts := s.accounts[:0]
	for _, a := range s.accounts {
		if a.ID == id {
			found = true
			continue
		}
		accts = append(accts, a)
	}
	if !found {
		return fmt.Errorf("mail: no account %s", id)
	}
	s.accounts = accts
	idents := s.identities[:0]
	for _, idn := range s.identities {
		if idn.AccountID != id {
			idents = append(idents, idn)
		}
	}
	s.identities = idents
	folders := s.folders[:0]
	for _, f := range s.folders {
		if f.AccountID != id {
			folders = append(folders, f)
		}
	}
	s.folders = folders
	msgs := s.messages[:0]
	for _, m := range s.messages {
		if m.AccountID != id {
			msgs = append(msgs, m)
		}
	}
	s.messages = msgs
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.rebuild(s.messages)
	}
	return nil
}

func (s *MemoryStore) ensureSpecialsLocked(accountID string) {
	for _, spec := range defaultSpecials() {
		if _, ok := s.specialLocked(accountID, spec.Kind); ok {
			continue
		}
		id := FolderID(accountID + "/" + strings.ToLower(spec.Name))
		s.folders = append(s.folders, Folder{
			ID: id, AccountID: accountID, Name: spec.Name, Kind: spec.Kind, Remote: spec.Remote,
		})
	}
}

func defaultSpecials() []Folder {
	return []Folder{
		{Name: "Inbox", Kind: FolderInbox, Remote: "INBOX"},
		{Name: "Drafts", Kind: FolderDrafts, Remote: "Drafts"},
		{Name: "Sent", Kind: FolderSent, Remote: "Sent"},
		{Name: "Junk", Kind: FolderJunk, Remote: "Junk"},
		{Name: "Trash", Kind: FolderTrash, Remote: "Trash"},
		{Name: "Archives", Kind: FolderArchive, Remote: "Archives"},
	}
}

func (s *MemoryStore) Identities(accountID string) []Identity {
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

func (s *MemoryStore) PutIdentity(id Identity) (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id.ID == "" {
		id.ID = fmt.Sprintf("id-%d", len(s.identities)+1)
	}
	s.identities = upsertIdentity(s.identities, id)
	return id, nil
}

func (s *MemoryStore) DeleteIdentity(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.identities[:0]
	for _, x := range s.identities {
		if x.ID != id {
			out = append(out, x)
		}
	}
	s.identities = out
	return nil
}

func (s *MemoryStore) ListTags() []Tag {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags = mergeTagStore(s.tags)
	return cloneTags(s.tags)
}

func (s *MemoryStore) PutTag(t Tag) (Tag, error) {
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
		return t, nil
	}
	s.tags = upsertTag(s.tags, t)
	if got, ok := tagByName(s.tags, t.Name); ok {
		t = got
	}
	return t, nil
}

func (s *MemoryStore) DeleteTag(name string) error {
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
	return nil
}

func (s *MemoryStore) VirtualFolders() []Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := defaultVirtualFolders()
	if s.feat != nil {
		// defaultVirtualFolders already includes empty extras; replace with live smart folders
		base := []Folder{
			{ID: FolderUnifiedInbox, AccountID: AccountUnified, Name: "Unified Inbox", Kind: FolderInbox, Virtual: true, MatchKind: FolderInbox},
			{ID: FolderUnifiedUnread, AccountID: AccountUnified, Name: "Unread", Kind: FolderCustom, Virtual: true},
			{ID: FolderUnifiedStarred, AccountID: AccountUnified, Name: "Starred", Kind: FolderCustom, Virtual: true},
		}
		out = append(base, extraVirtualFolders(s.feat.snap())...)
	}
	for _, t := range s.tags {
		out = append(out, Folder{
			ID: TagFolderID(t.Name), AccountID: AccountTags, Name: t.Name,
			Kind: FolderCustom, Virtual: true, Tag: t.Name,
		})
	}
	return out
}

func (s *MemoryStore) ListRules() []FilterRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRules(s.rules)
}

func (s *MemoryStore) PutRule(r FilterRule) (FilterRule, error) {
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
	return r, nil
}

func (s *MemoryStore) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.rules[:0]
	for _, r := range s.rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	s.rules = out
	return nil
}

func (s *MemoryStore) ApplyRules(folder FolderID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.messages {
		if folder != "" && !IsVirtual(folder) && s.messages[i].Folder != folder {
			continue
		}
		if s.applyRulesOnLocked(&s.messages[i]) {
			n++
		}
	}
	return n, nil
}

func (s *MemoryStore) applyRulesOnLocked(m *Message) bool {
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

func (s *MemoryStore) deleteOne(id MessageID) error {
	i, ok := s.indexLocked(id)
	if !ok {
		return fmt.Errorf("mail: no message %s", id)
	}
	s.messages = append(s.messages[:i], s.messages[i+1:]...)
	return nil
}

func (s *MemoryStore) moveOne(id MessageID, dest FolderID) error {
	i, ok := s.indexLocked(id)
	if !ok {
		return fmt.Errorf("mail: no message %s", id)
	}
	s.messages[i].Folder = dest
	return nil
}

func (s *MemoryStore) indexOf(id MessageID) (int, bool) { return s.indexLocked(id) }
func (s *MemoryStore) messageAt(i int) *Message         { return &s.messages[i] }

func (s *MemoryStore) GetPart(id MessageID, partID string) (PartData, error) {
	m, ok := s.GetMessage(id)
	if !ok {
		return PartData{}, fmt.Errorf("mail: no message %s", id)
	}
	// Demo messages carry Parts without a raw blob, so attachments are
	// matched by section id, by the synthetic att-N id, and by filename.
	for i, name := range m.Attachments {
		if fmt.Sprintf("att-%d", i+1) != partID && name != partID {
			if i >= len(m.Parts) || m.Parts[i].ID != partID {
				continue
			}
		}
		return PartData{
			Part: Part{ID: partID, MIMEType: guessMIME(name), Filename: name},
			Data: []byte(name + " (demo attachment)\n"),
		}, nil
	}
	for _, p := range m.Parts {
		if p.ID != partID || p.Filename == "" {
			continue
		}
		return PartData{Part: p, Data: []byte(p.Filename + " (demo attachment)\n")}, nil
	}
	if partID == "" || partID == "1" {
		return PartData{Part: Part{ID: "1", MIMEType: "text/plain", Size: len(m.Body)}, Data: []byte(m.Body)}, nil
	}
	if strings.EqualFold(partID, "html") || partID == "1.2" {
		return PartData{Part: Part{ID: partID, MIMEType: "text/html", Size: len(m.HTML)}, Data: []byte(m.HTML)}, nil
	}
	return PartData{Part: Part{ID: partID}, Data: []byte(m.Body)}, nil
}
func (s *MemoryStore) OpenPart(id MessageID, partID string) (PartData, error) {
	p, err := s.GetPart(id, partID)
	if err != nil {
		return p, err
	}
	name := attachFileName(p.Filename)
	if name == "attachment" {
		name = attachFileName(safeID(string(id))+"-"+safeID(partID)) + ".txt"
	}
	if unsafeAttachmentName(name) {
		return p, fmt.Errorf("mail: refusing to open %q — save it and inspect it instead", name)
	}
	dir, err := os.MkdirTemp("", "uitk-mail-")
	if err != nil {
		return p, err
	}
	path := filepath.Join(dir, name)
	if err := writeFileAtomic(path, p.Data, 0o600); err != nil {
		return p, err
	}
	p.Path = path
	openCachedFile(path)
	return p, nil
}
func (s *MemoryStore) Sync(accountID string) (SyncResult, error) {
	n, err := s.Fetch(accountID)
	if s.feat != nil && s.feat.Online() {
		_, _ = s.FlushOutbox()
	}
	return SyncResult{AccountID: accountID, New: n}, err
}

func (s *MemoryStore) extras() *featureHost {
	if s.feat == nil {
		s.feat = newFeatureHost()
	}
	return s.feat
}

func (s *MemoryStore) SetOnline(v bool)       { s.extras().SetOnline(v) }
func (s *MemoryStore) Online() bool           { return s.extras().Online() }
func (s *MemoryStore) ListOutbox() []OutboxOp { return s.extras().ListOutbox() }
func (s *MemoryStore) ListSmartFolders() []SmartFolder {
	return s.extras().ListSmartFolders()
}
func (s *MemoryStore) PutSmartFolder(sf SmartFolder) (SmartFolder, error) {
	return s.extras().PutSmartFolder(sf)
}
func (s *MemoryStore) DeleteSmartFolder(id string) error { return s.extras().DeleteSmartFolder(id) }
func (s *MemoryStore) MuteThread(id string, muted bool) error {
	return s.extras().MuteThread(id, muted)
}
func (s *MemoryStore) MutedThreads() []string { return s.extras().MutedThreads() }
func (s *MemoryStore) ListVIPs() []VIP        { return s.extras().ListVIPs() }
func (s *MemoryStore) PutVIP(v VIP) (VIP, error) {
	return s.extras().PutVIP(v)
}
func (s *MemoryStore) DeleteVIP(address string) error { return s.extras().DeleteVIP(address) }
func (s *MemoryStore) NotifyPrefs() NotifyPrefs       { return s.extras().NotifyPrefs() }
func (s *MemoryStore) PutNotifyPrefs(p NotifyPrefs) NotifyPrefs {
	return s.extras().PutNotifyPrefs(p)
}
func (s *MemoryStore) SetSenderCategory(address, category string) error {
	err := s.extras().SetSenderCategory(address, category)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cat := category
	for i := range s.messages {
		if canonAddr(s.messages[i].From) == canonAddr(address) {
			s.messages[i].Category = cat
		}
	}
	return nil
}
func (s *MemoryStore) ListSenderCategories() []SenderCat {
	return s.extras().ListSenderCategories()
}

func (s *MemoryStore) FlushOutbox() (int, error) {
	if s.feat == nil {
		return 0, nil
	}
	s.feat.mu.Lock()
	n := len(s.feat.outbox)
	s.feat.outbox = nil
	s.feat.mu.Unlock()
	return n, nil
}
