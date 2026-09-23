// Package mailcore is comms-mail's message core: the Store interface and
// its backends, the account and folder model, MIME parsing, IMAP, POP3,
// SMTP and OAuth, and the JSON-RPC protocol the daemon and the UI speak
// over a Unix socket.
//
// It deliberately links no GUI code — nothing here imports
// github.com/codemodify/uitoolkit — so cmd/comms-maild is a headless
// daemon. The three-pane desktop UI lives in the sibling mailui package.
//
// The default backend is an empty LocalStore (IMAP or POP3 + SMTP with a
// disk cache). The seeded MemoryStore is UITK_MAIL=memory only.
package mailcore

import (
	"fmt"
	"strings"
	"time"
)

// FolderID names one mailbox (account-local, like a maildir directory).
type FolderID string

// MessageID is a stable id inside one Store.
type MessageID string

// FolderKind is a special-use mailbox (RFC 6154-ish) or a user folder.
type FolderKind int

const (
	FolderCustom FolderKind = iota
	FolderInbox
	FolderDrafts
	FolderSent
	FolderJunk
	FolderTrash
	FolderArchive
)

func (k FolderKind) String() string {
	switch k {
	case FolderInbox:
		return "Inbox"
	case FolderDrafts:
		return "Drafts"
	case FolderSent:
		return "Sent"
	case FolderJunk:
		return "Junk"
	case FolderTrash:
		return "Trash"
	case FolderArchive:
		return "Archives"
	default:
		return "Folder"
	}
}

// Account is one store/transport (IMAP or POP3 mailbox + SMTP submission).
// Identities (From name/address/signature) are separate — see Identity.
type Account struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Transport string `json:"transport,omitempty"` // "memory", "imap", or "pop3"
	Protocol  string `json:"protocol,omitempty"`  // "imap" or "pop3" (user-visible)
}

// ProtocolLabel is IMAP / POP3 for chrome (prefs, account central).
func ProtocolLabel(a Account) string {
	p := NormalizeProtocol(a.Protocol)
	if a.Protocol == "" && (a.Transport == "pop3" || a.Transport == "pop") {
		p = ProtoPOP3
	}
	if a.Transport == "memory" {
		if p == ProtoPOP3 {
			return "POP3 (demo)"
		}
		return "IMAP (demo)"
	}
	if p == ProtoPOP3 {
		return "POP3"
	}
	return "IMAP"
}

// Identity is a KMail-style From persona. Many identities can share one Account.
type Identity struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Signature string `json:"signature,omitempty"`
	Default   bool   `json:"default,omitempty"`
}

// DisplayFrom is `Name <Address>`.
func (id Identity) DisplayFrom() string {
	name := strings.TrimSpace(id.Name)
	addr := strings.TrimSpace(id.Address)
	if name == "" {
		return addr
	}
	if addr == "" {
		return name
	}
	return name + " <" + addr + ">"
}

// Tag is a colored keyword. System tags (Unread, Starred, Attachment)
// are locked pins shared by the sidebar Tags group and Preferences.
type Tag struct {
	Name     string `json:"name"`
	Color    string `json:"color"` // #rrggbb
	System   bool   `json:"system,omitempty"`
	Previous string `json:"previous,omitempty"` // tags.put rename-from
}

// Part is one MIME part (attachment or alternative).
type Part struct {
	ID       string `json:"id"`
	MIMEType string `json:"mimeType"`
	Filename string `json:"filename,omitempty"`
	Size     int    `json:"size,omitempty"`
	Charset  string `json:"charset,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

// PartData is a downloaded MIME section.
type PartData struct {
	Part
	Data []byte `json:"data,omitempty"`
	Path string `json:"path,omitempty"`
}

// FilterRule is one Sorting Office / Outlook-style rule.
type FilterRule struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Stop       bool            `json:"stop"`
	Conditions []RuleCondition `json:"conditions,omitempty"`
	Actions    []RuleAction    `json:"actions,omitempty"`
}

// RuleCondition is a match clause (AND together).
type RuleCondition struct {
	Field string `json:"field"` // from, to, subject, body, attachment, unread, tag
	Op    string `json:"op"`    // contains, is, equals (default contains)
	Value string `json:"value,omitempty"`
}

// RuleAction runs when all conditions match.
type RuleAction struct {
	Type   string   `json:"type"` // move, tag, markRead, markUnread, delete, stop
	Folder FolderID `json:"folder,omitempty"`
	Tag    string   `json:"tag,omitempty"`
}

// SyncResult is what sync.run / Fetch reports.
type SyncResult struct {
	AccountID string `json:"accountId,omitempty"`
	New       int    `json:"new"`
	Updated   int    `json:"updated,omitempty"`
	Folders   int    `json:"folders,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Folder is one mailbox under an account. Parent is empty for top-level.
// Virtual folders (Unified Inbox, tag views) have Virtual set.
type Folder struct {
	ID        FolderID   `json:"id"`
	AccountID string     `json:"accountId"`
	Name      string     `json:"name"`
	Kind      FolderKind `json:"kind"`
	Parent    FolderID   `json:"parent,omitempty"`
	Virtual   bool       `json:"virtual,omitempty"`
	MatchKind FolderKind `json:"matchKind,omitempty"`
	Tag       string     `json:"tag,omitempty"`
	Remote    string     `json:"remote,omitempty"` // IMAP mailbox name
}

// Message is a full RFC-822-ish record. Body is plain text (no MIME tree).
type Message struct {
	ID           MessageID
	Folder       FolderID
	AccountID    string
	From         string
	To           string
	Cc           string
	Bcc          string
	Subject      string
	Date         time.Time
	Size         int
	Read         bool
	Starred      bool
	HasAttach    bool
	Tags         []string
	Body         string
	HTML         string `json:"html,omitempty"`
	Snippet      string `json:"snippet,omitempty"`
	UID          uint32 `json:"uid,omitempty"`
	Parts        []Part `json:"parts,omitempty"`
	IdentityID   string `json:"identityId,omitempty"`
	RFCMessageID string `json:"rfcMessageId,omitempty"`
	ReplyTo      string `json:"replyTo,omitempty"`
	InReplyTo    string `json:"inReplyTo,omitempty"`
	References   string `json:"references,omitempty"`
	ThreadID     string `json:"threadId,omitempty"`
	Category     string `json:"category,omitempty"`
	Attachments  []string
}

func messageHasBody(m Message) bool {
	return m.Body != "" || m.HTML != ""
}

// Clone returns a shallow copy (tags / attachments copied).
func (m Message) Clone() Message {
	out := m
	if m.Tags != nil {
		out.Tags = append([]string(nil), m.Tags...)
	}
	if m.Attachments != nil {
		out.Attachments = append([]string(nil), m.Attachments...)
	}
	if m.Parts != nil {
		out.Parts = append([]Part(nil), m.Parts...)
	}
	return out
}

// DisplayName returns the phrase before <email>, or the whole string.
func DisplayName(addr string) string {
	addr = strings.TrimSpace(addr)
	if i := strings.IndexByte(addr, '<'); i > 0 {
		return strings.TrimSpace(addr[:i])
	}
	return addr
}

// Correspondent is From in incoming folders and To in Sent/Drafts.
func (m Message) Correspondent(kind FolderKind) string {
	if kind == FolderSent || kind == FolderDrafts {
		return DisplayName(FirstAddr(m.To))
	}
	return DisplayName(FirstAddr(m.From))
}

// FirstAddr is the first address in a comma-separated header value.
func FirstAddr(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// FlagPatch is a partial flag update (nil pointer = leave unchanged).
type FlagPatch struct {
	Read    *bool     `json:"read,omitempty"`
	Starred *bool     `json:"starred,omitempty"`
	Tags    *[]string `json:"tags,omitempty"`
}

// SearchQuery is a daemon-side scan (Quick Filter or global search).
type SearchQuery struct {
	AccountID string
	Folder    FolderID // empty = all folders (optionally scoped by AccountID)
	Filter    Filter
}

// DaemonStatus is what mailclientd reports on status.get.
type DaemonStatus struct {
	Backend  string `json:"backend"` // "memory" or "imap"
	Socket   string `json:"socket"`
	Online   bool   `json:"online"`
	Accounts int    `json:"accounts"`
	Health   string `json:"health,omitempty"`
	Outbox   int    `json:"outbox,omitempty"`
}

// Store is the mailclientd backend. LocalStore is the default (IMAP+SMTP).
// MemoryStore is UITK_MAIL=memory only. mailclientui never calls this.
//
// IMAP mapping (IMAPStore skeleton, UITK_MAIL=imap):
//
//	Accounts     — LOGIN identity (UITK_MAIL_USER) + settings UI
//	ListFolders  — LIST / SPECIAL-USE; map \Inbox \Sent \Trash \Junk \Drafts
//	ListMessages — SELECT + FETCH 1:* (FLAGS RFC822.SIZE headers)
//	GetMessage   — FETCH BODY.PEEK[HEADER] BODY.PEEK[TEXT]
//	SetFlags     — STORE +FLAGS.SILENT (\Seen \Flagged)
//	Move         — MOVE (RFC 6851) or COPY + STORE \Deleted + EXPUNGE
//	Delete       — MOVE to \Trash, or EXPUNGE when already in Trash
//	Append       — APPEND to \Drafts / \Sent
//	Update       — APPEND replacement + delete old UID
//	Fetch        — NOOP or IDLE, then FETCH unseen
//	Search       — IMAP SEARCH / local Filter.Match
//	CreateFolder — CREATE
//
// SMTP is compose.send: submission then Append to Sent.
type Store interface {
	Backend() string
	Health() error

	Accounts() []Account
	ListFolders(accountID string) []Folder
	GetFolder(id FolderID) (Folder, bool)
	CreateFolder(accountID, name string, parent FolderID) (Folder, error)

	ListMessages(folder FolderID) []Message
	GetMessage(id MessageID) (Message, bool)
	// GetRaw is the on-disk / IMAP RFC822 bytes (Thunderbird message source).
	GetRaw(id MessageID) ([]byte, error)
	Search(q SearchQuery) []Message

	SetFlags(id MessageID, patch FlagPatch) error
	Move(ids []MessageID, dest FolderID) error
	Delete(ids []MessageID) error
	Append(folder FolderID, msg Message) (MessageID, error)
	Update(id MessageID, msg Message) error

	// Fetch is “Get Messages”. MemoryStore injects demo arrivals.
	Fetch(accountID string) (newCount int, err error)

	Unread(folder FolderID) int
	UnreadTotal() int
	MessageCount(folder FolderID) int

	Identities(accountID string) []Identity
	PutIdentity(Identity) (Identity, error)
	DeleteIdentity(id string) error

	ListTags() []Tag
	PutTag(Tag) (Tag, error)
	DeleteTag(name string) error

	VirtualFolders() []Folder

	ListRules() []FilterRule
	PutRule(FilterRule) (FilterRule, error)
	DeleteRule(id string) error
	ApplyRules(folder FolderID) (int, error)

	GetPart(id MessageID, partID string) (PartData, error)
	OpenPart(id MessageID, partID string) (PartData, error)
	Sync(accountID string) (SyncResult, error)

	// PutAccount writes IMAP/POP3/SMTP settings (inline password and/or passEnv).
	PutAccount(AccountConfig) (Account, error)
	// DeleteAccount drops the account from config and the local cache.
	DeleteAccount(id string) error
}

// ExtraStore is Tier A/B state (OAuth tokens live beside it). MemoryStore
// and LocalStore implement this; the IMAP skeleton does not.
type ExtraStore interface {
	SetOnline(bool)
	Online() bool
	ListOutbox() []OutboxOp
	FlushOutbox() (int, error)

	ListSmartFolders() []SmartFolder
	PutSmartFolder(SmartFolder) (SmartFolder, error)
	DeleteSmartFolder(id string) error

	MuteThread(threadID string, muted bool) error
	MutedThreads() []string

	ListVIPs() []VIP
	PutVIP(VIP) (VIP, error)
	DeleteVIP(address string) error

	NotifyPrefs() NotifyPrefs
	PutNotifyPrefs(NotifyPrefs) NotifyPrefs

	SetSenderCategory(address, category string) error
	ListSenderCategories() []SenderCat
}

func asExtra(s Store) ExtraStore {
	if e, ok := s.(ExtraStore); ok {
		return e
	}
	return nil
}

// BoolPtr is the address of b, for the optional flags of a FlagPatch.
func BoolPtr(v bool) *bool { return &v }

func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HasTag reports whether m carries the tag id.
func HasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// ToggleTag adds the tag id to m if absent, removes it if present.
func ToggleTag(tags []string, tag string) []string {
	out := tags[:0:0]
	found := false
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			found = true
			continue
		}
		out = append(out, t)
	}
	if !found {
		return append(append([]string(nil), tags...), tag)
	}
	return out
}

func formatSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		kb := (n + 512) / 1024
		if kb < 1 {
			kb = 1
		}
		return fmt.Sprintf("%d KB", kb)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// FormatDate renders t the way the message list shows a date.
func FormatDate(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	local := t.In(now.Location())
	y1, m1, d1 := now.Date()
	y2, m2, d2 := local.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return local.Format("3:04 PM")
	}
	yd := now.AddDate(0, 0, -1)
	yy, ym, ydN := yd.Date()
	if y2 == yy && m2 == ym && d2 == ydN {
		return "Yesterday"
	}
	if now.Sub(local) >= 0 && now.Sub(local) < 7*24*time.Hour {
		return local.Format("Monday")
	}
	return local.Format("2006-01-02")
}
