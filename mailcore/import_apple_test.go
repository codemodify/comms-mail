package mailcore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// execSQLite creates path and runs stmts in it.
func execSQLite(t *testing.T, path string, stmts ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
}

// Contacts' database in the AddressBook folder and in an account's
// Sources/ copy (the schema of macOS 15, trimmed to what is read).
func TestReadAppleContacts(t *testing.T) {
	home := fakeHome(t)
	schema := []string{
		`CREATE TABLE ZABCDRECORD (Z_PK INTEGER PRIMARY KEY, Z_ENT INTEGER, ZFIRSTNAME VARCHAR, ZMIDDLENAME VARCHAR, ZLASTNAME VARCHAR,
			ZNICKNAME VARCHAR, ZORGANIZATION VARCHAR, ZDISPLAYFLAGS INTEGER)`,
		`CREATE TABLE ZABCDEMAILADDRESS (Z_PK INTEGER PRIMARY KEY, Z_ENT INTEGER, ZOWNER INTEGER, ZADDRESS VARCHAR,
			ZADDRESSNORMALIZED VARCHAR, ZLABEL VARCHAR, ZISPRIMARY INTEGER, ZORDERINGINDEX INTEGER)`,
	}
	root := filepath.Join(home, "Library", "Application Support", "AddressBook")
	execSQLite(t, filepath.Join(root, "AddressBook-v22.abcddb"), append(schema,
		`INSERT INTO ZABCDRECORD VALUES (1, 22, 'Grace', 'B.', 'Hopper', NULL, NULL, 0)`,
		`INSERT INTO ZABCDEMAILADDRESS VALUES (1, 5, 1, 'grace@example.org', 'grace@example.org', '_$!<Work>!$_', 1, 0)`)...)
	execSQLite(t, filepath.Join(root, "Sources", "4F1C-UUID", "AddressBook-v22.abcddb"), append(schema,
		`INSERT INTO ZABCDRECORD VALUES (1, 22, 'Alice', NULL, 'Example', NULL, 'Example Corp', 0), (2, 22, NULL, NULL, NULL, NULL, 'Acme Example Ltd', 1),
			(3, 22, NULL, NULL, NULL, 'Kay', NULL, 0)`,
		`INSERT INTO ZABCDEMAILADDRESS VALUES (1, 5, 1, 'Alice@Example.com', 'alice@example.com', NULL, 1, 0),
			(2, 5, 2, 'info@example.com', 'info@example.com', NULL, 0, 0), (3, 5, 3, 'kay@example.net', NULL, NULL, 0, 0),
			(4, 5, 1, 'not an address', NULL, NULL, 0, 1)`)...)

	got := map[string]string{}
	for _, c := range readAppleContacts() {
		got[c.Address] = c.Name
	}
	want := map[string]string{
		"grace@example.org": "Grace B. Hopper",
		"Alice@Example.com": "Alice Example",    // a person, though with a company
		"info@example.com":  "Acme Example Ltd", // shown as a company
		"kay@example.net":   "Kay",              // only a nickname
	}
	if len(got) != len(want) {
		t.Fatalf("contacts %v", got)
	}
	for a, n := range want {
		if got[a] != n {
			t.Errorf("%s: %q, want %q", a, got[a], n)
		}
	}
	if src := importAppleMail(); len(src.Contacts) != 4 {
		t.Fatalf("the Apple Mail source has %d contacts", len(src.Contacts))
	}
}

// Internet Accounts (Accounts4.sqlite): an IMAP account with its SMTP
// server, and a POP one, their settings archived as NSKeyedArchiver does.
func TestReadAppleAccounts4(t *testing.T) {
	home := fakeHome(t)
	arch := map[string]string{
		"imapHost": bplistArchStr, "true": bplistArchTrue, "port993": bplistArchNum, "port587": bplistArchNumStr,
		"aliases":  bplistArchAliases,
		"smtpHost": "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGxfEBBzbXRwLmV4YW1wbGUuY29t0QoLVHJvb3SAARIAAYagCBEbJCkyREdNYGNoagAAAAAAAAEBAAAAAAAAAA0AAAAAAAAAAAAAAAAAAABv",
		"smtpID":   "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGxWU01UUC0x0QoLVHJvb3SAARIAAYagCBEbJCkyREdNVFdcXgAAAAAAAAEBAAAAAAAAAA0AAAAAAAAAAAAAAAAAAABj",
		"fullName": "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGxcQWRhIExvdmVsYWNl0QoLVHJvb3SAARIAAYagCBEbJCkyREdNWl1iZAAAAAAAAAEBAAAAAAAAAA0AAAAAAAAAAAAAAAAAAABp",
		"false":    "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGwI0QoLVHJvb3SAARIAAYagCBEbJCkyREdNTlFWWAAAAAAAAAEBAAAAAAAAAA0AAAAAAAAAAAAAAAAAAABd",
		"popHost":  "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGxfEA9wb3AuZXhhbXBsZS5uZXTRCgtUcm9vdIABEgABhqAIERskKTJER01fYmdpAAAAAAAAAQEAAAAAAAAADQAAAAAAAAAAAAAAAAAAAG4=",
	}
	blob := func(k string) string { return "X'" + hexOf(mustB64(t, arch[k])) + "'" }
	path := filepath.Join(home, "Library", "Accounts", "Accounts4.sqlite")
	execSQLite(t, path,
		`CREATE TABLE ZACCOUNTTYPE (Z_PK INTEGER PRIMARY KEY, ZIDENTIFIER VARCHAR, ZACCOUNTTYPEDESCRIPTION VARCHAR)`,
		`CREATE TABLE ZACCOUNT (Z_PK INTEGER PRIMARY KEY, ZACCOUNTTYPE INTEGER, ZPARENTACCOUNT INTEGER, ZIDENTIFIER VARCHAR,
			ZUSERNAME VARCHAR, ZACCOUNTDESCRIPTION VARCHAR)`,
		`CREATE TABLE ZACCOUNTPROPERTY (Z_PK INTEGER PRIMARY KEY, ZOWNER INTEGER, ZKEY VARCHAR, ZVALUE BLOB)`,
		`INSERT INTO ZACCOUNTTYPE VALUES (1, 'com.apple.account.IMAP', 'IMAP'), (2, 'com.apple.account.SMTP', 'SMTP'),
			(3, 'com.apple.account.POP', 'POP'), (4, 'com.apple.account.CalDAV', 'CalDAV')`,
		`INSERT INTO ZACCOUNT VALUES (1, 1, NULL, 'IMAP-1', 'ada', 'Work'), (2, 2, NULL, 'SMTP-1', 'ada', NULL),
			(3, 3, NULL, 'POP-1', 'ada.pop@example.net', NULL), (4, 4, NULL, 'CAL-1', 'ada', 'Calendar')`,
		`INSERT INTO ZACCOUNTPROPERTY (ZOWNER, ZKEY, ZVALUE) VALUES
			(1, 'Hostname', `+blob("imapHost")+`), (1, 'PortNumber', `+blob("port993")+`), (1, 'SSLEnabled', `+blob("true")+`),
			(1, 'EmailAliases', `+blob("aliases")+`), (1, 'ACPropertyFullName', `+blob("fullName")+`),
			(1, 'SendingAccountIdentifier', `+blob("smtpID")+`),
			(2, 'Hostname', `+blob("smtpHost")+`), (2, 'PortNumber', `+blob("port587")+`), (2, 'SSLEnabled', `+blob("false")+`),
			(3, 'Hostname', `+blob("popHost")+`), (3, 'SSLEnabled', `+blob("true")+`),
			(4, 'Hostname', `+blob("imapHost")+`)`,
	)
	got := appleAccounts4(path)
	if len(got) != 2 {
		t.Fatalf("accounts %+v", got)
	}
	imap, pop := got[0].Account, got[1].Account
	if imap.Protocol != ProtoIMAP || imap.Name != "Work" || imap.Address != "ada@example.com" ||
		imap.IMAP != (ServerConfig{Host: "imap.example.com:993", User: "ada", TLSMode: string(TLSImplicit)}) ||
		imap.SMTP != (ServerConfig{Host: "smtp.example.com:587", User: "ada", TLSMode: string(TLSStartTLS)}) ||
		len(imap.Identities) != 1 || imap.Identities[0].Name != "Ada Lovelace" {
		t.Fatalf("imap %+v", imap)
	}
	if pop.Protocol != ProtoPOP3 || pop.Address != "ada.pop@example.net" || pop.POP.Host != "pop.example.net" || pop.POP.TLSMode != string(TLSImplicit) {
		t.Fatalf("pop %+v", pop)
	}
	// The import finds them when there is no older Accounts.plist.
	if err := os.MkdirAll(filepath.Join(home, "Library", "Mail", "V10"), 0o700); err != nil {
		t.Fatal(err)
	}
	if src := importAppleMail(); len(src.Accounts) != 2 || src.Note == "" {
		t.Fatalf("source %+v", src)
	}
}

func hexOf(b []byte) string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, 0, 2*len(b))
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}
