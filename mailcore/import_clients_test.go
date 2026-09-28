package mailcore

import (
	"path/filepath"
	"testing"
)

// Fixtures for the other clients' settings, written the way each client
// writes them (see the formats documented in its source). Every test runs
// in a home directory of its own.

func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	return home
}

func onlyAccount(t *testing.T, got []ImportedAccount, address string) AccountConfig {
	t.Helper()
	for _, a := range got {
		if a.Account.Address == address {
			return a.Account
		}
	}
	t.Fatalf("no account %s in %+v", address, got)
	return AccountConfig{}
}

func wantServer(t *testing.T, what string, got ServerConfig, host, user, tls string) {
	t.Helper()
	if got.Host != host || got.User != user || got.TLSMode != tls {
		t.Fatalf("%s: %+v, want host %s user %s tls %s", what, got, host, user, tls)
	}
}

// Evolution splits an account over three ESource key files — account,
// identity, transport — linked by IdentityUid and TransportUid.
func TestImportEvolutionSources(t *testing.T) {
	home := fakeHome(t)
	src := filepath.Join(home, ".config", "evolution", "sources")
	mustWrite(t, filepath.Join(src, "acct-1.source"), `[Data Source]
DisplayName=Work
Enabled=true
Parent=

[Authentication]
Host=imap.example.com
Method=none
Port=993
User=alice@example.com

[Security]
Method=ssl-on-alternate-port

[Mail Account]
BackendName=imapx
IdentityUid=ident-1

[Imapx Backend]
UseIdle=true
`)
	mustWrite(t, filepath.Join(src, "ident-1.source"), `[Data Source]
DisplayName=Work
Enabled=true
Parent=acct-1

[Mail Identity]
Address=alice@example.com
Name=Alice Example
SignatureUid=sig-1

[Mail Submission]
TransportUid=trans-1
`)
	mustWrite(t, filepath.Join(src, "trans-1.source"), `[Data Source]
DisplayName=Work
Parent=acct-1

[Authentication]
Host=smtp.example.com
Method=PLAIN
Port=587
User=alice@example.com

[Security]
Method=starttls-on-standard-port

[Mail Transport]
BackendName=smtp
`)
	mustWrite(t, filepath.Join(src, "sig-1.source"), "[Data Source]\nDisplayName=Sig\n\n[Mail Signature]\nMimeType=text/plain\n")
	mustWrite(t, filepath.Join(home, ".config", "evolution", "signatures", "sig-1"), "Alice\nExample Corp\n")
	// A POP account with the generic "tls" security, and a GNOME Online
	// Accounts one with no host (it signs in there).
	mustWrite(t, filepath.Join(src, "acct-2.source"), "[Data Source]\nDisplayName=Home\n\n[Authentication]\nHost=pop.example.org\nPort=995\nUser=bob\n\n[Security]\nMethod=tls\n\n[Mail Account]\nBackendName=pop\nIdentityUid=ident-2\n\n[Pop Backend]\nKeepOnServer=true\n")
	mustWrite(t, filepath.Join(src, "ident-2.source"), "[Data Source]\nParent=acct-2\n\n[Mail Identity]\nAddress=bob@example.org\nName=Bob\n")
	mustWrite(t, filepath.Join(src, "goa-1.source"), "[Data Source]\nDisplayName=Google\n\n[Mail Account]\nBackendName=imapx\nIdentityUid=ident-3\n")

	got := importEvolution()
	if len(got) != 2 {
		t.Fatalf("accounts %+v", got)
	}
	a := onlyAccount(t, got, "alice@example.com")
	if a.Protocol != ProtoIMAP || a.Name != "Alice Example" {
		t.Fatalf("account %+v", a)
	}
	wantServer(t, "imap", a.IMAP, "imap.example.com:993", "alice@example.com", string(TLSImplicit))
	wantServer(t, "smtp", a.SMTP, "smtp.example.com:587", "alice@example.com", string(TLSStartTLS))
	if len(a.Identities) != 1 || a.Identities[0].Signature != "Alice\nExample Corp" {
		t.Fatalf("identity %+v", a.Identities)
	}
	b := onlyAccount(t, got, "bob@example.org")
	if b.Protocol != ProtoPOP3 {
		t.Fatalf("pop account %+v", b)
	}
	wantServer(t, "pop", b.POP, "pop.example.org:995", "bob", string(TLSImplicit))
}

// Claws Mail's accountrc: protocol 1 is IMAP and 0 POP3 today (before
// config_version 1, 3 was IMAP); ports count only with set_*port=1.
func TestImportClawsAccountrc(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".claws-mail")
	mustWrite(t, filepath.Join(home, ".signature"), "-- \nAlice\n")
	mustWrite(t, filepath.Join(dir, "accountrc"), `[Account: 1]
account_name=Work
is_default=1
name=Alice Example
address=alice@example.com
protocol=1
receive_server=imap.example.com
smtp_server=smtp.example.com
user_id=alice@example.com
config_version=5
use_smtp_auth=1
smtp_user_id=
signature_type=0
signature_path=.signature
ssl_imap=1
ssl_smtp=2
set_smtpport=1
smtp_port=587
set_imapport=0
imap_port=143

[Account: 2]
account_name=Old home
name=Bob
address=bob@example.org
protocol=3
receive_server=mail.example.org
smtp_server=mail.example.org
user_id=bob
config_version=0
ssl_imap=0
ssl_smtp=0

[Account: 3]
account_name=News
address=carol@example.net
protocol=2
nntp_server=news.example.net
receive_server=news.example.net
config_version=5
`)
	got := importClaws()
	if len(got) != 2 {
		t.Fatalf("accounts %+v", got)
	}
	a := onlyAccount(t, got, "alice@example.com")
	if a.Protocol != ProtoIMAP || a.Name != "Work" {
		t.Fatalf("account %+v", a)
	}
	wantServer(t, "imap", a.IMAP, "imap.example.com:993", "alice@example.com", string(TLSImplicit))
	wantServer(t, "smtp", a.SMTP, "smtp.example.com:587", "alice@example.com", string(TLSStartTLS))
	if a.Identities[0].Signature != "Alice" && a.Identities[0].Signature != "-- \nAlice" {
		t.Fatalf("signature %q", a.Identities[0].Signature)
	}
	b := onlyAccount(t, got, "bob@example.org")
	if b.Protocol != ProtoIMAP {
		t.Fatalf("a config_version 0 protocol=3 is IMAP: %+v", b)
	}
	wantServer(t, "old imap", b.IMAP, "mail.example.org:143", "bob", string(TLSPlain))
	wantServer(t, "old smtp", b.SMTP, "mail.example.org:25", "bob", string(TLSPlain))
}

// Geary's geary.ini: hosts only for service_provider=other; Gmail signs
// in with Google; SMTP "use-incoming" takes the IMAP login.
func TestImportGearyINI(t *testing.T) {
	home := fakeHome(t)
	cfg := filepath.Join(home, ".config", "geary")
	mustWrite(t, filepath.Join(cfg, "account_01", "geary.ini"), `[Metadata]
version=1
status=enabled

[Account]
ordinal=1
label=Work
use_signature=true
signature=Alice\nExample Corp
sender_mailboxes=Alice Example <alice@example.com>;alice.alias@example.com;
service_provider=other

[Incoming]
login=alice@example.com
remember_password=true
host=imap.example.com
transport_security=transport
credentials=custom

[Outgoing]
remember_password=true
host=smtp.example.com
port=587
transport_security=start-tls
credentials=use-incoming
`)
	mustWrite(t, filepath.Join(cfg, "account_02", "geary.ini"), "[Metadata]\nversion=1\nstatus=enabled\n\n[Account]\nsender_mailboxes=Bob <bob@gmail.com>;\nservice_provider=gmail\n\n[Incoming]\nlogin=bob@gmail.com\n\n[Outgoing]\n")
	mustWrite(t, filepath.Join(cfg, "account_03", "geary.ini"), "[Metadata]\nversion=1\nstatus=removed\n\n[Account]\nsender_mailboxes=gone@example.com;\nservice_provider=other\n\n[Incoming]\nhost=imap.example.com\n")
	// The oldest layout, in the data directory.
	mustWrite(t, filepath.Join(home, ".local", "share", "geary", "carol@example.net", "geary.ini"), `[AccountInformation]
real_name=Carol
primary_email=carol@example.net
service_provider=other
imap_host=imap.example.net
imap_port=143
imap_starttls=true
imap_username=carol
smtp_host=smtp.example.net
smtp_port=465
smtp_ssl=true
smtp_use_imap_credentials=true
`)
	got := importGeary()
	if len(got) != 3 {
		t.Fatalf("accounts %+v", got)
	}
	a := onlyAccount(t, got, "alice@example.com")
	if a.Name != "Alice Example" || a.Identities[0].Signature != "Alice\nExample Corp" {
		t.Fatalf("account %+v", a)
	}
	wantServer(t, "imap", a.IMAP, "imap.example.com:993", "alice@example.com", string(TLSImplicit))
	wantServer(t, "smtp", a.SMTP, "smtp.example.com:587", "alice@example.com", string(TLSStartTLS))
	g := onlyAccount(t, got, "bob@gmail.com")
	if g.Provider != "google" || g.IMAP.Host != "imap.gmail.com:993" || g.IMAP.Auth != "xoauth2" {
		t.Fatalf("gmail %+v", g)
	}
	c := onlyAccount(t, got, "carol@example.net")
	wantServer(t, "old imap", c.IMAP, "imap.example.net:143", "carol", string(TLSStartTLS))
	wantServer(t, "old smtp", c.SMTP, "smtp.example.net:465", "carol", string(TLSImplicit))
}

// mutt's muttrc and NeoMutt's neomuttrc (real_name / spool_file, its new
// names), found where each looks first.
func TestImportMuttAndNeoMutt(t *testing.T) {
	home := fakeHome(t)
	mustWrite(t, filepath.Join(home, ".muttrc"), `# mutt
set realname = "Alice Example"
set from = "alice@example.com"
set imap_user = "alice@example.com"
set folder = "imaps://imap.example.com:993/"
set spoolfile = "+INBOX"
set record = "+Sent"
set smtp_url = "smtp://alice@example.com@smtp.example.com:587/"
set signature = "~/.signature"
mailboxes imaps://imap.example.com/INBOX
`)
	mustWrite(t, filepath.Join(home, ".signature"), "Alice\n")
	mustWrite(t, filepath.Join(home, ".config", "neomutt", "neomuttrc"), `set real_name = "Bob"
set from = "Bob <bob@example.org>"
set folder = "~/Maildir"
set spool_file = "+INBOX"
set smtp_url = "smtps://bob@smtp.example.org"
source ~/.config/neomutt/extra
`)
	mustWrite(t, filepath.Join(home, ".config", "neomutt", "extra"), "mailboxes +Lists\n")
	for _, box := range []string{"INBOX", "Lists"} {
		mustWrite(t, filepath.Join(home, "Maildir", box, "cur", "1.host:2,S"), msg(box+"@ex", box))
		mustWrite(t, filepath.Join(home, "Maildir", box, "new", ".keep"), "")
	}

	got := importMuttSources()
	if len(got) != 2 {
		t.Fatalf("sources %+v", got)
	}
	m, n := got[0], got[1]
	if m.Source != "mutt" || n.Source != "NeoMutt" {
		t.Fatalf("sources %q %q", m.Source, n.Source)
	}
	a := onlyAccount(t, m.Accounts, "alice@example.com")
	if a.Name != "Alice Example" || a.Identities[0].Signature != "Alice" {
		t.Fatalf("mutt account %+v", a)
	}
	wantServer(t, "mutt imap", a.IMAP, "imap.example.com:993", "alice@example.com", string(TLSImplicit))
	wantServer(t, "mutt smtp", a.SMTP, "smtp.example.com:587", "alice@example.com", string(TLSStartTLS))
	if len(m.Mail) != 0 {
		t.Fatalf("+INBOX on an IMAP folder is not local mail: %+v", m.Mail)
	}
	if len(n.Accounts) != 0 {
		t.Fatalf("a local-only NeoMutt has no server account: %+v", n.Accounts)
	}
	names := map[string]bool{}
	for _, st := range n.Mail {
		names[filepath.Base(st.Path)] = true
	}
	if !names["INBOX"] || !names["Lists"] {
		t.Fatalf("NeoMutt mailboxes %+v", n.Mail)
	}

	// When both would read the same file, it is one source.
	home2 := fakeHome(t)
	mustWrite(t, filepath.Join(home2, ".muttrc"), "set from = \"x@example.com\"\nset folder = \"imap://mail.example.com/\"\n")
	got = importMuttSources()
	if len(got) != 1 || got[0].Source != "mutt / NeoMutt" {
		t.Fatalf("shared muttrc: %+v", got)
	}
	x := onlyAccount(t, got[0].Accounts, "x@example.com")
	wantServer(t, "imap://", x.IMAP, "mail.example.com:143", "", string(TLSStartTLS))
}

// Apple Mail's MailData/Accounts.plist (OS X 10.7–10.10): MailAccounts,
// joined to DeliveryAccounts by SMTPIdentifier "host:user".
func TestImportAppleAccountsPlist(t *testing.T) {
	home := fakeHome(t)
	mustWrite(t, filepath.Join(home, "Library", "Mail", "V2", "MailData", "Accounts.plist"), `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>DeliveryAccounts</key>
  <array>
    <dict>
      <key>AccountType</key><string>SMTPAccount</string>
      <key>Hostname</key><string>smtp.other.example</string>
      <key>Username</key><string>someone</string>
    </dict>
    <dict>
      <key>AccountType</key><string>SMTPAccount</string>
      <key>Hostname</key><string>smtp.example.com</string>
      <key>PortNumber</key><string>587</string>
      <key>SSLEnabled</key><string>NO</string>
      <key>Username</key><string>alice@example.com</string>
    </dict>
  </array>
  <key>MailAccounts</key>
  <array>
    <dict>
      <key>AccountName</key><string>Work</string>
      <key>AccountType</key><string>IMAPAccount</string>
      <key>EmailAddresses</key><array><string>alice@example.com</string></array>
      <key>FullUserName</key><string>Alice Example</string>
      <key>Hostname</key><string>imap.example.com</string>
      <key>PortNumber</key><integer>993</integer>
      <key>SSLEnabled</key><string>YES</string>
      <key>SMTPIdentifier</key><string>smtp.example.com:alice@example.com</string>
      <key>Username</key><string>alice@example.com</string>
    </dict>
    <dict>
      <key>AccountType</key><string>LocalAccount</string>
      <key>AccountName</key><string>On My Mac</string>
    </dict>
  </array>
</dict>
</plist>
`)
	src := importAppleMail()
	if len(src.Accounts) != 1 || src.Note != "" {
		t.Fatalf("apple %+v", src)
	}
	a := onlyAccount(t, src.Accounts, "alice@example.com")
	if a.Name != "Work" || a.Identities[0].Name != "Alice Example" {
		t.Fatalf("account %+v", a)
	}
	wantServer(t, "imap", a.IMAP, "imap.example.com:993", "alice@example.com", string(TLSImplicit))
	wantServer(t, "smtp", a.SMTP, "smtp.example.com:587", "alice@example.com", string(TLSStartTLS))
}
