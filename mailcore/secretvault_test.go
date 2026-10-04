package mailcore

import (
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/internal/svtest"
)

// startFakeSecretVault runs a stand-in secretvaultd (internal/svtest) for
// the test, with comms-maild's view of secretvault starting afresh and
// looking for a daemon that is not running every 20ms rather than 5s.
func startFakeSecretVault(t *testing.T, locked bool) *svtest.Vault {
	t.Helper()
	f := svtest.Start(t, locked)
	prev := svRetryEvery
	svRetryEvery = 20 * time.Millisecond
	theSecretVault.reset()
	t.Cleanup(func() {
		theSecretVault.reset()
		svRetryEvery = prev
	})
	return f
}

// waitFor polls cond for a second: the daemon's notifications arrive on
// their own goroutine.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal(what)
}

// Choosing secretvault moves every secret into it, as items a person can
// recognise in secretvault's own windows, and out of mail.json.
func TestChoosingSecretVault(t *testing.T) {
	f := startFakeSecretVault(t, false)
	st, dir := vaultFixture(t)
	if s := st.SecretsStatus(); !s.SecretVaultAvailable || s.SecretVaultProblem != "" {
		t.Fatalf("status before %+v", s)
	}
	if err := st.UseStore(StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	if inConfig, legacy, _ := secretsIn(t, st, dir); inConfig || legacy {
		t.Fatalf("left behind: in mail.json %v, old token files %v", inConfig, legacy)
	}
	pass, ok := f.Item("comms-mail/pass/home/imap")
	if !ok || pass.Kind != svKindPassword || len(pass.Fields) != 1 || pass.Fields[0].Name != svFieldPass ||
		string(pass.Fields[0].Value) != "imap-secret" || !strings.Contains(pass.Label, "IMAP password for home") {
		t.Fatalf("the IMAP password in secretvault: %+v (found %v)", pass, ok)
	}
	if tok, ok := f.Item("comms-mail/oauth/home"); !ok || tok.Kind != svKindToken || tok.Fields[0].Name != svFieldSecret {
		t.Fatalf("the sign-in in secretvault: %+v (found %v)", tok, ok)
	}
	if s := st.SecretsStatus(); s.Store != StoreSecretVault || !s.Ready || s.Locked {
		t.Fatalf("status after %+v", s)
	}
	checkSecretsWork(t, st, "secretvault")

	// Read once a run: the second connection does not ask again.
	gets := f.Gets()
	checkSecretsWork(t, st, "secretvault, again")
	again := f.Gets()
	if again != gets {
		t.Fatalf("read %d more times", again-gets)
	}

	// And moving away takes them out of it.
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Item("comms-mail/pass/home/imap"); ok {
		t.Fatal("the password stayed in secretvault")
	}
	checkSecretsWork(t, st, "moved back out")
}

// While secretvault is locked comms-maild holds nothing: what it read is
// dropped, its sessions (which keep a password to reconnect with) are
// closed, and nothing connects until the vault unlocks.
func TestSecretVaultLockedWaits(t *testing.T) {
	f := startFakeSecretVault(t, false)
	st, _ := vaultFixture(t)
	if err := st.UseStore(StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	checkSecretsWork(t, st, "open")
	cfg, _ := st.accountCfg("home")
	st.mu.Lock()
	st.clients["home"] = newIMAPClient(cfg.IMAP, cfg.Address)
	st.mu.Unlock()
	events := make(chan StoreEvent, 16)
	st.SetOnChange(func(ev StoreEvent) {
		select {
		case events <- ev:
		default:
		}
	})

	f.Lock()
	waitFor(t, "the lock was not noticed", func() bool { return st.SecretsStatus().Locked })
	st.mu.Lock()
	sessions := len(st.clients)
	st.mu.Unlock()
	if sessions != 0 {
		t.Fatal("a session that keeps the password stayed open")
	}
	theSecretVault.mu.Lock()
	held := len(theSecretVault.known)
	theSecretVault.mu.Unlock()
	if held != 0 {
		t.Fatalf("%d secrets held while locked", held)
	}
	if cfg, _ := st.accountCfg("home"); !cfg.IMAP.locked || cfg.IMAP.Pass != "" {
		t.Fatalf("locked: the account would connect: %+v", cfg.IMAP)
	}
	if !gotStoreEvent(events, "vault") {
		t.Fatal("the window was not told")
	}

	f.Unlock()
	waitFor(t, "the unlock was not noticed", func() bool { return st.SecretsStatus().Ready })
	checkSecretsWork(t, st, "unlocked again")
}

func gotStoreEvent(ch <-chan StoreEvent, reason string) bool {
	for {
		select {
		case ev := <-ch:
			if ev.Reason == reason {
				return true
			}
		case <-time.After(time.Second):
			return false
		}
	}
}

// A daemon that starts with the vault locked waits for it, and connects
// once it unlocks; choosing secretvault while it is locked asks it to
// unlock (the person is there, choosing).
func TestSecretVaultLockedAtStart(t *testing.T) {
	f := startFakeSecretVault(t, true)
	st, dir := vaultFixture(t)
	if err := st.UseStore(StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	asked := f.Unlocks()
	if asked != 1 {
		t.Fatalf("choosing a locked secretvault asked it to unlock %d times", asked)
	}

	// A new run of the daemon, with the vault locked again.
	f.Lock()
	theSecretVault.reset()
	again := reopenStore(t, dir)
	if s := again.SecretsStatus(); s.Store != StoreSecretVault || !s.Locked {
		t.Fatalf("restarted: %+v", s)
	}
	if cfg, _ := again.accountCfg("home"); !cfg.IMAP.locked {
		t.Fatal("restarted locked: the account would connect")
	}
	asked = f.Unlocks()
	if asked != 1 {
		t.Fatal("the daemon asked secretvault to unlock by itself")
	}
	f.Unlock()
	waitFor(t, "the unlock was not noticed", func() bool { return again.SecretsStatus().Ready })
	checkSecretsWork(t, again, "after the unlock")
}

// When the person says no, comms-maild stops asking, says why, and asks
// again when they want it to (Settings, or the vault's next unlock).
func TestSecretVaultRefused(t *testing.T) {
	f := startFakeSecretVault(t, false)
	st, _ := vaultFixture(t)
	if err := st.UseStore(StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	theSecretVault.mu.Lock()
	theSecretVault.forgetLocked()
	theSecretVault.mu.Unlock()
	f.Deny(true)
	if cfg, _ := st.accountCfg("home"); !cfg.IMAP.locked {
		t.Fatal("refused: the account would connect")
	}
	s := st.SecretsStatus()
	if s.Ready || !strings.Contains(s.Problem, "did not let comms-mail") {
		t.Fatalf("refused: %+v", s)
	}
	f.Deny(false)
	if err := st.UnlockSecrets(""); err != nil {
		t.Fatal(err)
	}
	checkSecretsWork(t, st, "asked again")
}

// A secretvault that starts after comms-maild (both at login) is found,
// and so is one that restarts.
func TestSecretVaultStartsLater(t *testing.T) {
	f := startFakeSecretVault(t, false)
	st, dir := vaultFixture(t)
	if err := st.UseStore(StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	f.Stop()
	waitFor(t, "secretvault going away was not noticed", func() bool { return !st.SecretsStatus().Ready })
	if s := st.SecretsStatus(); s.SecretVaultAvailable || !strings.Contains(s.Problem, "not running") {
		t.Fatalf("gone: %+v", s)
	}
	_ = dir
	f.Listen()
	f.Unlock()
	waitFor(t, "secretvault coming back was not noticed", func() bool { return st.SecretsStatus().Ready })
	checkSecretsWork(t, st, "back")
}
