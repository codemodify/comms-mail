package mailcore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportKMail(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("emailidentities", `[Identity #0]
Name=Ada Lovelace
Email Address=ada@example.com
Signature=Ada Lovelace
Default Identity=true
`)
	write("mailtransports", `[General]
default-transport=3

[Transport 3]
name=smtp
host=smtp.example.com
port=587
encryption=TLS
user=ada@example.com
`)
	write("akonadi_imap_resource_0rc", `[network]
ImapServer=imap.example.com
ImapPort=993
Safety=SSL
UserName=ada@example.com
`)
	write("akonadi_pop3_resource_2rc", `[General]
host=pop.example.org
Port=995
login=bob
UseSSL=true
`)

	accs, err := ImportKMail()
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 2 {
		t.Fatalf("got %d accounts, want 2: %+v", len(accs), accs)
	}
	byAddr := map[string]AccountConfig{}
	for _, a := range accs {
		if a.Source != "KMail" {
			t.Errorf("source %q", a.Source)
		}
		byAddr[a.Account.Address] = a.Account
	}
	ada := byAddr["ada@example.com"]
	if ada.Protocol != "imap" || ada.IMAP.Host != "imap.example.com:993" || ada.IMAP.TLSMode != "ssl" {
		t.Fatalf("ada imap = %+v", ada.IMAP)
	}
	if ada.SMTP.Host != "smtp.example.com:587" || ada.SMTP.TLSMode != "starttls" {
		t.Fatalf("ada smtp = %+v", ada.SMTP)
	}
	if ada.Name != "Ada Lovelace" || len(ada.Identities) != 1 {
		t.Fatalf("ada name/identity = %q %+v", ada.Name, ada.Identities)
	}
	if ada.IMAP.Pass != "" {
		t.Fatal("password must be empty")
	}
	bob := byAddr["bob"]
	if bob.Protocol != "pop3" || bob.POP.Host != "pop.example.org:995" || bob.POP.TLSMode != "ssl" {
		t.Fatalf("bob pop = %+v", bob.POP)
	}
}
