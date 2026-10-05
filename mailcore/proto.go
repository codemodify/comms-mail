package mailcore

import "encoding/json"

// JSON-RPC 2.0 over a Unix socket, one JSON object per line (NDJSON).
// comms-maild is the server; comms-mail is a client. Notifications
// (events) have no "id".
//
// Methods:
//
//	ping
//	status.get
//	accounts.list
//	accounts.put        AccountConfig (password and/or passEnv)
//	accounts.delete     {id}
//	folders.list        {accountId}
//	folders.get         {id}
//	folders.create      {accountId, name, parent?}
//	messages.list       {folderId, filter?}
//	messages.get        {id}
//	messages.getSource  {id}            // raw RFC822 / .eml bytes
//	messages.search     {accountId?, folderId?, filter}
//	messages.setFlags   {id, patch}
//	messages.move       {ids, dest}
//	messages.delete     {ids}
//	messages.append     {folderId, message}
//	messages.update     {id, message}
//	messages.fetch      {accountId}     // Get Messages
//	unread.get          {folderId?}     // omit folderId → total
//	unread.all                          // every folder + virtual, one call
//	compose.send        {accountId, identityId?, message, attachments?}
//	compose.saveDraft   {accountId, message, id?}
//	identities.list     {accountId?}
//	identities.put      Identity
//	identities.delete   {id}
//	tags.list
//	tags.put            {name, color, system?, previous?}
//	tags.delete         {name}
//	folders.virtual
//	filters.list
//	filters.put         FilterRule
//	filters.delete      {id}
//	filters.apply       {folderId?}
//	messages.getPart    {id, partId}
//	messages.openPart   {id, partId}
//	sync.run            {accountId?}
//	status.set          {online}
//	outbox.list / outbox.flush
//	smart.* / threads.mute / vip.* / notify.*
//	senders.setCategory / oauth.* / hosts.guess / hosts.probe / accounts.test
//	accounts.delete
//
// Events (server → client, no id):
//
//	mail.changed        {folderId, reason}
//	mail.fetched        {accountId, count}
//	mail.synced         {accountId, count}
//	mail.notify         {title, body, vip, count}

const RPCVersion = "2.0"

// Method names.
const (
	MethodPing           = "ping"
	MethodStatusGet      = "status.get"
	MethodAccountsList   = "accounts.list"
	MethodAccountsPut    = "accounts.put"
	MethodAccountsDel    = "accounts.delete"
	MethodAccountsGet    = "accounts.get"
	MethodFoldersList    = "folders.list"
	MethodFoldersGet     = "folders.get"
	MethodFoldersCreate  = "folders.create"
	MethodMessagesList   = "messages.list"
	MethodMessagesGet    = "messages.get"
	MethodMessagesSource = "messages.getSource"
	MethodMessagesRaw    = "messages.getRaw"
	// MethodMessagesSecurity has secretvault check a signed or encrypted
	// message, and decrypt it with decrypt (MessageSecurity).
	MethodMessagesSecurity = "messages.security"
	// MethodComposeKeys says whether secretvault can sign as a From
	// address (SigningKeys).
	MethodComposeKeys = "compose.keys"
	// MethodMessagesSender checks a message's sender (SenderCheck).
	MethodMessagesSender = "messages.sender"
	// Your own keys, in secretvault (OwnKeys, MakePGPKey, ImportSMIME).
	MethodKeysList        = "keys.list"
	MethodKeysMakePGP     = "keys.makePGP"
	MethodKeysImportSMIME = "keys.importSMIME"
	MethodKeysView        = "keys.view"
	MethodKeysUse         = "keys.use"
	MethodKeysUnlock      = "keys.unlock"
	MethodKeysImport      = "keys.import"
	MethodKeysRemove      = "keys.remove"
	MethodKeysBackup      = "keys.backup"
	MethodMessagesSearch  = "messages.search"
	MethodSearchServer    = "messages.searchServer"
	MethodContactsSuggest = "contacts.suggest"
	MethodFoldersDelete   = "folders.delete"
	MethodFoldersRename   = "folders.rename"
	MethodFoldersMove     = "folders.move"
	MethodFoldersCompact  = "folders.compact"
	MethodFoldersFocus    = "folders.focus"
	// Where secrets are kept (secrets.go), and the encrypted file's
	// passphrase (vault.go).
	MethodSecretsStatus   = "secrets.status"
	MethodSecretsUse      = "secrets.use"
	MethodSecretsUnlock   = "secrets.unlock"
	MethodVaultChange     = "vault.change"
	MethodVaultReset      = "vault.reset"
	MethodFoldersMarkRead = "folders.markRead"
	MethodImportScan      = "import.scan"
	MethodImportScanPath  = "import.scanPath"
	MethodImportMail      = "import.mail"
	MethodImportContacts  = "import.contacts"
	MethodImportFilters   = "import.filters"
	MethodMessagesFlags   = "messages.setFlags"
	MethodMessagesMove    = "messages.move"
	MethodMessagesDelete  = "messages.delete"
	MethodMessagesAppend  = "messages.append"
	MethodMessagesUpdate  = "messages.update"
	MethodMessagesFetch   = "messages.fetch"
	MethodUnreadGet       = "unread.get"
	MethodUnreadAll       = "unread.all"
	MethodComposeSend     = "compose.send"
	MethodComposeDraft    = "compose.saveDraft"
	MethodIdentitiesList  = "identities.list"
	MethodIdentitiesPut   = "identities.put"
	MethodIdentitiesDel   = "identities.delete"
	MethodTagsList        = "tags.list"
	MethodTagsPut         = "tags.put"
	MethodTagsDel         = "tags.delete"
	MethodFoldersVirtual  = "folders.virtual"
	MethodFiltersList     = "filters.list"
	MethodFiltersPut      = "filters.put"
	MethodFiltersDel      = "filters.delete"
	MethodFiltersApply    = "filters.apply"
	MethodMessagesPart    = "messages.getPart"
	MethodMessagesInvite  = "messages.invite"
	MethodMessagesImages  = "messages.inlineImages"
	MethodImagesFetch     = "images.fetch"
	MethodImagesSenders   = "images.senders"
	MethodImagesAllow     = "images.allowSender"
	MethodInviteReply     = "invite.reply"
	MethodMessagesOpen    = "messages.openPart"
	MethodSyncRun         = "sync.run"
	MethodStatusSet       = "status.set"
	MethodOutboxList      = "outbox.list"
	MethodOutboxFlush     = "outbox.flush"
	MethodSmartList       = "smart.list"
	MethodSmartPut        = "smart.put"
	MethodSmartDel        = "smart.delete"
	MethodThreadMute      = "threads.mute"
	MethodThreadMuted     = "threads.muted"
	MethodVIPList         = "vip.list"
	MethodVIPPut          = "vip.put"
	MethodVIPDel          = "vip.delete"
	MethodNotifyGet       = "notify.get"
	MethodNotifyPut       = "notify.put"
	MethodCategorySet     = "senders.setCategory"
	MethodCategoryList    = "senders.categories"
	MethodOAuthStart      = "oauth.start"
	MethodOAuthPoll       = "oauth.poll"
	MethodOAuthCancel     = "oauth.cancel"
	MethodHostsGuess      = "hosts.guess"
	MethodHostsProbe      = "hosts.probe"
	MethodAccountsTest    = "accounts.test"

	EventChanged = "mail.changed"
	EventFetched = "mail.fetched"
	EventSynced  = "mail.synced"
	EventNotify  = "mail.notify"
	// EventProgress reports a long operation's progress (Title says it).
	EventProgress = "mail.progress"
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Event is a server notification (no id).
type Event struct {
	Method    string
	Reason    string
	Folder    FolderID
	AccountID string
	Count     int
	Title     string
	Body      string
	VIP       bool
}

// Wire params.

type folderListParams struct {
	AccountID string `json:"accountId"`
}

type folderGetParams struct {
	ID FolderID `json:"id"`
}

type folderCreateParams struct {
	AccountID string   `json:"accountId"`
	Name      string   `json:"name"`
	Parent    FolderID `json:"parent,omitempty"`
}

type messagesListParams struct {
	FolderID FolderID `json:"folderId"`
	Filter   *Filter  `json:"filter,omitempty"`
}

type messageIDParams struct {
	ID MessageID `json:"id"`
}

type makePGPParams struct {
	Address string `json:"address"`
}

// keysParams are a keys.* call's: the format, and what the call needs.
type keysParams struct {
	Format     string `json:"format"`
	Store      string `json:"store,omitempty"`
	Vault      string `json:"vault,omitempty"`
	Passphrase []byte `json:"passphrase,omitempty"`
	Data       []byte `json:"data,omitempty"`
	ID         string `json:"id,omitempty"`
	Own        bool   `json:"own,omitempty"`
}

// keyBackup is keys.backup's answer.
type keyBackup struct {
	Data []byte `json:"data"`
	Name string `json:"name"`
}

type importSMIMEParams struct {
	PKCS12   []byte `json:"pkcs12"`
	Password []byte `json:"password,omitempty"`
}

type composeKeysParams struct {
	From string `json:"from"`
}

type securityParams struct {
	ID      MessageID `json:"id"`
	Decrypt bool      `json:"decrypt,omitempty"`
}

type searchParams struct {
	AccountID string   `json:"accountId,omitempty"`
	FolderID  FolderID `json:"folderId,omitempty"`
	Filter    Filter   `json:"filter"`
}

type importPathParams struct {
	Path string `json:"path"`
}

type importMailParams struct {
	Stores []LocalMailStore `json:"stores"`
}

type folderIDParams struct {
	FolderID FolderID `json:"folderId"`
}

type folderMoveParams struct {
	FolderID FolderID `json:"folderId"`
	Parent   FolderID `json:"parent,omitempty"`
}

type folderRenameParams struct {
	FolderID FolderID `json:"folderId"`
	Name     string   `json:"name"`
}

type contactsSuggestParams struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type setFlagsParams struct {
	ID    MessageID     `json:"id"`
	Patch wireFlagPatch `json:"patch"`
}

type wireFlagPatch struct {
	Read    *bool     `json:"read,omitempty"`
	Starred *bool     `json:"starred,omitempty"`
	Tags    *[]string `json:"tags,omitempty"`
}

func (w wireFlagPatch) to() FlagPatch {
	return FlagPatch{Read: w.Read, Starred: w.Starred, Tags: w.Tags}
}

func patchToWire(p FlagPatch) wireFlagPatch {
	return wireFlagPatch{Read: p.Read, Starred: p.Starred, Tags: p.Tags}
}

type moveParams struct {
	IDs  []MessageID `json:"ids"`
	Dest FolderID    `json:"dest"`
}

type deleteParams struct {
	IDs []MessageID `json:"ids"`
}

type appendParams struct {
	FolderID FolderID `json:"folderId"`
	Message  Message  `json:"message"`
}

type updateParams struct {
	ID      MessageID `json:"id"`
	Message Message   `json:"message"`
}

type fetchParams struct {
	AccountID string `json:"accountId"`
}

type unreadParams struct {
	FolderID FolderID `json:"folderId,omitempty"`
}

// ComposeParams are the parameters of the mail.send RPC.
type ComposeParams struct {
	AccountID  string    `json:"accountId"`
	IdentityID string    `json:"identityId,omitempty"`
	Message    Message   `json:"message"`
	ID         MessageID `json:"id,omitempty"`
	// Attachments are the file bytes themselves. The daemon never opens a
	// path supplied by a client (see Server.send).
	Attachments []AttachedFile `json:"attachments,omitempty"`
	// ForwardOf is the message this one forwards: once sent, it is marked
	// forwarded ($Forwarded), here and on the server.
	ForwardOf MessageID `json:"forwardOf,omitempty"`
}

type identityListParams struct {
	AccountID string `json:"accountId,omitempty"`
}

type identityIDParams struct {
	ID string `json:"id"`
}

type accountDelParams struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId,omitempty"`
}

func (p accountDelParams) id() string {
	if p.ID != "" {
		return p.ID
	}
	return p.AccountID
}

type ruleIDParams struct {
	ID string `json:"id"`
}

type tagNameParams struct {
	Name string `json:"name"`
}

type applyRulesParams struct {
	FolderID FolderID `json:"folderId,omitempty"`
}

type vaultParams struct {
	Store      string `json:"store,omitempty"` // secrets.use: where to
	Vault      string `json:"vault,omitempty"` // secrets.use: secretvault's vault ("" its default)
	Passphrase string `json:"passphrase,omitempty"`
	Next       string `json:"next,omitempty"` // vault.change: the new one
}

type searchServerParams struct {
	FolderID FolderID `json:"folderId,omitempty"`
	Query    string   `json:"query"`
}

type importContactsParams struct {
	Contacts []Contact `json:"contacts"`
}

type importFiltersParams struct {
	Sets []FilterSet `json:"sets"`
}

type countResult struct {
	Count int `json:"count"`
}

type imagesFetchParams struct {
	URLs []string `json:"urls"`
}

type imagesAllowParams struct {
	Address string `json:"address"`
	Allow   bool   `json:"allow"`
}

// inviteReplyParams answers the invitation in message ID.
type inviteReplyParams struct {
	ID         MessageID `json:"id"`
	PartStat   string    `json:"partstat"`
	Comment    string    `json:"comment,omitempty"`
	NoSend     bool      `json:"noSend,omitempty"`
	IdentityID string    `json:"identityId,omitempty"`
}

type partParams struct {
	ID     MessageID `json:"id"`
	PartID string    `json:"partId,omitempty"`
}

type sourceResult struct {
	ID     MessageID `json:"id"`
	RFC822 string    `json:"rfc822"`
}

type applyResult struct {
	Count int `json:"count"`
}

type unreadResult struct {
	Count int `json:"count"`
}

// unreadAllResult is one round trip for every folder's unread count.
// Rebuilding the sidebar used to issue one unread.get per folder and per tag.
type unreadAllResult struct {
	Counts map[FolderID]int `json:"counts"`
	Total  int              `json:"total"`
}

type fetchResult struct {
	Count int `json:"count"`
}

type appendResult struct {
	ID MessageID `json:"id"`
	// Queued says the message was not sent but waits in the Outbox, and why.
	Queued string `json:"queued,omitempty"`
}

type eventParams struct {
	FolderID  FolderID `json:"folderId,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	AccountID string   `json:"accountId,omitempty"`
	Count     int      `json:"count,omitempty"`
	Title     string   `json:"title,omitempty"`
	Body      string   `json:"body,omitempty"`
	VIP       bool     `json:"vip,omitempty"`
}

type onlineParams struct {
	Online bool `json:"online"`
}

type muteParams struct {
	ThreadID string `json:"threadId"`
	Muted    bool   `json:"muted"`
}

type vipDelParams struct {
	Address string `json:"address"`
}

type categoryParams struct {
	Address  string `json:"address"`
	Category string `json:"category"`
}

type oauthPollParams struct {
	SessionID string `json:"sessionId"`
}

type guessParams struct {
	Address string `json:"address"`
}

func decodeParams[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}
