package mailcore

import (
	"path/filepath"
	"testing"
	"time"
)

// Editing an account starts from its settings without the passwords, and
// saving them back keeps what was not changed: the saved passwords, the
// connection security and the signature — also once the daemon starts
// again.
func TestEditAccountKeepsWhatItDoesNotShow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutAccount(AccountConfig{ID: "w", Name: "Ada", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993", Pass: "open sesame", TLSMode: string(TLSImplicit)},
		SMTP: ServerConfig{Host: "smtp.example.com:587", Pass: "open sesame"}}); err != nil {
		t.Fatal(err)
	}
	id := st.Identities("w")[0]
	id.Signature = "-- \nAda"
	if _, err := st.PutIdentity(id); err != nil {
		t.Fatal(err)
	}

	cfg, err := accountSettings(st, "w")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IMAP.Pass != "" || cfg.POP.Pass != "" || cfg.SMTP.Pass != "" {
		t.Fatal("the settings to edit carry a password")
	}
	if cfg.Name != "Ada" || cfg.IMAP.Host != "imap.example.com:993" || cfg.IMAP.TLSMode != string(TLSImplicit) {
		t.Fatalf("settings %+v", cfg)
	}
	cfg.Name = "Ada Lovelace"
	if _, err := st.PutAccount(cfg); err != nil {
		t.Fatal(err)
	}
	got, _ := st.AccountConfig("w")
	got = st.withSecrets(accountKeyID(got), got)
	if got.Name != "Ada Lovelace" || got.IMAP.TLSMode != string(TLSImplicit) {
		t.Fatalf("saved %+v", got)
	}
	if got.IMAP.Pass != "open sesame" || got.SMTP.Pass != "open sesame" {
		t.Fatal("the saved passwords were lost")
	}
	if ids := st.Identities("w"); len(ids) != 1 || ids[0].Signature != "-- \nAda" {
		t.Fatalf("after the edit, identities %+v", ids)
	}
	file, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewLocalStoreDir(file, dir)
	if err != nil {
		t.Fatal(err)
	}
	if ids := again.Identities("w"); len(ids) != 1 || ids[0].Signature != "-- \nAda" {
		t.Fatalf("after a restart, identities %+v", ids)
	}

	if _, err := accountSettings(st, LocalAccountID); err == nil {
		t.Fatal("On This Computer has no server settings to edit")
	}
	if _, err := accountSettings(st, "nobody"); err == nil {
		t.Fatal("settings for an account that is not there")
	}
}

// Where the settings hold the passwords — mail.json for the plain store,
// the demo's memory — the settings to edit still leave them out, and a
// save without them keeps them.
func TestEditAccountSettingsLeaveOutPasswords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	plain, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.UseStore(StorePlain, ""); err != nil {
		t.Fatal(err)
	}
	stores := map[string]Store{"plain": plain, "memory": NewMemoryStore(time.Time{})}
	for name, st := range stores {
		if _, err := st.PutAccount(AccountConfig{ID: "w", Address: "ada@example.com",
			POP: ServerConfig{Host: "pop.example.com:995", Pass: "open sesame"}, Protocol: ProtoPOP3}); err != nil {
			t.Fatal(name, err)
		}
		cfg, err := accountSettings(st, "w")
		if err != nil {
			t.Fatal(name, err)
		}
		if cfg.IMAP.Pass != "" || cfg.POP.Pass != "" || cfg.SMTP.Pass != "" {
			t.Fatalf("%s: the settings to edit carry a password", name)
		}
		if !cfg.IsPOP3() || cfg.POP.Host != "pop.example.com:995" {
			t.Fatalf("%s: settings %+v", name, cfg)
		}
	}
	cfg, _ := accountSettings(plain, "w")
	cfg.Name = "Ada Lovelace"
	if _, err := plain.PutAccount(cfg); err != nil {
		t.Fatal(err)
	}
	file, _ := LoadConfig()
	if a := file.Accounts[0]; a.Name != "Ada Lovelace" || a.POP.Pass != "open sesame" || a.SMTP.Pass != "open sesame" {
		t.Fatal("the plain store lost the saved passwords")
	}
	// The demo's own accounts are edited from their guessed hosts.
	demo := NewDemoStore()
	if cfg, err := accountSettings(demo, demo.Accounts()[0].ID); err != nil || cfg.Incoming().Host == "" {
		t.Fatalf("demo settings %+v, %v", cfg, err)
	}
}
