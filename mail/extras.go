package mail

import (
	"fmt"
	"strings"
	"sync"
)

// Synthetic accounts for Tier A/B virtual panes.
const (
	AccountSmart      = "smart"
	AccountCategories = "categories"
	AccountVIP        = "vip"

	FolderVIP           FolderID = "virtual/vip"
	FolderOutbox        FolderID = "virtual/outbox"
	FolderCatPrimary    FolderID = "virtual/cat/primary"
	FolderCatTransact   FolderID = "virtual/cat/transactions"
	FolderCatUpdates    FolderID = "virtual/cat/updates"
	FolderCatPromotions FolderID = "virtual/cat/promotions"
	FolderCatOther      FolderID = "virtual/cat/other"
)

// Category names (Gmail-lite / Apple Other).
const (
	CatPrimary      = "primary"
	CatTransactions = "transactions"
	CatUpdates      = "updates"
	CatPromotions   = "promotions"
	CatOther        = "other"
)

// SmartFolder is a user-defined saved search shown as a virtual folder.
type SmartFolder struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	AccountID string   `json:"accountId,omitempty"`
	FolderID  FolderID `json:"folderId,omitempty"`
	Filter    Filter   `json:"filter"`
}

// FolderIDFor is smart/<id>.
func (s SmartFolder) FolderIDFor() FolderID {
	return FolderID("smart/" + s.ID)
}

// VIP is a highlighted sender.
type VIP struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

// SenderCat is a user override of the local classifier.
type SenderCat struct {
	Address  string `json:"address"`
	Category string `json:"category"`
}

// NotifyPrefs controls desktop / in-app new-mail alerts.
type NotifyPrefs struct {
	Enabled bool `json:"enabled"`
	VIPOnly bool `json:"vipOnly"`
	Desktop bool `json:"desktop"`
}

// OutboxOp is one queued mutation while offline (or after a transport error).
type OutboxOp struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"` // send, move, delete, flag
	AccountID   string         `json:"accountId,omitempty"`
	MessageID   MessageID      `json:"messageId,omitempty"`
	Dest        FolderID       `json:"dest,omitempty"`
	Patch       FlagPatch      `json:"patch,omitempty"`
	Message     *Message       `json:"message,omitempty"`
	IdentityID  string         `json:"identityId,omitempty"`
	Attachments []AttachedFile `json:"attachments,omitempty"`
	Error       string         `json:"error,omitempty"`
	Tries       int            `json:"tries,omitempty"`
	UID         uint32         `json:"uid,omitempty"`
	UIDVal      uint32         `json:"uidValidity,omitempty"`
}

// featureHost is the shared Tier A/B state (MemoryStore + LocalStore).
type featureHost struct {
	mu     sync.Mutex
	smart  []SmartFolder
	vips   []VIP
	muted  []string
	cats   []SenderCat
	notify NotifyPrefs
	outbox []OutboxOp
	online bool
	index  *searchIndex
	nextOp int
}

func newFeatureHost() *featureHost {
	return &featureHost{
		online: true,
		notify: NotifyPrefs{Enabled: true, Desktop: true},
		index:  newSearchIndex(),
	}
}

type featureSnap struct {
	vips   map[string]bool
	smart  []SmartFolder
	muted  map[string]bool
	cats   map[string]string
	outbox []OutboxOp
}

func (f *featureHost) snap() featureSnap {
	if f == nil {
		return featureSnap{
			vips:  map[string]bool{},
			muted: map[string]bool{},
			cats:  map[string]string{},
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapLocked()
}

func (f *featureHost) snapLocked() featureSnap {
	s := featureSnap{
		vips:   map[string]bool{},
		muted:  map[string]bool{},
		cats:   map[string]string{},
		smart:  append([]SmartFolder(nil), f.smart...),
		outbox: append([]OutboxOp(nil), f.outbox...),
	}
	for _, v := range f.vips {
		s.vips[canonAddr(v.Address)] = true
	}
	for _, id := range f.muted {
		s.muted[id] = true
	}
	for _, c := range f.cats {
		s.cats[canonAddr(c.Address)] = c.Category
	}
	return s
}

func (f *featureHost) Online() bool {
	if f == nil {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.online
}

func (f *featureHost) SetOnline(online bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.online = online
	f.mu.Unlock()
}

func (f *featureHost) ListSmartFolders() []SmartFolder {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SmartFolder(nil), f.smart...)
}

func (f *featureHost) PutSmartFolder(in SmartFolder) (SmartFolder, error) {
	if f == nil {
		return SmartFolder{}, fmt.Errorf("mail: no feature host")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return SmartFolder{}, fmt.Errorf("mail: smart folder name required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.ID == "" {
		in.ID = fmt.Sprintf("sf-%02d", len(f.smart)+1)
	}
	for i, x := range f.smart {
		if x.ID == in.ID {
			f.smart[i] = in
			return in, nil
		}
	}
	f.smart = append(f.smart, in)
	return in, nil
}

func (f *featureHost) DeleteSmartFolder(id string) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.smart[:0]
	for _, x := range f.smart {
		if x.ID != id {
			out = append(out, x)
		}
	}
	f.smart = out
	return nil
}

func (f *featureHost) ListVIPs() []VIP {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]VIP(nil), f.vips...)
}

func (f *featureHost) PutVIP(v VIP) (VIP, error) {
	if f == nil {
		return VIP{}, fmt.Errorf("mail: no feature host")
	}
	v.Address = canonAddr(v.Address)
	if v.Address == "" {
		return VIP{}, fmt.Errorf("mail: VIP address required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, x := range f.vips {
		if canonAddr(x.Address) == v.Address {
			if v.Name != "" {
				f.vips[i].Name = v.Name
			}
			return f.vips[i], nil
		}
	}
	f.vips = append(f.vips, v)
	return v, nil
}

func (f *featureHost) DeleteVIP(address string) error {
	if f == nil {
		return nil
	}
	want := canonAddr(address)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.vips[:0]
	for _, x := range f.vips {
		if canonAddr(x.Address) != want {
			out = append(out, x)
		}
	}
	f.vips = out
	return nil
}

func (f *featureHost) MuteThread(threadID string, muted bool) error {
	if f == nil {
		return fmt.Errorf("mail: no feature host")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("mail: empty thread id")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if muted {
		for _, id := range f.muted {
			if id == threadID {
				return nil
			}
		}
		f.muted = append(f.muted, threadID)
		return nil
	}
	out := f.muted[:0]
	for _, id := range f.muted {
		if id != threadID {
			out = append(out, id)
		}
	}
	f.muted = out
	return nil
}

func (f *featureHost) MutedThreads() []string {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.muted...)
}

func (f *featureHost) NotifyPrefs() NotifyPrefs {
	if f == nil {
		return NotifyPrefs{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.notify
}

func (f *featureHost) PutNotifyPrefs(p NotifyPrefs) NotifyPrefs {
	if f == nil {
		return p
	}
	f.mu.Lock()
	f.notify = p
	f.mu.Unlock()
	return p
}

func (f *featureHost) SetSenderCategory(address, category string) error {
	if f == nil {
		return fmt.Errorf("mail: no feature host")
	}
	address = canonAddr(address)
	category = strings.ToLower(strings.TrimSpace(category))
	if address == "" {
		return fmt.Errorf("mail: sender address required")
	}
	if !validCategory(category) {
		return fmt.Errorf("mail: unknown category %q", category)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.cats {
		if canonAddr(c.Address) == address {
			f.cats[i].Category = category
			return nil
		}
	}
	f.cats = append(f.cats, SenderCat{Address: address, Category: category})
	return nil
}

func (f *featureHost) ListSenderCategories() []SenderCat {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SenderCat(nil), f.cats...)
}

func (f *featureHost) ListOutbox() []OutboxOp {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]OutboxOp(nil), f.outbox...)
}

func (f *featureHost) enqueueLocked(op OutboxOp) OutboxOp {
	f.nextOp++
	if op.ID == "" {
		op.ID = fmt.Sprintf("op-%04d", f.nextOp)
	}
	f.outbox = append(f.outbox, op)
	return op
}

func (f *featureHost) dropOutboxLocked(id string) {
	out := f.outbox[:0]
	for _, op := range f.outbox {
		if op.ID != id {
			out = append(out, op)
		}
	}
	f.outbox = out
}

func (f *featureHost) pendingFor(id MessageID) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, op := range f.outbox {
		if op.MessageID == id {
			return true
		}
	}
	return false
}

func validCategory(c string) bool {
	switch c {
	case CatPrimary, CatTransactions, CatUpdates, CatPromotions, CatOther:
		return true
	}
	return false
}

func canonAddr(s string) string {
	return strings.ToLower(extractAddr(s))
}

func isVIPAddr(addr string, vips map[string]bool) bool {
	return vips[canonAddr(addr)]
}

func SmartFolderID(id FolderID) string {
	s := string(id)
	if strings.HasPrefix(s, "smart/") {
		return s[len("smart/"):]
	}
	return ""
}

func categoryFromFolder(id FolderID) string {
	s := string(id)
	if strings.HasPrefix(s, "virtual/cat/") {
		return s[len("virtual/cat/"):]
	}
	return ""
}

func extraVirtualFolders(snap featureSnap) []Folder {
	out := []Folder{
		{ID: FolderVIP, AccountID: AccountUnified, Name: "VIP", Kind: FolderCustom, Virtual: true},
		{ID: FolderOutbox, AccountID: AccountUnified, Name: "Outbox", Kind: FolderCustom, Virtual: true},
		{ID: FolderCatPrimary, AccountID: AccountCategories, Name: "Primary", Kind: FolderInbox, Virtual: true},
		{ID: FolderCatTransact, AccountID: AccountCategories, Name: "Transactions", Kind: FolderCustom, Virtual: true},
		{ID: FolderCatUpdates, AccountID: AccountCategories, Name: "Updates", Kind: FolderCustom, Virtual: true},
		{ID: FolderCatPromotions, AccountID: AccountCategories, Name: "Promotions", Kind: FolderCustom, Virtual: true},
		{ID: FolderCatOther, AccountID: AccountCategories, Name: "Other", Kind: FolderCustom, Virtual: true},
	}
	for _, sf := range snap.smart {
		out = append(out, Folder{
			ID: sf.FolderIDFor(), AccountID: AccountSmart, Name: sf.Name,
			Kind: FolderCustom, Virtual: true,
		})
	}
	return out
}

func demoExtras() *featureHost {
	f := newFeatureHost()
	f.vips = []VIP{
		{Address: "kai@paintengine.example", Name: "Kai Nakamura"},
		{Address: "hello@thunderbird.example", Name: "Thunderbird Team"},
	}
	f.smart = []SmartFolder{
		{ID: "sf-invoices", Name: "Invoices", Filter: Filter{Query: "Invoice", SubjectOnly: true}},
		{ID: "sf-unread-star", Name: "Unread starred", Filter: Filter{Unread: true, Starred: true}},
	}
	f.cats = []SenderCat{
		{Address: "morgan@lists.example", Category: CatPromotions},
		{Address: "noah@ops.example", Category: CatUpdates},
	}
	f.notify = NotifyPrefs{Enabled: true, VIPOnly: false, Desktop: true}
	return f
}
