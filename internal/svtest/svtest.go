// Package svtest is a stand-in secretvaultd for comms-mail's tests: its
// socket, the methods comms-maild calls (client.hello, vault.list,
// vault.unlock, item.get, item.list, item.put, item.delete, mail.inspect,
// trust.seen, pgp.public, smime.list, mail.compose) and the notifications
// it sends (vault.locked, vault.unlocked), with one vault named
// "personal". It checks, signs, encrypts and decrypts nothing: a test says
// what mail.inspect answers and whose keys it holds, and mail.compose
// wraps the message in a structure that reads as signed or encrypted. Only tests import it, so it is never in a binary, and
// tests never reach the person's own secretvault.
package svtest

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Item is an item as stored.
type Item struct {
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Label  string  `json:"label,omitempty"`
	Fields []Field `json:"fields,omitempty"`
}

// Field is one named value of an item.
type Field struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value []byte `json:"value,omitempty"`
}

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// secretvault's error codes.
const (
	codeLocked   = -32001
	codeDenied   = -32002
	codeNotFound = -32003
)

// Vault is the stand-in daemon.
type Vault struct {
	t    testing.TB
	path string
	ln   net.Listener

	wmu     sync.Mutex // one line at a time on every connection
	mu      sync.Mutex
	locked  bool
	deny    bool
	items   map[string]Item
	conns   []net.Conn
	unlocks int
	gets    int

	inspect  func(raw []byte, decrypt bool) any
	inspects []Inspected
	seen     []Seen
	ownPGP   map[string]bool // addresses with an OpenPGP key of the person's
	composed []Composed
	refuse   string // mail.compose refuses, with this
}

// Composed is one mail.compose asked.
type Composed struct {
	Message       []byte
	Sign, Encrypt bool
}

// OwnKeys gives the person an OpenPGP key for each address.
func (v *Vault) OwnKeys(addrs ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ownPGP = map[string]bool{}
	for _, a := range addrs {
		v.ownPGP[strings.ToLower(a)] = true
	}
}

// RefuseCompose makes mail.compose fail with why ("" to stop).
func (v *Vault) RefuseCompose(why string) {
	v.mu.Lock()
	v.refuse = why
	v.mu.Unlock()
}

// Composes are the mail.compose calls answered.
func (v *Vault) Composes() []Composed {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]Composed(nil), v.composed...)
}

// Wrapped is what mail.compose returns for message: the original's
// header fields, and a body that reads as PGP/MIME encrypted or signed.
// Encrypted, nothing of the original body is in it.
func Wrapped(message []byte, sign, encrypt bool) []byte {
	head, body, _ := strings.Cut(strings.ReplaceAll(string(message), "\r\n", "\n"), "\n\n")
	var keep []string
	for _, l := range strings.Split(head, "\n") {
		low := strings.ToLower(l)
		if strings.HasPrefix(low, "content-") || strings.HasPrefix(low, "mime-version") || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			continue
		}
		keep = append(keep, l)
	}
	keep = append(keep, "MIME-Version: 1.0")
	var b strings.Builder
	b.WriteString(strings.Join(keep, "\r\n"))
	if encrypt {
		b.WriteString("\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\"svx\"\r\n\r\n" +
			"--svx\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n" +
			"--svx\r\nContent-Type: application/octet-stream; name=\"encrypted.asc\"\r\n\r\n" +
			"-----BEGIN PGP MESSAGE-----\r\nwcBMA8+stand+in+ciphertext\r\n-----END PGP MESSAGE-----\r\n--svx--\r\n")
		return []byte(b.String())
	}
	b.WriteString("\r\nContent-Type: multipart/signed; micalg=pgp-sha256; protocol=\"application/pgp-signature\"; boundary=\"svs\"\r\n\r\n" +
		"--svs\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n") +
		"\r\n--svs\r\nContent-Type: application/pgp-signature; name=\"signature.asc\"\r\n\r\n" +
		"-----BEGIN PGP SIGNATURE-----\r\niQ==\r\n-----END PGP SIGNATURE-----\r\n--svs--\r\n")
	return []byte(b.String())
}

// Inspected is one mail.inspect asked.
type Inspected struct {
	Raw     []byte
	Decrypt bool
}

// Seen is one key passed to trust.seen.
type Seen struct {
	Key     []byte    `json:"key"`
	Address string    `json:"address"`
	Time    time.Time `json:"time"`
	Source  string    `json:"source"`
	Name    string    `json:"name"`
}

// InspectWith makes mail.inspect answer what fn returns for the message
// as received and whether decrypting was asked: secretvault's
// MailInspectResult ({"report": …, "verdicts": …}). Without it, a report
// of nothing found.
func (v *Vault) InspectWith(fn func(raw []byte, decrypt bool) any) {
	v.mu.Lock()
	v.inspect = fn
	v.mu.Unlock()
}

// Inspects are the mail.inspect calls answered; Seens the keys recorded.
func (v *Vault) Inspects() []Inspected {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]Inspected(nil), v.inspects...)
}

func (v *Vault) Seens() []Seen {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]Seen(nil), v.seen...)
}

// Start runs one, its vault locked or not, and points SECRETVAULT_SOCK at
// it for the test.
func Start(t testing.TB, locked bool) *Vault {
	t.Helper()
	dir, err := os.MkdirTemp("", "sv-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	v := &Vault{t: t, path: filepath.Join(dir, "secretvaultd.sock"), locked: locked, items: map[string]Item{}}
	v.Listen()
	t.Setenv("SECRETVAULT_SOCK", v.path)
	t.Cleanup(v.Stop)
	return v
}

// Listen starts the daemon (again, after Stop).
func (v *Vault) Listen() {
	ln, err := net.Listen("unix", v.path)
	if err != nil {
		v.t.Fatal(err)
	}
	v.mu.Lock()
	v.ln = ln
	v.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			v.mu.Lock()
			v.conns = append(v.conns, c)
			v.mu.Unlock()
			go v.serve(c)
		}
	}()
}

// Stop is the daemon going away; its vault is locked when it starts again.
func (v *Vault) Stop() {
	v.mu.Lock()
	ln, conns := v.ln, v.conns
	v.conns, v.locked = nil, true
	v.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	_ = os.Remove(v.path)
}

// Lock locks the vault, as the screen locking does.
func (v *Vault) Lock() {
	v.mu.Lock()
	v.locked = true
	v.mu.Unlock()
	v.notify("vault.locked")
}

// Unlock unlocks it, as the person does in secretvault.
func (v *Vault) Unlock() {
	v.mu.Lock()
	v.locked = false
	v.mu.Unlock()
	v.notify("vault.unlocked")
}

// Deny makes item.get refused, as when the person says no.
func (v *Vault) Deny(on bool) {
	v.mu.Lock()
	v.deny = on
	v.mu.Unlock()
}

// Item is what the vault holds under name.
func (v *Vault) Item(name string) (Item, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	it, ok := v.items[name]
	return it, ok
}

// Unlocks counts vault.unlock asked; Gets counts item.get answered.
func (v *Vault) Unlocks() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.unlocks
}

func (v *Vault) Gets() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.gets
}

func (v *Vault) notify(method string) {
	b, _ := json.Marshal(message{JSONRPC: "2.0", Method: method, Params: json.RawMessage(`{"vault":"personal"}`)})
	v.mu.Lock()
	conns := append([]net.Conn(nil), v.conns...)
	v.mu.Unlock()
	v.wmu.Lock()
	defer v.wmu.Unlock()
	for _, c := range conns {
		_, _ = c.Write(append(b, '\n'))
	}
}

func (v *Vault) serve(c net.Conn) {
	sc := bufio.NewScanner(c)
	sc.Buffer(nil, 16<<20)
	reply := func(id *json.RawMessage, result any, code int, msg string) {
		m := message{JSONRPC: "2.0", ID: id}
		if code != 0 {
			m.Error = &rpcError{Code: code, Message: msg}
		} else {
			m.Result, _ = json.Marshal(result)
		}
		b, _ := json.Marshal(m)
		v.wmu.Lock()
		_, _ = c.Write(append(b, '\n'))
		v.wmu.Unlock()
	}
	type vaultInfo struct {
		Name    string `json:"name"`
		Default bool   `json:"default"`
		Locked  bool   `json:"locked"`
	}
	type entry struct {
		Item Item `json:"item"`
	}
	for sc.Scan() {
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == nil {
			continue
		}
		var p struct {
			Name    string `json:"name"`
			Prefix  string `json:"prefix"`
			Item    Item   `json:"item"`
			Message []byte `json:"message"`
			Decrypt bool   `json:"decrypt"`
			Key     string `json:"key"`
			Sign    bool   `json:"sign"`
			Encrypt bool   `json:"encrypt"`
		}
		_ = json.Unmarshal(m.Params, &p)
		v.mu.Lock()
		locked, deny := v.locked, v.deny
		v.mu.Unlock()
		switch m.Method {
		case "client.hello":
			reply(m.ID, map[string]string{"daemon": "secretvaultd", "version": "0.1.0", "caller": "comms-maild"}, 0, "")
		case "vault.list":
			reply(m.ID, []vaultInfo{{Name: "personal", Default: true, Locked: locked}}, 0, "")
		case "vault.unlock":
			v.mu.Lock()
			v.unlocks++
			v.mu.Unlock()
			v.Unlock()
			reply(m.ID, vaultInfo{Name: "personal", Default: true}, 0, "")
		case "mail.inspect":
			if locked {
				reply(m.ID, nil, codeLocked, "the vault is locked")
				continue
			}
			v.mu.Lock()
			v.inspects = append(v.inspects, Inspected{Raw: p.Message, Decrypt: p.Decrypt})
			fn := v.inspect
			v.mu.Unlock()
			var out any = map[string]any{"report": map[string]any{"signed": "none", "encrypted": "none"}}
			if fn != nil {
				out = fn(p.Message, p.Decrypt)
			}
			reply(m.ID, out, 0, "")
		case "pgp.public", "smime.list", "mail.compose":
			if locked {
				reply(m.ID, nil, codeLocked, "the vault is locked")
				continue
			}
			v.mu.Lock()
			own, refuse := v.ownPGP[strings.ToLower(p.Key)], v.refuse
			if m.Method == "mail.compose" && refuse == "" {
				v.composed = append(v.composed, Composed{Message: p.Message, Sign: p.Sign, Encrypt: p.Encrypt})
			}
			v.mu.Unlock()
			switch {
			case m.Method == "smime.list":
				reply(m.ID, []any{}, 0, "")
			case m.Method == "pgp.public" && !own:
				reply(m.ID, nil, codeNotFound, "no OpenPGP key in vault \"personal\" is "+p.Key)
			case m.Method == "pgp.public":
				reply(m.ID, map[string]any{"fingerprint": "OWNKEY", "user_ids": []string{p.Key}}, 0, "")
			case refuse != "":
				reply(m.ID, nil, -32000, refuse)
			default:
				format := "openpgp"
				reply(m.ID, map[string]any{"message": Wrapped(p.Message, p.Sign, p.Encrypt), "format": format}, 0, "")
			}
		case "trust.seen":
			var seen Seen
			_ = json.Unmarshal(m.Params, &seen)
			v.mu.Lock()
			v.seen = append(v.seen, seen)
			v.mu.Unlock()
			reply(m.ID, map[string]any{"fingerprint": "SEEN"}, 0, "")
		case "item.get", "item.list", "item.put", "item.delete":
			if locked {
				reply(m.ID, nil, codeLocked, "the vault is locked")
				continue
			}
			if deny && m.Method == "item.get" {
				reply(m.ID, nil, codeDenied, "access denied")
				continue
			}
			v.mu.Lock()
			switch m.Method {
			case "item.get":
				v.gets++
				it, ok := v.items[p.Name]
				v.mu.Unlock()
				if !ok {
					reply(m.ID, nil, codeNotFound, "not found")
					continue
				}
				reply(m.ID, entry{Item: it}, 0, "")
			case "item.list":
				var out []entry
				for name, it := range v.items {
					if strings.HasPrefix(name, p.Prefix) {
						out = append(out, entry{Item: Item{Kind: it.Kind, Name: it.Name}})
					}
				}
				v.mu.Unlock()
				reply(m.ID, out, 0, "")
			case "item.put":
				v.items[p.Item.Name] = p.Item
				v.mu.Unlock()
				reply(m.ID, entry{Item: Item{Kind: p.Item.Kind, Name: p.Item.Name}}, 0, "")
			case "item.delete":
				_, ok := v.items[p.Name]
				delete(v.items, p.Name)
				v.mu.Unlock()
				if !ok {
					reply(m.ID, nil, codeNotFound, "not found")
					continue
				}
				reply(m.ID, nil, 0, "")
			}
		default:
			reply(m.ID, nil, -32601, "method not found")
		}
	}
}
