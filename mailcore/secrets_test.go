package mailcore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretsIn reports where the fixture's secrets are now: in mail.json, in
// the old token files, in the encrypted file.
func secretsIn(t *testing.T, st *LocalStore, dir string) (inConfig, legacyFiles, vaultFile bool) {
	t.Helper()
	raw, _ := os.ReadFile(ConfigPath())
	inConfig = strings.Contains(string(raw), "imap-secret")
	_, err := os.Stat(filepath.Join(dir, "secrets", "home.tok"))
	_, err2 := os.Stat(filepath.Join(dir, "secrets", "master.key"))
	legacyFiles = err == nil || err2 == nil
	vaultFile = st.vaultOf().Exists()
	return
}

// Whatever the store, the account connects with its password and the
// OAuth token is found.
func checkSecretsWork(t *testing.T, st *LocalStore, where string) {
	t.Helper()
	cfg, _ := st.accountCfg("home")
	if cfg.IMAP.Pass != "imap-secret" || cfg.SMTP.Pass != "smtp-secret" || cfg.IMAP.locked {
		t.Fatalf("%s: passwords %+v / %+v", where, cfg.IMAP, cfg.SMTP)
	}
	if tok, err := DefaultTokenStore().Get("home"); err != nil || tok.AccessToken != "acc-token" {
		t.Fatalf("%s: token %+v, %v", where, tok, err)
	}
}

func TestChoosingTheEncryptedFile(t *testing.T) {
	st, dir := vaultFixture(t)
	if s := st.SecretsStatus(); s.Store != "" || !s.PlainSecrets || len(s.PlainAccounts) != 1 {
		t.Fatalf("before: %+v", s)
	}
	if err := st.UseStore(StoreEncrypted, "short"); err == nil {
		t.Fatal("a 5-character passphrase was accepted")
	}
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	inConfig, legacy, vault := secretsIn(t, st, dir)
	if inConfig || legacy || !vault {
		t.Fatalf("after: in mail.json %v, old files %v, vault %v", inConfig, legacy, vault)
	}
	if s := st.SecretsStatus(); s.Store != StoreEncrypted || !s.Ready || s.PlainSecrets {
		t.Fatalf("status %+v", s)
	}
	checkSecretsWork(t, st, "encrypted")
	if cfg, _ := LoadConfig(); cfg.SecretStore != StoreEncrypted {
		t.Fatalf("mail.json records %q", cfg.SecretStore)
	}
}

func TestChoosingThePlainFile(t *testing.T) {
	st, dir := vaultFixture(t)
	if err := st.UseStore(StorePlain, ""); err != nil {
		t.Fatal(err)
	}
	inConfig, legacy, vault := secretsIn(t, st, dir)
	if !inConfig || legacy || vault {
		t.Fatalf("after: in mail.json %v, old files %v, vault %v", inConfig, legacy, vault)
	}
	if _, err := os.Stat(plainTokensPath()); err != nil {
		t.Fatalf("tokens file: %v", err)
	}
	checkSecretsWork(t, st, "plain")
	// A password saved from now on goes to mail.json.
	cfg, _ := st.accountCfg("home")
	cfg.IMAP.Pass = "changed-secret"
	if _, err := st.PutAccount(cfg); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(ConfigPath()); !strings.Contains(string(raw), "changed-secret") {
		t.Fatal("the plain store did not keep the new password in mail.json")
	}
}

// From the encrypted file to the plain one and back: each move empties
// the place the secrets left.
func TestSwitchingStores(t *testing.T) {
	st, dir := vaultFixture(t)
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := st.UseStore(StorePlain, ""); err != nil {
		t.Fatal(err)
	}
	if inConfig, _, vault := secretsIn(t, st, dir); !inConfig || vault {
		t.Fatalf("to plain: in mail.json %v, vault %v", inConfig, vault)
	}
	checkSecretsWork(t, st, "plain")
	if err := st.UseStore(StoreEncrypted, "another passphrase"); err != nil {
		t.Fatal(err)
	}
	if inConfig, _, vault := secretsIn(t, st, dir); inConfig || !vault {
		t.Fatalf("back to encrypted: in mail.json %v, vault %v", inConfig, vault)
	}
	if _, err := os.Stat(plainTokensPath()); err == nil {
		t.Fatal("the plain tokens file stayed")
	}
	checkSecretsWork(t, st, "encrypted again")
	// No secretvault daemon runs here (tests point its socket nowhere).
	if err := st.UseStore(StoreSecretVault, ""); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("secretvault: %v", err)
	}
	if s := st.SecretsStatus(); s.SecretVaultAvailable || s.Store != StoreEncrypted || !strings.Contains(s.SecretVaultProblem, "not running") {
		t.Fatalf("status %+v", s)
	}
}

// A daemon that starts with the encrypted file locked connects to
// nothing: no session, sends wait in the Outbox, accounts cannot change.
func TestLockedDaemonConnectsNowhere(t *testing.T) {
	st, dir := vaultFixture(t)
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
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
	if err := again.UseStore(StorePlain, ""); err == nil {
		t.Fatal("secrets moved out of a locked file")
	}
	if s := again.SecretsStatus(); s.Store != StoreEncrypted || !s.Locked || s.Ready {
		t.Fatalf("status %+v", s)
	}
	if err := again.UnlockSecrets("not it at all"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if err := again.UnlockSecrets("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	checkSecretsWork(t, again, "unlocked")
}

// A forgotten passphrase: reset drops every saved secret and the choice;
// the accounts stay and need their passwords again.
func TestResetForgetsTheSecrets(t *testing.T) {
	st, _ := vaultFixture(t)
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetVault(); err != nil {
		t.Fatal(err)
	}
	if s := st.SecretsStatus(); s.Store != "" || !s.Ready {
		t.Fatalf("status %+v", s)
	}
	cfg, ok := st.accountCfg("home")
	if !ok || cfg.IMAP.Pass != "" || cfg.IMAP.locked {
		t.Fatalf("after reset: %v %+v", ok, cfg.IMAP)
	}
}
