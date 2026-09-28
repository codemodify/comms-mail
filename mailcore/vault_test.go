package mailcore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultLocksItsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "vault.json")
	v := OpenVault(path)
	if err := v.Create("short", nil); err == nil {
		t.Fatal("a 5-character passphrase was accepted")
	}
	if err := v.Create("correct horse battery", map[string]string{"pass/home/imap": "s3cret-imap"}); err != nil {
		t.Fatal(err)
	}
	if err := v.Create("another passphrase", nil); err == nil {
		t.Fatal("a second Create replaced the vault")
	}
	if got, _ := v.Get("pass/home/imap"); got != "s3cret-imap" {
		t.Fatalf("after create: %q", got)
	}
	if err := v.Update(map[string]string{"oauth/home": "tok"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "s3cret") || strings.Contains(string(raw), "home") {
		t.Fatalf("the file shows its contents:\n%s", raw)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}

	v.Lock()
	if _, ok := v.Get("pass/home/imap"); ok || v.Unlocked() {
		t.Fatal("a locked vault gave a secret")
	}
	if err := v.Update(map[string]string{"x": "y"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("update while locked: %v", err)
	}
	if err := v.Unlock("wrong passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if err := v.Unlock("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("oauth/home"); got != "tok" {
		t.Fatalf("after unlock: %q", got)
	}

	if err := v.ChangePassphrase("wrong passphrase", "a new passphrase"); err == nil {
		t.Fatal("changed with the wrong old passphrase")
	}
	if err := v.ChangePassphrase("correct horse battery", "a new passphrase"); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if err := v.Unlock("correct horse battery"); err == nil {
		t.Fatal("the old passphrase still opens it")
	}
	if err := v.Unlock("a new passphrase"); err != nil {
		t.Fatal(err)
	}

	// Weaker KDF settings written into the file do not open it.
	var f vaultFile
	raw, _ = os.ReadFile(path)
	_ = json.Unmarshal(raw, &f)
	f.KDF.MemoryKiB *= 2
	b, _ := json.Marshal(f)
	_ = os.WriteFile(path, b, 0o600)
	v.Lock()
	if err := v.Unlock("a new passphrase"); err == nil {
		t.Fatal("a vault with its KDF settings changed opened")
	}

	if err := v.Reset(); err != nil || v.Exists() {
		t.Fatalf("reset: %v, exists %v", err, v.Exists())
	}
}

// vaultFixture is a store with an account whose passwords are in
// mail.json and an OAuth token in the old token files — an install from
// before the vault.
func vaultFixture(t *testing.T) (st *LocalStore, dir string) {
	t.Helper()
	dir = t.TempDir()
	prev := DefaultTokenStore()
	SetDefaultTokenStore(NewTokenStore(filepath.Join(dir, "secrets")))
	t.Cleanup(func() { SetDefaultTokenStore(prev) })
	st = newTestLocalStore(t, dir)
	if _, err := st.PutAccount(AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "127.0.0.1:1", User: "ada", Pass: "imap-secret", TLSMode: "ssl"},
		SMTP: ServerConfig{Host: "127.0.0.1:1", User: "ada", Pass: "smtp-secret", TLSMode: "ssl"}}); err != nil {
		t.Fatal(err)
	}
	if err := DefaultTokenStore().Put("home", TokenBlob{AccessToken: "acc-token", RefreshToken: "ref-token"}); err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func TestSettingAPassphraseMovesEverySecretIn(t *testing.T) {
	st, dir := vaultFixture(t)
	if s := st.VaultStatus(); s.Exists || !s.PlainSecrets {
		t.Fatalf("before: %+v", s)
	}
	if err := st.CreateVault("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if s := st.VaultStatus(); !s.Exists || !s.Unlocked || s.PlainSecrets {
		t.Fatalf("after: %+v", s)
	}
	cfgRaw, _ := os.ReadFile(ConfigPath())
	for _, secret := range []string{"imap-secret", "smtp-secret"} {
		if strings.Contains(string(cfgRaw), secret) {
			t.Fatalf("mail.json still holds %s:\n%s", secret, cfgRaw)
		}
	}
	for _, name := range []string{"home.tok", "master.key"} {
		if _, err := os.Stat(filepath.Join(dir, "secrets", name)); err == nil {
			t.Errorf("%s is still there", name)
		}
	}
	cfg, _ := st.accountCfg("home")
	if cfg.IMAP.Pass != "imap-secret" || cfg.SMTP.Pass != "smtp-secret" || cfg.IMAP.locked {
		t.Fatalf("passwords from the vault: %+v", cfg)
	}
	if tok, err := DefaultTokenStore().Get("home"); err != nil || tok.AccessToken != "acc-token" || tok.RefreshToken != "ref-token" {
		t.Fatalf("token from the vault: %+v, %v", tok, err)
	}
	// A password saved from now on goes to the vault, not the file.
	edit := cfg
	edit.IMAP.Pass = "changed-secret"
	if _, err := st.PutAccount(edit); err != nil {
		t.Fatal(err)
	}
	cfgRaw, _ = os.ReadFile(ConfigPath())
	if strings.Contains(string(cfgRaw), "changed-secret") {
		t.Fatal("a new password went to mail.json")
	}
	if got, _ := st.vaultOf().Get(passSecret("home", "imap")); got != "changed-secret" {
		t.Fatalf("vault has %q", got)
	}
}

// A daemon that starts with the vault locked connects to nothing: no
// session, sends wait in the Outbox, accounts cannot be changed. Unlocked,
// it has the passwords.
func TestLockedDaemonConnectsNowhere(t *testing.T) {
	st, dir := vaultFixture(t)
	if err := st.CreateVault("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	st.vaultOf().Lock() // a new run of the daemon
	again := reopenStore(t, dir)

	cfg, _ := again.accountCfg("home")
	if !cfg.IMAP.locked || cfg.IMAP.Pass != "" {
		t.Fatalf("locked config: %+v", cfg.IMAP)
	}
	if _, err := again.client("home"); !errors.Is(err, ErrLocked) {
		t.Fatalf("session while locked: %v", err)
	}
	_, err := again.SendViaSMTP("home", "", Message{From: "ada@example.com", To: "bob@example.org", Subject: "s", Body: "b"}, nil)
	var queued *QueuedError
	if !errors.As(err, &queued) || len(again.ListOutbox()) != 1 {
		t.Fatalf("send while locked: %v, outbox %d", err, len(again.ListOutbox()))
	}
	if _, err := again.PutAccount(cfg); !errors.Is(err, ErrLocked) {
		t.Fatalf("account change while locked: %v", err)
	}
	if s := again.VaultStatus(); !s.Exists || s.Unlocked {
		t.Fatalf("status %+v", s)
	}
	if err := again.UnlockVault("not it at all"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if err := again.UnlockVault("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := again.accountCfg("home"); cfg.IMAP.Pass != "imap-secret" || cfg.IMAP.locked {
		t.Fatalf("after unlock: %+v", cfg.IMAP)
	}
}

// A move into the vault that stopped half-way (the vault written, the
// file not yet cleaned) is finished at the next unlock.
func TestUnlockFinishesAnInterruptedMove(t *testing.T) {
	st, dir := vaultFixture(t)
	if err := st.vaultOf().Create("correct horse battery", nil); err != nil {
		t.Fatal(err)
	}
	st.vaultOf().Lock()
	again := reopenStore(t, dir)
	if err := again.UnlockVault("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	cfgRaw, _ := os.ReadFile(ConfigPath())
	if strings.Contains(string(cfgRaw), "imap-secret") {
		t.Fatal("mail.json kept its password")
	}
	if got, _ := again.vaultOf().Get(passSecret("home", "imap")); got != "imap-secret" {
		t.Fatalf("vault has %q", got)
	}
	if tok, err := DefaultTokenStore().Get("home"); err != nil || tok.AccessToken != "acc-token" {
		t.Fatalf("token: %+v %v", tok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets", "master.key")); err == nil {
		t.Fatal("master.key is still there")
	}
}

// A forgotten passphrase: reset drops every saved secret; the accounts
// stay and need their passwords again.
func TestResetForgetsTheSecrets(t *testing.T) {
	st, _ := vaultFixture(t)
	if err := st.CreateVault("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetVault(); err != nil {
		t.Fatal(err)
	}
	if s := st.VaultStatus(); s.Exists {
		t.Fatalf("status %+v", s)
	}
	cfg, ok := st.accountCfg("home")
	if !ok || cfg.IMAP.Pass != "" || cfg.IMAP.locked {
		t.Fatalf("after reset: %v %+v", ok, cfg.IMAP)
	}
}

// reopenStore is the daemon starting again: mail.json read, the cache
// opened.
func reopenStore(t *testing.T, dir string) *LocalStore {
	t.Helper()
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	st, err := NewLocalStoreDir(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
