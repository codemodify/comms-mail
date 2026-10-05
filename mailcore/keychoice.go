package mailcore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Who signs, encrypts, checks and decrypts mail, format by format, and
// where comms-mail keeps its own keys. OpenPGP and S/MIME each have their
// engine: secretvault, which keeps the keys and does the work in its own
// daemon (protection.go, sendprotect.go), or comms-mail's own, built in
// (pgpown.go, smimeown.go), whose private keys are kept in a place chosen
// the way the passwords' is — the system keyring, a secretvault vault, the
// encrypted file or a plain file — and independently of it.
//
// The passwords and the keys can share a place. Each keeps to its own
// names there (pass/…, oauth/… and keys/<format>/…), so moving one never
// takes the other with it, and the encrypted file is deleted only when
// nothing is left in it.

// The formats.
const (
	FormatOpenPGP = "openpgp"
	FormatSMIME   = "smime"
)

// keyFormats are the formats, in the order a message is tried in when
// either would do.
var keyFormats = []string{FormatOpenPGP, FormatSMIME}

// The engines.
const (
	EngineSecretVault = "secretvault"
	EngineOwn         = "own"
)

// KeyChoice is a format's engine and, for comms-mail's own, where its keys
// are kept: a store kind, and with secretvault the vault ("" its default).
// The place is kept while secretvault does the work, so choosing comms-mail
// again finds the keys where they were.
type KeyChoice struct {
	Engine string `json:"engine,omitempty"` // "" is secretvault
	Store  string `json:"store,omitempty"`
	Vault  string `json:"vault,omitempty"`
}

// engine is c's engine, secretvault when none was chosen.
func (c KeyChoice) engine() string {
	if c.Engine == EngineOwn {
		return EngineOwn
	}
	return EngineSecretVault
}

func validFormat(f string) bool { return f == FormatOpenPGP || f == FormatSMIME }

// keyChoice is format's choice.
func (s *LocalStore) keyChoice(format string) KeyChoice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keyChoiceLocked(format)
}

func (s *LocalStore) keyChoiceLocked(format string) KeyChoice {
	var c *KeyChoice
	switch format {
	case FormatOpenPGP:
		c = s.cfg.OpenPGP
	case FormatSMIME:
		c = s.cfg.SMIME
	}
	if c == nil {
		return KeyChoice{}
	}
	return *c
}

// engineOf is the engine doing format's work.
func (s *LocalStore) engineOf(format string) string { return s.keyChoice(format).engine() }

// ---- the part of a store one purpose keeps ----

// scopedStore is the names keep says are one purpose's, in a store others
// may share.
type scopedStore struct {
	secretStore
	keep func(name string) bool
}

func (s scopedStore) Names() ([]string, error) {
	all, err := s.secretStore.Names()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range all {
		if s.keep(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// Forget removes this purpose's names; an encrypted file left with nothing
// in it is deleted, as it was before it could be shared.
func (s scopedStore) Forget() error {
	names, err := s.Names()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		if err := s.Update(nil, names...); err != nil {
			return err
		}
	}
	if e, ok := s.secretStore.(encryptedStore); ok && e.v.Exists() && e.v.Unlocked() && len(e.v.Names()) == 0 {
		return e.v.Reset()
	}
	return nil
}

// isPasswordName says a secret is the passwords': an account's password or
// an OAuth sign-in.
func isPasswordName(n string) bool {
	return strings.HasPrefix(n, "pass/") || strings.HasPrefix(n, "oauth/")
}

// keyName is where a private key of format is kept, by its id.
func keyName(format, id string) string { return "keys/" + format + "/" + id }

func keyPrefix(format string) string { return "keys/" + format + "/" }

// passwordStore is the passwords' part of the store kind.
func (s *LocalStore) passwordStore(kind string) secretStore {
	if kind == "" {
		return s.storeFor(kind) // before a choice: mail.json and the old token files, nothing else
	}
	return scopedStore{s.storeFor(kind), isPasswordName}
}

// keyStore is where comms-mail's own keys of format are kept; nil before a
// place is chosen.
func (s *LocalStore) keyStore(format string) secretStore {
	return s.keyStoreAt(format, s.keyChoice(format))
}

func (s *LocalStore) keyStoreAt(format string, c KeyChoice) secretStore {
	prefix := keyPrefix(format)
	keep := func(n string) bool { return strings.HasPrefix(n, prefix) }
	switch c.Store {
	case StoreKeyring:
		return scopedStore{theKeyring, keep}
	case StoreEncrypted:
		return scopedStore{encryptedStore{s.vaultOf()}, keep}
	case StorePlain:
		return scopedStore{plainKeyStore{}, keep}
	case StoreSecretVault:
		return scopedStore{svKeyStore{vault: c.Vault}, keep}
	}
	return nil
}

// encryptedUsers are who keeps secrets in the encrypted file: "passwords",
// and each format whose own keys are there.
func (s *LocalStore) encryptedUsers() []string {
	var out []string
	if s.secretKind() == StoreEncrypted {
		out = append(out, "passwords")
	}
	for _, f := range keyFormats {
		if s.keyChoice(f).Store == StoreEncrypted {
			out = append(out, f)
		}
	}
	return out
}

// intoEncrypted puts values in the encrypted file for purpose. A file
// another purpose keeps secrets in is added to — opened with passphrase,
// its own, when it is not open yet; otherwise the file is made anew with
// passphrase, replacing one left from an earlier choice.
func (s *LocalStore) intoEncrypted(purpose, passphrase string, values map[string]string) error {
	v := s.vaultOf()
	shared := slices.ContainsFunc(s.encryptedUsers(), func(u string) bool { return u != purpose })
	if v.Exists() && shared {
		if !v.Unlocked() {
			if passphrase == "" {
				return errors.New("the encrypted file is locked: enter its passphrase")
			}
			if err := v.Unlock(passphrase); err != nil {
				return err
			}
		}
		return v.Update(values)
	}
	if v.Exists() {
		if err := v.Reset(); err != nil {
			return err
		}
	}
	return v.Create(passphrase, values)
}

// ---- comms-mail's own keys in a plain file ----

// plainKeyStore keeps the keys readable, in keys.json beside mail.json
// (0600): the plain file, for keys.
type plainKeyStore struct{}

var plainKeysMu sync.Mutex

func plainKeysPath() string { return filepath.Join(filepath.Dir(ConfigPath()), "keys.json") }

func readPlainKeys() (map[string]string, error) {
	out := map[string]string{}
	b, err := os.ReadFile(plainKeysPath())
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", plainKeysPath(), err)
	}
	return out, nil
}

func (plainKeyStore) Kind() string { return StorePlain }
func (plainKeyStore) Ready() error { return nil }

func (plainKeyStore) Get(name string) (string, bool, error) {
	plainKeysMu.Lock()
	defer plainKeysMu.Unlock()
	all, err := readPlainKeys()
	if err != nil {
		return "", false, err
	}
	v, ok := all[name]
	return v, ok, nil
}

func (plainKeyStore) Update(set map[string]string, del ...string) error {
	plainKeysMu.Lock()
	defer plainKeysMu.Unlock()
	all, err := readPlainKeys()
	if err != nil {
		return err
	}
	for k, v := range set {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	for _, k := range del {
		delete(all, k)
	}
	if len(all) == 0 {
		if err := os.Remove(plainKeysPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plainKeysPath()), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(plainKeysPath(), b, 0o600)
}

func (plainKeyStore) Names() ([]string, error) {
	plainKeysMu.Lock()
	defer plainKeysMu.Unlock()
	all, err := readPlainKeys()
	if err != nil {
		return nil, err
	}
	var out []string
	for k := range all {
		out = append(out, k)
	}
	return out, nil
}

func (p plainKeyStore) Forget() error {
	names, err := p.Names()
	if err != nil {
		return err
	}
	return p.Update(nil, names...)
}

// ---- comms-mail's own keys in a secretvault vault ----

// svKeyStore keeps comms-mail's own keys as items in a secretvault vault
// ("" its default), which may not be the passwords'. Nothing is cached: a
// key is read each time it is used, so nothing is held past the vault
// locking.
type svKeyStore struct{ vault string }

const svKindKey = "key"

func (svKeyStore) Kind() string { return StoreSecretVault }

// resolve is the vault's name as secretvault has it, ErrLocked when it is
// locked.
func (k svKeyStore) resolve() (string, error) {
	if _, err := theSecretVault.connect(); err != nil {
		return "", err
	}
	var vaults []svVaultInfo
	if err := theSecretVault.call("vault.list", nil, &vaults); err != nil {
		return "", err
	}
	vi, ok := pickVault(vaults, k.vault)
	if !ok {
		return "", missingVault(k.vault)
	}
	if vi.Locked {
		return vi.Name, ErrLocked
	}
	return vi.Name, nil
}

func (k svKeyStore) Ready() error {
	_, err := k.resolve()
	return err
}

// fail is a refusal in words; locked is ErrLocked.
func (svKeyStore) fail(err error) error {
	switch svCode(err) {
	case svCodeLocked:
		return ErrLocked
	case svCodeDenied, svCodeCanceled:
		return errors.New("secretvault did not let comms-mail at its keys")
	}
	return err
}

func (k svKeyStore) Get(name string) (string, bool, error) {
	vault, err := k.resolve()
	if err != nil {
		return "", false, err
	}
	var e svEntry
	err = theSecretVault.call("item.get", svItemRef{Vault: vault, Name: svItemName(name)}, &e)
	if svCode(err) == svCodeNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, k.fail(err)
	}
	for _, f := range e.Item.Fields {
		if f.Name == svFieldSecret {
			v := string(f.Value)
			clear(f.Value)
			return v, true, nil
		}
	}
	return "", false, nil
}

func (k svKeyStore) Update(set map[string]string, del ...string) error {
	vault, err := k.resolve()
	if err != nil {
		return err
	}
	for name, v := range set {
		if v == "" {
			del = append(del, name)
			continue
		}
		it := svItem{Kind: svKindKey, Name: svItemName(name), Label: "comms-mail: " + strings.ReplaceAll(strings.TrimPrefix(name, "keys/"), "/", " key "),
			Fields: []svField{{Name: svFieldSecret, Type: "key", Value: []byte(v)}}}
		if err := theSecretVault.call("item.put", map[string]any{"vault": vault, "item": it}, nil); err != nil {
			return k.fail(err)
		}
	}
	for _, name := range del {
		if err := theSecretVault.deleteIn(vault, name); err != nil {
			return k.fail(err)
		}
	}
	return nil
}

func (k svKeyStore) Names() ([]string, error) {
	vault, err := k.resolve()
	if err != nil {
		return nil, err
	}
	var entries []svEntry
	if err := theSecretVault.call("item.list", map[string]string{"vault": vault, "prefix": svItemName("keys/")}, &entries); err != nil {
		return nil, k.fail(err)
	}
	var out []string
	for _, e := range entries {
		if n, ok := strings.CutPrefix(e.Item.Name, svItemPrefix); ok && !e.Deleted {
			out = append(out, n)
		}
	}
	return out, nil
}

func (k svKeyStore) Forget() error {
	names, err := k.Names()
	if err != nil {
		return err
	}
	return k.Update(nil, names...)
}

// unlock asks secretvault to unlock the vault, with its own prompt.
func (k svKeyStore) unlock() error {
	vault, err := k.resolve()
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrLocked) {
		return err
	}
	if err := theSecretVault.call("vault.unlock", map[string]string{"vault": vault}, nil); err != nil {
		if c := svCode(err); c == svCodeCanceled || c == svCodeDenied {
			return errors.New("secretvault stayed locked")
		}
		return err
	}
	return nil
}

// ---- choosing ----

// UseKeys has engine do format's work and, for comms-mail's own, keeps its
// keys in kind (vault: secretvault's vault for them, "" its default). A
// new place takes the keys comms-mail has, written there first and taken
// out of the old place last; the encrypted file takes passphrase — a new
// one, or the file's own when other secrets are kept in it and it is not
// open. Keys secretvault keeps stay in secretvault.
func (s *LocalStore) UseKeys(format, engine, kind, passphrase, vault string) error {
	if !validFormat(format) {
		return fmt.Errorf("mail: no key format %q", format)
	}
	switch engine {
	case EngineSecretVault, EngineOwn:
	default:
		return fmt.Errorf("mail: no engine %q", engine)
	}
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
	cur := s.keyChoice(format)
	next := KeyChoice{Engine: engine, Store: cur.Store, Vault: cur.Vault}
	if engine == EngineSecretVault {
		next.Engine = ""
	} else {
		switch kind {
		case StoreKeyring, StoreSecretVault, StoreEncrypted, StorePlain:
		default:
			return errors.New("choose where comms-mail keeps its own keys")
		}
		next.Store, next.Vault = kind, ""
		if kind == StoreSecretVault {
			next.Vault = strings.TrimSpace(vault)
		}
	}
	if next.Store != cur.Store || next.Vault != cur.Vault {
		if err := s.moveKeys(format, cur, next, passphrase); err != nil {
			return err
		}
	}
	s.mu.Lock()
	c := next
	switch format {
	case FormatOpenPGP:
		s.cfg.OpenPGP = &c
	case FormatSMIME:
		s.cfg.SMIME = &c
	}
	file, _ := LoadConfig()
	err := s.saveConfig(file)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	Logf("keys: %s by %s, kept in %s", format, next.engine(), firstNonEmpty(next.Store, "no place yet"))
	s.afterUnlock()
	return nil
}

// moveKeys takes format's own keys from cur's place to next's.
func (s *LocalStore) moveKeys(format string, cur, next KeyChoice, passphrase string) error {
	values := map[string]string{}
	src := s.keyStoreAt(format, cur)
	if src != nil {
		names, err := src.Names()
		if errors.Is(err, ErrLocked) {
			return fmt.Errorf("the keys in %s cannot be read now: unlock it first", StoreLabel(cur.Store))
		}
		if err != nil {
			return err
		}
		for _, n := range names {
			v, ok, err := src.Get(n)
			if err != nil {
				return err
			}
			if ok && v != "" {
				values[n] = v
			}
		}
	}
	dst := s.keyStoreAt(format, next)
	switch next.Store {
	case StoreEncrypted:
		// The choice is recorded only below; the file's users are counted
		// as they are now, without this format.
		if err := s.intoEncrypted(format, passphrase, values); err != nil {
			return err
		}
	case StoreSecretVault:
		ks := svKeyStore{vault: next.Vault}
		if err := ks.Ready(); errors.Is(err, ErrLocked) {
			if err := ks.unlock(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := dst.Update(values); err != nil {
			return err
		}
	default:
		if err := dst.Ready(); err != nil {
			return err
		}
		if len(values) > 0 {
			if err := dst.Update(values); err != nil {
				return err
			}
		}
	}
	if src != nil && len(values) > 0 {
		if err := src.Forget(); err != nil {
			Logf("keys: clearing %s: %v", StoreLabel(cur.Store), err)
		}
	}
	return nil
}

// UnlockKeys opens where format's own keys are kept, for this run: the
// encrypted file with passphrase, the keyring and secretvault with their
// own prompts.
func (s *LocalStore) UnlockKeys(format, passphrase string) error {
	if !validFormat(format) {
		return fmt.Errorf("mail: no key format %q", format)
	}
	c := s.keyChoice(format)
	switch c.Store {
	case StoreEncrypted:
		v := s.vaultOf()
		if !v.Unlocked() {
			if err := v.Unlock(passphrase); err != nil {
				return err
			}
		}
	case StoreKeyring:
		if err := keyringUnlock(); err != nil {
			return err
		}
		theKeyring.forgetLookups()
	case StoreSecretVault:
		if err := (svKeyStore{vault: c.Vault}).unlock(); err != nil {
			return err
		}
	}
	s.afterUnlock()
	return nil
}

// KeyPlace is a format's engine and where its own keys are, for Settings.
type KeyPlace struct {
	Engine string `json:"engine"`
	Store  string `json:"store,omitempty"`
	Vault  string `json:"vault,omitempty"`
	// Ready: the keys can be used now; Locked: the place must be unlocked
	// first; Problem why not, otherwise. All three are of comms-mail's own
	// keys' place, while it has one.
	Ready   bool   `json:"ready,omitempty"`
	Locked  bool   `json:"locked,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// keyPlace is format's KeyPlace.
func (s *LocalStore) keyPlace(format string) KeyPlace {
	c := s.keyChoice(format)
	p := KeyPlace{Engine: c.engine(), Store: c.Store, Vault: c.Vault}
	st := s.keyStoreAt(format, c)
	if st == nil {
		return p
	}
	switch err := st.Ready(); {
	case err == nil:
		p.Ready = true
	case errors.Is(err, ErrLocked):
		p.Locked = true
	default:
		p.Problem = err.Error()
	}
	return p
}
