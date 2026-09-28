package mailcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const samplePrefsJS = `
# comment
user_pref("mail.accountmanager.accounts", "account2,account1");
user_pref("mail.accountmanager.defaultaccount", "account1");

user_pref("mail.account.account1.server", "server1");
user_pref("mail.account.account1.identities", "id1");
user_pref("mail.server.server1.hostname", "imap.gmail.com");
user_pref("mail.server.server1.port", 993);
user_pref("mail.server.server1.type", "imap");
user_pref("mail.server.server1.userName", "ada@gmail.com");
user_pref("mail.server.server1.socketType", 3);

user_pref("mail.identity.id1.fullName", "Ada Lovelace");
user_pref("mail.identity.id1.useremail", "ada@gmail.com");
user_pref("mail.identity.id1.smtpServer", "smtp1");
user_pref("mail.identity.id1.htmlSigText", "Ada Lovelace\nMathematician");

user_pref("mail.smtpserver.smtp1.hostname", "smtp.gmail.com");
user_pref("mail.smtpserver.smtp1.port", 587);
user_pref("mail.smtpserver.smtp1.username", "ada@gmail.com");
user_pref("mail.smtpserver.smtp1.try_ssl", 2);

user_pref("mail.account.account2.server", "server2");
user_pref("mail.account.account2.identities", "id2");
user_pref("mail.server.server2.hostname", "pop.example.org");
user_pref("mail.server.server2.port", 995);
user_pref("mail.server.server2.type", "pop3");
user_pref("mail.server.server2.userName", "bob");
user_pref("mail.server.server2.socketType", 3);
user_pref("mail.identity.id2.useremail", "bob@example.org");
user_pref("mail.identity.id2.fullName", "Bob");

# Local Folders — must be skipped
user_pref("mail.account.account9.server", "server9");
user_pref("mail.server.server9.type", "none");
user_pref("mail.accountmanager.localfoldersserver", "server9");
`

func TestImportThunderbird(t *testing.T) {
	accs := importThunderbird(parseThunderbirdPrefs(strings.NewReader(samplePrefsJS)))
	if len(accs) != 2 {
		t.Fatalf("got %d accounts, want 2 (Local Folders skipped): %+v", len(accs), accs)
	}
	byAddr := map[string]AccountConfig{}
	for _, a := range accs {
		if a.Source != "Thunderbird" {
			t.Errorf("source = %q", a.Source)
		}
		byAddr[a.Account.Address] = a.Account
	}

	gmail := byAddr["ada@gmail.com"]
	if gmail.Protocol != "imap" || gmail.IMAP.Host != "imap.gmail.com:993" || gmail.IMAP.TLSMode != "ssl" {
		t.Fatalf("gmail incoming = %+v", gmail.IMAP)
	}
	if gmail.IMAP.User != "ada@gmail.com" || gmail.IMAP.Pass != "" {
		t.Fatalf("gmail user/pass = %q/%q (password must be empty)", gmail.IMAP.User, gmail.IMAP.Pass)
	}
	if gmail.SMTP.Host != "smtp.gmail.com:587" || gmail.SMTP.TLSMode != "starttls" {
		t.Fatalf("gmail smtp = %+v", gmail.SMTP)
	}
	if len(gmail.Identities) != 1 || gmail.Identities[0].Name != "Ada Lovelace" {
		t.Fatalf("gmail identity = %+v", gmail.Identities)
	}
	if !strings.Contains(gmail.Identities[0].Signature, "Mathematician") {
		t.Fatalf("gmail signature = %q", gmail.Identities[0].Signature)
	}
	if !gmail.Identities[0].Default {
		t.Fatal("first identity should be the default")
	}

	pop := byAddr["bob@example.org"]
	if pop.Protocol != "pop3" || pop.POP.Host != "pop.example.org:995" || pop.POP.User != "bob" {
		t.Fatalf("pop = %+v", pop.POP)
	}
	if pop.Name != "Bob" {
		t.Fatalf("pop name = %q", pop.Name)
	}
}

func TestParseThunderbirdPrefsQuoting(t *testing.T) {
	p := parseThunderbirdPrefs(strings.NewReader(
		`user_pref("a.b", "he said \"hi\"");` + "\n" +
			`user_pref("n", 42);` + "\n" +
			`user_pref("flag", true);` + "\n"))
	if p["a.b"] != `he said "hi"` {
		t.Fatalf("escaped quote = %q", p["a.b"])
	}
	if p["n"] != "42" || p["flag"] != "true" {
		t.Fatalf("n=%q flag=%q", p["n"], p["flag"])
	}
}

func TestImportThunderbirdDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prof := filepath.Join(home, ".thunderbird", "abc.default")
	if err := os.MkdirAll(prof, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prof, "prefs.js"), []byte(samplePrefsJS), 0o600); err != nil {
		t.Fatal(err)
	}
	accs, err := ImportThunderbird()
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 2 {
		t.Fatalf("discovered %d accounts, want 2", len(accs))
	}
}
