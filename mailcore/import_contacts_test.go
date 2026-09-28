package mailcore

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func contactsString(cs []Contact) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.Name+" <"+c.Address+">")
	}
	return strings.Join(parts, "|")
}

func makeSQLite(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestParseVCards(t *testing.T) {
	got := parseVCards("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Alice Ex\r\n ample\r\nEMAIL;TYPE=work:alice@example.com\r\nitem1.EMAIL:a2@example.com\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\nN:Bond;James;;;\nEMAIL:jb@example.org\nEND:VCARD\nBEGIN:VCARD\nFN:No Mail\nEND:VCARD\n")
	if s := contactsString(got); s != "Alice Example <alice@example.com>|Alice Example <a2@example.com>|James Bond <jb@example.org>" {
		t.Fatalf("vcards %s", s)
	}
}

// Thunderbird's address books (abook.sqlite, and history.sqlite for
// collected addresses): the properties table, a row per card and field.
func TestReadThunderbirdAddressBooks(t *testing.T) {
	home := fakeHome(t)
	prof := filepath.Join(home, ".thunderbird", "abc.default")
	mustWrite(t, filepath.Join(prof, "prefs.js"), "")
	schema := "CREATE TABLE properties (card TEXT, name TEXT, value TEXT)"
	makeSQLite(t, filepath.Join(prof, "abook.sqlite"), schema,
		`INSERT INTO properties VALUES ('c1','DisplayName','Alice Example'),('c1','PrimaryEmail','alice@example.com'),('c1','SecondEmail','alice@home.example'),
		 ('c2','FirstName','Bob'),('c2','LastName','Stone'),('c2','PrimaryEmail','bob@example.org'),('c3','DisplayName','No Address')`)
	makeSQLite(t, filepath.Join(prof, "history.sqlite"), schema,
		`INSERT INTO properties VALUES ('h1','PrimaryEmail','ALICE@example.com'),('h2','PrimaryEmail','carol@example.net')`)
	got := readThunderbirdAddressBooks()
	if s := contactsString(got); s != "Alice Example <alice@example.com>|Alice Example <alice@home.example>|Bob Stone <bob@example.org>| <carol@example.net>" {
		t.Fatalf("thunderbird %s", s)
	}
}

func TestReadEvolutionClawsAndMuttContacts(t *testing.T) {
	home := fakeHome(t)
	makeSQLite(t, filepath.Join(mkdirs(t, home, ".local/share/evolution/addressbook/system"), "contacts.db"),
		"CREATE TABLE folder_id (uid TEXT, vcard TEXT)",
		"INSERT INTO folder_id VALUES ('1', 'BEGIN:VCARD\nFN:Eve\nEMAIL:eve@example.com\nEND:VCARD')")
	if s := contactsString(readEvolutionAddressBooks()); s != "Eve <eve@example.com>" {
		t.Fatalf("evolution %s", s)
	}
	mustWrite(t, filepath.Join(home, ".claws-mail", "accountrc"), "")
	mustWrite(t, filepath.Join(home, ".claws-mail", "addrbook", "addrbook-000001.xml"), `<?xml version="1.0" encoding="UTF-8" ?>
<address-book name="Personal">
  <person uid="1" first-name="Carl" last-name="Claws" nick-name="" cn="Carl Claws">
    <address-list><address uid="2" alias="" email="carl@example.com" remarks="" /></address-list>
  </person>
</address-book>`)
	if s := contactsString(readClawsAddressBook()); s != "Carl Claws <carl@example.com>" {
		t.Fatalf("claws %s", s)
	}
	rc := filepath.Join(home, ".muttrc")
	mustWrite(t, rc, "alias dan Dan Mutt <dan@example.com>\nset alias_file = ~/.mutt/aliases\n")
	mustWrite(t, filepath.Join(home, ".mutt", "aliases"), "alias team a@example.com, \"Bee\" <b@example.com>\n")
	if s := contactsString(readMuttAliases(rc)); s != " <a@example.com>|Bee <b@example.com>|Dan Mutt <dan@example.com>" {
		t.Fatalf("mutt %s", s)
	}
}

// Imported contacts join the suggestions, a name filling in where the mail
// gave none, and stay after a restart.
func TestImportedContactsSuggestAndPersist(t *testing.T) {
	dir := t.TempDir()
	st := newTestLocalStore(t, dir)
	st.feat.setContacts([]Contact{{Address: "alice@example.com", Count: 3}})
	n := st.ImportContacts([]Contact{{Name: "Alice Example", Address: "ALICE@example.com"}, {Name: "Zed", Address: "zed@example.com"}, {Address: "not-an-address"}})
	if n != 2 {
		t.Fatalf("new contacts %d", n)
	}
	got := st.SuggestContacts("zed", 5)
	if len(got) != 1 || got[0].Name != "Zed" {
		t.Fatalf("suggest zed %+v", got)
	}
	if got := st.SuggestContacts("alice", 5); len(got) != 1 || got[0].Name != "Alice Example" || got[0].Count != 3 {
		t.Fatalf("suggest alice %+v", got)
	}
	again := newTestLocalStore(t, dir)
	if got := again.SuggestContacts("zed", 5); len(got) != 1 {
		t.Fatalf("after restart %+v", got)
	}
	if st.ImportContacts([]Contact{{Address: "zed@example.com"}}) != 0 {
		t.Fatal("a known contact counted as new")
	}
}

func mkdirs(t *testing.T, base, rel string) string {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	mustWrite(t, filepath.Join(p, ".keep"), "")
	return p
}
