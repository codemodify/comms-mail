package mailcore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// secretvault (codemodify/secretvault) is the owner's own secret store.
// comms-maild reaches it the way any program does — JSON-RPC 2.0, one
// message per line, over its socket (secretvault's docs/clients.md) — and
// links none of its code. comms-mail's secrets are items under
// "comms-mail/" in the vault chosen for them — secretvault's default, or
// one named in Settings (mail.json "secretVault") — an account's password a
// "password" item, an OAuth sign-in an "api-key" one. Every item call names
// that vault: secretvault answers an unknown vault with the same "not
// found" as a missing item, so the vault is looked up in vault.list first
// and a missing one is said, never taken for no secrets.
//
// secretvault decides who may read them: the first time comms-maild asks,
// it asks the person and remembers the answer. While the vault is locked —
// it locks with the screen, on sleep and at logout — comms-maild reads
// nothing and holds nothing: what it read is dropped, its mail sessions
// (which keep a password to reconnect with) are closed, and it waits for
// the vault to unlock. It asks secretvault to unlock only when the person
// does (UnlockSecrets).

const (
	svItemPrefix  = "comms-mail/"
	svClientName  = "comms-maild"
	svDialTimeout = 2 * time.Second
	// A call may wait on a person: secretvault asking whether comms-maild
	// may read, or for the vault's passphrase.
	svCallTimeout  = 3 * time.Minute
	svMaxMessage   = 16 << 20
	svFieldPass    = "password"
	svFieldSecret  = "secret"
	svKindPassword = "password"
	svKindToken    = "api-key"
)

// svRetryEvery is how often comms-maild looks for a secretvault daemon
// that is not running yet (a variable for tests).
var svRetryEvery = 5 * time.Second

// secretvault's error codes (its svrpc/errors.go).
const (
	svCodeLocked     = -32001
	svCodeDenied     = -32002
	svCodeNotFound   = -32003
	svCodeCanceled   = -32005
	svCodeNoPrompter = -32007
)

// svError is an error secretvaultd answered with.
type svError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *svError) Error() string { return "secretvault: " + e.Message }

func svCode(err error) int {
	var e *svError
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// svSocket is secretvaultd's socket for this user, as secretvault
// computes it (svrpc.DefaultSocket).
func svSocket() string {
	if p := os.Getenv("SECRETVAULT_SOCK"); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "secretvault", "run", "secretvaultd.sock")
	}
	if runtime.GOOS != "darwin" {
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			return filepath.Join(dir, "secretvault", "secretvaultd.sock")
		}
	}
	return filepath.Join(os.TempDir(), "secretvault-"+strconv.Itoa(os.Getuid()), "secretvaultd.sock")
}

// The shapes comms-mail sends and reads (secretvault's svrpc/methods.go).

type svMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *svError         `json:"error,omitempty"`
}

type svHello struct {
	Client  string `json:"client"`
	Version string `json:"version"`
}

type svVaultInfo struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
	Locked  bool   `json:"locked"`
}

type svVaultEvent struct {
	Vault string `json:"vault"`
	Item  string `json:"item,omitempty"`
}

type svField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value []byte `json:"value,omitempty"`
}

type svItem struct {
	Kind   string    `json:"kind"`
	Name   string    `json:"name"`
	Label  string    `json:"label,omitempty"`
	Fields []svField `json:"fields,omitempty"`
}

type svEntry struct {
	Deleted bool   `json:"deleted,omitempty"`
	Item    svItem `json:"item"`
}

type svItemRef struct {
	Vault string `json:"vault,omitempty"`
	Name  string `json:"name"`
}

// ---- one connection ----

type svReply struct {
	result json.RawMessage
	err    error
}

// svConn is one connection to secretvaultd. Notifications (vault.locked,
// vault.unlocked, item.changed) go to note, in order, on a goroutine of
// their own so a handler may call the daemon.
type svConn struct {
	conn net.Conn
	wmu  sync.Mutex
	w    *bufio.Writer

	mu      sync.Mutex
	next    int64
	pending map[int64]chan svReply
	err     error
	done    chan struct{}

	notes chan svMessage
}

func dialSecretVault(note func(method string, params json.RawMessage)) (*svConn, error) {
	path := svSocket()
	conn, err := net.DialTimeout("unix", path, svDialTimeout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "connection refused") {
			return nil, fmt.Errorf("secretvault is not running (nothing answers at %s)", path)
		}
		return nil, fmt.Errorf("secretvault: %w", err)
	}
	c := &svConn{
		conn: conn, w: bufio.NewWriter(conn),
		pending: map[int64]chan svReply{}, done: make(chan struct{}),
		notes: make(chan svMessage, 64),
	}
	go c.read()
	go func() {
		for m := range c.notes {
			if note != nil {
				note(m.Method, m.Params)
			}
		}
	}()
	return c, nil
}

func (c *svConn) read() {
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 0, 64<<10), svMaxMessage)
	for sc.Scan() {
		var m svMessage
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		switch {
		case m.Method != "" && m.ID == nil:
			c.notes <- m
		case m.Method != "":
			// The daemon calls back only into clients that offered to ask
			// the person (prompt.ask); comms-maild never offers.
			c.send(svMessage{JSONRPC: "2.0", ID: m.ID, Error: &svError{Code: -32601, Message: "method not found"}})
		case m.ID != nil:
			var id int64
			if json.Unmarshal(*m.ID, &id) != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch == nil {
				continue
			}
			if m.Error != nil {
				ch <- svReply{err: m.Error}
			} else {
				ch <- svReply{result: m.Result}
			}
		}
	}
	err := sc.Err()
	if err == nil {
		err = errors.New("secretvault closed the connection")
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		ch <- svReply{err: err}
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.notes)
	close(c.done)
}

func (c *svConn) send(m svMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *svConn) call(method string, params, result any, timeout time.Duration) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan svReply, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()
	idRaw := json.RawMessage(strconv.FormatInt(id, 10))
	if params == nil {
		raw = nil
	}
	if err := c.send(svMessage{JSONRPC: "2.0", ID: &idRaw, Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("secretvault: %w", err)
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if result != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, result)
		}
		return nil
	case <-t.C:
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("secretvault did not answer %s", method)
	}
}

func (c *svConn) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *svConn) close() { _ = c.conn.Close() }

// ---- the vault, as comms-maild sees it ----

// secretVault is comms-maild's one connection to secretvaultd and what it
// knows: the vault the secrets are in, whether it is locked, and what it
// has read since it was last unlocked.
type secretVault struct {
	mu   sync.Mutex
	conn *svConn
	// want is the vault chosen for comms-mail's secrets, "" for
	// secretvault's default; vault is its name as found in vault.list, ""
	// while there is none (no vault yet, or none of that name);
	// defaultName is secretvault's default vault.
	want, vault, defaultName string
	locked                   bool
	refused                  error // the person said no (or dismissed the question): until the next unlock
	known                    map[string]string
	none                     map[string]bool

	onLock, onUnlock func()
	watching         bool
	stop             chan struct{} // ends watch's loop (tests)
}

var theSecretVault = &secretVault{known: map[string]string{}, none: map[string]bool{}}

// connect makes sure there is a connection, reading the default vault and
// whether it is locked. reachedNow reports a connection just made.
func (v *secretVault) connect() (reachedNow bool, err error) {
	v.mu.Lock()
	if v.conn != nil && v.conn.alive() {
		v.mu.Unlock()
		return false, nil
	}
	v.mu.Unlock()
	c, err := dialSecretVault(v.note)
	if err != nil {
		return false, err
	}
	var hello json.RawMessage
	if err := c.call("client.hello", svHello{Client: svClientName, Version: "1"}, &hello, svDialTimeout*5); err != nil {
		c.close()
		return false, err
	}
	var vaults []svVaultInfo
	if err := c.call("vault.list", nil, &vaults, svDialTimeout*5); err != nil {
		c.close()
		return false, err
	}
	v.mu.Lock()
	if v.conn != nil && v.conn.alive() { // another goroutine won
		v.mu.Unlock()
		c.close()
		return false, nil
	}
	v.conn = c
	v.takeVault(vaults)
	v.forgetLocked()
	v.mu.Unlock()
	go v.watchConn(c)
	return true, nil
}

// pickVault finds want in vaults ("" for the default, or the only one).
func pickVault(vaults []svVaultInfo, want string) (vi svVaultInfo, ok bool) {
	for _, x := range vaults {
		if want != "" && x.Name == want || want == "" && (x.Default || len(vaults) == 1) {
			return x, true
		}
	}
	return svVaultInfo{}, false
}

// takeVault takes the chosen vault, and the default's name, from a
// vault.list answer (v.mu held).
func (v *secretVault) takeVault(vaults []svVaultInfo) {
	v.defaultName = ""
	if d, ok := pickVault(vaults, ""); ok {
		v.defaultName = d.Name
	}
	vi, ok := pickVault(vaults, v.want)
	v.vault, v.locked = vi.Name, !ok || vi.Locked
}

// relist asks secretvault for its vaults again, for a vault not found
// before (made since) or another one chosen.
func (v *secretVault) relist() error {
	var vaults []svVaultInfo
	if err := v.call("vault.list", nil, &vaults); err != nil {
		return err
	}
	v.mu.Lock()
	before := v.vault
	v.takeVault(vaults)
	if v.vault != before {
		v.forgetLocked()
		v.refused = nil
	}
	v.mu.Unlock()
	return nil
}

// useVault makes name the vault comms-mail's secrets are in ("" for
// secretvault's default). A vault secretvault does not have is an error,
// and changes nothing.
func (v *secretVault) useVault(name string) error {
	if _, err := v.connect(); err != nil {
		return err
	}
	var vaults []svVaultInfo
	if err := v.call("vault.list", nil, &vaults); err != nil {
		return err
	}
	if _, ok := pickVault(vaults, name); !ok {
		return missingVault(name)
	}
	v.mu.Lock()
	v.want = name
	v.takeVault(vaults)
	v.forgetLocked()
	v.refused = nil
	v.mu.Unlock()
	return nil
}

// setWant chooses the vault before anything is read (mail.json's choice,
// at start).
func (v *secretVault) setWant(name string) {
	v.mu.Lock()
	v.want = name
	v.mu.Unlock()
}

// current is the vault the secrets are in now, and the one chosen.
func (v *secretVault) current() (vault, want, defaultName string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.vault, v.want, v.defaultName
}

// missingVault says secretvault has no vault of that name; secretvault
// lets only its own programs make one.
func missingVault(name string) error {
	if name == "" {
		return errors.New("secretvault has no vault yet: make one in secretvault first")
	}
	return fmt.Errorf("secretvault has no vault named %q, and it lets only its own programs make one: make it in secretvault (File › New vault…, or: secretvault vault create --name %s), then Apply again", name, name)
}

// watchConn notices the daemon going away: everything read is dropped,
// as if the vault had locked (a restarted daemon's vaults are locked).
func (v *secretVault) watchConn(c *svConn) {
	<-c.done
	v.mu.Lock()
	if v.conn != c {
		v.mu.Unlock()
		return
	}
	wasOpen := !v.locked
	v.conn, v.locked = nil, true
	v.forgetLocked()
	lock := v.onLock
	v.mu.Unlock()
	if wasOpen && lock != nil {
		lock()
	}
}

// note handles the daemon's notifications.
func (v *secretVault) note(method string, params json.RawMessage) {
	var ev svVaultEvent
	_ = json.Unmarshal(params, &ev)
	v.mu.Lock()
	if ev.Vault != "" && ev.Vault != v.vault {
		v.mu.Unlock()
		return
	}
	switch method {
	case "vault.locked":
		was := v.locked
		v.locked = true
		v.forgetLocked()
		lock := v.onLock
		v.mu.Unlock()
		if !was && lock != nil {
			lock()
		}
	case "vault.unlocked":
		was := v.locked
		v.locked, v.refused = false, nil
		v.none = map[string]bool{}
		unlock := v.onUnlock
		v.mu.Unlock()
		if was && unlock != nil {
			unlock()
		}
	case "item.changed":
		if name, ok := strings.CutPrefix(ev.Item, svItemPrefix); ok {
			delete(v.known, name)
			delete(v.none, name)
		}
		v.mu.Unlock()
	default:
		v.mu.Unlock()
	}
}

// forgetLocked drops everything read (v.mu held).
func (v *secretVault) forgetLocked() {
	v.known = map[string]string{}
	v.none = map[string]bool{}
}

// watch makes onLock and onUnlock run as the vault locks and unlocks, and
// keeps trying to reach a daemon that is not running yet (one started
// after comms-maild at login), calling onUnlock when it is reached open.
func (v *secretVault) watch(onLock, onUnlock func()) {
	v.mu.Lock()
	v.onLock, v.onUnlock = onLock, onUnlock
	start := !v.watching
	v.watching = true
	if v.stop == nil {
		v.stop = make(chan struct{})
	}
	stop := v.stop
	v.mu.Unlock()
	if !start {
		return
	}
	go func() {
		tick := time.NewTicker(svRetryEvery)
		defer tick.Stop()
		for {
			reached, err := v.connect()
			if err == nil && reached {
				v.mu.Lock()
				open, unlock := !v.locked && v.vault != "", v.onUnlock
				v.mu.Unlock()
				if open && unlock != nil {
					unlock()
				}
			}
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
}

// reset forgets the daemon and stops watching it (tests).
func (v *secretVault) reset() {
	v.mu.Lock()
	c := v.conn
	if v.stop != nil {
		close(v.stop)
	}
	v.conn, v.want, v.vault, v.defaultName, v.locked, v.refused = nil, "", "", "", false, nil
	v.onLock, v.onUnlock, v.watching, v.stop = nil, nil, false, nil
	v.forgetLocked()
	v.mu.Unlock()
	if c != nil {
		c.close()
	}
}

func (v *secretVault) call(method string, params, result any) error {
	if _, err := v.connect(); err != nil {
		return err
	}
	v.mu.Lock()
	c := v.conn
	v.mu.Unlock()
	if c == nil {
		return errors.New("secretvault is not running")
	}
	return c.call(method, params, result, svCallTimeout)
}

// available says whether secretvault can be used here: its daemon answers.
func (v *secretVault) available() error {
	_, err := v.connect()
	return err
}

// ---- the store ----

// secretVaultStore keeps comms-mail's secrets in secretvault.
type secretVaultStore struct{ v *secretVault }

func (secretVaultStore) Kind() string { return StoreSecretVault }

// Ready never asks anyone: a locked vault is ErrLocked, and comms-maild
// waits for it to be unlocked.
func (s secretVaultStore) Ready() error {
	v := s.v
	if _, err := v.connect(); err != nil {
		return err
	}
	v.mu.Lock()
	missing := v.vault == ""
	v.mu.Unlock()
	if missing {
		// Made in secretvault since it was looked for, perhaps.
		if err := v.relist(); err != nil {
			return err
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case v.vault == "":
		return missingVault(v.want)
	case v.locked:
		return ErrLocked
	case v.refused != nil:
		return v.refused
	}
	return nil
}

func svItemName(name string) string { return svItemPrefix + name }

// svPlace describes a secret for secretvault's own windows: its kind, the
// field it is in, and a label a person can read.
func svPlace(name string) (kind, field, label string) {
	if id, which, ok := splitPassName(name); ok {
		return svKindPassword, svFieldPass, "comms-mail: " + strings.ToUpper(which) + " password for " + id
	}
	return svKindToken, svFieldSecret, "comms-mail: sign-in " + strings.TrimPrefix(name, "oauth/")
}

// fail turns a refusal into the store's state, so comms-maild stops
// asking until the vault is next unlocked or the person asks again.
func (s secretVaultStore) fail(err error) error {
	v := s.v
	switch svCode(err) {
	case svCodeLocked:
		v.mu.Lock()
		was := v.locked
		v.locked = true
		v.forgetLocked()
		lock := v.onLock
		v.mu.Unlock()
		if !was && lock != nil {
			go lock()
		}
		return ErrLocked
	case svCodeDenied, svCodeCanceled:
		refused := errors.New("secretvault did not let comms-mail read its passwords; it asks again after the vault next unlocks, or from Settings › Security › Passwords")
		v.mu.Lock()
		v.refused = refused
		v.mu.Unlock()
		return refused
	case svCodeNoPrompter:
		return errors.New("secretvault had to ask you, and could not show its question here")
	}
	return err
}

func (s secretVaultStore) Get(name string) (string, bool, error) {
	if err := s.Ready(); err != nil {
		return "", false, err
	}
	v := s.v
	v.mu.Lock()
	if val, ok := v.known[name]; ok {
		v.mu.Unlock()
		return val, true, nil
	}
	if v.none[name] {
		v.mu.Unlock()
		return "", false, nil
	}
	v.mu.Unlock()
	var e svEntry
	err := v.call("item.get", svItemRef{Vault: v.name(), Name: svItemName(name)}, &e)
	if svCode(err) == svCodeNotFound {
		v.mu.Lock()
		v.none[name] = true
		v.mu.Unlock()
		return "", false, nil
	}
	if err != nil {
		return "", false, s.fail(err)
	}
	for _, f := range e.Item.Fields {
		if f.Name == svFieldPass || f.Name == svFieldSecret {
			val := string(f.Value)
			clear(f.Value)
			v.mu.Lock()
			if !v.locked {
				v.known[name] = val
			}
			v.mu.Unlock()
			return val, true, nil
		}
	}
	return "", false, nil
}

func (s secretVaultStore) Update(set map[string]string, del ...string) error {
	if err := s.Ready(); err != nil {
		return err
	}
	v := s.v
	for name, val := range set {
		if val == "" {
			del = append(del, name)
			continue
		}
		kind, field, label := svPlace(name)
		it := svItem{Kind: kind, Name: svItemName(name), Label: label,
			Fields: []svField{{Name: field, Type: "concealed", Value: []byte(val)}}}
		if err := v.call("item.put", map[string]any{"vault": v.name(), "item": it}, nil); err != nil {
			return s.fail(err)
		}
		v.mu.Lock()
		v.known[name] = val
		delete(v.none, name)
		v.mu.Unlock()
	}
	for _, name := range del {
		if err := v.deleteIn(v.name(), name); err != nil {
			return s.fail(err)
		}
		v.mu.Lock()
		delete(v.known, name)
		v.none[name] = true
		v.mu.Unlock()
	}
	return nil
}

func (s secretVaultStore) Names() ([]string, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	var entries []svEntry
	if err := s.v.call("item.list", map[string]string{"vault": s.v.name(), "prefix": svItemPrefix}, &entries); err != nil {
		return nil, s.fail(err)
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutPrefix(e.Item.Name, svItemPrefix); ok && !e.Deleted {
			names = append(names, n)
		}
	}
	return names, nil
}

func (s secretVaultStore) Forget() error {
	names, err := s.Names()
	if err != nil {
		return err
	}
	return s.Update(nil, names...)
}

// name is the vault the secrets are in.
func (v *secretVault) name() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.vault
}

// deleteIn takes one of comms-mail's secrets out of vault; one already
// gone is no error.
func (v *secretVault) deleteIn(vault, name string) error {
	err := v.call("item.delete", svItemRef{Vault: vault, Name: svItemName(name)}, nil)
	if err != nil && svCode(err) != svCodeNotFound {
		return err
	}
	return nil
}

// unlock asks secretvault to unlock the vault the secrets are in, which it
// does with its own prompt; comms-mail never sees the passphrase. It is
// also how the person asks again after saying no. Whichever comes first —
// this answer or secretvault's vault.unlocked — lets comms-maild connect,
// once.
func (s secretVaultStore) unlock() error {
	v := s.v
	if err := v.call("vault.unlock", map[string]string{"vault": v.name()}, nil); err != nil {
		if c := svCode(err); c == svCodeCanceled || c == svCodeDenied {
			return errors.New("secretvault stayed locked")
		}
		return err
	}
	v.mu.Lock()
	fire := v.locked || v.refused != nil
	v.locked, v.refused = false, nil
	v.none = map[string]bool{}
	unlock := v.onUnlock
	v.mu.Unlock()
	if fire && unlock != nil {
		unlock()
	}
	return nil
}
