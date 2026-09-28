package mailcore

import (
	"path/filepath"
	"strings"
	"testing"
)

// A save that leaves the password out keeps the saved one only for the
// server and user it was saved for. Pointing a server elsewhere needs its
// password again, so no request can send a saved password to a host of
// its choosing.
func TestSavedPasswordStaysWithItsServer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	base := AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993", User: "ada", Pass: "imap-secret"},
		SMTP: ServerConfig{Host: "smtp.example.com:465", User: "ada", Pass: "smtp-secret"}}
	if _, err := st.PutAccount(base); err != nil {
		t.Fatal(err)
	}
	stored := func() AccountConfig {
		cfg, _ := LoadConfig()
		for _, a := range cfg.Accounts {
			if a.ID == "home" {
				return a
			}
		}
		t.Fatal("account gone")
		return AccountConfig{}
	}

	// Same servers, another port, no passwords given: both kept.
	edit := base
	edit.IMAP.Host, edit.IMAP.Pass, edit.SMTP.Pass = "IMAP.example.com:143", "", ""
	if _, err := st.PutAccount(edit); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got.IMAP.Pass != "imap-secret" || got.SMTP.Pass != "smtp-secret" {
		t.Fatalf("a port change lost the passwords: %+v", got)
	}

	// The outgoing server pointed elsewhere, no password: refused.
	edit.SMTP.Host = "smtp.elsewhere.example:465"
	_, err = st.PutAccount(edit)
	if err == nil || !strings.Contains(err.Error(), "smtp.elsewhere.example") {
		t.Fatalf("err = %v", err)
	}
	if got := stored(); got.SMTP.Host != "smtp.example.com:465" || got.SMTP.Pass != "smtp-secret" {
		t.Fatalf("a refused save changed the account: %+v", got)
	}

	// Another user on the same server is another login too.
	edit.SMTP.Host, edit.IMAP.User = "smtp.example.com:465", "someone-else"
	if _, err := st.PutAccount(edit); err == nil {
		t.Fatal("a new user took the saved password")
	}

	// With the new password, the move is fine.
	edit.IMAP.User, edit.SMTP.Host, edit.SMTP.Pass = "ada", "smtp.elsewhere.example:465", "new-secret"
	if _, err := st.PutAccount(edit); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got.SMTP.Pass != "new-secret" || got.IMAP.Pass != "imap-secret" {
		t.Fatalf("after the move: %+v", got)
	}
}
