package mailcore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// How LocalStore uses the vault (vault.go): account passwords and OAuth
// tokens are in it once it exists; mail.json then holds none. Until the
// owner sets a passphrase (CreateVault) an install keeps working the old
// way, passwords in mail.json, so nothing breaks before they choose one.

// VaultStatus is what the window needs to know to prompt.
type VaultStatus struct {
	// Supported: the daemon's store keeps secrets (the demo store does not).
	Supported bool `json:"supported"`
	// Exists: a passphrase has been set.
	Exists bool `json:"exists"`
	// Unlocked: the daemon has the key for this run.
	Unlocked bool `json:"unlocked"`
	// PlainSecrets: there is no vault yet and secrets sit unprotected —
	// passwords in mail.json, or token files with their key beside them.
	PlainSecrets bool `json:"plainSecrets"`
	// PlainAccounts are the addresses whose passwords are in mail.json.
	PlainAccounts []string `json:"plainAccounts,omitempty"`
}

// accountKeyID is the id an account's secrets are filed under.
func accountKeyID(a AccountConfig) string {
	if a.ID != "" {
		return a.ID
	}
	return slug(a.Address)
}

// vaultOf is the store's vault.
func (s *LocalStore) vaultOf() *Vault {
	if s.vault != nil {
		return s.vault
	}
	return OpenVault(filepath.Join(s.dir, "secrets", "vault.json"))
}

// saveConfig writes mail.json; once the vault exists it holds no
// passwords.
func (s *LocalStore) saveConfig(cfg MailConfig) error {
	if s.vaultOf().Exists() {
		for i := range cfg.Accounts {
			a := &cfg.Accounts[i]
			a.IMAP.Pass, a.POP.Pass, a.SMTP.Pass = "", "", ""
		}
	}
	return SaveConfig(cfg)
}

// withSecrets fills an account's passwords from the vault, or marks its
// servers locked when the vault is locked. Without a vault the config is
// as mail.json has it.
func (s *LocalStore) withSecrets(id string, a AccountConfig) AccountConfig {
	v := s.vaultOf()
	if !v.Exists() || a.IsLocal() {
		return a
	}
	if !v.Unlocked() {
		a.IMAP.locked, a.POP.locked, a.SMTP.locked = true, true, true
		return a
	}
	fill := func(sc *ServerConfig, which string) {
		if sc.Pass == "" {
			sc.Pass, _ = v.Get(passSecret(id, which))
		}
	}
	fill(&a.IMAP, "imap")
	fill(&a.POP, "pop")
	fill(&a.SMTP, "smtp")
	return a
}

// VaultStatus reports the vault's state.
func (s *LocalStore) VaultStatus() VaultStatus {
	v := s.vaultOf()
	st := VaultStatus{Supported: true, Exists: v.Exists(), Unlocked: v.Unlocked()}
	if !st.Exists {
		s.mu.Lock()
		for _, a := range s.cfg.Accounts {
			if a.IMAP.Pass != "" || a.POP.Pass != "" || a.SMTP.Pass != "" {
				st.PlainSecrets = true
				st.PlainAccounts = append(st.PlainAccounts, firstNonEmpty(a.Address, a.Name, a.ID))
			}
		}
		s.mu.Unlock()
		if len(DefaultTokenStore().legacyTokens()) > 0 {
			st.PlainSecrets = true
		}
	}
	return st
}

// CreateVault sets the passphrase: it makes the vault, moves every secret
// into it — the passwords in mail.json, the token files — and deletes the
// unprotected copies. The vault is written first, so a crash part-way
// leaves the secrets in both places, never in neither; the next unlock
// finishes the move.
func (s *LocalStore) CreateVault(passphrase string) error {
	v := s.vaultOf()
	if v.Exists() {
		return errors.New("a passphrase is already set; change it in Settings › Privacy")
	}
	s.mu.Lock()
	entries := plainSecrets(s.cfg.Accounts)
	s.mu.Unlock()
	if file, err := LoadConfig(); err == nil {
		for k, val := range plainSecrets(file.Accounts) {
			entries[k] = val
		}
	}
	for key, tok := range DefaultTokenStore().legacyTokens() {
		if raw, err := jsonString(tok); err == nil {
			entries[tokenSecret(key)] = raw
		}
	}
	if err := v.Create(passphrase, entries); err != nil {
		return err
	}
	s.afterUnlock()
	return nil
}

// UnlockVault opens the vault for this run of the daemon.
func (s *LocalStore) UnlockVault(passphrase string) error {
	v := s.vaultOf()
	if !v.Exists() {
		return errors.New("no passphrase has been set yet")
	}
	if !v.Unlocked() {
		if err := v.Unlock(passphrase); err != nil {
			return err
		}
	}
	s.afterUnlock()
	return nil
}

// ChangePassphrase locks the vault with a new passphrase.
func (s *LocalStore) ChangePassphrase(old, next string) error {
	v := s.vaultOf()
	if !v.Exists() {
		return errors.New("no passphrase has been set yet")
	}
	return v.ChangePassphrase(old, next)
}

// ResetVault deletes the vault, for a forgotten passphrase: every saved
// password and sign-in is gone, and each account needs its password (or
// its OAuth sign-in) again. The accounts and their mail stay.
func (s *LocalStore) ResetVault() error {
	if err := s.vaultOf().Reset(); err != nil {
		return err
	}
	s.mu.Lock()
	for _, pool := range []map[string]*imapClient{s.clients, s.fgClients} {
		for id, c := range pool {
			c.close()
			delete(pool, id)
		}
	}
	s.mu.Unlock()
	s.Emit(StoreEvent{Reason: "vault"})
	return nil
}

// afterUnlock finishes a move into the vault that a crash interrupted,
// then lets the daemon connect: push restarts with the secrets, a sync
// runs, and the Outbox (sends that waited while locked) goes.
func (s *LocalStore) afterUnlock() {
	v := s.vaultOf()
	s.mu.Lock()
	left := plainSecrets(s.cfg.Accounts)
	s.mu.Unlock()
	if file, err := LoadConfig(); err == nil {
		for k, val := range plainSecrets(file.Accounts) {
			left[k] = val
		}
	}
	tokens := DefaultTokenStore().legacyTokens()
	for key, tok := range tokens {
		if raw, err := jsonString(tok); err == nil {
			if _, have := v.Get(tokenSecret(key)); !have {
				left[tokenSecret(key)] = raw
			}
		}
	}
	for k := range left {
		if _, have := v.Get(k); have && strings.HasPrefix(k, "pass/") {
			delete(left, k) // the vault's copy is the newer one
		}
	}
	if len(left) > 0 {
		if err := v.Update(left); err != nil {
			Logf("vault: moving secrets in: %v", err)
			return
		}
	}
	// Nothing unprotected stays behind: the file loses its passwords
	// (SaveConfig strips them now that the vault exists), the token files
	// and master.key go.
	s.mu.Lock()
	for i := range s.cfg.Accounts {
		a := &s.cfg.Accounts[i]
		a.IMAP.Pass, a.POP.Pass, a.SMTP.Pass = "", "", ""
	}
	s.mu.Unlock()
	if file, err := LoadConfig(); err == nil && len(plainSecrets(file.Accounts)) > 0 {
		if err := s.saveConfig(file); err != nil {
			Logf("vault: taking the passwords out of %s: %v", ConfigPath(), err)
		}
	}
	if len(tokens) > 0 || fileExists(DefaultTokenStore().dir, "master.key") {
		DefaultTokenStore().removeLegacyFiles()
	}
	Logf("vault: unlocked")
	s.Emit(StoreEvent{Reason: "vault"})
	if s.restartPush() {
		// The daemon: what waited while locked goes now.
		go func() {
			_, _ = s.flushOutbox(func(op OutboxOp) bool { return true })
			if res, _ := s.Sync(""); res.New > 0 {
				s.Emit(StoreEvent{Reason: "poll", Count: res.New})
			}
		}()
	}
}

// plainSecrets are the passwords in accounts, as vault entries.
func plainSecrets(accounts []AccountConfig) map[string]string {
	out := map[string]string{}
	for _, a := range accounts {
		id := accountKeyID(a)
		for which, p := range map[string]string{"imap": a.IMAP.Pass, "pop": a.POP.Pass, "smtp": a.SMTP.Pass} {
			if p != "" {
				out[passSecret(id, which)] = p
			}
		}
	}
	return out
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

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
