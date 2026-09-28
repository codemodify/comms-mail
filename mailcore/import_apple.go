package mailcore

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// macOS keeps two things Mail import wants in Core Data SQLite databases:
// the people in Contacts (AddressBook-v22.abcddb) and, since OS X 10.11,
// the mail accounts (Accounts4.sqlite, set up in Internet Accounts). Both
// are read from a copy, so a database in use is neither locked nor
// changed; column names are used, never Core Data's entity numbers, which
// differ between versions. Passwords are in the Keychain and never read.

// openSQLiteSnapshot opens a copy of another program's database, with its
// write-ahead log so what was written lately is there too. close removes
// the copy. A database too large to copy is opened in place, read-only.
func openSQLiteSnapshot(path string) (db *sql.DB, closeDB func(), err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	wal, _ := os.Stat(path + "-wal")
	size := fi.Size()
	if wal != nil {
		size += wal.Size()
	}
	if size > 64<<20 {
		db, err := openReadOnlySQLite(path)
		if err != nil {
			return nil, nil, err
		}
		return db, func() { _ = db.Close() }, nil
	}
	tmp, err := os.MkdirTemp("", "comms-mail-import-")
	if err != nil {
		return nil, nil, err
	}
	gone := func() { _ = os.RemoveAll(tmp) }
	cp := filepath.Join(tmp, "copy.db")
	for _, ext := range []string{"", "-wal"} {
		if err := copyFileTo(path+ext, cp+ext); err != nil && ext == "" {
			gone()
			return nil, nil, err
		}
	}
	db, err = sql.Open("sqlite", "file:"+cp)
	if err != nil {
		gone()
		return nil, nil, err
	}
	return db, func() { _ = db.Close(); gone() }, nil
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// readAppleContacts reads Contacts' database in the AddressBook folder and
// each account's (Sources/…: on recent macOS most people are there).
func readAppleContacts() []Contact {
	root := filepath.Join(homeDir(), "Library", "Application Support", "AddressBook")
	files := []string{filepath.Join(root, "AddressBook-v22.abcddb")}
	more, _ := filepath.Glob(filepath.Join(root, "Sources", "*", "AddressBook-v22.abcddb"))
	var out []Contact
	for _, f := range append(files, more...) {
		out = append(out, readAppleAddressBook(f)...)
	}
	return dedupContacts(out)
}

// Contacts' "show as company" display flag (kABShowAsCompany in kABShowAsMask).
const (
	appleShowAsMask    = 7
	appleShowAsCompany = 1
)

func readAppleAddressBook(path string) []Contact {
	db, done, err := openSQLiteSnapshot(path)
	if err != nil {
		return nil
	}
	defer done()
	const cols = `SELECT IFNULL(r.ZFIRSTNAME, ''), IFNULL(r.ZMIDDLENAME, ''), IFNULL(r.ZLASTNAME, ''),
		IFNULL(r.ZNICKNAME, ''), IFNULL(r.ZORGANIZATION, ''), IFNULL(r.ZDISPLAYFLAGS, 0), IFNULL(e.ZADDRESS, '')
		FROM ZABCDEMAILADDRESS e JOIN ZABCDRECORD r ON r.Z_PK = e.ZOWNER`
	rows, err := db.Query(cols + ` ORDER BY r.Z_PK, e.ZORDERINGINDEX`)
	if err != nil {
		rows, err = db.Query(cols) // a version without the ordering column
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		var first, middle, last, nick, org, addr string
		var flags int64
		if rows.Scan(&first, &middle, &last, &nick, &org, &flags, &addr) != nil {
			continue
		}
		addr = strings.TrimSpace(addr)
		if !strings.Contains(addr, "@") {
			continue
		}
		name := strings.Join(strings.Fields(first+" "+middle+" "+last), " ")
		if flags&appleShowAsMask == appleShowAsCompany && strings.TrimSpace(org) != "" {
			name = strings.TrimSpace(org)
		}
		out = append(out, Contact{Name: firstNonEmpty(name, strings.TrimSpace(nick), strings.TrimSpace(org)), Address: addr})
	}
	return out
}

// appleAccounts4 reads the mail accounts of Internet Accounts: one ZACCOUNT
// row each for the incoming (IMAP, POP) and the outgoing (SMTP) servers,
// with their settings as ZACCOUNTPROPERTY rows whose values are
// NSKeyedArchiver archives.
func appleAccounts4(path string) []ImportedAccount {
	db, done, err := openSQLiteSnapshot(path)
	if err != nil {
		return nil
	}
	defer done()
	type acct struct {
		pk                   int64
		id, user, desc, kind string
		props                map[string]any
	}
	rows, err := db.Query(`SELECT a.Z_PK, IFNULL(a.ZIDENTIFIER, ''), IFNULL(a.ZUSERNAME, ''), IFNULL(a.ZACCOUNTDESCRIPTION, ''),
		IFNULL(t.ZIDENTIFIER, '') FROM ZACCOUNT a JOIN ZACCOUNTTYPE t ON t.Z_PK = a.ZACCOUNTTYPE ORDER BY a.Z_PK`)
	if err != nil {
		return nil
	}
	byPK := map[int64]*acct{}
	var order []*acct
	for rows.Next() {
		a := &acct{props: map[string]any{}}
		if rows.Scan(&a.pk, &a.id, &a.user, &a.desc, &a.kind) != nil {
			continue
		}
		kind := strings.ToLower(a.kind)
		switch {
		case strings.HasSuffix(kind, ".imap"):
			a.kind = ProtoIMAP
		case strings.HasSuffix(kind, ".pop"):
			a.kind = ProtoPOP3
		case strings.HasSuffix(kind, ".smtp"):
			a.kind = "smtp"
		default:
			continue
		}
		byPK[a.pk] = a
		order = append(order, a)
	}
	rows.Close()
	if len(order) == 0 {
		return nil
	}
	props, err := db.Query(`SELECT ZOWNER, IFNULL(ZKEY, ''), ZVALUE FROM ZACCOUNTPROPERTY WHERE ZOWNER IS NOT NULL`)
	if err != nil {
		return nil
	}
	for props.Next() {
		var owner int64
		var key string
		var raw []byte
		if props.Scan(&owner, &key, &raw) != nil || byPK[owner] == nil {
			continue
		}
		byPK[owner].props[key] = appleArchivedValue(raw)
	}
	props.Close()

	tlsOf := func(p map[string]any) string {
		if asBool(p["SSLEnabled"]) {
			return string(TLSImplicit)
		}
		return string(TLSStartTLS)
	}
	smtp := map[string]ServerConfig{}
	var onlySMTP []ServerConfig
	for _, a := range order {
		if a.kind != "smtp" || asString(a.props["Hostname"]) == "" {
			continue
		}
		cfg := ServerConfig{Host: hostPort(asString(a.props["Hostname"]), asString(a.props["PortNumber"])), User: a.user, TLSMode: tlsOf(a.props)}
		smtp[a.id] = cfg
		onlySMTP = append(onlySMTP, cfg)
	}
	var out []ImportedAccount
	for _, a := range order {
		host := asString(a.props["Hostname"])
		if a.kind == "smtp" || host == "" {
			continue
		}
		in := ServerConfig{Host: hostPort(host, asString(a.props["PortNumber"])), User: a.user, TLSMode: tlsOf(a.props)}
		address := firstNonEmpty(asString(a.props["IdentityEmailAddress"]), appleAliasAddress(a.props["EmailAliases"]))
		if address == "" && strings.Contains(a.user, "@") {
			address = a.user
		}
		name := asString(a.props["ACPropertyFullName"])
		cfg := AccountConfig{Name: firstNonEmpty(a.desc, name, address), Address: address, Protocol: a.kind}
		if a.kind == ProtoPOP3 {
			cfg.POP = in
		} else {
			cfg.IMAP = in
		}
		if s, ok := smtp[asString(a.props["SendingAccountIdentifier"])]; ok {
			cfg.SMTP = s
		} else if len(onlySMTP) == 1 {
			cfg.SMTP = onlySMTP[0]
		}
		if address != "" {
			cfg.Identities = []Identity{{ID: "apple-" + strings.ToLower(address), Name: name, Address: address, Default: true}}
		}
		out = append(out, ImportedAccount{Source: "Apple Mail", Account: cfg})
	}
	return dedupImported(out)
}

// appleArchivedValue decodes a property value: an archived plist, or (in
// some versions) the value itself.
func appleArchivedValue(raw []byte) any {
	if !isBinaryPlist(raw) {
		return string(raw)
	}
	v, err := decodeBinaryPlist(raw)
	if err != nil {
		return nil
	}
	if u, ok := unarchiveKeyed(v); ok {
		return u
	}
	return v
}

// appleAliasAddress is the default (else the first) address in the
// EmailAliases property: [{DisplayName, EmailAddresses: [{EmailAddress,
// IsDefault}], IsPrimary}].
func appleAliasAddress(v any) string {
	first := ""
	for _, alias := range asList(v) {
		m, _ := alias.(map[string]any)
		for _, e := range asList(m["EmailAddresses"]) {
			em, _ := e.(map[string]any)
			addr := asString(em["EmailAddress"])
			if addr == "" {
				continue
			}
			if asBool(em["IsDefault"]) || asBool(m["IsPrimary"]) {
				return addr
			}
			first = firstNonEmpty(first, addr)
		}
	}
	return first
}
