package mail

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server is mailclientd: one Store, many Unix-socket JSON-RPC clients.
type Server struct {
	Store  Store
	Socket string

	mu      sync.Mutex
	conns   map[*rpcConn]struct{}
	serving bool
	oauth   *oauthHub
}

type rpcConn struct {
	net.Conn
	mu sync.Mutex
	w  *bufio.Writer
}

func (c *rpcConn) send(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeJSON(c.w, v)
}

// sendTimeout writes an event with a deadline and drops the client when it
// stops reading, so a wedged UI cannot stall the daemon's event fan-out.
func (c *rpcConn) sendTimeout(v any, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.Conn.SetWriteDeadline(time.Now().Add(d))
	defer func() { _ = c.Conn.SetWriteDeadline(time.Time{}) }()
	if err := writeJSON(c.w, v); err != nil {
		_ = c.Conn.Close()
		return err
	}
	return nil
}

// NewServer owns store and advertises socket in status.get.
func NewServer(store Store, socket string) *Server {
	if store == nil {
		store = NewMemoryStore(time.Time{})
	}
	if socket == "" {
		socket = DefaultSocket()
	}
	return &Server{Store: store, Socket: socket, conns: map[*rpcConn]struct{}{}, oauth: newOAuthHub()}
}

// ListenAndServe binds a Unix socket and serves until ctx is cancelled.
// ListenAndServe binds a hardened Unix socket and serves until ctx is
// cancelled. The socket lives in an owner-only directory, is chmodded 0600,
// is protected by a lock file, and every connection is checked against our
// own uid — it is the only authorisation boundary the daemon has.
func ListenAndServe(ctx context.Context, socket string, store Store) error {
	ln, lock, err := listenSocket(socket)
	if err != nil {
		return err
	}
	defer lock.release()
	defer func() { _ = os.Remove(socket + ".lock") }()

	srv := NewServer(store, socket)
	if ls, ok := store.(*LocalStore); ok {
		// Background work (IDLE, the periodic poll) tells connected clients
		// what changed instead of leaving the UI to poll.
		ls.SetOnChange(func(ev StoreEvent) {
			srv.broadcast(EventChanged, eventParams{
				FolderID: ev.FolderID, AccountID: ev.AccountID,
				Count: ev.Count, Reason: ev.Reason,
			})
			if ev.Count > 0 {
				srv.broadcast(EventFetched, eventParams{AccountID: ev.AccountID, Count: ev.Count})
				srv.maybeNotify(ev.AccountID, ev.Count)
			}
		})
		ls.StartPush(ctx)
		defer ls.StopPush()
		defer ls.SetOnChange(nil)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	return srv.Serve(ln)
}
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.serving = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.serving = false
		s.mu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if s.ctxClosed(err) {
				return nil
			}
			return err
		}
		go s.handleConn(c)
	}
}

func (s *Server) ctxClosed(err error) bool {
	return err != nil && (isClosed(err) || !s.isServing())
}

func (s *Server) clientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

func (s *Server) isServing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serving
}

func isClosed(err error) bool {
	if err == nil {
		return false
	}
	return err == net.ErrClosed || strings.Contains(err.Error(), "use of closed network connection")
}

func (s *Server) handleConn(raw net.Conn) {
	if ok, err := peerAllowed(raw); err != nil || !ok {
		// Another local user must not be able to read the mailbox or send
		// mail through this daemon.
		_ = raw.Close()
		return
	}
	c := &rpcConn{Conn: raw, w: bufio.NewWriter(raw)}
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		_ = c.Close()
	}()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), maxRPCLine)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = c.send(Response{JSONRPC: RPCVersion, Error: &RPCError{Code: -32700, Message: err.Error()}})
			continue
		}
		resp := s.dispatch(req)
		if req.ID == nil {
			continue
		}
		resp.JSONRPC = RPCVersion
		resp.ID = req.ID
		if err := c.send(resp); err != nil {
			return
		}
	}
}

// maxRPCLine bounds one NDJSON request. Attachments travel as base64 inside
// compose.send, so the limit has to accommodate a real message.
const maxRPCLine = 64 * 1024 * 1024

func writeJSON(w *bufio.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

func (s *Server) dispatch(req Request) Response {
	var (
		result any
		err    error
	)
	switch req.Method {
	case MethodPing:
		result = map[string]string{"pong": "ok"}
	case MethodStatusGet:
		result, err = s.status()
	case MethodAccountsList:
		result = s.Store.Accounts()
	case MethodAccountsPut:
		var p AccountConfig
		p, err = decodeParams[AccountConfig](req.Params)
		if err == nil {
			var acct Account
			acct, err = s.Store.PutAccount(p)
			if err == nil {
				result = acct
				s.broadcast(EventChanged, eventParams{Reason: "account"})
			}
		}
	case MethodAccountsDel:
		var p accountDelParams
		p, err = decodeParams[accountDelParams](req.Params)
		if err == nil {
			err = s.Store.DeleteAccount(p.id())
			if err == nil {
				result = map[string]bool{"ok": true}
				s.broadcast(EventChanged, eventParams{Reason: "account"})
			}
		}
	case MethodFoldersList:
		var p folderListParams
		p, err = decodeParams[folderListParams](req.Params)
		if err == nil {
			result = s.Store.ListFolders(p.AccountID)
		}
	case MethodFoldersGet:
		var p folderGetParams
		p, err = decodeParams[folderGetParams](req.Params)
		if err == nil {
			f, ok := s.Store.GetFolder(p.ID)
			if !ok {
				err = fmt.Errorf("mail: no folder %s", p.ID)
			} else {
				result = f
			}
		}
	case MethodFoldersCreate:
		var p folderCreateParams
		p, err = decodeParams[folderCreateParams](req.Params)
		if err == nil {
			result, err = s.Store.CreateFolder(p.AccountID, p.Name, p.Parent)
			if err == nil {
				s.broadcast(EventChanged, eventParams{FolderID: result.(Folder).ID, Reason: "create-folder"})
			}
		}
	case MethodMessagesList:
		var p messagesListParams
		p, err = decodeParams[messagesListParams](req.Params)
		if err == nil {
			all := s.Store.ListMessages(p.FolderID)
			if p.Filter != nil {
				var hit []Message
				for _, m := range all {
					if p.Filter.Match(m) {
						hit = append(hit, plainMessage(m))
					}
				}
				result = hit
			} else {
				out := make([]Message, len(all))
				for i, m := range all {
					out[i] = plainMessage(m)
				}
				result = out
			}
		}
	case MethodMessagesGet:
		var p messageIDParams
		p, err = decodeParams[messageIDParams](req.Params)
		if err == nil {
			m, ok := s.Store.GetMessage(p.ID)
			if !ok {
				err = fmt.Errorf("mail: no message %s", p.ID)
			} else {
				result = plainMessage(m)
			}
		}
	case MethodMessagesSource:
		var p messageIDParams
		p, err = decodeParams[messageIDParams](req.Params)
		if err == nil {
			var raw []byte
			raw, err = s.Store.GetRaw(p.ID)
			if err == nil {
				result = sourceResult{ID: p.ID, RFC822: rawAsText(raw)}
			}
		}
	case MethodMessagesSearch:
		var p searchParams
		p, err = decodeParams[searchParams](req.Params)
		if err == nil {
			result = s.Store.Search(SearchQuery{AccountID: p.AccountID, Folder: p.FolderID, Filter: p.Filter})
		}
	case MethodMessagesFlags:
		var p setFlagsParams
		p, err = decodeParams[setFlagsParams](req.Params)
		if err == nil {
			err = s.Store.SetFlags(p.ID, p.Patch.to())
			if err == nil {
				if m, ok := s.Store.GetMessage(p.ID); ok {
					s.broadcast(EventChanged, eventParams{FolderID: m.Folder, Reason: "flags"})
				}
			}
		}
	case MethodMessagesMove:
		var p moveParams
		p, err = decodeParams[moveParams](req.Params)
		if err == nil {
			err = s.Store.Move(p.IDs, p.Dest)
			if err == nil {
				s.broadcast(EventChanged, eventParams{FolderID: p.Dest, Reason: "move"})
			}
		}
	case MethodMessagesDelete:
		var p deleteParams
		p, err = decodeParams[deleteParams](req.Params)
		if err == nil {
			err = s.Store.Delete(p.IDs)
			if err == nil {
				s.broadcast(EventChanged, eventParams{Reason: "delete"})
			}
		}
	case MethodMessagesAppend:
		var p appendParams
		p, err = decodeParams[appendParams](req.Params)
		if err == nil {
			var id MessageID
			id, err = s.Store.Append(p.FolderID, p.Message)
			if err == nil {
				result = appendResult{ID: id}
				s.broadcast(EventChanged, eventParams{FolderID: p.FolderID, Reason: "append"})
			}
		}
	case MethodMessagesUpdate:
		var p updateParams
		p, err = decodeParams[updateParams](req.Params)
		if err == nil {
			err = s.Store.Update(p.ID, p.Message)
			if err == nil {
				s.broadcast(EventChanged, eventParams{Reason: "update"})
			}
		}
	case MethodMessagesFetch:
		var p fetchParams
		p, err = decodeParams[fetchParams](req.Params)
		if err == nil {
			var n int
			n, err = s.Store.Fetch(p.AccountID)
			if err == nil {
				result = fetchResult{Count: n}
				s.broadcast(EventFetched, eventParams{AccountID: p.AccountID, Count: n})
				s.maybeNotify(p.AccountID, n)
			}
		}
	case MethodUnreadGet:
		var p unreadParams
		p, err = decodeParams[unreadParams](req.Params)
		if err == nil {
			if p.FolderID == "" {
				result = unreadResult{Count: s.Store.UnreadTotal()}
			} else {
				result = unreadResult{Count: s.Store.Unread(p.FolderID)}
			}
		}
	case MethodUnreadAll:
		counts := map[FolderID]int{}
		for _, a := range s.Store.Accounts() {
			for _, f := range s.Store.ListFolders(a.ID) {
				counts[f.ID] = s.Store.Unread(f.ID)
			}
		}
		for _, f := range s.Store.VirtualFolders() {
			counts[f.ID] = s.Store.Unread(f.ID)
		}
		result = unreadAllResult{Counts: counts, Total: s.Store.UnreadTotal()}
	case MethodComposeSend:
		var p composeParams
		p, err = decodeParams[composeParams](req.Params)
		if err == nil {
			result, err = s.send(p)
		}
	case MethodComposeDraft:
		var p composeParams
		p, err = decodeParams[composeParams](req.Params)
		if err == nil {
			result, err = s.saveDraft(p)
		}
	case MethodIdentitiesList:
		var p identityListParams
		p, err = decodeParams[identityListParams](req.Params)
		if err == nil {
			result = s.Store.Identities(p.AccountID)
		}
	case MethodIdentitiesPut:
		var id Identity
		id, err = decodeParams[Identity](req.Params)
		if err == nil {
			result, err = s.Store.PutIdentity(id)
			if err == nil {
				s.broadcast(EventChanged, eventParams{Reason: "identity"})
			}
		}
	case MethodIdentitiesDel:
		var p identityIDParams
		p, err = decodeParams[identityIDParams](req.Params)
		if err == nil {
			err = s.Store.DeleteIdentity(p.ID)
		}
	case MethodTagsList:
		result = s.Store.ListTags()
	case MethodTagsPut:
		var t Tag
		t, err = decodeParams[Tag](req.Params)
		if err == nil {
			result, err = s.Store.PutTag(t)
			if err == nil {
				s.broadcast(EventChanged, eventParams{Reason: "tags"})
			}
		}
	case MethodTagsDel:
		var p tagNameParams
		p, err = decodeParams[tagNameParams](req.Params)
		if err == nil {
			err = s.Store.DeleteTag(p.Name)
			if err == nil {
				s.broadcast(EventChanged, eventParams{Reason: "tags"})
			}
		}
	case MethodFoldersVirtual:
		result = s.Store.VirtualFolders()
	case MethodFiltersList:
		result = s.Store.ListRules()
	case MethodFiltersPut:
		var r FilterRule
		r, err = decodeParams[FilterRule](req.Params)
		if err == nil {
			result, err = s.Store.PutRule(r)
		}
	case MethodFiltersDel:
		var p ruleIDParams
		p, err = decodeParams[ruleIDParams](req.Params)
		if err == nil {
			err = s.Store.DeleteRule(p.ID)
		}
	case MethodFiltersApply:
		var p applyRulesParams
		p, err = decodeParams[applyRulesParams](req.Params)
		if err == nil {
			var n int
			n, err = s.Store.ApplyRules(p.FolderID)
			if err == nil {
				result = applyResult{Count: n}
				s.broadcast(EventChanged, eventParams{FolderID: p.FolderID, Reason: "filters"})
			}
		}
	case MethodMessagesPart:
		var p partParams
		p, err = decodeParams[partParams](req.Params)
		if err == nil {
			result, err = s.Store.GetPart(p.ID, p.PartID)
		}
	case MethodMessagesOpen:
		var p partParams
		p, err = decodeParams[partParams](req.Params)
		if err == nil {
			result, err = s.Store.OpenPart(p.ID, p.PartID)
		}
	case MethodSyncRun:
		var p fetchParams
		p, err = decodeParams[fetchParams](req.Params)
		if err == nil {
			var r SyncResult
			r, err = s.Store.Sync(p.AccountID)
			if err == nil {
				result = r
				s.broadcast(EventSynced, eventParams{AccountID: p.AccountID, Count: r.New, Reason: "sync"})
				s.broadcast(EventFetched, eventParams{AccountID: p.AccountID, Count: r.New})
				s.maybeNotify(p.AccountID, r.New)
			}
		}
	case MethodStatusSet:
		var p onlineParams
		p, err = decodeParams[onlineParams](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				ex.SetOnline(p.Online)
				if p.Online {
					n, ferr := ex.FlushOutbox()
					result = fetchResult{Count: n}
					if ferr != nil && err == nil {
						err = ferr
					}
				} else {
					result = map[string]bool{"online": false}
				}
			} else {
				result = map[string]bool{"online": p.Online}
			}
		}
	case MethodOutboxList:
		if ex := asExtra(s.Store); ex != nil {
			result = ex.ListOutbox()
		} else {
			result = []OutboxOp{}
		}
	case MethodOutboxFlush:
		if ex := asExtra(s.Store); ex != nil {
			var n int
			n, err = ex.FlushOutbox()
			result = fetchResult{Count: n}
		} else {
			result = fetchResult{}
		}
	case MethodSmartList:
		if ex := asExtra(s.Store); ex != nil {
			result = ex.ListSmartFolders()
		} else {
			result = []SmartFolder{}
		}
	case MethodSmartPut:
		var sf SmartFolder
		sf, err = decodeParams[SmartFolder](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				result, err = ex.PutSmartFolder(sf)
				if err == nil {
					s.broadcast(EventChanged, eventParams{Reason: "smart"})
				}
			} else {
				err = fmt.Errorf("mail: smart folders not supported")
			}
		}
	case MethodSmartDel:
		var p ruleIDParams
		p, err = decodeParams[ruleIDParams](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				err = ex.DeleteSmartFolder(p.ID)
			}
		}
	case MethodThreadMute:
		var p muteParams
		p, err = decodeParams[muteParams](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				err = ex.MuteThread(p.ThreadID, p.Muted)
			} else {
				err = fmt.Errorf("mail: mute not supported")
			}
		}
	case MethodThreadMuted:
		if ex := asExtra(s.Store); ex != nil {
			result = ex.MutedThreads()
		} else {
			result = []string{}
		}
	case MethodVIPList:
		if ex := asExtra(s.Store); ex != nil {
			result = ex.ListVIPs()
		} else {
			result = []VIP{}
		}
	case MethodVIPPut:
		var v VIP
		v, err = decodeParams[VIP](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				result, err = ex.PutVIP(v)
				if err == nil {
					s.broadcast(EventChanged, eventParams{Reason: "vip"})
				}
			} else {
				err = fmt.Errorf("mail: VIP not supported")
			}
		}
	case MethodVIPDel:
		var p vipDelParams
		p, err = decodeParams[vipDelParams](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				err = ex.DeleteVIP(p.Address)
			}
		}
	case MethodNotifyGet:
		if ex := asExtra(s.Store); ex != nil {
			result = ex.NotifyPrefs()
		} else {
			result = NotifyPrefs{}
		}
	case MethodNotifyPut:
		var p NotifyPrefs
		p, err = decodeParams[NotifyPrefs](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				result = ex.PutNotifyPrefs(p)
			} else {
				result = p
			}
		}
	case MethodCategorySet:
		var p categoryParams
		p, err = decodeParams[categoryParams](req.Params)
		if err == nil {
			if ex := asExtra(s.Store); ex != nil {
				err = ex.SetSenderCategory(p.Address, p.Category)
			} else {
				err = fmt.Errorf("mail: categories not supported")
			}
		}
	case MethodCategoryList:
		if ex := asExtra(s.Store); ex != nil {
			result = ex.ListSenderCategories()
		} else {
			result = []SenderCat{}
		}
	case MethodOAuthStart:
		var p oauthReq
		p, err = decodeParams[oauthReq](req.Params)
		if err == nil {
			result, err = s.oauth.start(p)
		}
	case MethodOAuthPoll:
		var p oauthPollParams
		p, err = decodeParams[oauthPollParams](req.Params)
		if err == nil {
			var poll OAuthPoll
			poll, err = s.oauth.poll(p.SessionID)
			if err == nil && poll.Done && poll.Error == "" {
				if cfg, ok := s.oauth.takeConfig(p.SessionID); ok {
					var acct Account
					acct, err = s.Store.PutAccount(cfg)
					if err == nil {
						poll.Account = acct
						s.broadcast(EventChanged, eventParams{Reason: "account"})
					}
				}
			}
			result = poll
		}
	case MethodOAuthCancel:
		var p oauthPollParams
		p, err = decodeParams[oauthPollParams](req.Params)
		if err == nil {
			s.oauth.cancel(p.SessionID)
			result = map[string]bool{"ok": true}
		}
	case MethodHostsGuess:
		var p guessParams
		p, err = decodeParams[guessParams](req.Params)
		if err == nil {
			result = GuessMailHosts(p.Address)
		}
	case MethodHostsProbe, MethodAccountsTest:
		var p ProbeRequest
		p, err = decodeParams[ProbeRequest](req.Params)
		if err == nil {
			result = ProbeAccount(p)
		}
	default:
		err = fmt.Errorf("unknown method %s", req.Method)
	}
	if err != nil {
		return Response{Error: &RPCError{Code: -32000, Message: err.Error()}}
	}
	raw, jerr := json.Marshal(result)
	if jerr != nil {
		return Response{Error: &RPCError{Code: -32603, Message: jerr.Error()}}
	}
	return Response{Result: raw}
}

func (s *Server) status() (DaemonStatus, error) {
	st := DaemonStatus{
		Backend:  s.Store.Backend(),
		Socket:   s.Socket,
		Online:   true,
		Accounts: len(s.Store.Accounts()),
	}
	if ex := asExtra(s.Store); ex != nil {
		st.Online = ex.Online()
		st.Outbox = len(ex.ListOutbox())
	}
	if err := s.Store.Health(); err != nil {
		if st.Online {
			st.Online = false
		}
		st.Health = err.Error()
	}
	return st, nil
}

func (s *Server) maybeNotify(accountID string, n int) {
	if n <= 0 {
		return
	}
	ex := asExtra(s.Store)
	if ex == nil {
		return
	}
	p := ex.NotifyPrefs()
	if !p.Enabled {
		return
	}
	title, body := formatNewMailNotice(s.Store, accountID, n, p.VIPOnly)
	vip := p.VIPOnly
	if p.Desktop && s.clientCount() == 0 {
		notifyDesktop(title, body)
	}
	s.broadcast(EventNotify, eventParams{AccountID: accountID, Count: n, Title: title, Body: body, VIP: vip})
}

func (s *Server) send(p composeParams) (appendResult, error) {
	accountID := p.AccountID
	var ident Identity
	if p.IdentityID != "" {
		for _, id := range s.Store.Identities("") {
			if id.ID == p.IdentityID {
				ident = id
				if id.AccountID != "" {
					accountID = id.AccountID
				}
				break
			}
		}
	}
	if ident.ID == "" {
		ids := s.Store.Identities(accountID)
		for _, id := range ids {
			if id.Default || ident.ID == "" {
				ident = id
			}
		}
	}
	msg := p.Message
	if msg.From == "" && ident.Address != "" {
		msg.From = ident.DisplayFrom()
	}
	if ident.Signature != "" && !strings.Contains(msg.Body, ident.Signature) {
		msg.Body = strings.TrimRight(msg.Body, "\n") + "\n\n-- \n" + ident.Signature + "\n"
	}
	msg.IdentityID = ident.ID
	msg.Read = true
	// Attachments arrive as bytes from the UI. The daemon deliberately does
	// not read arbitrary paths on behalf of a client: that turned the socket
	// into a file-exfiltration primitive.
	var files []AttachedFile
	for _, f := range p.Attachments {
		name := attachFileName(f.Name)
		if name == "attachment" && f.Name != "" {
			name = attachFileName(filepath.Base(f.Name))
		}
		mimeType := f.MIME
		if mimeType == "" {
			mimeType = guessMIME(name)
		}
		files = append(files, AttachedFile{Name: name, MIME: mimeType, Data: f.Data})
		msg.HasAttach = true
		msg.Attachments = append(msg.Attachments, name)
	}
	if ls, ok := s.Store.(*LocalStore); ok && ls.hasConfiguredAccounts() {
		id, err := ls.SendViaSMTP(accountID, ident.ID, msg, files)
		if err != nil {
			return appendResult{}, err
		}
		if p.ID != "" {
			_ = s.Store.Delete([]MessageID{p.ID})
		}
		s.broadcast(EventChanged, eventParams{Reason: "send"})
		return appendResult{ID: id}, nil
	}
	sent, ok := specialFolder(s.Store, accountID, FolderSent)
	if !ok {
		return appendResult{}, fmt.Errorf("mail: no Sent folder for %s", accountID)
	}
	id, err := s.Store.Append(sent.ID, msg)
	if err != nil {
		return appendResult{}, err
	}
	if p.ID != "" {
		_ = s.Store.Delete([]MessageID{p.ID})
	}
	s.broadcast(EventChanged, eventParams{FolderID: sent.ID, Reason: "send"})
	return appendResult{ID: id}, nil
}

func (s *Server) saveDraft(p composeParams) (appendResult, error) {
	drafts, ok := specialFolder(s.Store, p.AccountID, FolderDrafts)
	if !ok {
		return appendResult{}, fmt.Errorf("mail: no Drafts folder for %s", p.AccountID)
	}
	msg := p.Message
	msg.Read = true
	if p.ID != "" {
		if err := s.Store.Update(p.ID, msg); err != nil {
			return appendResult{}, err
		}
		s.broadcast(EventChanged, eventParams{FolderID: drafts.ID, Reason: "draft"})
		return appendResult{ID: p.ID}, nil
	}
	id, err := s.Store.Append(drafts.ID, msg)
	if err != nil {
		return appendResult{}, err
	}
	s.broadcast(EventChanged, eventParams{FolderID: drafts.ID, Reason: "draft"})
	return appendResult{ID: id}, nil
}

// broadcast fans an event out to every client. The connection list is copied
// under the lock and the writes happen outside it with a deadline, so one
// client that stops reading cannot freeze the daemon.
func (s *Server) broadcast(method string, params eventParams) {
	raw, err := json.Marshal(params)
	if err != nil {
		return
	}
	note := Request{JSONRPC: RPCVersion, Method: method, Params: raw}
	s.mu.Lock()
	conns := make([]*rpcConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.sendTimeout(note, 5*time.Second)
	}
}
func specialFolder(store Store, accountID string, kind FolderKind) (Folder, bool) {
	for _, f := range store.ListFolders(accountID) {
		if f.Kind == kind && f.Parent == "" {
			return f, true
		}
	}
	return Folder{}, false
}
