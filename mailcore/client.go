package mailcore

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

// Client is the comms-mail side of the Unix JSON-RPC. No IMAP here.
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

// Dial connects to comms-maild at socket.
func Dial(socket string) (*Client, error) {
	if socket == "" {
		socket = DefaultSocket()
	}
	c, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("comms-mail: dial %s: %w (is comms-maild running?)", socket, err)
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
		last = fmt.Errorf("comms-mail: timeout dialing %s", socket)
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
		case ch <- Response{Error: &RPCError{Code: -32000, Message: "comms-maild: connection closed"}}:
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
	case MethodComposeSend, MethodMessagesPart, MethodMessagesOpen, MethodMessagesGet, MethodMessagesSource,
		MethodMessagesInvite, MethodInviteReply, MethodMessagesImages, MethodMessagesRaw, MethodMessagesSecurity, MethodComposeKeys,
		MethodKeysList, MethodKeysMakePGP, MethodKeysImportSMIME,
		MethodKeysView, MethodKeysUse, MethodKeysUnlock, MethodKeysImport, MethodKeysRemove, MethodKeysBackup:
		return 5 * time.Minute
	case MethodImportMail:
		return 30 * time.Minute // a large mbox takes a while
	case MethodAccountsTest, MethodHostsProbe, MethodImportScan, MethodImportScanPath, MethodImagesFetch, MethodSearchServer:
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
		return fmt.Errorf("comms-mail: not connected")
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
		return fmt.Errorf("comms-mail: timeout on %s", method)
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

// AccountConfig is an account's settings, to edit them: everything but its
// passwords, which a save that leaves them empty keeps.
func (c *Client) AccountConfig(id string) (AccountConfig, error) {
	var out AccountConfig
	err := c.call(MethodAccountsGet, accountDelParams{ID: id}, &out)
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
	if id == "" {
		return Folder{}, false, nil // no folder chosen (no account yet): nothing to ask
	}
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

// SenderCheck checks message id's sender: the server's verdict on its
// domain, and look-alikes of the people you write to.
func (c *Client) SenderCheck(id MessageID) (SenderCheck, error) {
	var r SenderCheck
	err := c.call(MethodMessagesSender, messageIDParams{ID: id}, &r)
	return r, err
}

// OwnKeys are your keys in secretvault, for each address you send from.
func (c *Client) OwnKeys() (OwnKeys, error) {
	var r OwnKeys
	err := c.call(MethodKeysList, nil, &r)
	return r, err
}

// MakePGPKey has secretvault make an OpenPGP key for address.
func (c *Client) MakePGPKey(address string) (OwnKeys, error) {
	var r OwnKeys
	err := c.call(MethodKeysMakePGP, makePGPParams{Address: address}, &r)
	return r, err
}

// ImportSMIME hands a .p12 file and its password to secretvault. The
// password is wiped once sent.
func (c *Client) ImportSMIME(pkcs12, password []byte) (OwnKeys, error) {
	defer clear(password)
	var r OwnKeys
	err := c.call(MethodKeysImportSMIME, importSMIMEParams{PKCS12: pkcs12, Password: password}, &r)
	return r, err
}

// KeysView is Settings › Security › Keys for format (FormatOpenPGP,
// FormatSMIME).
func (c *Client) KeysView(format string) (KeysView, error) {
	var r KeysView
	err := c.call(MethodKeysView, keysParams{Format: format}, &r)
	return r, err
}

// UseKeys has engine do format's work and, for comms-mail's own, keeps its
// keys in store (vault: secretvault's vault, "" its default; passphrase
// for the encrypted file, wiped once sent).
func (c *Client) UseKeys(format, engine, store, vault string, passphrase []byte) error {
	defer clear(passphrase)
	return c.call(MethodKeysUse, keysParams{Format: format, Engine: engine, Store: store, Vault: vault, Passphrase: passphrase}, nil)
}

// UnlockKeys opens where comms-mail keeps format's keys, for this run.
func (c *Client) UnlockKeys(format string, passphrase []byte) error {
	defer clear(passphrase)
	return c.call(MethodKeysUnlock, keysParams{Format: format, Passphrase: passphrase}, nil)
}

// ImportKeys brings in keys of format from data; passphrase opens a
// protected key or a .p12 (ErrPassphraseNeeded's text when one is needed).
func (c *Client) ImportKeys(format string, data, passphrase []byte) (int, error) {
	defer clear(passphrase)
	var r struct {
		Imported int `json:"imported"`
	}
	err := c.call(MethodKeysImport, keysParams{Format: format, Data: data, Passphrase: passphrase}, &r)
	return r.Imported, err
}

// RemoveKey forgets a key of format: yours (own) or someone else's.
func (c *Client) RemoveKey(format, id string, own bool) error {
	return c.call(MethodKeysRemove, keysParams{Format: format, ID: id, Own: own}, nil)
}

// KeyBackup is your key of format with id, locked with passphrase, and the
// file name to save it as.
func (c *Client) KeyBackup(format, id string, passphrase []byte) ([]byte, string, error) {
	defer clear(passphrase)
	var r keyBackup
	err := c.call(MethodKeysBackup, keysParams{Format: format, ID: id, Passphrase: passphrase}, &r)
	return r.Data, r.Name, err
}

// SigningKeys says whether secretvault can sign as from (and whether it
// is in use at all, or locked).
func (c *Client) SigningKeys(from string) (SigningKeys, error) {
	var r SigningKeys
	err := c.call(MethodComposeKeys, composeKeysParams{From: from}, &r)
	return r, err
}

// MessageSecurity has secretvault check message id and, with decrypt,
// decrypt it (it may ask the person first). The decrypted content is the
// caller's to hold, and is not cached here.
func (c *Client) MessageSecurity(id MessageID, decrypt bool) (MessageSecurity, error) {
	var r MessageSecurity
	err := c.call(MethodMessagesSecurity, securityParams{ID: id, Decrypt: decrypt}, &r)
	return r, err
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

// ImportScan finds the mail clients installed for this user and what each
// has: accounts (passwords blank) and on-disk mail.
func (c *Client) ImportScan() ([]ImportSource, error) {
	var out []ImportSource
	err := c.call(MethodImportScan, nil, &out)
	return out, err
}

// ImportScanPath finds the mail in a file or folder the user chose.
func (c *Client) ImportScanPath(path string) (ImportSource, error) {
	var out ImportSource
	err := c.call(MethodImportScanPath, importPathParams{Path: path}, &out)
	return out, err
}

// ImportFilters adds other clients' filters as rules.
func (c *Client) ImportFilters(sets []FilterSet) (FilterImport, error) {
	var r FilterImport
	err := c.call(MethodImportFilters, importFiltersParams{Sets: sets}, &r)
	return r, err
}

// ImportContacts adds contacts to the address book; it returns how many
// were new.
func (c *Client) ImportContacts(cs []Contact) (int, error) {
	var r countResult
	err := c.call(MethodImportContacts, importContactsParams{Contacts: cs}, &r)
	return r.Count, err
}

// ImportMail reads the chosen on-disk stores into the local account.
func (c *Client) ImportMail(stores []LocalMailStore) (ImportResult, error) {
	var r ImportResult
	err := c.call(MethodImportMail, importMailParams{Stores: stores}, &r)
	return r, err
}

// DeleteFolder removes a user-created folder.
func (c *Client) DeleteFolder(id FolderID) error {
	return c.call(MethodFoldersDelete, folderIDParams{FolderID: id}, nil)
}

// MoveFolder puts a user-created folder under parent ("" = top level).
func (c *Client) MoveFolder(id, parent FolderID) (Folder, error) {
	var f Folder
	err := c.call(MethodFoldersMove, folderMoveParams{FolderID: id, Parent: parent}, &f)
	return f, err
}

// FocusFolder tells the daemon which folder the window shows, so changes to
// it arrive at once (IDLE), not at the next poll.
// SecretsStatus says where the secrets are kept and whether they can be
// read now.
func (c *Client) SecretsStatus() (SecretsStatus, error) {
	var st SecretsStatus
	err := c.call(MethodSecretsStatus, struct{}{}, &st)
	return st, err
}

// UseStore moves every saved secret to store (keyring, encrypted, plain)
// and keeps them there from now on; passphrase locks the encrypted file.
func (c *Client) UseStore(store, passphrase string) error {
	return c.UseStoreIn(store, passphrase, "")
}

// UseStoreIn is UseStore, naming secretvault's vault for the secretvault
// store ("" for its default vault).
func (c *Client) UseStoreIn(store, passphrase, vault string) error {
	return c.call(MethodSecretsUse, vaultParams{Store: store, Passphrase: passphrase, Vault: vault}, nil)
}

// UnlockSecrets unlocks the store for this run of the daemon: the
// encrypted file with the passphrase, the desktop keyring with its prompt.
func (c *Client) UnlockSecrets(passphrase string) error {
	return c.call(MethodSecretsUnlock, vaultParams{Passphrase: passphrase}, nil)
}

// ChangePassphrase locks the secrets with next instead of old.
func (c *Client) ChangePassphrase(old, next string) error {
	return c.call(MethodVaultChange, vaultParams{Passphrase: old, Next: next}, nil)
}

// ResetVault deletes every saved password and sign-in (a forgotten
// passphrase).
func (c *Client) ResetVault() error {
	return c.call(MethodVaultReset, struct{}{}, nil)
}

func (c *Client) FocusFolder(id FolderID) error {
	return c.call(MethodFoldersFocus, folderIDParams{FolderID: id}, nil)
}

// CompactFolder removes what is marked deleted in a folder, on the server.
func (c *Client) CompactFolder(id FolderID) error {
	return c.call(MethodFoldersCompact, folderIDParams{FolderID: id}, nil)
}

// RenameFolder gives a user-created folder a new name.
func (c *Client) RenameFolder(id FolderID, name string) (Folder, error) {
	var f Folder
	err := c.call(MethodFoldersRename, folderRenameParams{FolderID: id, Name: name}, &f)
	return f, err
}

// MarkFolderRead marks every message in a folder read.
func (c *Client) MarkFolderRead(id FolderID) error {
	return c.call(MethodFoldersMarkRead, folderIDParams{FolderID: id}, nil)
}

// SuggestContacts completes a recipient from the address book.
func (c *Client) SuggestContacts(query string, limit int) ([]Contact, error) {
	var out []Contact
	err := c.call(MethodContactsSuggest, contactsSuggestParams{Query: query, Limit: limit}, &out)
	if out == nil {
		out = []Contact{}
	}
	return out, err
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
	err := c.call(MethodMessagesUpdate, updateParams{ID: id, Message: msg}, nil)
	c.dropCachedBodies(id)
	return err
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
// SendFiles submits a message. A *QueuedError means it was not sent now
// but waits in the Outbox to be sent again: done, not failed.
func (c *Client) SendFiles(accountID, identityID string, msg Message, draftID MessageID, files []AttachedFile) (MessageID, error) {
	return c.SendForward(accountID, identityID, msg, draftID, files, "")
}

// SendForward is SendFiles for a message that forwards forwardOf, which is
// marked forwarded once it is sent.
func (c *Client) SendForward(accountID, identityID string, msg Message, draftID MessageID, files []AttachedFile, forwardOf MessageID) (MessageID, error) {
	var r appendResult
	err := c.call(MethodComposeSend, ComposeParams{
		AccountID: accountID, IdentityID: identityID, Message: msg, ID: draftID, Attachments: files, ForwardOf: forwardOf,
	}, &r)
	if err == nil && r.Queued != "" {
		err = &QueuedError{Reason: r.Queued}
	}
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
		name := AttachFileName(filepath.Base(path))
		out = append(out, AttachedFile{Name: name, MIME: guessMIME(name), Data: b})
	}
	return out, nil
}

// maxAttachmentBytes caps one compose attachment (the RPC line limit is
// larger so a few of these still fit in one request).
const maxAttachmentBytes = 32 << 20

func (c *Client) SaveDraft(accountID string, msg Message, draftID MessageID) (MessageID, error) {
	var r appendResult
	err := c.call(MethodComposeDraft, ComposeParams{AccountID: accountID, Message: msg, ID: draftID}, &r)
	// The draft's old text must not come back from the body cache.
	c.dropCachedBodies(draftID, r.ID)
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

// SearchServer asks the mail server which messages contain every word of
// query — in folder, or every folder when it is empty — including mail
// whose body is not downloaded.
func (c *Client) SearchServer(folder FolderID, query string) ([]Message, error) {
	var out []Message
	err := c.call(MethodSearchServer, searchServerParams{FolderID: folder, Query: query}, &out)
	return out, err
}

// Raw returns message id exactly as stored (the bytes of its .eml).
func (c *Client) Raw(id MessageID) ([]byte, error) {
	var raw []byte
	err := c.call(MethodMessagesRaw, messageIDParams{ID: id}, &raw)
	return raw, err
}

// InlineImages returns message id's image parts by Content-ID, for the
// HTML view's cid: images.
func (c *Client) InlineImages(id MessageID) ([]InlineImage, error) {
	var out []InlineImage
	err := c.call(MethodMessagesImages, messageIDParams{ID: id}, &out)
	return out, err
}

// FetchImages has the daemon download remote images (public addresses
// only, bounded in size and time).
func (c *Client) FetchImages(urls []string) ([]RemoteImage, error) {
	var out []RemoteImage
	err := c.call(MethodImagesFetch, imagesFetchParams{URLs: urls}, &out)
	return out, err
}

// RemoteImageSenders lists the senders whose remote images load without
// asking.
func (c *Client) RemoteImageSenders() ([]string, error) {
	var out []string
	err := c.call(MethodImagesSenders, nil, &out)
	return out, err
}

// AllowRemoteImages remembers (or forgets) a sender whose remote images
// load without asking.
func (c *Client) AllowRemoteImages(address string, allow bool) error {
	return c.call(MethodImagesAllow, imagesAllowParams{Address: address, Allow: allow}, nil)
}

// Invite returns the calendar invitation message id carries, or nil.
func (c *Client) Invite(id MessageID) (*Invite, error) {
	var inv *Invite
	err := c.call(MethodMessagesInvite, messageIDParams{ID: id}, &inv)
	return inv, err
}

// ReplyInvite answers the invitation in message id — ACCEPTED, TENTATIVE
// or DECLINED — by mail to its organizer, and returns it updated.
func (c *Client) ReplyInvite(id MessageID, partstat string) (Invite, error) {
	return c.AnswerInvite(id, InviteAnswer{PartStat: partstat})
}

// AnswerInvite answers the invitation in message id with a comment, as an
// identity, or without telling the organizer; DECLINECOUNTER turns down a
// guest's proposed new time.
func (c *Client) AnswerInvite(id MessageID, ans InviteAnswer) (Invite, error) {
	var inv Invite
	err := c.call(MethodInviteReply, inviteReplyParams{ID: id, PartStat: ans.PartStat, Comment: ans.Comment,
		NoSend: ans.NoSend, IdentityID: ans.IdentityID}, &inv)
	return inv, err
}

// OpenPart has the daemon write a part to its cache and open it. When the
// daemon could not open it (started without a display), this process —
// the window's, same user and machine — opens the file it wrote.
func (c *Client) OpenPart(id MessageID, partID string) (PartData, error) {
	var p PartData
	err := c.call(MethodMessagesOpen, partParams{ID: id, PartID: partID}, &p)
	if err == nil && p.Path != "" && !p.Opened {
		p.Opened = openCachedFile(p.Path)
	}
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

// CachedMessage is the message with its text as this client last fetched
// it, without asking the daemon; false when it has not fetched it.
func (c *Client) CachedMessage(id MessageID) (Message, bool) { return c.cachedBody(id) }

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
	if patch.Answered != nil {
		m.Answered = *patch.Answered
	}
	if patch.Forwarded != nil {
		m.Forwarded = *patch.Forwarded
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

// SpecialFolderClient asks the daemon for an account's top-level folder
// of the given kind.
func SpecialFolderClient(cli *Client, accountID string, kind FolderKind) (Folder, bool) {
	folders, err := cli.ListFolders(accountID)
	if err != nil {
		return Folder{}, false
	}
	for _, f := range folders {
		if f.Kind == kind && f.Parent == "" {
			return f, true
		}
	}
	return Folder{}, false
}
