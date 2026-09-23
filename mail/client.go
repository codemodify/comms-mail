package mail

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Client is the mailclientui side of the Unix JSON-RPC. No IMAP here.
type Client struct {
	Socket string

	mu      sync.Mutex
	conn    net.Conn
	w       *bufio.Writer
	pending map[uint64]chan Response
	nextID  atomic.Uint64
	onEvent func(Event)
	closed  atomic.Bool

	bodyMu sync.Mutex
	bodies map[MessageID]Message
}

// Dial connects to mailclientd at socket.
func Dial(socket string) (*Client, error) {
	if socket == "" {
		socket = DefaultSocket()
	}
	c, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("mailclientui: dial %s: %w (is mailclientd running?)", socket, err)
	}
	cli := &Client{
		Socket:  socket,
		conn:    c,
		w:       bufio.NewWriter(c),
		pending: map[uint64]chan Response{},
	}
	go cli.readLoop()
	return cli, nil
}

// DialWait retries Dial until timeout.
func DialWait(socket string, wait time.Duration) (*Client, error) {
	deadline := time.Now().Add(wait)
	var last error
	for time.Now().Before(deadline) {
		cli, err := Dial(socket)
		if err == nil {
			return cli, nil
		}
		last = err
		time.Sleep(25 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("mailclientui: timeout dialing %s", socket)
	}
	return nil, last
}

// Close drops the socket.
func (c *Client) Close() error {
	c.closed.Store(true)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// OnEvent registers a handler for mail.changed / mail.fetched.
func (c *Client) OnEvent(fn func(Event)) {
	c.mu.Lock()
	c.onEvent = fn
	c.mu.Unlock()
}

func (c *Client) readLoop() {
	defer c.failPending()
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 0, 64*1024), maxRPCLine)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var peek map[string]json.RawMessage
		if err := json.Unmarshal(line, &peek); err != nil {
			continue
		}
		if _, hasErr := peek["error"]; hasErr || len(peek["result"]) > 0 || len(peek["id"]) > 0 && peek["method"] == nil {
			var resp Response
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			c.deliver(resp)
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.Method == "" {
			continue
		}
		ev := Event{Method: req.Method}
		var p eventParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
			ev.Folder = p.FolderID
			ev.Reason = p.Reason
			ev.Count = p.Count
			ev.Title = p.Title
			ev.Body = p.Body
			ev.VIP = p.VIP
			ev.AccountID = p.AccountID
		}
		c.mu.Lock()
		fn := c.onEvent
		c.mu.Unlock()
		if fn != nil {
			fn(ev)
		}
	}
}

// failPending releases every in-flight call when the socket dies, instead of
// making each one wait out its timeout.
func (c *Client) failPending() {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[uint64]chan Response{}
	c.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- Response{Error: &RPCError{Code: -32000, Message: "mailclientd: connection closed"}}:
		default:
		}
	}
}
func (c *Client) deliver(resp Response) {
	id, ok := jsonNumber(resp.ID)
	if !ok {
		return
	}
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- resp
	}
}

func jsonNumber(v any) (uint64, bool) {
	switch n := v.(type) {
	case float64:
		return uint64(n), true
	case json.Number:
		u, err := n.Int64()
		return uint64(u), err == nil
	case int:
		return uint64(n), true
	case int64:
		return uint64(n), true
	case uint64:
		return n, true
	default:
		return 0, false
	}
}

// callTimeout is how long a client waits for one method. Sync, fetch and the
// connection probe legitimately take minutes on a first run, so they are not
// held to the interactive budget.
func callTimeout(method string) time.Duration {
	switch method {
	case MethodSyncRun, MethodMessagesFetch, MethodOutboxFlush, MethodStatusSet:
		return 30 * time.Minute
	case MethodComposeSend, MethodMessagesPart, MethodMessagesOpen, MethodMessagesGet, MethodMessagesSource:
		return 5 * time.Minute
	case MethodAccountsTest, MethodHostsProbe:
		return 2 * time.Minute
	default:
		return 30 * time.Second
	}
}

func (c *Client) call(method string, params any, result any) error {
	id := c.nextID.Add(1)
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req := Request{JSONRPC: RPCVersion, ID: id, Method: method}
	if params != nil {
		req.Params = raw
	}
	ch := make(chan Response, 1)
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("mailclientui: not connected")
	}
	c.pending[id] = ch
	if err := writeJSON(c.w, req); err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	timer := time.NewTimer(callTimeout(method))
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if result == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
			return nil
		}
		return json.Unmarshal(resp.Result, result)
	case <-timer.C:
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("mailclientui: timeout on %s", method)
	}
}
func (c *Client) Ping() error {
	var out map[string]string
	return c.call(MethodPing, nil, &out)
}

func (c *Client) Status() (DaemonStatus, error) {
	var st DaemonStatus
	err := c.call(MethodStatusGet, nil, &st)
	return st, err
}

func (c *Client) Accounts() ([]Account, error) {
	var out []Account
	err := c.call(MethodAccountsList, nil, &out)
	return out, err
}

func (c *Client) PutAccount(cfg AccountConfig) (Account, error) {
	var out Account
	err := c.call(MethodAccountsPut, cfg, &out)
	return out, err
}

func (c *Client) DeleteAccount(id string) error {
	return c.call(MethodAccountsDel, accountDelParams{ID: id}, nil)
}

func (c *Client) ListFolders(accountID string) ([]Folder, error) {
	var out []Folder
	err := c.call(MethodFoldersList, folderListParams{AccountID: accountID}, &out)
	return out, err
}

func (c *Client) GetFolder(id FolderID) (Folder, bool, error) {
	var f Folder
	err := c.call(MethodFoldersGet, folderGetParams{ID: id}, &f)
	if err != nil {
		return Folder{}, false, err
	}
	return f, f.ID != "", nil
}

func (c *Client) CreateFolder(accountID, name string, parent FolderID) (Folder, error) {
	var f Folder
	err := c.call(MethodFoldersCreate, folderCreateParams{AccountID: accountID, Name: name, Parent: parent}, &f)
	return f, err
}

func (c *Client) ListMessages(folder FolderID, filter Filter) ([]Message, error) {
	var out []Message
	p := messagesListParams{FolderID: folder}
	if !filterEmpty(filter) {
		p.Filter = &filter
	}
	err := c.call(MethodMessagesList, p, &out)
	if out == nil {
		out = []Message{}
	}
	return out, err
}

func (c *Client) GetSource(id MessageID) (string, error) {
	var r sourceResult
	err := c.call(MethodMessagesSource, messageIDParams{ID: id}, &r)
	if err != nil {
		return "", err
	}
	return r.RFC822, nil
}

func (c *Client) GetMessage(id MessageID) (Message, bool, error) {
	if m, ok := c.cachedBody(id); ok {
		return m, true, nil
	}
	var m Message
	err := c.call(MethodMessagesGet, messageIDParams{ID: id}, &m)
	if err != nil {
		return Message{}, false, err
	}
	c.rememberBody(m)
	return m, m.ID != "", nil
}

func (c *Client) Search(q SearchQuery) ([]Message, error) {
	var out []Message
	err := c.call(MethodMessagesSearch, searchParams{AccountID: q.AccountID, FolderID: q.Folder, Filter: q.Filter}, &out)
	if out == nil {
		out = []Message{}
	}
	return out, err
}

func (c *Client) SetFlags(id MessageID, patch FlagPatch) error {
	err := c.call(MethodMessagesFlags, setFlagsParams{ID: id, Patch: patchToWire(patch)}, nil)
	if err == nil {
		c.patchCachedBody(id, patch)
	}
	return err
}

func (c *Client) Move(ids []MessageID, dest FolderID) error {
	err := c.call(MethodMessagesMove, moveParams{IDs: ids, Dest: dest}, nil)
	if err == nil {
		c.dropCachedBodies(ids...)
	}
	return err
}

func (c *Client) Delete(ids []MessageID) error {
	err := c.call(MethodMessagesDelete, deleteParams{IDs: ids}, nil)
	if err == nil {
		c.dropCachedBodies(ids...)
	}
	return err
}

func (c *Client) Append(folder FolderID, msg Message) (MessageID, error) {
	var r appendResult
	err := c.call(MethodMessagesAppend, appendParams{FolderID: folder, Message: msg}, &r)
	return r.ID, err
}

func (c *Client) Update(id MessageID, msg Message) error {
	return c.call(MethodMessagesUpdate, updateParams{ID: id, Message: msg}, nil)
}

func (c *Client) Fetch(accountID string) (int, error) {
	var r fetchResult
	err := c.call(MethodMessagesFetch, fetchParams{AccountID: accountID}, &r)
	return r.Count, err
}

func (c *Client) Unread(folder FolderID) (int, error) {
	var r unreadResult
	err := c.call(MethodUnreadGet, unreadParams{FolderID: folder}, &r)
	return r.Count, err
}

// UnreadAll returns every folder's unread count in one round trip.
func (c *Client) UnreadAll() (map[FolderID]int, int, error) {
	var r unreadAllResult
	err := c.call(MethodUnreadAll, nil, &r)
	if r.Counts == nil {
		r.Counts = map[FolderID]int{}
	}
	return r.Counts, r.Total, err
}

func (c *Client) UnreadTotal() (int, error) {
	var r unreadResult
	err := c.call(MethodUnreadGet, unreadParams{}, &r)
	return r.Count, err
}

func (c *Client) Send(accountID string, msg Message, draftID MessageID) (MessageID, error) {
	return c.SendIdent(accountID, "", msg, draftID, nil)
}

// SendIdent submits a message. Attachments are read by the caller (the UI)
// and shipped as bytes: the daemon never opens a path on a client's behalf.
func (c *Client) SendIdent(accountID, identityID string, msg Message, draftID MessageID, attachPaths []string) (MessageID, error) {
	files, err := ReadAttachments(attachPaths)
	if err != nil {
		return "", err
	}
	return c.SendFiles(accountID, identityID, msg, draftID, files)
}

// SendFiles is SendIdent with the attachment bytes already in hand.
func (c *Client) SendFiles(accountID, identityID string, msg Message, draftID MessageID, files []AttachedFile) (MessageID, error) {
	var r appendResult
	err := c.call(MethodComposeSend, composeParams{
		AccountID: accountID, IdentityID: identityID, Message: msg, ID: draftID, Attachments: files,
	}, &r)
	return r.ID, err
}

// ReadAttachments loads compose attachments in the UI process.
func ReadAttachments(paths []string) ([]AttachedFile, error) {
	var out []AttachedFile
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("attach %s: %w", path, err)
		}
		if len(b) > maxAttachmentBytes {
			return nil, fmt.Errorf("attach %s: %d bytes exceeds the %d byte limit", path, len(b), maxAttachmentBytes)
		}
		name := attachFileName(filepath.Base(path))
		out = append(out, AttachedFile{Name: name, MIME: guessMIME(name), Data: b})
	}
	return out, nil
}

// maxAttachmentBytes caps one compose attachment (the RPC line limit is
// larger so a few of these still fit in one request).
const maxAttachmentBytes = 32 << 20

func (c *Client) SaveDraft(accountID string, msg Message, draftID MessageID) (MessageID, error) {
	var r appendResult
	err := c.call(MethodComposeDraft, composeParams{AccountID: accountID, Message: msg, ID: draftID}, &r)
	return r.ID, err
}

func (c *Client) Identities(accountID string) ([]Identity, error) {
	var out []Identity
	err := c.call(MethodIdentitiesList, identityListParams{AccountID: accountID}, &out)
	if out == nil {
		out = []Identity{}
	}
	return out, err
}

func (c *Client) PutIdentity(id Identity) (Identity, error) {
	var out Identity
	err := c.call(MethodIdentitiesPut, id, &out)
	return out, err
}

func (c *Client) DeleteIdentity(id string) error {
	return c.call(MethodIdentitiesDel, identityIDParams{ID: id}, nil)
}

func (c *Client) Tags() ([]Tag, error) {
	var out []Tag
	err := c.call(MethodTagsList, nil, &out)
	if out == nil {
		out = []Tag{}
	}
	return out, err
}

func (c *Client) PutTag(t Tag) (Tag, error) {
	var out Tag
	err := c.call(MethodTagsPut, t, &out)
	return out, err
}

func (c *Client) DeleteTag(name string) error {
	return c.call(MethodTagsDel, tagNameParams{Name: name}, nil)
}

func (c *Client) VirtualFolders() ([]Folder, error) {
	var out []Folder
	err := c.call(MethodFoldersVirtual, nil, &out)
	if out == nil {
		out = []Folder{}
	}
	return out, err
}

func (c *Client) Rules() ([]FilterRule, error) {
	var out []FilterRule
	err := c.call(MethodFiltersList, nil, &out)
	if out == nil {
		out = []FilterRule{}
	}
	return out, err
}

func (c *Client) PutRule(r FilterRule) (FilterRule, error) {
	var out FilterRule
	err := c.call(MethodFiltersPut, r, &out)
	return out, err
}

func (c *Client) DeleteRule(id string) error {
	return c.call(MethodFiltersDel, ruleIDParams{ID: id}, nil)
}

func (c *Client) ApplyRules(folder FolderID) (int, error) {
	var r applyResult
	err := c.call(MethodFiltersApply, applyRulesParams{FolderID: folder}, &r)
	return r.Count, err
}

func (c *Client) GetPart(id MessageID, partID string) (PartData, error) {
	var p PartData
	err := c.call(MethodMessagesPart, partParams{ID: id, PartID: partID}, &p)
	return p, err
}

func (c *Client) OpenPart(id MessageID, partID string) (PartData, error) {
	var p PartData
	err := c.call(MethodMessagesOpen, partParams{ID: id, PartID: partID}, &p)
	return p, err
}

func (c *Client) Sync(accountID string) (SyncResult, error) {
	var r SyncResult
	err := c.call(MethodSyncRun, fetchParams{AccountID: accountID}, &r)
	return r, err
}

func (c *Client) SetOnline(online bool) (int, error) {
	var r fetchResult
	err := c.call(MethodStatusSet, onlineParams{Online: online}, &r)
	return r.Count, err
}

func (c *Client) Outbox() ([]OutboxOp, error) {
	var out []OutboxOp
	err := c.call(MethodOutboxList, nil, &out)
	if out == nil {
		out = []OutboxOp{}
	}
	return out, err
}

func (c *Client) FlushOutbox() (int, error) {
	var r fetchResult
	err := c.call(MethodOutboxFlush, nil, &r)
	return r.Count, err
}

func (c *Client) SmartFolders() ([]SmartFolder, error) {
	var out []SmartFolder
	err := c.call(MethodSmartList, nil, &out)
	if out == nil {
		out = []SmartFolder{}
	}
	return out, err
}

func (c *Client) PutSmartFolder(sf SmartFolder) (SmartFolder, error) {
	var out SmartFolder
	err := c.call(MethodSmartPut, sf, &out)
	return out, err
}

func (c *Client) DeleteSmartFolder(id string) error {
	return c.call(MethodSmartDel, ruleIDParams{ID: id}, nil)
}

func (c *Client) MuteThread(threadID string, muted bool) error {
	return c.call(MethodThreadMute, muteParams{ThreadID: threadID, Muted: muted}, nil)
}

func (c *Client) MutedThreads() ([]string, error) {
	var out []string
	err := c.call(MethodThreadMuted, nil, &out)
	if out == nil {
		out = []string{}
	}
	return out, err
}

func (c *Client) VIPs() ([]VIP, error) {
	var out []VIP
	err := c.call(MethodVIPList, nil, &out)
	if out == nil {
		out = []VIP{}
	}
	return out, err
}

func (c *Client) PutVIP(v VIP) (VIP, error) {
	var out VIP
	err := c.call(MethodVIPPut, v, &out)
	return out, err
}

func (c *Client) DeleteVIP(address string) error {
	return c.call(MethodVIPDel, vipDelParams{Address: address}, nil)
}

func (c *Client) NotifyPrefs() (NotifyPrefs, error) {
	var p NotifyPrefs
	err := c.call(MethodNotifyGet, nil, &p)
	return p, err
}

func (c *Client) PutNotifyPrefs(p NotifyPrefs) (NotifyPrefs, error) {
	var out NotifyPrefs
	err := c.call(MethodNotifyPut, p, &out)
	return out, err
}

func (c *Client) SetSenderCategory(address, category string) error {
	return c.call(MethodCategorySet, categoryParams{Address: address, Category: category}, nil)
}

func (c *Client) SenderCategories() ([]SenderCat, error) {
	var out []SenderCat
	err := c.call(MethodCategoryList, nil, &out)
	if out == nil {
		out = []SenderCat{}
	}
	return out, err
}

func (c *Client) StartOAuth(provider, address, name, clientID, clientSecret, flow string) (OAuthStart, error) {
	var out OAuthStart
	err := c.call(MethodOAuthStart, oauthReq{
		Provider: provider, Address: address, Name: name,
		ClientID: clientID, ClientSecret: clientSecret, Flow: flow,
	}, &out)
	return out, err
}

func (c *Client) PollOAuth(sessionID string) (OAuthPoll, error) {
	var out OAuthPoll
	err := c.call(MethodOAuthPoll, oauthPollParams{SessionID: sessionID}, &out)
	return out, err
}

func (c *Client) CancelOAuth(sessionID string) error {
	return c.call(MethodOAuthCancel, oauthPollParams{SessionID: sessionID}, nil)
}

func (c *Client) GuessHosts(address string) (GuessedHosts, error) {
	var out GuessedHosts
	err := c.call(MethodHostsGuess, guessParams{Address: address}, &out)
	return out, err
}

func (c *Client) TestAccount(req ProbeRequest) (ProbeResult, error) {
	var out ProbeResult
	err := c.call(MethodAccountsTest, req, &out)
	return out, err
}

func (c *Client) ProbeHosts(req ProbeRequest) (ProbeResult, error) {
	var out ProbeResult
	err := c.call(MethodHostsProbe, req, &out)
	return out, err
}

func filterEmpty(f Filter) bool {
	return f.Query == "" && !f.Unread && !f.Starred && !f.Attachment && f.Tag == "" &&
		!f.Sender && !f.Recipients && !f.SubjectOnly && !f.Body
}

func (c *Client) cachedBody(id MessageID) (Message, bool) {
	c.bodyMu.Lock()
	defer c.bodyMu.Unlock()
	m, ok := c.bodies[id]
	if !ok || !messageHasBody(m) {
		return Message{}, false
	}
	return m.Clone(), true
}

func (c *Client) rememberBody(m Message) {
	if m.ID == "" || !messageHasBody(m) {
		return
	}
	c.bodyMu.Lock()
	defer c.bodyMu.Unlock()
	if c.bodies == nil {
		c.bodies = map[MessageID]Message{}
	}
	c.bodies[m.ID] = m.Clone()
}

func (c *Client) patchCachedBody(id MessageID, patch FlagPatch) {
	c.bodyMu.Lock()
	defer c.bodyMu.Unlock()
	m, ok := c.bodies[id]
	if !ok {
		return
	}
	if patch.Read != nil {
		m.Read = *patch.Read
	}
	if patch.Starred != nil {
		m.Starred = *patch.Starred
	}
	if patch.Tags != nil {
		m.Tags = append([]string(nil), (*patch.Tags)...)
	}
	c.bodies[id] = m
}

func (c *Client) dropCachedBodies(ids ...MessageID) {
	c.bodyMu.Lock()
	defer c.bodyMu.Unlock()
	for _, id := range ids {
		delete(c.bodies, id)
	}
}
