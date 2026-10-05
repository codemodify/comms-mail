package mailcore

import (
	"strings"
	"testing"
)

// Your keys: for each address you send from, what secretvault holds;
// making an OpenPGP key and bringing in a .p12 are secretvault's, and
// comms-mail keeps nothing of either.
func TestOwnKeysInSecretVault(t *testing.T) {
	smtp := startFakeSMTP(t)
	st, sv := sendingStore(t, smtp)
	if _, err := st.PutIdentity(Identity{ID: "w-ada", AccountID: "w", Name: "Ada Lovelace", Address: "ada@example.com"}); err != nil {
		t.Fatal(err)
	}
	keys := st.OwnKeys()
	if !keys.Available || keys.Locked || len(keys.Addresses) != 1 || keys.Addresses[0].Address != "ada@example.com" {
		t.Fatalf("keys %+v", keys)
	}
	if a := keys.Addresses[0]; a.PGP != nil || len(a.SMIME) != 0 {
		t.Fatalf("no keys yet: %+v", a)
	}

	keys, err := st.MakePGPKey("Ada Lovelace <ada@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if g := sv.Generates(); len(g) != 1 || g[0].Name != "Ada Lovelace" || len(g[0].Emails) != 1 || g[0].Emails[0] != "ada@example.com" {
		t.Fatalf("secretvault was asked %+v", g)
	}
	if k := keys.Addresses[0].PGP; k == nil || k.Fingerprint == "" || !strings.Contains(k.PublicKey, "PUBLIC KEY BLOCK") {
		t.Fatalf("after making it: %+v", keys.Addresses[0])
	}

	sv.P12("open sesame", "ada@example.com")
	file, wrong := []byte("PKCS12 bytes"), []byte("wrong")
	if _, err := st.ImportSMIME(file, wrong); err == nil || !strings.Contains(err.Error(), "does not open") {
		t.Fatalf("wrong password: %v", err)
	}
	if string(file) != "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" || string(wrong) != "\x00\x00\x00\x00\x00" {
		t.Fatal("the file and its password were not wiped")
	}
	keys, err = st.ImportSMIME([]byte("PKCS12 bytes"), []byte("open sesame"))
	if err != nil {
		t.Fatal(err)
	}
	if c := keys.Addresses[0].SMIME; len(c) != 1 || certIssuerCN(c[0].Issuer) != "Example CA" || c[0].NotAfter.Year() != 2027 {
		t.Fatalf("after importing: %+v", keys.Addresses[0])
	}

	// Locked: nothing is shown, and nothing is asked to unlock.
	sv.Lock()
	waitFor(t, "the lock was not noticed", func() bool { return st.SecretsStatus().Locked })
	if keys := st.OwnKeys(); !keys.Locked || len(keys.Addresses) != 0 {
		t.Fatalf("locked: %+v", keys)
	}
	if _, err := st.MakePGPKey("ada@example.com"); err == nil || !strings.Contains(err.Error(), "locked") || sv.Unlocks() != 0 {
		t.Fatalf("make while locked: %v", err)
	}
	sv.Unlock()
	waitFor(t, "the unlock was not noticed", func() bool { return st.SecretsStatus().Ready })

	// The passwords kept elsewhere: secretvault still keeps the keys.
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if keys := st.OwnKeys(); !keys.Available || len(keys.Addresses) == 0 {
		t.Fatalf("passwords elsewhere: %+v", keys)
	}
	// secretvault not running: the keys page says so.
	sv.Stop()
	theSecretVault.reset()
	if keys := st.OwnKeys(); keys.Available || !strings.Contains(keys.Why, "not running") {
		t.Fatalf("secretvault gone: %+v", keys)
	}
}

func certIssuerCN(dn string) string {
	for _, p := range strings.Split(dn, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(p), "CN="); ok {
			return v
		}
	}
	return dn
}
