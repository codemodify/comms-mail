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
// where the keys are: OpenPGP and S/MIME each have their place, chosen
// like the passwords' and independently of them. In Secret Vault (its
// default vault, or one named), secretvault keeps the keys and does the
// work in its own daemon (protection.go, sendprotect.go): comms-mail never
// holds a private key. In the system keyring, the encrypted file or a
// plain file, comms-mail does the work itself (pgpown.go, smimeown.go),
// with its own keys kept there.
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

// The engines: who does the work, which follows from where the keys are.
const (
	EngineSecretVault = "secretvault"
	EngineOwn         = "own"
)

// KeyChoice is where a format's keys are: a store kind ("" is Secret
// Vault, the default) and, with Secret Vault, its vault ("" its default).
// Own is where comms-mail's own keys stay while Secret Vault is chosen, so
// choosing a place of comms-mail's again finds them.
type KeyChoice struct {
	Store string `json:"store,omitempty"`
	Vault string `json:"vault,omitempty"`
	Own   string `json:"own,omitempty"`
	// Engine was written for a day (2026-10-04) before the place alone
	// said who does the work; the place says the same, and it is dropped.
	Engine string `json:"engine,omitempty"`
}

// isOwnPlace says comms-mail keeps keys in kind and does the work itself.
func isOwnPlace(kind string) bool {
	return kind == StoreKeyring || kind == StoreEncrypted || kind == StorePlain
}

// normal is c as the place alone says it.
func (c KeyChoice) normal() KeyChoice {
	c.Engine = ""
	return c
}

// engine is who does c's work: comms-mail in a place of its own, else
// secretvault.
func (c KeyChoice) engine() string {
	if isOwnPlace(c.Store) {
		return EngineOwn
	}
	return EngineSecretVault
}

// ownPlace is where comms-mail's own keys are: the place, when it is
// comms-mail's, else where they stayed; "" when there are none.
func (c KeyChoice) ownPlace() string {
	if isOwnPlace(c.Store) {
		return c.Store
	}
	return c.Own
}

// svVault is the secretvault vault for c's keys, "" its default.
func (c KeyChoice) svVault() string {
	if c.engine() == EngineSecretVault {
		return c.Vault
	}
	return ""
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
	return c.normal()
}

// engineOf is the engine doing format's work.
func (s *LocalStore) engineOf(format string) string { return s.keyChoice(format).engine() }

// svVaultOf is the secretvault vault holding format's keys, "" its
// default.
func (s *LocalStore) svVaultOf(format string) string { return s.keyChoice(format).svVault() }

// svVaultReady says whether secretvault's vault for keys (name, "" its
// default) can be used now: ErrLocked, or why not. It asks nothing.
func svVaultReady(name string) error {
	if _, err := theSecretVault.connect(); err != nil {
		return err
	}
	var vaults []svVaultInfo
	if err := theSecretVault.call("vault.list", nil, &vaults); err != nil {
		return err
	}
	vi, ok := pickVault(vaults, name)
	switch {
	case !ok:
		return missingVault(name)
	case vi.Locked:
		return ErrLocked
	}
	return nil
}

// svUnlockVault asks secretvault to unlock the vault for keys, with its
// own prompt.
func svUnlockVault(name string) error {
	err := svVaultReady(name)
	if err == nil || !errors.Is(err, ErrLocked) {
		return err
	}
	var vaults []svVaultInfo
	_ = theSecretVault.call("vault.list", nil, &vaults)
	vi, _ := pickVault(vaults, name)
	if err := theSecretVault.call("vault.unlock", map[string]string{"vault": vi.Name}, nil); err != nil {
		if c := svCode(err); c == svCodeCanceled || c == svCodeDenied {
			return errors.New("secretvault stayed locked")
		}
		return err
	}
	return nil
}

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

// keyStore is where comms-mail's own keys of format are kept; nil when it
// has no place for them.
func (s *LocalStore) keyStore(format string) secretStore {
	return s.keyStoreIn(format, s.keyChoice(format).ownPlace())
}

func (s *LocalStore) keyStoreIn(format, kind string) secretStore {
	prefix := keyPrefix(format)
	keep := func(n string) bool { return strings.HasPrefix(n, prefix) }
	switch kind {
	case StoreKeyring:
		return scopedStore{theKeyring, keep}
	case StoreEncrypted:
		return scopedStore{encryptedStore{s.vaultOf()}, keep}
	case StorePlain:
		return scopedStore{plainKeyStore{}, keep}
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
		if s.keyChoice(f).ownPlace() == StoreEncrypted {
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

// ---- choosing ----

// UseKeys keeps format's keys in kind: Secret Vault (vault: its vault for
// them, "" its default — asked for when it is not there), where
// secretvault does the work; or a place of comms-mail's, where it does.
// Choosing one of comms-mail's places takes the keys it has there,
// written first and taken out of the old place last; the encrypted file
// takes passphrase — a new one, or the file's own when other secrets are
// kept in it and it is not open. Choosing Secret Vault leaves comms-mail's
// keys where they are, for when one of its places is chosen again.
func (s *LocalStore) UseKeys(format, kind, passphrase, vault string) error {
	if !validFormat(format) {
		return fmt.Errorf("mail: no key format %q", format)
	}
	switch kind {
	case StoreKeyring, StoreSecretVault, StoreEncrypted, StorePlain:
	default:
		return errors.New("choose where the keys are kept")
	}
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
	cur := s.keyChoice(format)
	from := cur.ownPlace()
	next := KeyChoice{Store: kind}
	if kind == StoreSecretVault {
		next.Vault, next.Own = strings.TrimSpace(vault), from
		if err := theSecretVault.ensureVault(next.Vault); err != nil {
			return err
		}
		if err := svVaultReady(next.Vault); errors.Is(err, ErrLocked) {
			if err := svUnlockVault(next.Vault); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	} else if kind != from {
		if err := s.moveKeys(format, from, kind, passphrase); err != nil {
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
	Logf("keys: %s kept in %s, the work done by %s", format, StoreLabel(kind), next.engine())
	s.afterUnlock()
	return nil
}

// moveKeys takes format's own keys from the place from ("" none) to to.
func (s *LocalStore) moveKeys(format, from, to, passphrase string) error {
	values := map[string]string{}
	src := s.keyStoreIn(format, from)
	if src != nil {
		names, err := src.Names()
		if errors.Is(err, ErrLocked) {
			return fmt.Errorf("the keys in %s cannot be read now: unlock it first", StoreLabel(from))
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
	if to == StoreEncrypted {
		// The choice is recorded only after; the file's users are counted
		// as they are now, without this format.
		if err := s.intoEncrypted(format, passphrase, values); err != nil {
			return err
		}
	} else {
		dst := s.keyStoreIn(format, to)
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
			Logf("keys: clearing %s: %v", StoreLabel(from), err)
		}
	}
	return nil
}

// UnlockKeys opens where format's keys are, for this run: the encrypted
// file with passphrase, the keyring and secretvault with their own
// prompts.
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
	case StorePlain:
	default:
		if err := svUnlockVault(c.svVault()); err != nil {
			return err
		}
	}
	s.afterUnlock()
	return nil
}

// KeyPlace is where a format's keys are, for Settings.
type KeyPlace struct {
	Engine string `json:"engine"`
	Store  string `json:"store"`
	Vault  string `json:"vault,omitempty"`
	// Ready: the keys can be used now; Locked: their place must be
	// unlocked first; Problem why not, otherwise.
	Ready   bool   `json:"ready,omitempty"`
	Locked  bool   `json:"locked,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// keyPlace is format's KeyPlace.
func (s *LocalStore) keyPlace(format string) KeyPlace {
	c := s.keyChoice(format)
	p := KeyPlace{Engine: c.engine(), Store: firstNonEmpty(c.Store, StoreSecretVault), Vault: c.svVault()}
	var err error
	if p.Engine == EngineOwn {
		err = s.keyStore(format).Ready()
	} else {
		err = svVaultReady(p.Vault)
	}
	switch {
	case err == nil:
		p.Ready = true
	case errors.Is(err, ErrLocked):
		p.Locked = true
	default:
		p.Problem = err.Error()
	}
	return p
}
