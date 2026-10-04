package mailcore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Where comms-mail keeps its secrets — account passwords and OAuth tokens
// (later the private keys for PGP and S/MIME). The owner chooses one of
// four stores, and can move everything from one to another (Settings ›
// Security › Passwords):
//
//   - keyring: the desktop's own (Secret Service on Linux, the Keychain on
//     macOS, the Credential Manager on Windows), unlocked at login.
//   - secretvault: codemodify/secretvault, the owner's own (secretvault.go):
//     it asks before letting comms-mail read, and while it is locked
//     comms-maild holds nothing and waits.
//   - encrypted: a file only the owner's passphrase opens (vault.go).
//   - plain: passwords in mail.json as they always were, OAuth tokens in
//     oauth-tokens.json beside it; readable by anything running as the
//     user.
//
// The choice is "secretStore" in mail.json. Until one is made, secrets
// are where older builds kept them — passwords in mail.json, tokens in
// encrypted files with their key beside them — and the window asks.

// The stores.
const (
	StoreKeyring     = "keyring"
	StoreSecretVault = "secretvault"
	StoreEncrypted   = "encrypted"
	StorePlain       = "plain"
)

// StoreLabel is how the window names a store.
func StoreLabel(kind string) string {
	switch kind {
	case StoreKeyring:
		return "the desktop keyring"
	case StoreSecretVault:
		return "secretvault"
	case StoreEncrypted:
		return "an encrypted file"
	case StorePlain:
		return "a plain file (mail.json)"
	}
	return "mail.json, readable (not chosen yet)"
}

// secretStore is one place secrets can live. Names are "pass/<account>/
// <imap|pop|smtp>" and "oauth/<token key>".
type secretStore interface {
	Kind() string
	// Ready is nil when secrets can be read and written now; ErrLocked
	// when the store must be unlocked first.
	Ready() error
	Get(name string) (string, bool, error)
	// Update sets the secrets in set (an empty value removes one) and
	// removes those named in del.
	Update(set map[string]string, del ...string) error
	Names() ([]string, error)
	// Forget removes everything the store holds for comms-mail: the old
	// copies, once they have moved elsewhere.
	Forget() error
}

// active is the store in use, for the OAuth token store (a process-wide
// thing); nil before a choice is made.
var active atomic.Pointer[secretStore]

func setActiveStore(st secretStore) {
	if st == nil || st.Kind() == "" {
		active.Store(nil)
		return
	}
	active.Store(&st)
}

func activeStore() secretStore {
	if p := active.Load(); p != nil {
		return *p
	}
	return nil
}

// ---- encrypted file ----

type encryptedStore struct{ v *Vault }

func (e encryptedStore) Kind() string { return StoreEncrypted }

func (e encryptedStore) Ready() error {
	switch {
	case !e.v.Exists():
		return errors.New("the encrypted file has not been set up")
	case !e.v.Unlocked():
		return ErrLocked
	}
	return nil
}

func (e encryptedStore) Get(name string) (string, bool, error) {
	if err := e.Ready(); err != nil {
		return "", false, err
	}
	v, ok := e.v.Get(name)
	return v, ok, nil
}

func (e encryptedStore) Update(set map[string]string, del ...string) error {
	return e.v.Update(set, del...)
}

func (e encryptedStore) Names() ([]string, error) {
	if err := e.Ready(); err != nil {
		return nil, err
	}
	return e.v.Names(), nil
}

func (e encryptedStore) Forget() error { return e.v.Reset() }

// ---- desktop keyring ----

// keyringStore keeps what it has read in memory, so an account's password
// is fetched from the keyring once per run, not on every connection.
type keyringStore struct {
	mu    sync.Mutex
	known map[string]string // read or written this run
	none  map[string]bool   // looked for, not there
}

var theKeyring = &keyringStore{known: map[string]string{}, none: map[string]bool{}}

func (k *keyringStore) Kind() string { return StoreKeyring }
func (k *keyringStore) Ready() error { return keyringReady() }

func (k *keyringStore) Get(name string) (string, bool, error) {
	k.mu.Lock()
	if v, ok := k.known[name]; ok {
		k.mu.Unlock()
		return v, true, nil
	}
	if k.none[name] {
		k.mu.Unlock()
		return "", false, nil
	}
	k.mu.Unlock()
	v, ok, err := keyringGet(name)
	if err != nil {
		return "", false, err
	}
	k.mu.Lock()
	if ok {
		k.known[name] = v
	} else {
		k.none[name] = true
	}
	k.mu.Unlock()
	return v, ok, nil
}

func (k *keyringStore) Update(set map[string]string, del ...string) error {
	for name, v := range set {
		var err error
		if v == "" {
			err = keyringDelete(name)
		} else {
			err = keyringSet(name, v)
		}
		if err != nil {
			return err
		}
		k.mu.Lock()
		delete(k.none, name)
		if v == "" {
			delete(k.known, name)
			k.none[name] = true
		} else {
			k.known[name] = v
		}
		k.mu.Unlock()
	}
	for _, name := range del {
		if err := keyringDelete(name); err != nil {
			return err
		}
		k.mu.Lock()
		delete(k.known, name)
		k.none[name] = true
		k.mu.Unlock()
	}
	return nil
}

func (k *keyringStore) Names() ([]string, error) { return keyringNames() }

func (k *keyringStore) Forget() error {
	names, err := keyringNames()
	if err != nil {
		return err
	}
	return k.Update(nil, names...)
}

// forgetLookups drops what the store remembers of lookups that found
// nothing (after an unlock, they may now).
func (k *keyringStore) forgetLookups() {
	k.mu.Lock()
	k.none = map[string]bool{}
	k.mu.Unlock()
}

// keyring index: the macOS Keychain cannot list items by service, so the
// names stored there are kept here (names only, never values).

var keyringIndexMu sync.Mutex

func keyringIndexPath() string { return filepath.Join(DataDir(), "secrets", "keyring-names.json") }

func keyringIndexNames() ([]string, error) {
	keyringIndexMu.Lock()
	defer keyringIndexMu.Unlock()
	var names []string
	b, err := os.ReadFile(keyringIndexPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return names, json.Unmarshal(b, &names)
}

func keyringIndexEdit(name string, add bool) error {
	names, err := keyringIndexNames()
	if err != nil {
		return err
	}
	keyringIndexMu.Lock()
	defer keyringIndexMu.Unlock()
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	if add {
		set[name] = true
	} else {
		delete(set, name)
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	b, _ := json.Marshal(out)
	if err := os.MkdirAll(filepath.Dir(keyringIndexPath()), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(keyringIndexPath(), b, 0o600)
}

func keyringIndexAdd(name string) error    { return keyringIndexEdit(name, true) }
func keyringIndexRemove(name string) error { return keyringIndexEdit(name, false) }

// ---- plain file ----

// plainStore keeps passwords in mail.json, as comms-mail always did, and
// OAuth tokens in oauth-tokens.json beside it (0600 both).
type plainStore struct{}

var plainMu sync.Mutex

func plainTokensPath() string { return filepath.Join(filepath.Dir(ConfigPath()), "oauth-tokens.json") }

func (plainStore) Kind() string { return StorePlain }
func (plainStore) Ready() error { return nil }

func splitPassName(name string) (id, which string, ok bool) {
	rest, found := strings.CutPrefix(name, "pass/")
	if !found {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '/')
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

func serverOf(a *AccountConfig, which string) *ServerConfig {
	switch which {
	case "imap":
		return &a.IMAP
	case "pop":
		return &a.POP
	case "smtp":
		return &a.SMTP
	}
	return nil
}

func readPlainTokens() (map[string]string, error) {
	out := map[string]string{}
	b, err := os.ReadFile(plainTokensPath())
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal(b, &out)
}

func writePlainTokens(m map[string]string) error {
	if len(m) == 0 {
		if err := os.Remove(plainTokensPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(plainTokensPath(), b, 0o600)
}

func (plainStore) Get(name string) (string, bool, error) {
	plainMu.Lock()
	defer plainMu.Unlock()
	if id, which, ok := splitPassName(name); ok {
		cfg, err := LoadConfig()
		if err != nil {
			return "", false, nil
		}
		for i := range cfg.Accounts {
			if accountKeyID(cfg.Accounts[i]) == id {
				if sc := serverOf(&cfg.Accounts[i], which); sc != nil && sc.Pass != "" {
					return sc.Pass, true, nil
				}
			}
		}
		return "", false, nil
	}
	toks, err := readPlainTokens()
	if err != nil {
		return "", false, err
	}
	v, ok := toks[strings.TrimPrefix(name, "oauth/")]
	return v, ok, nil
}

func (plainStore) Update(set map[string]string, del ...string) error {
	plainMu.Lock()
	defer plainMu.Unlock()
	all := map[string]string{}
	for k, v := range set {
		all[k] = v
	}
	for _, k := range del {
		all[k] = ""
	}
	var cfg *MailConfig
	toks, err := readPlainTokens()
	if err != nil {
		return err
	}
	tokChanged := false
	for name, v := range all {
		if id, which, ok := splitPassName(name); ok {
			if cfg == nil {
				c, err := LoadConfig()
				if err != nil {
					return err
				}
				cfg = &c
			}
			for i := range cfg.Accounts {
				if accountKeyID(cfg.Accounts[i]) == id {
					if sc := serverOf(&cfg.Accounts[i], which); sc != nil {
						sc.Pass = v
					}
				}
			}
			continue
		}
		key := strings.TrimPrefix(name, "oauth/")
		if v == "" {
			delete(toks, key)
		} else {
			toks[key] = v
		}
		tokChanged = true
	}
	if cfg != nil {
		if err := SaveConfig(*cfg); err != nil {
			return err
		}
	}
	if tokChanged {
		return writePlainTokens(toks)
	}
	return nil
}

func (plainStore) Names() ([]string, error) {
	var names []string
	if cfg, err := LoadConfig(); err == nil {
		names = append(names, passNames(cfg.Accounts)...)
	}
	toks, err := readPlainTokens()
	if err != nil {
		return nil, err
	}
	for k := range toks {
		names = append(names, tokenSecret(k))
	}
	return names, nil
}

func (plainStore) Forget() error {
	plainMu.Lock()
	defer plainMu.Unlock()
	if cfg, err := LoadConfig(); err == nil && len(passNames(cfg.Accounts)) > 0 {
		for i := range cfg.Accounts {
			a := &cfg.Accounts[i]
			a.IMAP.Pass, a.POP.Pass, a.SMTP.Pass = "", "", ""
		}
		if err := SaveConfig(cfg); err != nil {
			return err
		}
	}
	return writePlainTokens(nil)
}

// passNames are the vault names of the passwords in accounts.
func passNames(accounts []AccountConfig) []string {
	var out []string
	for _, a := range accounts {
		for _, which := range []string{"imap", "pop", "smtp"} {
			if sc := serverOf(&a, which); sc.Pass != "" {
				out = append(out, passSecret(accountKeyID(a), which))
			}
		}
	}
	return out
}

// ---- nothing chosen yet ----

// legacyStore is where older builds kept secrets: passwords in mail.json,
// tokens in encrypted files with their key (master.key) beside them.
type legacyStore struct{}

func (legacyStore) Kind() string { return "" }
func (legacyStore) Ready() error { return nil }

func (legacyStore) Get(name string) (string, bool, error) {
	if _, _, ok := splitPassName(name); ok {
		return plainStore{}.Get(name)
	}
	tok, ok := DefaultTokenStore().legacyTokens()[strings.TrimPrefix(name, "oauth/")]
	if !ok {
		return "", false, nil
	}
	raw, err := json.Marshal(tok)
	return string(raw), err == nil, err
}

func (legacyStore) Update(set map[string]string, del ...string) error {
	pass := map[string]string{}
	for k, v := range set {
		if _, _, ok := splitPassName(k); ok {
			pass[k] = v
		}
	}
	var passDel []string
	for _, k := range del {
		if _, _, ok := splitPassName(k); ok {
			passDel = append(passDel, k)
		}
	}
	return plainStore{}.Update(pass, passDel...)
}

func (legacyStore) Names() ([]string, error) {
	var names []string
	if cfg, err := LoadConfig(); err == nil {
		names = append(names, passNames(cfg.Accounts)...)
	}
	for k := range DefaultTokenStore().legacyTokens() {
		names = append(names, tokenSecret(k))
	}
	return names, nil
}

// Forget removes the old token files, master.key and the key's copy in the
// desktop keyring. mail.json's passwords go when the file is next saved
// for a store that keeps them elsewhere (saveConfig), and stay when the
// choice is the plain file, which keeps them there.
func (legacyStore) Forget() error {
	DefaultTokenStore().removeLegacyFiles()
	return nil
}

// ---- the store in LocalStore ----

// SecretsStatus is what the window needs to ask the right question.
type SecretsStatus struct {
	// Supported: the daemon keeps secrets (the demo store does not).
	Supported bool `json:"supported"`
	// Store is the choice made ("" before one is).
	Store string `json:"store"`
	// Ready: the secrets can be read now.
	Ready bool `json:"ready"`
	// Locked: the store must be unlocked (the encrypted file's passphrase,
	// or the desktop keyring's own prompt).
	Locked bool `json:"locked"`
	// Problem says why the store is not ready, when it is not locked.
	Problem string `json:"problem,omitempty"`
	// PlainSecrets: secrets sit readable (passwords in mail.json, or token
	// files with their key beside them); PlainAccounts are whose.
	PlainSecrets  bool     `json:"plainSecrets"`
	PlainAccounts []string `json:"plainAccounts,omitempty"`
	// PlainTokens: OAuth sign-ins sit readable (the old token files with
	// their key beside them, or the plain store's tokens file).
	PlainTokens bool `json:"plainTokens,omitempty"`
	// The desktop keyring: what it is here, and whether it can be used.
	KeyringName string `json:"keyringName"`
	// KeyringBackend is the keyring's own name: Secret Service, the macOS
	// Keychain, the Windows Credential Manager ("" where there is none).
	KeyringBackend   string `json:"keyringBackend,omitempty"`
	KeyringAvailable bool   `json:"keyringAvailable"`
	KeyringProblem   string `json:"keyringProblem,omitempty"`
	// SecretVaultAvailable: codemodify/secretvault's daemon answers here;
	// SecretVaultProblem says why not.
	SecretVaultAvailable bool   `json:"secretVaultAvailable"`
	SecretVaultProblem   string `json:"secretVaultProblem,omitempty"`
	// PlainFile is where the plain store keeps the passwords (mail.json),
	// EncryptedFile the encrypted file: the daemon's own paths.
	PlainFile     string `json:"plainFile,omitempty"`
	EncryptedFile string `json:"encryptedFile,omitempty"`
}

// absPath is p made absolute, or p when it cannot be.
func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// accountKeyID is the id an account's secrets are filed under.
func accountKeyID(a AccountConfig) string {
	if a.ID != "" {
		return a.ID
	}
	return slug(a.Address)
}

// vaultOf is the store's encrypted file.
func (s *LocalStore) vaultOf() *Vault {
	if s.vault != nil {
		return s.vault
	}
	return OpenVault(filepath.Join(s.dir, "secrets", "vault.json"))
}

// secretKind is the store in use ("" before a choice).
func (s *LocalStore) secretKind() string {
	if k, ok := s.kind.Load().(string); ok {
		return k
	}
	return ""
}

// initSecretKind settles the store at start: the choice in mail.json, or
// the encrypted file when one exists from before the choice was offered.
func (s *LocalStore) initSecretKind() {
	kind := s.cfg.SecretStore
	if kind == "" && s.vaultOf().Exists() {
		kind = StoreEncrypted
	}
	s.kind.Store(kind)
	setActiveStore(s.storeFor(kind))
	if kind == StoreSecretVault {
		s.watchSecretVault()
	}
}

// watchSecretVault makes the daemon follow secretvault: when its vault
// locks, comms-maild drops its sessions — each keeps a password to
// reconnect with — and its watchers find the store locked and wait; when
// it unlocks, push restarts, a sync runs and the Outbox goes.
func (s *LocalStore) watchSecretVault() {
	theSecretVault.watch(func() {
		if s.secretKind() != StoreSecretVault {
			return
		}
		s.mu.Lock()
		var drop []*imapClient
		for _, pool := range []map[string]*imapClient{s.clients, s.fgClients} {
			for id, c := range pool {
				drop = append(drop, c)
				delete(pool, id)
			}
		}
		s.mu.Unlock()
		for _, c := range drop {
			go c.close()
		}
		s.restartPush()
		Logf("secrets: secretvault is locked; waiting for it to unlock")
		s.Emit(StoreEvent{Reason: "vault"})
	}, func() {
		if s.secretKind() != StoreSecretVault {
			return
		}
		Logf("secrets: secretvault is unlocked")
		s.afterUnlock()
	})
}

func (s *LocalStore) storeFor(kind string) secretStore {
	switch kind {
	case StoreKeyring:
		return theKeyring
	case StoreSecretVault:
		return secretVaultStore{theSecretVault}
	case StoreEncrypted:
		return encryptedStore{s.vaultOf()}
	case StorePlain:
		return plainStore{}
	}
	return legacyStore{}
}

// keepsPlainPasswords: the store in use keeps passwords in mail.json.
func keepsPlainPasswords(kind string) bool { return kind == "" || kind == StorePlain }

// saveConfig writes mail.json; it holds passwords only for the plain
// store (and before a choice is made).
func (s *LocalStore) saveConfig(cfg MailConfig) error {
	cfg.SecretStore = s.secretKind()
	if !keepsPlainPasswords(cfg.SecretStore) {
		for i := range cfg.Accounts {
			a := &cfg.Accounts[i]
			a.IMAP.Pass, a.POP.Pass, a.SMTP.Pass = "", "", ""
		}
	}
	return SaveConfig(cfg)
}

// withSecrets fills an account's passwords from the store in use, or marks
// its servers locked when the store cannot be read now (nothing connects
// with a locked config).
func (s *LocalStore) withSecrets(id string, a AccountConfig) AccountConfig {
	kind := s.secretKind()
	if keepsPlainPasswords(kind) || a.IsLocal() {
		return a
	}
	st := s.storeFor(kind)
	for _, which := range []string{"imap", "pop", "smtp"} {
		sc := serverOf(&a, which)
		if sc.Pass != "" || strings.TrimSpace(sc.Host) == "" {
			continue
		}
		v, ok, err := st.Get(passSecret(id, which))
		if err != nil {
			a.IMAP.locked, a.POP.locked, a.SMTP.locked = true, true, true
			return a
		}
		if ok {
			sc.Pass = v
		}
	}
	return a
}

// SecretsStatus reports where the secrets are and whether they can be read.
func (s *LocalStore) SecretsStatus() SecretsStatus {
	kind := s.secretKind()
	st := SecretsStatus{Supported: true, Store: kind, KeyringName: keyringName(), KeyringBackend: keyringBackend(),
		PlainFile: absPath(ConfigPath()), EncryptedFile: absPath(s.vaultOf().path)}
	if err := s.storeFor(kind).Ready(); err == nil {
		st.Ready = true
	} else if errors.Is(err, ErrLocked) {
		st.Locked = true
	} else {
		st.Problem = err.Error()
	}
	if keepsPlainPasswords(kind) {
		s.mu.Lock()
		for _, a := range s.cfg.Accounts {
			if a.IMAP.Pass != "" || a.POP.Pass != "" || a.SMTP.Pass != "" {
				st.PlainSecrets = true
				st.PlainAccounts = append(st.PlainAccounts, firstNonEmpty(a.Address, a.Name, a.ID))
			}
		}
		s.mu.Unlock()
		if kind == "" && len(DefaultTokenStore().legacyTokens()) > 0 {
			st.PlainSecrets, st.PlainTokens = true, true
		}
		if kind == StorePlain {
			if toks, err := readPlainTokens(); err == nil && len(toks) > 0 {
				st.PlainSecrets, st.PlainTokens = true, true
			}
		}
	}
	switch err := keyringReady(); {
	case err == nil || errors.Is(err, ErrLocked):
		st.KeyringAvailable = true
	default:
		st.KeyringProblem = err.Error()
	}
	if err := theSecretVault.available(); err == nil {
		st.SecretVaultAvailable = true
	} else {
		st.SecretVaultProblem = err.Error()
	}
	return st
}

// UseStore moves every secret to the store named kind and makes it the
// one in use. For the encrypted file, passphrase locks it. The secrets are
// written to the new store first and taken out of the old one last, so a
// failure part-way leaves them where they were.
func (s *LocalStore) UseStore(kind, passphrase string) error {
	switch kind {
	case StoreKeyring, StoreSecretVault, StoreEncrypted, StorePlain:
	default:
		return fmt.Errorf("mail: no secret store %q", kind)
	}
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
	cur := s.secretKind()
	if cur == kind {
		return nil
	}
	src := s.storeFor(cur)
	if err := src.Ready(); err != nil {
		return fmt.Errorf("the secrets in %s cannot be read now: %w", StoreLabel(cur), err)
	}
	names, err := src.Names()
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, n := range names {
		v, ok, err := src.Get(n)
		if err != nil {
			return err
		}
		if ok && v != "" {
			values[n] = v
		}
	}
	switch kind {
	case StoreEncrypted:
		v := s.vaultOf()
		if v.Exists() {
			if err := v.Reset(); err != nil { // left from an earlier choice
				return err
			}
		}
		if err := v.Create(passphrase, values); err != nil {
			return err
		}
	case StoreKeyring:
		if err := keyringReady(); err != nil {
			return fmt.Errorf("%s: %w", keyringName(), err)
		}
		if err := theKeyring.Update(values); err != nil {
			return err
		}
	case StorePlain:
		if err := (plainStore{}).Update(values); err != nil {
			return err
		}
	case StoreSecretVault:
		sv := secretVaultStore{theSecretVault}
		// The person is choosing it now, so a locked vault is unlocked
		// (secretvault asks) rather than waited for.
		if err := sv.Ready(); errors.Is(err, ErrLocked) {
			if err := sv.unlock(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := sv.Update(values); err != nil {
			return err
		}
	}

	// The choice is recorded; mail.json keeps passwords only for the
	// plain store.
	s.mu.Lock()
	s.kind.Store(kind)
	s.cfg.SecretStore = kind
	file, _ := LoadConfig()
	err = s.saveConfig(file)
	for i := range s.cfg.Accounts {
		a := &s.cfg.Accounts[i]
		for _, which := range []string{"imap", "pop", "smtp"} {
			sc := serverOf(a, which)
			sc.Pass = ""
			if kind == StorePlain {
				sc.Pass = values[passSecret(accountKeyID(*a), which)]
			}
		}
	}
	s.mu.Unlock()
	setActiveStore(s.storeFor(kind))
	if kind == StoreSecretVault {
		s.watchSecretVault()
	}
	if err != nil {
		return err
	}

	// The old copies go last.
	if err := src.Forget(); err != nil {
		Logf("secrets: clearing %s: %v", StoreLabel(cur), err)
	}
	Logf("secrets: now kept in %s", StoreLabel(kind))
	s.afterUnlock()
	return nil
}

// UnlockSecrets opens the store for this run: the encrypted file with the
// passphrase, the desktop keyring and secretvault each with its own prompt.
func (s *LocalStore) UnlockSecrets(passphrase string) error {
	switch s.secretKind() {
	case StoreSecretVault:
		// Unlocking (or asking again after a no) lets the daemon connect
		// through the vault's watcher (watchSecretVault).
		return secretVaultStore{theSecretVault}.unlock()
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
	}
	s.afterUnlock()
	return nil
}

// ChangePassphrase locks the encrypted file with a new passphrase.
func (s *LocalStore) ChangePassphrase(old, next string) error {
	if s.secretKind() != StoreEncrypted {
		return errors.New("the secrets are not in an encrypted file")
	}
	return s.vaultOf().ChangePassphrase(old, next)
}

// ResetVault is for a forgotten passphrase: the encrypted file and every
// secret in it are deleted, no store is chosen, and each account needs
// its password (or its OAuth sign-in) again. The accounts and their mail
// stay.
func (s *LocalStore) ResetVault() error {
	if err := s.vaultOf().Reset(); err != nil {
		return err
	}
	s.mu.Lock()
	s.kind.Store("")
	s.cfg.SecretStore = ""
	file, _ := LoadConfig()
	err := s.saveConfig(file)
	for _, pool := range []map[string]*imapClient{s.clients, s.fgClients} {
		for id, c := range pool {
			c.close()
			delete(pool, id)
		}
	}
	s.mu.Unlock()
	setActiveStore(nil)
	s.Emit(StoreEvent{Reason: "vault"})
	return err
}

// afterUnlock lets the daemon connect once the secrets can be read: push
// restarts, a sync runs, and the Outbox (sends that waited) goes.
func (s *LocalStore) afterUnlock() {
	s.Emit(StoreEvent{Reason: "vault"})
	if s.restartPush() {
		go func() {
			_, _ = s.flushOutbox(func(op OutboxOp) bool { return true })
			if res, _ := s.Sync(""); res.New > 0 {
				s.Emit(StoreEvent{Reason: "poll", Count: res.New})
			}
		}()
	}
}

// restartPush restarts the IDLE watchers, which gave up while locked, and
// reports whether they were running (in the daemon they always are).
func (s *LocalStore) restartPush() bool {
	s.mu.Lock()
	running := s.pushCancel != nil
	ctx := s.pushCtx
	s.mu.Unlock()
	if !running || ctx == nil {
		return false
	}
	s.StopPush()
	s.StartPush(ctx)
	return true
}
