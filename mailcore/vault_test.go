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
