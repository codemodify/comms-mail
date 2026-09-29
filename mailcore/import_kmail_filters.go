package mailcore

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// KMail's filters, imported as rules the way Thunderbird's are: read into
// the same filter shape (in Thunderbird's words) and one FilterSet per
// KMail account each filter runs on, so tbRule turns them into rules and
// leaves out, with the reason, any a rule here cannot do whole.
//
// mailcommon keeps them in akonadi_mailfilter_agentrc: [General] filters=N
// and a [Filter #i] group each (older KMail: in kmail2rc). A filter that
// moves mail names the folder by its Akonadi collection id, looked up in
// Akonadi's database — readable when it is SQLite (the default since KDE
// Gear 26.04), not when it is Akonadi's own MySQL.

// kmailAccount is one KMail incoming account: its Akonadi resource.
type kmailAccount struct {
	agent string // akonadi_imap_resource_0
	host  string // without the port
	user  string
	pop   bool
}

// readKMailFilters reads KMail's filters from its config dir. note says
// what could not be looked up, for the import window.
func readKMailFilters(dir string) (sets []FilterSet, note string) {
	if dir == "" {
		return nil, ""
	}
	var f iniFile
	for _, name := range []string{"akonadi_mailfilter_agentrc", "kmail2rc"} {
		if g, ok := readINIFile(filepath.Join(dir, name)); ok && kmailFilterCount(g) > 0 {
			f = g
			break
		}
	}
	if f == nil {
		return nil, ""
	}
	accounts := kmailAccounts(dir)
	akonadi, akonadiProblem := openAkonadiDB(dir)
	if akonadi != nil {
		defer akonadi.close()
	}

	bySet := map[string]*FilterSet{}
	var order []string
	add := func(key string, set FilterSet, flt ThunderbirdFilter) {
		s := bySet[key]
		if s == nil {
			set.Source = "KMail"
			s = &set
			bySet[key] = s
			order = append(order, key)
		}
		s.Filters = append(s.Filters, flt)
	}
	unresolved := false
	for i := 0; i < kmailFilterCount(f); i++ {
		sec := "Filter #" + strconv.Itoa(i)
		if f[sec] == nil {
			continue
		}
		targets := kmailFilterAccounts(f, sec, accounts)
		if len(targets) == 0 {
			flt := kmailFilter(f, sec, kmailAccount{}, akonadi, &unresolved)
			if flt.Skip == "" {
				flt.Skip = "runs in KMail on no account that has mail here (it applies to POP and local accounts only, or to none)"
			}
			add("", FilterSet{}, flt)
			continue
		}
		for _, a := range targets {
			add(a.agent, FilterSet{Host: a.host, User: a.user}, kmailFilter(f, sec, a, akonadi, &unresolved))
		}
	}
	for _, k := range order {
		sets = append(sets, *bySet[k])
	}
	if unresolved {
		note = "Some KMail filters name a folder or a tag by an id in Akonadi's database, and those filters are left out. " + akonadiProblem
	}
	return sets, note
}

func kmailFilterCount(f iniFile) int {
	if n, err := strconv.Atoi(f.get("General", "filters")); err == nil && n > 0 {
		return min(n, 1000)
	}
	return 0
}

// kmailAccounts are the Akonadi IMAP and POP resources of dir.
func kmailAccounts(dir string) []kmailAccount {
	var out []kmailAccount
	for _, kind := range []struct {
		glob string
		pop  bool
	}{{"akonadi_imap_resource_*rc", false}, {"akonadi_pop3_resource_*rc", true}} {
		matches, _ := filepath.Glob(filepath.Join(dir, kind.glob))
		sort.Strings(matches)
		for _, p := range matches {
			f, ok := readINIFile(p)
			if !ok {
				continue
			}
			in, user, ok := kmailIncoming(f, kind.pop)
			if !ok {
				continue
			}
			host := in.Host
			if h, _, found := strings.Cut(host, ":"); found {
				host = h
			}
			out = append(out, kmailAccount{agent: strings.TrimSuffix(filepath.Base(p), "rc"), host: host, user: user, pop: kind.pop})
		}
	}
	return out
}

// kmailFilterAccounts are the accounts a filter runs on: Applicability 0
// all, 1 (the default) all but IMAP, 2 those in accounts-set.
func kmailFilterAccounts(f iniFile, sec string, accounts []kmailAccount) []kmailAccount {
	var out []kmailAccount
	switch strings.TrimSpace(f[sec]["Applicability"]) {
	case "0":
		return accounts
	case "2":
		want := map[string]bool{}
		for _, id := range kconfigList(f[sec]["accounts-set"]) {
			want[id] = true
		}
		for _, a := range accounts {
			if want[a.agent] {
				out = append(out, a)
			}
		}
	default:
		for _, a := range accounts {
			if a.pop {
				out = append(out, a)
			}
		}
	}
	return out
}

// kmailFilter reads one [Filter #i] for account a, in Thunderbird's words.
func kmailFilter(f iniFile, sec string, a kmailAccount, akonadi *akonadiDB, unresolved *bool) ThunderbirdFilter {
	g := f[sec]
	val := func(k string) string { return kconfigUnescape(g[k]) }
	flt := ThunderbirdFilter{Name: val("name"), Enabled: !strings.EqualFold(g["Enabled"], "false")}
	if flt.Name == "" {
		flt.Name = sec
	}
	if on := g["apply-on"]; on != "" && !strings.Contains(on, "check-mail") {
		flt.Skip = "runs in KMail only by hand or on sending, not on new mail"
	}

	switch op := strings.ToLower(strings.TrimSpace(g["operator"])); op {
	case "or":
		flt.Match = "OR"
	case "all":
		flt.Match = "ALL"
	case "and", "":
		flt.Match = "AND"
	default: // ignore, unless (old configs)
		flt.Match = "AND"
		if flt.Skip == "" {
			flt.Skip = "uses KMail's " + strconv.Quote(op) + ", which rules here cannot"
		}
	}
	n, err := strconv.Atoi(g["rules"])
	if err != nil {
		n = 2 // old configs: fieldA and fieldB
	}
	for i := 0; i < min(n, 26); i++ {
		l := string(rune('A' + i))
		field := val("field" + l)
		if field == "" {
			continue
		}
		flt.Conds = append(flt.Conds, [3]string{kmailField(field), kmailFunc(val("func" + l)), val("contents" + l)})
	}

	nActs, _ := strconv.Atoi(g["actions"])
	for i := 0; i < min(nActs, 64); i++ {
		name := strings.ToLower(val("action-name-" + strconv.Itoa(i)))
		arg := val("action-args-" + strconv.Itoa(i))
		switch name {
		case "transfer":
			uri, why := kmailMoveTarget(arg, a, akonadi)
			if why != "" {
				if flt.Skip == "" {
					flt.Skip = why
				}
				if _, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64); err == nil && akonadi == nil {
					*unresolved = true
				}
				continue
			}
			flt.Actions = append(flt.Actions, [2]string{"Move to folder", uri})
		case "set status":
			switch strings.TrimSpace(arg) {
			case "R":
				flt.Actions = append(flt.Actions, [2]string{"Mark read", ""})
			case "U":
				flt.Actions = append(flt.Actions, [2]string{"Mark unread", ""})
			case "G":
				flt.Actions = append(flt.Actions, [2]string{"Mark flagged", ""})
			default:
				flt.Actions = append(flt.Actions, [2]string{"set status " + arg, ""})
			}
		case "add tag":
			tag, ok := akonadi.tagName(arg)
			if !ok {
				why := "adds a KMail tag whose name cannot be looked up"
				if akonadi == nil {
					why = "adds a KMail tag named by an Akonadi id, which cannot be looked up here"
					*unresolved = true
				}
				if flt.Skip == "" {
					flt.Skip = why
				}
				continue
			}
			flt.Actions = append(flt.Actions, [2]string{"Tag", tag})
		case "delete":
			flt.Actions = append(flt.Actions, [2]string{"Delete", ""})
		case "":
		default:
			flt.Actions = append(flt.Actions, [2]string{name, arg})
		}
	}
	if !strings.EqualFold(g["StopProcessingHere"], "false") {
		flt.Actions = append(flt.Actions, [2]string{"Stop execution", ""})
	}
	return flt
}

// kmailField and kmailFunc put a KMail search rule in Thunderbird's words
// (tbField / tbOp); what has no counterpart stays as KMail wrote it, and
// tbRule names it when it leaves the filter out.
func kmailField(f string) string {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "from":
		return "from"
	case "to", "cc", "<recipients>", "<to or cc>":
		return "to or cc"
	case "subject":
		return "subject"
	case "<body>":
		return "body"
	}
	if f = strings.TrimSpace(f); f != "" && !strings.HasPrefix(f, "<") {
		return "header:" + f // List-Id, X-Spam-Flag, Reply-To, …
	}
	return f
}

func kmailFunc(fn string) string {
	switch strings.ToLower(strings.TrimSpace(fn)) {
	case "contains":
		return "contains"
	case "contains-not":
		return "doesn't contain"
	case "equals":
		return "is"
	case "not-equal":
		return "isn't"
	case "start-with":
		return "begins with"
	case "end-with":
		return "ends with"
	}
	return fn
}

// kmailMoveTarget is the folder a transfer action moves to, as a folder
// URI (imap://user@host/Path) for tbRule, or why it cannot be named. The
// argument is an Akonadi collection id, or — in a file KMail exported — a
// path whose first part is the account's name.
func kmailMoveTarget(arg string, a kmailAccount, akonadi *akonadiDB) (string, string) {
	arg = strings.TrimSpace(arg)
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		_, rest, ok := strings.Cut(strings.Trim(arg, "/"), "/")
		if !ok || rest == "" || a.host == "" {
			return "", "moves to a folder whose name cannot be read"
		}
		return folderURI(a.user, a.host, rest), ""
	}
	if akonadi == nil {
		return "", "moves to a folder KMail names by an Akonadi id, which cannot be looked up here"
	}
	c, ok := akonadi.collection(id)
	switch {
	case !ok:
		return "", "moves to a folder that no longer exists in KMail"
	case c.host == "":
		return "", "moves to a local KMail folder"
	}
	return folderURI(c.user, c.host, c.path), ""
}

func folderURI(user, host, path string) string {
	u := url.URL{Scheme: "imap", Host: host, Path: "/" + path}
	if user != "" {
		u.User = url.User(user)
	}
	return u.String()
}

// kconfigUnescape undoes KConfig's value escapes (\s \t \n \r \\).
func kconfigUnescape(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i+1 == len(v) {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case 's':
			b.WriteByte(' ')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(v[i])
		}
	}
	return b.String()
}

// kconfigList splits a KConfig list: commas, \, for a literal one.
func kconfigList(v string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '\\' && i+1 < len(v) && v[i+1] == ',':
			b.WriteByte(',')
			i++
		case v[i] == ',':
			out = append(out, kconfigUnescape(strings.TrimSpace(b.String())))
			b.Reset()
		default:
			b.WriteByte(v[i])
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		out = append(out, kconfigUnescape(s))
	}
	return out
}

// akonadiDB is Akonadi's SQLite database, read-only: its collections (the
// folders) and tags.
type akonadiDB struct {
	db *sql.DB
	// cast is the type a BLOB column is read as text with: TEXT in
	// SQLite, CHAR in MySQL.
	cast string
	// resources are the agent id of each resource, by its row id.
	resources map[int64]string
	cols      map[int64]akonadiCol
	accounts  map[string]kmailAccount // by agent id
}

type akonadiCol struct {
	remoteID, name string
	parent         int64 // 0: a top-level collection (the account)
	resource       int64
}

// akonadiFolder is a collection as a server folder: the account (host and
// user; none for a local maildir) and the mailbox path, "/" between levels.
type akonadiFolder struct {
	host, user, path string
}

// akonadiSource is where Akonadi keeps its database, from its
// akonadiserverrc: a SQLite file, or its own MySQL server's socket.
type akonadiSource struct {
	driver string // "sqlite", "mysql", or another this cannot read
	path   string // the SQLite file
	socket string // the MySQL server's socket
	dbName string // the MySQL database
}

func akonadiSourceOf(configDir string) akonadiSource {
	f, _ := readINIFile(filepath.Join(configDir, "akonadi", "akonadiserverrc"))
	driver := ""
	if f != nil {
		driver = strings.ToUpper(firstNonEmpty(f.get("%General", "Driver"), f.get("General", "Driver")))
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(homeDir(), ".local", "share")
	}
	sqlitePath := filepath.Join(data, "akonadi", "akonadi.db")
	if f != nil {
		sqlitePath = firstNonEmpty(f.get("QSQLITE", "Name"), f.get("QSQLITE3", "Name"), sqlitePath)
	}
	switch driver {
	case "QSQLITE", "QSQLITE3":
		return akonadiSource{driver: "sqlite", path: sqlitePath}
	case "":
		// No choice recorded: SQLite when its file is there (KDE Gear 26.04
		// and later), else the MySQL server older installs have.
		if _, err := os.Stat(sqlitePath); err == nil {
			return akonadiSource{driver: "sqlite", path: sqlitePath}
		}
		fallthrough
	case "QMYSQL":
		src := akonadiSource{driver: "mysql", dbName: "akonadi"}
		if f != nil {
			src.dbName = firstNonEmpty(f.get("QMYSQL", "Name"), "akonadi")
			// Options="UNIX_SOCKET=/run/user/1000/akonadi/mysql.socket;…"
			for _, opt := range strings.Split(strings.Trim(f.get("QMYSQL", "Options"), `"`), ";") {
				if k, v, ok := strings.Cut(strings.TrimSpace(opt), "="); ok && strings.EqualFold(k, "UNIX_SOCKET") {
					src.socket = strings.TrimSpace(v)
				}
			}
		}
		if src.socket == "" {
			src.socket = defaultAkonadiSocket(data)
		}
		return src
	}
	if driver == "QPSQL" {
		return akonadiSource{driver: "PostgreSQL"}
	}
	return akonadiSource{driver: strings.TrimPrefix(driver, "Q")}
}

// defaultAkonadiSocket is where Akonadi's MySQL server listens when its
// config does not say: under the runtime directory, which
// ~/.local/share/akonadi/socket-<host>-default also points at.
func defaultAkonadiSocket(data string) string {
	if host, err := os.Hostname(); err == nil {
		p := filepath.Join(data, "akonadi", "socket-"+host+"-default", "mysql.socket")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	run := os.Getenv("XDG_RUNTIME_DIR")
	if run == "" {
		run = filepath.Join(os.TempDir(), "runtime-"+strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(run, "akonadi", "mysql.socket")
}

// openAkonadiDB opens Akonadi's database to read its collections and tags,
// or says why it cannot.
func openAkonadiDB(configDir string) (*akonadiDB, string) {
	src := akonadiSourceOf(configDir)
	var db *sql.DB
	cast := "TEXT"
	switch src.driver {
	case "sqlite":
		var err error
		if db, err = openReadOnlySQLite(src.path); err != nil {
			return nil, "Akonadi's database (" + src.path + ") could not be opened."
		}
	case "mysql":
		var err error
		if db, err = openAkonadiMySQL(src); err != nil {
			return nil, "Akonadi keeps KMail's folders in its own MySQL server, which answers only while Akonadi runs: start KMail, then scan again."
		}
		cast = "CHAR"
	default:
		return nil, "Akonadi keeps its database in " + src.driver + ", which comms-mail does not read."
	}
	a := &akonadiDB{db: db, cast: cast, resources: map[int64]string{}, cols: map[int64]akonadiCol{}, accounts: map[string]kmailAccount{}}
	if err := a.load(); err != nil {
		_ = db.Close()
		return nil, "Akonadi's database could not be read: " + err.Error()
	}
	for _, acct := range kmailAccounts(configDir) {
		a.accounts[acct.agent] = acct
	}
	return a, ""
}

// openAkonadiMySQL connects to Akonadi's MySQL server over its socket.
// The server runs with its grant tables off (any user is let in), and
// nothing here writes: the session is made read-only too.
func openAkonadiMySQL(src akonadiSource) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.Net, cfg.Addr, cfg.DBName = "unix", src.socket, src.dbName
	cfg.User = firstNonEmpty(os.Getenv("USER"), "akonadi")
	cfg.Timeout, cfg.ReadTimeout = 3*time.Second, 10*time.Second
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, _ = db.Exec("SET SESSION TRANSACTION READ ONLY")
	return db, nil
}

// load reads the resources and collections.
func (a *akonadiDB) load() error {
	rows, err := a.db.Query("SELECT id, name FROM ResourceTable")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			a.resources[id] = name
		}
	}
	rows.Close()
	rows, err = a.db.Query(fmt.Sprintf("SELECT id, CAST(IFNULL(remoteId, '') AS %s), CAST(IFNULL(name, '') AS %s), IFNULL(parentId, 0), resourceId FROM CollectionTable", a.cast, a.cast))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var c akonadiCol
		if rows.Scan(&id, &c.remoteID, &c.name, &c.parent, &c.resource) == nil {
			a.cols[id] = c
		}
	}
	return rows.Err()
}

func (a *akonadiDB) close() { _ = a.db.Close() }

// collection is the server folder of collection id. An IMAP collection's
// remote id is the server's separator and its own name; the mailbox path
// is those names, from below the account down.
func (a *akonadiDB) collection(id int64) (akonadiFolder, bool) {
	var names []string
	c, ok := a.cols[id]
	for depth := 0; ok && c.parent != 0 && depth < 64; depth++ {
		leaf := c.remoteID
		if len(leaf) > 1 {
			leaf = leaf[1:] // the separator
		} else {
			leaf = c.name
		}
		names = append([]string{leaf}, names...)
		c, ok = a.cols[c.parent]
	}
	if !ok || len(names) == 0 {
		return akonadiFolder{}, false
	}
	agent := a.resources[c.resource]
	if !strings.HasPrefix(agent, "akonadi_imap_resource_") {
		return akonadiFolder{path: strings.Join(names, "/")}, true // local
	}
	acct := a.accounts[agent]
	host, user := acct.host, acct.user
	if u, err := url.Parse(c.remoteID); err == nil && u.Scheme == "imap" {
		// The account's own root says it too: imap://user@host/.
		host = firstNonEmpty(host, u.Hostname())
		if u.User != nil {
			user = firstNonEmpty(user, u.User.Username())
		}
	}
	if host == "" {
		return akonadiFolder{}, false
	}
	return akonadiFolder{host: host, user: user, path: strings.Join(names, "/")}, true
}

// tagName is the name of the tag in an "akonadi:?tag=<id>" reference.
func (a *akonadiDB) tagName(ref string) (string, bool) {
	if a == nil {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", false
	}
	id, err := strconv.ParseInt(u.Query().Get("tag"), 10, 64)
	if err != nil {
		return "", false
	}
	var attr, gid string
	_ = a.db.QueryRow(fmt.Sprintf("SELECT CAST(value AS %s) FROM TagAttributeTable WHERE tagId = ? AND CAST(type AS %s) = 'TAG'", a.cast, a.cast), id).Scan(&attr)
	_ = a.db.QueryRow(fmt.Sprintf("SELECT CAST(IFNULL(gid, '') AS %s) FROM TagTable WHERE id = ?", a.cast), id).Scan(&gid)
	// The attribute is ("Name" "icon" …); its first quoted string is the name.
	if _, rest, ok := strings.Cut(attr, `"`); ok {
		if name, _, ok := strings.Cut(rest, `"`); ok && strings.TrimSpace(name) != "" {
			return name, true
		}
	}
	if gid != "" {
		return gid, true
	}
	return "", false
}
