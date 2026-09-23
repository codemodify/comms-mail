package mail

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IMAP environment (mailclientd only — the UI never dials IMAP):
//
//	UITK_MAIL=imap
//	UITK_MAIL_HOST=imap.example.com:993
//	UITK_MAIL_USER=you@example.com
//	UITK_MAIL_PASS=secret
//	UITK_MAIL_TLS=1          # default on when port is 993
//	UITK_MAIL_NAME=Ada       # optional From: display name
//
// This is a skeleton: CONNECT + LOGIN + LIST + SELECT + FETCH + STORE +
// APPEND. MIME trees, IDLE, UTF-7 mailbox names, and MOVE are not
// production-complete. Missing config returns a clear Health() error and
// MemoryStore is available only when UITK_MAIL=memory.

// IMAPStore is a single-account IMAP backend for mailclientd.
type IMAPStore struct {
	mu       sync.Mutex
	host     string
	user     string
	pass     string
	name     string
	useTLS   bool
	conn     net.Conn
	r        *bufio.Reader
	tag      int
	selected string
	accounts []Account
	folders  []Folder
	cache    map[MessageID]Message
	seq      map[MessageID]int
	health   error
}

// NewIMAPStoreFromEnv builds an IMAPStore from UITK_MAIL_* (not connected).
func NewIMAPStoreFromEnv() *IMAPStore {
	host := strings.TrimSpace(os.Getenv("UITK_MAIL_HOST"))
	user := strings.TrimSpace(os.Getenv("UITK_MAIL_USER"))
	pass := os.Getenv("UITK_MAIL_PASS")
	name := strings.TrimSpace(os.Getenv("UITK_MAIL_NAME"))
	if name == "" {
		name = DisplayName(user)
	}
	tlsOn := true
	if v := strings.TrimSpace(os.Getenv("UITK_MAIL_TLS")); v != "" {
		tlsOn = v != "0" && !strings.EqualFold(v, "false")
	} else if host != "" && !strings.HasSuffix(host, ":993") && strings.Contains(host, ":") {
		tlsOn = false
	}
	s := &IMAPStore{
		host: host, user: user, pass: pass, name: name, useTLS: tlsOn,
		cache: map[MessageID]Message{}, seq: map[MessageID]int{},
	}
	if host == "" || user == "" || pass == "" {
		s.health = fmt.Errorf("imap: set UITK_MAIL_HOST, UITK_MAIL_USER, UITK_MAIL_PASS (see docs/mail.md)")
	}
	if user != "" {
		s.accounts = []Account{{ID: "imap", Name: name, Address: user}}
	}
	return s
}

// NewIMAPStore is a test helper (plain TCP, no TLS).
func NewIMAPStore(host, user, pass string) *IMAPStore {
	s := &IMAPStore{
		host: host, user: user, pass: pass, name: DisplayName(user),
		cache: map[MessageID]Message{}, seq: map[MessageID]int{},
		accounts: []Account{{ID: "imap", Name: DisplayName(user), Address: user}},
	}
	return s
}

func (s *IMAPStore) Backend() string { return "imap" }

func (s *IMAPStore) Health() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.health
}

func (s *IMAPStore) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectLocked()
}

func (s *IMAPStore) connectLocked() error {
	if s.conn != nil {
		return nil
	}
	if s.host == "" || s.user == "" || s.pass == "" {
		s.health = fmt.Errorf("imap: set UITK_MAIL_HOST, UITK_MAIL_USER, UITK_MAIL_PASS (see docs/mail.md)")
		return s.health
	}
	dialer := net.Dialer{Timeout: 12 * time.Second}
	var conn net.Conn
	var err error
	if s.useTLS {
		conn, err = tls.DialWithDialer(&dialer, "tcp", s.host, &tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", s.host)
	}
	if err != nil {
		s.health = fmt.Errorf("imap: connect %s: %w", s.host, err)
		return s.health
	}
	s.conn = conn
	s.r = bufio.NewReader(conn)
	if _, err := s.readLineLocked(); err != nil { // greeting
		s.closeLocked()
		s.health = fmt.Errorf("imap: greeting: %w", err)
		return s.health
	}
	if _, err := s.cmdLocked("LOGIN %s %s", imapQuote(s.user), imapQuote(s.pass)); err != nil {
		s.closeLocked()
		s.health = fmt.Errorf("imap: LOGIN: %w", err)
		return s.health
	}
	s.health = nil
	if len(s.accounts) == 0 {
		s.accounts = []Account{{ID: "imap", Name: s.name, Address: s.user}}
	}
	return nil
}

func (s *IMAPStore) Accounts() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Account, len(s.accounts))
	copy(out, s.accounts)
	return out
}

func (s *IMAPStore) ListFolders(accountID string) []Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connectLocked(); err != nil {
		return nil
	}
	lines, err := s.cmdLocked("LIST %s %s", imapQuote(""), imapQuote("*"))
	if err != nil {
		s.health = err
		return nil
	}
	var folders []Folder
	for _, ln := range lines {
		name, kind := parseListLine(ln)
		if name == "" {
			continue
		}
		folders = append(folders, Folder{
			ID: FolderID(name), AccountID: "imap", Name: name, Kind: kind,
		})
	}
	if len(folders) == 0 {
		folders = []Folder{{ID: "INBOX", AccountID: "imap", Name: "INBOX", Kind: FolderInbox}}
	}
	s.folders = folders
	out := make([]Folder, len(folders))
	copy(out, folders)
	return out
}

func (s *IMAPStore) GetFolder(id FolderID) (Folder, bool) {
	for _, f := range s.ListFolders("imap") {
		if f.ID == id {
			return f, true
		}
	}
	return Folder{}, false
}

func (s *IMAPStore) CreateFolder(accountID, name string, parent FolderID) (Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connectLocked(); err != nil {
		return Folder{}, err
	}
	mbox := name
	if parent != "" {
		mbox = string(parent) + "/" + name
	}
	if _, err := s.cmdLocked("CREATE %s", imapQuote(mbox)); err != nil {
		return Folder{}, err
	}
	f := Folder{ID: FolderID(mbox), AccountID: "imap", Name: mbox, Kind: FolderCustom, Parent: parent}
	s.folders = append(s.folders, f)
	return f, nil
}

func (s *IMAPStore) ListMessages(folder FolderID) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.selectLocked(string(folder)); err != nil {
		return nil
	}
	lines, err := s.cmdLocked("FETCH 1:* (FLAGS RFC822.SIZE BODY.PEEK[HEADER.FIELDS (FROM TO CC SUBJECT DATE)])")
	if err != nil {
		s.health = err
		return nil
	}
	msgs := parseFetchLines(lines, folder)
	s.cache = map[MessageID]Message{}
	s.seq = map[MessageID]int{}
	for i, m := range msgs {
		s.cache[m.ID] = m
		s.seq[m.ID] = i + 1
	}
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = m.Clone()
	}
	return out
}

func (s *IMAPStore) GetMessage(id MessageID) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.cache[id]; ok && messageHasBody(m) {
		return m.Clone(), true
	}
	seq, ok := s.seq[id]
	if !ok {
		if m, ok := s.cache[id]; ok {
			return m.Clone(), true
		}
		return Message{}, false
	}
	lines, err := s.cmdLocked("FETCH %d (FLAGS RFC822.SIZE BODY.PEEK[HEADER] BODY.PEEK[TEXT])", seq)
	if err != nil {
		if m, ok := s.cache[id]; ok {
			return m.Clone(), true
		}
		return Message{}, false
	}
	msgs := parseFetchLines(lines, "")
	if len(msgs) == 0 {
		if m, ok := s.cache[id]; ok {
			return m.Clone(), true
		}
		return Message{}, false
	}
	m := msgs[0]
	m.ID = id
	if f, ok := s.cache[id]; ok && m.Folder == "" {
		m.Folder = f.Folder
	}
	s.cache[id] = m
	return m.Clone(), true
}

func (s *IMAPStore) GetRaw(id MessageID) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.cache[id]
	seq, okSeq := s.seq[id]
	if !ok || !okSeq {
		return nil, fmt.Errorf("mail: no message %s", id)
	}
	remote := "INBOX"
	for _, f := range s.folders {
		if f.ID == m.Folder {
			if f.Remote != "" {
				remote = f.Remote
			} else if f.Name != "" {
				remote = f.Name
			}
			break
		}
	}
	if err := s.selectLocked(remote); err != nil {
		return nil, err
	}
	lines, err := s.cmdLocked("FETCH %d (BODY.PEEK[])", seq)
	if err != nil {
		return nil, err
	}
	raw := extractLiteralBody(lines)
	if len(raw) == 0 {
		return nil, fmt.Errorf("mail: empty RFC822 for %s", id)
	}
	return raw, nil
}

func (s *IMAPStore) Search(q SearchQuery) []Message {
	var all []Message
	if q.Folder != "" {
		all = s.ListMessages(q.Folder)
	} else {
		for _, f := range s.ListFolders("imap") {
			all = append(all, s.ListMessages(f.ID)...)
		}
	}
	var out []Message
	for _, m := range all {
		if q.Filter.Match(m) {
			out = append(out, m)
		}
	}
	return out
}

func (s *IMAPStore) SetFlags(id MessageID, patch FlagPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, ok := s.seq[id]
	if !ok {
		return fmt.Errorf("imap: no message %s (SELECT the folder first)", id)
	}
	var add, rem []string
	if patch.Read != nil {
		if *patch.Read {
			add = append(add, `\Seen`)
		} else {
			rem = append(rem, `\Seen`)
		}
	}
	if patch.Starred != nil {
		if *patch.Starred {
			add = append(add, `\Flagged`)
		} else {
			rem = append(rem, `\Flagged`)
		}
	}
	if len(add) > 0 {
		if _, err := s.cmdLocked("STORE %d +FLAGS.SILENT (%s)", seq, strings.Join(add, " ")); err != nil {
			return err
		}
	}
	if len(rem) > 0 {
		if _, err := s.cmdLocked("STORE %d -FLAGS.SILENT (%s)", seq, strings.Join(rem, " ")); err != nil {
			return err
		}
	}
	if m, ok := s.cache[id]; ok {
		if patch.Read != nil {
			m.Read = *patch.Read
		}
		if patch.Starred != nil {
			m.Starred = *patch.Starred
		}
		if patch.Tags != nil {
			m.Tags = append([]string(nil), (*patch.Tags)...)
		}
		s.cache[id] = m
	}
	return nil
}

func (s *IMAPStore) Move(ids []MessageID, dest FolderID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connectLocked(); err != nil {
		return err
	}
	for _, id := range ids {
		seq, ok := s.seq[id]
		if !ok {
			return fmt.Errorf("imap: no message %s", id)
		}
		if _, err := s.cmdLocked("COPY %d %s", seq, imapQuote(string(dest))); err != nil {
			return fmt.Errorf("imap: COPY (MOVE fallback): %w", err)
		}
		if _, err := s.cmdLocked("STORE %d +FLAGS.SILENT (\\Deleted)", seq); err != nil {
			return err
		}
	}
	_, err := s.cmdLocked("EXPUNGE")
	return err
}

func (s *IMAPStore) Delete(ids []MessageID) error {
	trash := FolderID("Trash")
	for _, f := range s.ListFolders("imap") {
		if f.Kind == FolderTrash {
			trash = f.ID
			break
		}
	}
	return s.Move(ids, trash)
}

func (s *IMAPStore) Append(folder FolderID, msg Message) (MessageID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connectLocked(); err != nil {
		return "", err
	}
	raw := string(BuildRFC822(msg, Identity{Address: msg.From, Name: msg.From}, nil))
	if _, err := s.cmdLiteralLocked(fmt.Sprintf("APPEND %s {%d}", imapQuote(string(folder)), len(raw)), raw); err != nil {
		return "", err
	}
	id := MessageID(fmt.Sprintf("imap-%d", time.Now().UnixNano()))
	msg.ID = id
	msg.Folder = folder
	msg.AccountID = "imap"
	s.cache[id] = msg
	return id, nil
}

func (s *IMAPStore) Update(id MessageID, msg Message) error {
	s.mu.Lock()
	cur, ok := s.cache[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("imap: no message %s", id)
	}
	msg.Folder = cur.Folder
	if _, err := s.Append(cur.Folder, msg); err != nil {
		return err
	}
	return s.Delete([]MessageID{id})
}

func (s *IMAPStore) Fetch(accountID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connectLocked(); err != nil {
		return 0, err
	}
	if _, err := s.cmdLocked("NOOP"); err != nil {
		return 0, err
	}
	return 0, nil
}

func (s *IMAPStore) Unread(folder FolderID) int {
	n := 0
	for _, m := range s.ListMessages(folder) {
		if !m.Read {
			n++
		}
	}
	return n
}

func (s *IMAPStore) UnreadTotal() int {
	n := 0
	for _, f := range s.ListFolders("imap") {
		n += s.Unread(f.ID)
	}
	return n
}

func (s *IMAPStore) MessageCount(folder FolderID) int {
	return len(s.ListMessages(folder))
}

func (s *IMAPStore) Identities(accountID string) []Identity {
	var out []Identity
	for _, a := range s.Accounts() {
		if accountID != "" && a.ID != accountID {
			continue
		}
		out = append(out, Identity{ID: a.ID + "-default", AccountID: a.ID, Name: a.Name, Address: a.Address, Default: true})
	}
	return out
}
func (s *IMAPStore) PutIdentity(id Identity) (Identity, error) { return id, nil }
func (s *IMAPStore) DeleteIdentity(id string) error            { return nil }
func (s *IMAPStore) ListTags() []Tag                           { return DefaultTags() }
func (s *IMAPStore) PutTag(t Tag) (Tag, error)                 { return t, nil }
func (s *IMAPStore) DeleteTag(name string) error {
	if IsSystemTag(name) {
		return fmt.Errorf("mail: cannot remove system tag %s", canonicalSystemTagName(name))
	}
	return nil
}
func (s *IMAPStore) VirtualFolders() []Folder                 { return defaultVirtualFolders() }
func (s *IMAPStore) ListRules() []FilterRule                  { return nil }
func (s *IMAPStore) PutRule(r FilterRule) (FilterRule, error) { return r, nil }
func (s *IMAPStore) DeleteRule(id string) error               { return nil }
func (s *IMAPStore) ApplyRules(folder FolderID) (int, error)  { return 0, nil }

func (s *IMAPStore) GetPart(id MessageID, partID string) (PartData, error) {
	m, ok := s.GetMessage(id)
	if !ok {
		return PartData{}, fmt.Errorf("imap: no message %s", id)
	}
	return PartData{Part: Part{ID: partID, MIMEType: "text/plain"}, Data: []byte(m.Body)}, nil
}
func (s *IMAPStore) OpenPart(id MessageID, partID string) (PartData, error) {
	return s.GetPart(id, partID)
}
func (s *IMAPStore) Sync(accountID string) (SyncResult, error) {
	n, err := s.Fetch(accountID)
	return SyncResult{AccountID: accountID, New: n}, err
}

func (s *IMAPStore) PutAccount(in AccountConfig) (Account, error) {
	a, err := SanitizeAccountConfig(in)
	if err != nil {
		return Account{}, err
	}
	file, _ := LoadConfig()
	file.Accounts = upsertAccountConfig(file.Accounts, a)
	if err := SaveConfig(file); err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.host = a.IMAP.Host
	s.user = a.IMAP.Username(a.Address)
	s.name = a.Name
	s.health = nil
	acct := accountFromConfig(a, ProtoIMAP)
	s.accounts = []Account{acct}
	return acct, nil
}

func (s *IMAPStore) DeleteAccount(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("mail: account id required")
	}
	file, _ := LoadConfig()
	file.Accounts = dropAccountConfig(file.Accounts, id)
	if err := SaveConfig(file); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	out := s.accounts[:0]
	for _, a := range s.accounts {
		if a.ID == id {
			found = true
			continue
		}
		out = append(out, a)
	}
	if !found && (id == "imap" || id == s.user || slug(s.user) == id) {
		found = true
		out = nil
	}
	if !found {
		return fmt.Errorf("mail: no account %s", id)
	}
	s.accounts = out
	s.closeLocked()
	s.health = fmt.Errorf("imap: account removed")
	return nil
}

func (s *IMAPStore) selectLocked(mbox string) error {
	if err := s.connectLocked(); err != nil {
		return err
	}
	if s.selected == mbox && mbox != "" {
		return nil
	}
	if _, err := s.cmdLocked("SELECT %s", imapQuote(mbox)); err != nil {
		return err
	}
	s.selected = mbox
	return nil
}

func (s *IMAPStore) cmdLocked(format string, args ...any) ([]string, error) {
	if s.conn == nil {
		return nil, fmt.Errorf("imap: not connected")
	}
	s.tag++
	tag := fmt.Sprintf("A%03d", s.tag)
	line := tag + " " + fmt.Sprintf(format, args...) + "\r\n"
	if _, err := io.WriteString(s.conn, line); err != nil {
		return nil, err
	}
	return s.readUntilTagged(tag)
}

func (s *IMAPStore) cmdLiteralLocked(head, literal string) ([]string, error) {
	if s.conn == nil {
		return nil, fmt.Errorf("imap: not connected")
	}
	s.tag++
	tag := fmt.Sprintf("A%03d", s.tag)
	if _, err := io.WriteString(s.conn, tag+" "+head+"\r\n"); err != nil {
		return nil, err
	}
	cont, err := s.readLineLocked()
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(cont, "+") {
		return nil, fmt.Errorf("imap: expected + continuation, got %s", cont)
	}
	if _, err := io.WriteString(s.conn, literal+"\r\n"); err != nil {
		return nil, err
	}
	return s.readUntilTagged(tag)
}

func (s *IMAPStore) readUntilTagged(tag string) ([]string, error) {
	var lines []string
	for {
		ln, err := s.readLineLocked()
		if err != nil {
			return lines, err
		}
		if strings.HasPrefix(ln, tag+" ") {
			rest := strings.TrimSpace(ln[len(tag)+1:])
			if strings.HasPrefix(rest, "OK") {
				return lines, nil
			}
			return lines, fmt.Errorf("imap: %s", rest)
		}
		lines = append(lines, ln)
	}
}

func (s *IMAPStore) readLineLocked() (string, error) {
	if s.r == nil {
		return "", fmt.Errorf("imap: not connected")
	}
	ln, err := s.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(ln, "\r\n"), nil
}

func (s *IMAPStore) closeLocked() {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
		s.r = nil
		s.selected = ""
	}
}

func imapQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func parseListLine(ln string) (string, FolderKind) {
	u := strings.ToUpper(ln)
	if !strings.Contains(u, " LIST ") && !strings.HasPrefix(u, "* LIST ") {
		return "", FolderCustom
	}
	name := ""
	if i := strings.LastIndex(ln, `"`); i > 0 {
		if j := strings.LastIndex(ln[:i], `"`); j >= 0 {
			name = ln[j+1 : i]
		}
	}
	if name == "" {
		fields := strings.Fields(ln)
		if len(fields) > 0 {
			name = strings.Trim(fields[len(fields)-1], `"`)
		}
	}
	kind := FolderCustom
	switch {
	case strings.EqualFold(name, "INBOX"):
		kind = FolderInbox
	case strings.Contains(u, `\TRASH`) || strings.EqualFold(name, "Trash"):
		kind = FolderTrash
	case strings.Contains(u, `\SENT`) || strings.EqualFold(name, "Sent"):
		kind = FolderSent
	case strings.Contains(u, `\DRAFTS`) || strings.EqualFold(name, "Drafts"):
		kind = FolderDrafts
	case strings.Contains(u, `\JUNK`) || strings.EqualFold(name, "Junk") || strings.EqualFold(name, "Spam"):
		kind = FolderJunk
	case strings.Contains(u, `\ARCHIVE`) || strings.EqualFold(name, "Archives"):
		kind = FolderArchive
	}
	return name, kind
}

func parseFetchLines(lines []string, folder FolderID) []Message {
	var out []Message
	var cur *Message
	var body strings.Builder
	inText := false
	flush := func() {
		if cur == nil {
			return
		}
		if body.Len() > 0 && cur.Body == "" {
			cur.Body = body.String()
		}
		if cur.ID == "" {
			cur.ID = MessageID(fmt.Sprintf("imap-%d", len(out)+1))
		}
		if cur.Folder == "" {
			cur.Folder = folder
		}
		cur.AccountID = "imap"
		if cur.Size <= 0 {
			cur.Size = len(cur.Subject) + len(cur.Body) + 80
		}
		out = append(out, *cur)
		cur = nil
		body.Reset()
		inText = false
	}
	for _, ln := range lines {
		u := strings.ToUpper(ln)
		if strings.HasPrefix(u, "* ") && strings.Contains(u, " FETCH ") {
			flush()
			m := Message{Folder: folder, AccountID: "imap"}
			if seq := fetchSeq(ln); seq > 0 {
				m.ID = MessageID(fmt.Sprintf("imap-%d", seq))
			}
			if strings.Contains(u, `\SEEN`) {
				m.Read = true
			}
			if strings.Contains(u, `\FLAGGED`) {
				m.Starred = true
			}
			if i := strings.Index(u, "RFC822.SIZE "); i >= 0 {
				rest := ln[i+len("RFC822.SIZE "):]
				m.Size, _ = strconv.Atoi(strings.Fields(rest)[0])
			}
			cur = &m
			continue
		}
		if cur == nil {
			continue
		}
		if strings.Contains(u, "BODY[TEXT]") {
			inText = true
			continue
		}
		if inText {
			if strings.HasPrefix(ln, ")") {
				inText = false
				continue
			}
			body.WriteString(ln)
			body.WriteByte('\n')
			continue
		}
		applyHeader(cur, ln)
	}
	flush()
	return out
}

func fetchSeq(ln string) int {
	fields := strings.Fields(ln)
	if len(fields) < 3 {
		return 0
	}
	n, _ := strconv.Atoi(fields[1])
	return n
}

func applyHeader(m *Message, ln string) {
	i := strings.IndexByte(ln, ':')
	if i < 0 {
		return
	}
	key := strings.ToLower(strings.TrimSpace(ln[:i]))
	val := strings.TrimSpace(ln[i+1:])
	switch key {
	case "from":
		m.From = val
	case "to":
		m.To = val
	case "cc":
		m.Cc = val
	case "subject":
		m.Subject = val
	case "date":
		if t, err := time.Parse(time.RFC1123Z, val); err == nil {
			m.Date = t
		} else if t, err := time.Parse(time.RFC1123, val); err == nil {
			m.Date = t
		}
	}
}
