package mailcore

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A KMail config dir: two IMAP accounts and a POP one, the filters as
// mailcommon writes them (KConfig escapes included), and Akonadi on SQLite.
func kmailFilterFixture(t *testing.T, driver string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, text string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("akonadi_imap_resource_0rc", "[network]\nImapServer=imap.example.com\nImapPort=993\nUserName=ada\nSafety=SSL\n")
	write("akonadi_imap_resource_1rc", "[network]\nImapServer=imap.other.example\nUserName=bob\n")
	write("akonadi_pop3_resource_0rc", "[General]\nhost=pop.example.net\nlogin=ada.pop\n")
	dbPath := filepath.Join(dir, "data", "akonadi.db")
	write("akonadi/akonadiserverrc", "[%General]\nDriver="+driver+"\n\n[QSQLITE]\nName="+dbPath+"\n")
	write("akonadi_mailfilter_agentrc", `[General]
filters=7

[Filter #0]
Applicability=2
accounts-set=akonadi_imap_resource_0
apply-on=check-mail,manual-filtering
name=Example list
operator=and
rules=1
fieldA=List-Id
funcA=contains
contentsA=<dev.lists.example.com>
actions=1
action-name-0=transfer
action-args-0=12

[Filter #1]
Applicability=2
accounts-set=akonadi_imap_resource_0
Enabled=true
StopProcessingHere=false
name=Invoices
operator=and
rules=2
fieldA=Subject
funcA=contains
contentsA=Invoice
fieldB=From
funcB=not-equal
contentsB=noreply@example.com
actions=3
action-name-0=set status
action-args-0=R
action-name-1=add tag
action-args-1=akonadi:?tag=3
action-name-2=transfer
action-args-2=12

[Filter #2]
Applicability=2
accounts-set=akonadi_imap_resource_0
name=To Bob
operator=and
rules=1
fieldA=From
funcA=contains
contentsA=bob
actions=1
action-name-0=transfer
action-args-0=21

[Filter #3]
Applicability=0
apply-on=manual-filtering
name=By hand
operator=all
rules=0
actions=1
action-name-0=delete

[Filter #4]
name=Spaced
operator=and
rules=1
fieldA=<recipients>
funcA=contains
contentsA=\sboard
actions=1
action-name-0=delete

[Filter #5]
Applicability=0
name=Everywhere
operator=or
rules=1
fieldA=Subject
funcA=start-with
contentsA=[x]
actions=1
action-name-0=set status
action-args-0=G

[Filter #6]
Applicability=2
accounts-set=akonadi_imap_resource_0
name=Local
operator=and
rules=1
fieldA=Subject
funcA=contains
contentsA=old
actions=1
action-name-0=transfer
action-args-0=31
`)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE ResourceTable (id INTEGER PRIMARY KEY, name TEXT, isVirtual INTEGER)`,
		`CREATE TABLE CollectionTable (id INTEGER PRIMARY KEY, remoteId BLOB, name TEXT, parentId INTEGER, resourceId INTEGER, enabled INTEGER, isVirtual INTEGER)`,
		`CREATE TABLE TagTable (id INTEGER PRIMARY KEY, gid BLOB)`,
		`CREATE TABLE TagAttributeTable (id INTEGER PRIMARY KEY, tagId INTEGER, type BLOB, value BLOB)`,
		`INSERT INTO ResourceTable VALUES (1, 'akonadi_imap_resource_0', 0), (2, 'akonadi_imap_resource_1', 0), (3, 'akonadi_maildir_resource_0', 0)`,
		`INSERT INTO CollectionTable VALUES
			(10, 'imap://ada@imap.example.com/', 'ada@imap.example.com', NULL, 1, 1, 0),
			(11, '/INBOX', 'Inbox', 10, 1, 1, 0),
			(12, '/Invoices', 'Invoices', 11, 1, 1, 0),
			(20, 'imap://bob@imap.other.example/', 'bob', NULL, 2, 1, 0),
			(21, '/INBOX', 'Inbox', 20, 2, 1, 0),
			(30, '/home/ada/.local/share/local-mail', 'Local Folders', NULL, 3, 1, 0),
			(31, 'trash', 'trash', 30, 3, 1, 0)`,
		`INSERT INTO TagTable VALUES (3, 'work-gid')`,
		`INSERT INTO TagAttributeTable VALUES (1, 3, 'TAG', '("Work" "tag-icon" "" ())')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return dir
}

func TestKMailFiltersBecomeRules(t *testing.T) {
	cfgDir := kmailFilterFixture(t, "QSQLITE")
	sets, note := readKMailFilters(cfgDir)
	if note != "" {
		t.Fatalf("note %q with Akonadi on SQLite", note)
	}
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{
		{ID: "work", Address: "ada@example.com", IMAP: ServerConfig{Host: "imap.example.com:993", User: "ada"}},
		{ID: "pop", Address: "ada@example.net", Protocol: ProtoPOP3, POP: ServerConfig{Host: "pop.example.net:995", User: "ada.pop"}},
	}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	res := st.ImportFilters(sets)
	joined := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		`"Example list" tests "List-Id"`,
		`"To Bob" moves mail to another account`,
		`"By hand" runs in KMail only by hand`,
		`"Everywhere": its account (imap.other.example) is not set up here`,
		`"Local" moves to a local KMail folder`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("skipped lacks %q:\n%s", want, joined)
		}
	}
	if res.Added != 4 || len(res.Skipped) != 5 {
		t.Fatalf("added %d, skipped %d:\n%s", res.Added, len(res.Skipped), joined)
	}

	rules := map[string]FilterRule{}
	for _, r := range st.ListRules() {
		rules[r.Name+"@"+r.Conditions[0].Value] = r
	}
	inv := rules["KMail: Invoices@work"]
	if len(inv.Conditions) != 4 || inv.Conditions[2] != (RuleCondition{Field: "subject", Op: "contains", Value: "Invoice"}) ||
		inv.Conditions[3] != (RuleCondition{Field: "from", Op: "isnot", Value: "noreply@example.com"}) || inv.Any || inv.Stop {
		t.Fatalf("Invoices: %+v", inv)
	}
	if len(inv.Actions) != 3 || inv.Actions[0].Type != "markRead" || inv.Actions[1] != (RuleAction{Type: "tag", Tag: "Work"}) ||
		inv.Actions[2] != (RuleAction{Type: "move", Account: "work", Path: "INBOX/Invoices"}) {
		t.Fatalf("Invoices actions: %+v", inv.Actions)
	}
	if sp := rules["KMail: Spaced@pop"]; len(sp.Conditions) != 3 || sp.Conditions[2].Value != " board" || sp.Conditions[2].Field != "to" || !sp.Stop {
		t.Fatalf("Spaced: %+v", sp)
	}
	if rules["KMail: Everywhere@work"].Name == "" || rules["KMail: Everywhere@pop"].Name == "" || !rules["KMail: Everywhere@pop"].Any {
		t.Fatalf("Everywhere is one rule an account: %v", rules)
	}
	if again := st.ImportFilters(sets); again.Added != 0 {
		t.Fatalf("imported twice: %+v", again)
	}
}

// With Akonadi on MySQL the folders cannot be looked up: those filters are
// left out, and the import says why.
func TestKMailFiltersWithoutAkonadiSQLite(t *testing.T) {
	sets, note := readKMailFilters(kmailFilterFixture(t, "QMYSQL"))
	if !strings.Contains(note, "Akonadi uses SQLite") {
		t.Fatalf("note %q", note)
	}
	for _, s := range sets {
		for _, f := range s.Filters {
			if f.Name == "Invoices" && !strings.Contains(f.Skip, "Akonadi id") {
				t.Fatalf("Invoices: %+v", f)
			}
		}
	}
}

func TestKConfigEscapes(t *testing.T) {
	if got := kconfigUnescape(`\sa\tb\\c\n`); got != " a\tb\\c\n" {
		t.Fatalf("%q", got)
	}
	if got := kconfigList(`a,b\,c, d`); len(got) != 3 || got[1] != "b,c" || got[2] != "d" {
		t.Fatalf("%q", got)
	}
}
