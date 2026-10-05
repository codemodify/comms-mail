package mailcore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// What comms-mail's own engines know of keys, besides the private halves:
// your keys' public halves, so they can be listed, offered and chosen
// without opening the place the private halves are kept in; and other
// people's — OpenPGP keys and S/MIME certificates — as they arrive with
// mail or you bring them in. None of it is secret. It is kept in the data
// folder, keys/index.json (0600).

// KeyEntry is one key or certificate.
type KeyEntry struct {
	Format string `json:"format"`
	// ID is an OpenPGP key's fingerprint, or an S/MIME certificate's
	// SHA-256, in upper-case hex.
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
	Name      string   `json:"name,omitempty"`
	// Public is the armoured OpenPGP public key, or the PEM certificates
	// (the key's own first, then its issuers).
	Public  string    `json:"public"`
	Created time.Time `json:"created,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
	Issuer  string    `json:"issuer,omitempty"`
	// Source is how someone else's key came: "autocrypt", "attached",
	// "signed" (an S/MIME signature carried it), or "imported" (you).
	Source    string    `json:"source,omitempty"`
	FirstSeen time.Time `json:"firstSeen,omitempty"`
	LastSeen  time.Time `json:"lastSeen,omitempty"`
}

// has says e is for addr.
func (e KeyEntry) has(addr string) bool {
	addr = normAddr(addr)
	return slices.ContainsFunc(e.Addresses, func(a string) bool { return normAddr(a) == addr })
}

func normAddr(a string) string { return strings.ToLower(strings.TrimSpace(ExtractAddr(a))) }

type keyIndex struct {
	mu       sync.Mutex
	path     string
	Own      []KeyEntry `json:"own,omitempty"`
	Contacts []KeyEntry `json:"contacts,omitempty"`
}

var (
	keyIndexesMu sync.Mutex
	keyIndexes   = map[string]*keyIndex{}
)

// keys is the store's key index, read once.
func (s *LocalStore) keys() *keyIndex {
	path := filepath.Join(s.dir, "keys", "index.json")
	keyIndexesMu.Lock()
	defer keyIndexesMu.Unlock()
	if k := keyIndexes[path]; k != nil {
		return k
	}
	k := &keyIndex{path: path}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, k); err != nil {
			Logf("keys: %s: %v", path, err)
		}
	}
	keyIndexes[path] = k
	return k
}

// saveLocked writes the index (k.mu held).
func (k *keyIndex) saveLocked() error {
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(k.path, b, 0o600)
}

// own are your keys of format for addr ("" for all of them), newest first.
func (k *keyIndex) own(format, addr string) []KeyEntry {
	k.mu.Lock()
	defer k.mu.Unlock()
	return pick(k.Own, format, addr, "")
}

// contacts are other people's keys of format for addr ("" for all),
// newest first.
func (k *keyIndex) contacts(format, addr string) []KeyEntry {
	k.mu.Lock()
	defer k.mu.Unlock()
	return pick(k.Contacts, format, addr, "")
}

// byID is the key of format with id, yours or someone's.
func (k *keyIndex) byID(format, id string) (e KeyEntry, own, ok bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if l := pick(k.Own, format, "", id); len(l) > 0 {
		return l[0], true, true
	}
	if l := pick(k.Contacts, format, "", id); len(l) > 0 {
		return l[0], false, true
	}
	return KeyEntry{}, false, false
}

func pick(list []KeyEntry, format, addr, id string) []KeyEntry {
	var out []KeyEntry
	for _, e := range list {
		if e.Format != format || addr != "" && !e.has(addr) || id != "" && !strings.EqualFold(e.ID, id) {
			continue
		}
		out = append(out, e)
	}
	slices.SortStableFunc(out, func(a, b KeyEntry) int { return newer(a).Compare(newer(b)) * -1 })
	return out
}

func newer(e KeyEntry) time.Time {
	if !e.LastSeen.IsZero() {
		return e.LastSeen
	}
	return e.Created
}

// addOwn records one of your keys (replacing one with its id).
func (k *keyIndex) addOwn(e KeyEntry) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.Own = slices.DeleteFunc(k.Own, func(x KeyEntry) bool { return x.Format == e.Format && strings.EqualFold(x.ID, e.ID) })
	k.Own = append(k.Own, e)
	return k.saveLocked()
}

// removeOwn forgets one of your keys.
func (k *keyIndex) removeOwn(format, id string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.Own = slices.DeleteFunc(k.Own, func(x KeyEntry) bool { return x.Format == format && strings.EqualFold(x.ID, id) })
	return k.saveLocked()
}

// seen records someone else's key: a new one as it came, a known one seen
// again. A key you imported stays imported; its addresses grow.
func (k *keyIndex) seen(e KeyEntry, when time.Time) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for i, x := range k.Contacts {
		if x.Format != e.Format || !strings.EqualFold(x.ID, e.ID) {
			continue
		}
		for _, a := range e.Addresses {
			if !x.has(a) {
				x.Addresses = append(x.Addresses, normAddr(a))
			}
		}
		if e.Source == "imported" {
			x.Source = "imported"
		}
		if e.Public != "" {
			x.Public = e.Public
		}
		if x.Name == "" {
			x.Name = e.Name
		}
		if when.After(x.LastSeen) {
			x.LastSeen = when
		}
		k.Contacts[i] = x
		return k.saveLocked()
	}
	for i := range e.Addresses {
		e.Addresses[i] = normAddr(e.Addresses[i])
	}
	e.FirstSeen, e.LastSeen = when, when
	k.Contacts = append(k.Contacts, e)
	return k.saveLocked()
}

// removeContact forgets someone else's key.
func (k *keyIndex) removeContact(format, id string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.Contacts = slices.DeleteFunc(k.Contacts, func(x KeyEntry) bool { return x.Format == format && strings.EqualFold(x.ID, id) })
	return k.saveLocked()
}
