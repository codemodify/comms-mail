package mail

import "encoding/json"

// JSON-RPC 2.0 over a Unix socket, one JSON object per line (NDJSON).
// mailclientd is the server; mailclientui is a client. Notifications
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
	MethodFoldersList    = "folders.list"
	MethodFoldersGet     = "folders.get"
	MethodFoldersCreate  = "folders.create"
	MethodMessagesList   = "messages.list"
	MethodMessagesGet    = "messages.get"
	MethodMessagesSource = "messages.getSource"
	MethodMessagesSearch = "messages.search"
	MethodMessagesFlags  = "messages.setFlags"
	MethodMessagesMove   = "messages.move"
	MethodMessagesDelete = "messages.delete"
	MethodMessagesAppend = "messages.append"
	MethodMessagesUpdate = "messages.update"
	MethodMessagesFetch  = "messages.fetch"
	MethodUnreadGet      = "unread.get"
	MethodUnreadAll      = "unread.all"
	MethodComposeSend    = "compose.send"
	MethodComposeDraft   = "compose.saveDraft"
	MethodIdentitiesList = "identities.list"
	MethodIdentitiesPut  = "identities.put"
	MethodIdentitiesDel  = "identities.delete"
	MethodTagsList       = "tags.list"
	MethodTagsPut        = "tags.put"
	MethodTagsDel        = "tags.delete"
	MethodFoldersVirtual = "folders.virtual"
	MethodFiltersList    = "filters.list"
	MethodFiltersPut     = "filters.put"
	MethodFiltersDel     = "filters.delete"
	MethodFiltersApply   = "filters.apply"
	MethodMessagesPart   = "messages.getPart"
	MethodMessagesOpen   = "messages.openPart"
	MethodSyncRun        = "sync.run"
	MethodStatusSet      = "status.set"
	MethodOutboxList     = "outbox.list"
	MethodOutboxFlush    = "outbox.flush"
	MethodSmartList      = "smart.list"
	MethodSmartPut       = "smart.put"
	MethodSmartDel       = "smart.delete"
	MethodThreadMute     = "threads.mute"
	MethodThreadMuted    = "threads.muted"
	MethodVIPList        = "vip.list"
	MethodVIPPut         = "vip.put"
	MethodVIPDel         = "vip.delete"
	MethodNotifyGet      = "notify.get"
	MethodNotifyPut      = "notify.put"
	MethodCategorySet    = "senders.setCategory"
	MethodCategoryList   = "senders.categories"
	MethodOAuthStart     = "oauth.start"
	MethodOAuthPoll      = "oauth.poll"
	MethodOAuthCancel    = "oauth.cancel"
	MethodHostsGuess     = "hosts.guess"
	MethodHostsProbe     = "hosts.probe"
	MethodAccountsTest   = "accounts.test"

	EventChanged = "mail.changed"
	EventFetched = "mail.fetched"
	EventSynced  = "mail.synced"
	EventNotify  = "mail.notify"
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

type searchParams struct {
	AccountID string   `json:"accountId,omitempty"`
	FolderID  FolderID `json:"folderId,omitempty"`
	Filter    Filter   `json:"filter"`
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

type composeParams struct {
	AccountID  string    `json:"accountId"`
	IdentityID string    `json:"identityId,omitempty"`
	Message    Message   `json:"message"`
	ID         MessageID `json:"id,omitempty"`
	// Attachments are the file bytes themselves. The daemon never opens a
	// path supplied by a client (see Server.send).
	Attachments []AttachedFile `json:"attachments,omitempty"`
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
