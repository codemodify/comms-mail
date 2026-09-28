//go:build linux

package mailcore

import (
	"errors"
	"testing"
)

// The desktop keyring as the store: every secret moves in, the old copies
// go, and a locked keyring stops connections until it is unlocked.
func TestChoosingTheKeyring(t *testing.T) {
	f := withFakeKeyring(t)
	st, dir := vaultFixture(t)
	if s := st.SecretsStatus(); !s.KeyringAvailable {
		t.Fatalf("keyring not seen: %+v", s)
	}
	if err := st.UseStore(StoreKeyring, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.values(); got["pass/home/imap"] != "imap-secret" || got["pass/home/smtp"] != "smtp-secret" || got["oauth/home"] == "" {
		t.Fatalf("keyring holds %v", got)
	}
	if inConfig, legacy, vault := secretsIn(t, st, dir); inConfig || legacy || vault {
		t.Fatalf("after: in mail.json %v, old files %v, vault %v", inConfig, legacy, vault)
	}
	checkSecretsWork(t, st, "keyring")

	// A new run with the keyring locked: nothing connects until it opens.
	f.mu.Lock()
	f.locked = true
	f.mu.Unlock()
	theKeyring.known, theKeyring.none = map[string]string{}, map[string]bool{}
	again := reopenStore(t, dir)
	if cfg, _ := again.accountCfg("home"); !cfg.IMAP.locked {
		t.Fatalf("locked keyring, config %+v", cfg.IMAP)
	}
	if _, err := again.client("home"); !errors.Is(err, ErrLocked) {
		t.Fatalf("session while locked: %v", err)
	}
	if s := again.SecretsStatus(); s.Store != StoreKeyring || !s.Locked {
		t.Fatalf("status %+v", s)
	}
	if err := again.UnlockSecrets(""); err != nil { // the desktop's prompt, answered
		t.Fatal(err)
	}
	checkSecretsWork(t, again, "keyring unlocked")

	// And out again, to the encrypted file: the keyring is emptied.
	if err := again.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if got := f.values(); len(got) != 0 {
		t.Fatalf("the keyring kept %v", got)
	}
	checkSecretsWork(t, again, "encrypted after keyring")
}
