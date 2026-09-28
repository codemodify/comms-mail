package mailcore

import (
	"bufio"
	"encoding/xml"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Readers for the settings and local mail of the other clients comms-mail
// imports from, besides Thunderbird and KMail. Every one is best-effort:
// these are other programs' private files, whose layout moves between
// versions. What cannot be read is left out, never guessed. No password is
// ever read.

// flatpakDir is an app's sandboxed home: ~/.var/app/<id>/<sub>.
func flatpakDir(appID, sub string) string {
	return filepath.Join(homeDir(), ".var", "app", appID, sub)
}

func existingDirs(dirs ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// tlsFromWord maps the words clients use for connection security to a
// TLSMode.
func tlsFromWord(w string) string {
	switch strings.ToLower(strings.TrimSpace(w)) {
	case "ssl", "tls", "ssl/tls", "true", "yes", "1", "transport", "ssl-on-alternate-port":
		return string(TLSImplicit)
	case "starttls", "start-tls", "2", "starttls-on-standard-port":
		return string(TLSStartTLS)
	case "none", "plain", "false", "no", "0":
		return string(TLSPlain)
	}
	return string(TLSImplicit)
}

func readSignatureFile(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasSuffix(p, "|") {
		return "" // a command, not a file
	}
	b, err := os.ReadFile(expandHome(p))
	if err != nil {
		return ""
	}
	return htmlToPlainSignature(string(b))
}

// ---- Evolution --------------------------------------------------------------

// importEvolution reads Evolution's ESource files: one keyfile per account,
// identity and transport, linked by UID. Accounts managed by GNOME Online
// Accounts carry no host (they sign in through GOA) and are left out.
func importEvolution() []ImportedAccount {
	cfgRoots := existingDirs(filepath.Join(xdgConfigHome(), "evolution"), flatpakDir("org.gnome.Evolution", "config/evolution"))
	dataRoots := existingDirs(filepath.Join(xdgDataHome(), "evolution"), flatpakDir("org.gnome.Evolution", "data/evolution"))
	var out []ImportedAccount
	for _, root := range cfgRoots {
		files, _ := filepath.Glob(filepath.Join(root, "sources", "*.source"))
		src := map[string]iniFile{}
		for _, f := range files {
			if ini, ok := readINIFile(f); ok {
				src[strings.TrimSuffix(filepath.Base(f), ".source")] = ini
			}
		}
		for _, f := range src {
			var proto string
			switch strings.ToLower(f.get("Mail Account", "BackendName")) {
			case "imapx", "imap":
				proto = ProtoIMAP
			case "pop":
				proto = ProtoPOP3
			default:
				continue
			}
			host := f.get("Authentication", "Host")
			if host == "" {
				continue
			}
			in := ServerConfig{
				Host:    hostPort(host, f.get("Authentication", "Port")),
				User:    f.get("Authentication", "User"),
				TLSMode: tlsFromWord(f.get("Security", "Method")),
			}
			idUID := f.get("Mail Account", "Identity")
			id := src[idUID]
			address := id.get("Mail Identity", "Address")
			name := id.get("Mail Identity", "Name")
			var smtp ServerConfig
			if t := src[id.get("Mail Submission", "TransportUid")]; t != nil {
				smtp = ServerConfig{
					Host:    hostPort(t.get("Authentication", "Host"), t.get("Authentication", "Port")),
					User:    t.get("Authentication", "User"),
					TLSMode: tlsFromWord(t.get("Security", "Method")),
				}
			}
			if address == "" {
				address = in.User
			}
			acct := AccountConfig{
				Name:     firstNonEmpty(name, f.get("Data Source", "DisplayName"), address),
				Address:  address,
				Protocol: proto,
				SMTP:     smtp,
			}
			if proto == ProtoPOP3 {
				acct.POP = in
			} else {
				acct.IMAP = in
			}
			if address != "" {
				sig := ""
				if uid := id.get("Mail Identity", "SignatureUid"); uid != "" && uid != "none" {
					for _, d := range dataRoots {
						if s := readSignatureFile(filepath.Join(d, "signatures", uid)); s != "" {
							sig = s
							break
						}
					}
				}
				acct.Identities = []Identity{{ID: "evo-" + idUID, Name: name, Address: address, Signature: sig, Default: true}}
			}
			out = append(out, ImportedAccount{Source: "Evolution", Account: acct})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account.Address < out[j].Account.Address })
	return dedupImported(out)
}

// evolutionLocalMail is Evolution's "On This Computer": a Maildir++ tree
// (the root is the inbox, subfolders are ".Name" beside it).
func evolutionLocalMail() []LocalMailStore {
	var out []LocalMailStore
	for _, d := range existingDirs(filepath.Join(xdgDataHome(), "evolution", "mail", "local"),
		flatpakDir("org.gnome.Evolution", "data/evolution/mail/local")) {
		out = append(out, scanAnyTree("Evolution", d, "", "Inbox", 8)...)
	}
	return out
}

// ---- Claws Mail -------------------------------------------------------------

func clawsDirs() []string {
	return existingDirs(filepath.Join(homeDir(), ".claws-mail"), filepath.Join(xdgConfigHome(), "claws-mail"))
}

// importClaws reads Claws Mail's accountrc: one [Account: N] section per
// account. protocol 0/1/2 is POP3, 3 is IMAP (NNTP and local accounts are
// skipped); ssl_* is 0 none, 1 SSL, 2 STARTTLS.
func importClaws() []ImportedAccount {
	var out []ImportedAccount
	for _, dir := range clawsDirs() {
		f, ok := readINIFile(filepath.Join(dir, "accountrc"))
		if !ok {
			continue
		}
		var sections []string
		for sec := range f {
			if strings.HasPrefix(sec, "Account:") {
				sections = append(sections, sec)
			}
		}
		sort.Strings(sections)
		for _, sec := range sections {
			var proto string
			switch f.get(sec, "protocol") {
			case "0", "1", "2":
				proto = ProtoPOP3
			case "3":
				proto = ProtoIMAP
			default:
				continue
			}
			recv := f.get(sec, "receive_server")
			if recv == "" {
				continue
			}
			tlsWord := func(k string) string {
				switch f.get(sec, k) {
				case "1":
					return string(TLSImplicit)
				case "2":
					return string(TLSStartTLS)
				}
				return string(TLSPlain)
			}
			port := func(setKey, key, ssl, plain string, tls string) string {
				if f.get(sec, setKey) == "1" && f.get(sec, key) != "" {
					return f.get(sec, key)
				}
				if tls == string(TLSImplicit) {
					return ssl
				}
				return plain
			}
			var in ServerConfig
			if proto == ProtoIMAP {
				t := tlsWord("ssl_imap")
				in = ServerConfig{Host: hostPort(recv, port("set_imapport", "imap_port", "993", "143", t)), TLSMode: t}
			} else {
				t := tlsWord("ssl_pop")
				in = ServerConfig{Host: hostPort(recv, port("set_popport", "pop_port", "995", "110", t)), TLSMode: t}
			}
			in.User = f.get(sec, "user_id")
			smtp := ServerConfig{}
			if h := f.get(sec, "smtp_server"); h != "" {
				t := tlsWord("ssl_smtp")
				def := "25"
				if t == string(TLSImplicit) {
					def = "465"
				} else if t == string(TLSStartTLS) {
					def = "587"
				}
				smtp = ServerConfig{
					Host:    hostPort(h, port("set_smtpport", "smtp_port", def, def, t)),
					User:    firstNonEmpty(f.get(sec, "smtp_user_id"), in.User),
					TLSMode: t,
				}
			}
			address := f.get(sec, "address")
			name := f.get(sec, "name")
			sig := ""
			switch f.get(sec, "sig_type") {
			case "2":
				sig = strings.ReplaceAll(f.get(sec, "sig_text"), `\n`, "\n")
			case "0", "":
				sig = readSignatureFile(f.get(sec, "sig_path"))
			}
			acct := AccountConfig{
				Name:     firstNonEmpty(f.get(sec, "account_name"), name, address),
				Address:  firstNonEmpty(address, in.User),
				Protocol: proto,
				SMTP:     smtp,
			}
			if proto == ProtoPOP3 {
				acct.POP = in
			} else {
				acct.IMAP = in
			}
			if address != "" {
				acct.Identities = []Identity{{ID: "claws-" + strings.TrimSpace(strings.TrimPrefix(sec, "Account:")), Name: name, Address: address, Signature: sig, Default: true}}
			}
			out = append(out, ImportedAccount{Source: "Claws Mail", Account: acct})
		}
	}
	return dedupImported(out)
}

var clawsMHRe = regexp.MustCompile(`<folder\b[^>]*>`)
var xmlAttrRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// clawsLocalMail reads the MH mailboxes Claws Mail lists in folderlist.xml
// (default ~/Mail). IMAP folders are a cache and re-sync; they are skipped.
func clawsLocalMail() []LocalMailStore {
	var roots []string
	for _, dir := range clawsDirs() {
		b, err := os.ReadFile(filepath.Join(dir, "folderlist.xml"))
		if err != nil {
			continue
		}
		for _, tag := range clawsMHRe.FindAllString(string(b), -1) {
			attrs := map[string]string{}
			for _, m := range xmlAttrRe.FindAllStringSubmatch(tag, -1) {
				attrs[m[1]] = m[2]
			}
			if attrs["type"] != "mh" || attrs["path"] == "" {
				continue
			}
			p := expandHome(attrs["path"])
			if !filepath.IsAbs(p) {
				p = filepath.Join(homeDir(), p)
			}
			roots = append(roots, p)
		}
	}
	if len(roots) == 0 && len(clawsDirs()) > 0 {
		roots = append(roots, filepath.Join(homeDir(), "Mail"))
	}
	var out []LocalMailStore
	for _, r := range existingDirs(roots...) {
		out = append(out, scanAnyTree("Claws Mail", r, "", "Mailbox", 8)...)
	}
	return out
}

// ---- Geary ------------------------------------------------------------------

// importGeary reads Geary's geary.ini per account: the current layout
// ([Account] / [Incoming] / [Outgoing]) and the older [AccountInformation].
// Gmail and Outlook accounts sign in through the provider, so they carry no
// host; the provider's servers are filled in and the account is marked for
// OAuth.
func importGeary() []ImportedAccount {
	var files []string
	for _, root := range existingDirs(filepath.Join(xdgConfigHome(), "geary"), filepath.Join(xdgDataHome(), "geary"),
		flatpakDir("org.gnome.Geary", "config/geary"), flatpakDir("org.gnome.Geary", "data/geary")) {
		m, _ := filepath.Glob(filepath.Join(root, "*", "geary.ini"))
		files = append(files, m...)
	}
	sort.Strings(files)
	var out []ImportedAccount
	for _, path := range files {
		f, ok := readINIFile(path)
		if !ok {
			continue
		}
		var a AccountConfig
		provider := strings.ToLower(firstNonEmpty(f.get("Account", "service_provider"), f.get("AccountInformation", "service_provider")))
		if f["Incoming"] != nil || f["Account"] != nil {
			if boxes := ParseAddrList(strings.ReplaceAll(f.get("Account", "sender_mailboxes"), ";", ",")); len(boxes) > 0 {
				a.Address, a.Name = boxes[0].Address, boxes[0].Name
			}
			a.IMAP = ServerConfig{
				Host:    hostPort(f.get("Incoming", "host"), f.get("Incoming", "port")),
				User:    f.get("Incoming", "login"),
				TLSMode: tlsFromWord(f.get("Incoming", "transport_security")),
			}
			a.SMTP = ServerConfig{
				Host:    hostPort(f.get("Outgoing", "host"), f.get("Outgoing", "port")),
				User:    f.get("Outgoing", "login"),
				TLSMode: tlsFromWord(f.get("Outgoing", "transport_security")),
			}
		} else {
			s := "AccountInformation"
			a.Address, a.Name = f.get(s, "primary_email"), f.get(s, "real_name")
			imapTLS := string(TLSPlain)
			if f.get(s, "imap_ssl") == "true" {
				imapTLS = string(TLSImplicit)
			} else if f.get(s, "imap_starttls") == "true" {
				imapTLS = string(TLSStartTLS)
			}
			smtpTLS := string(TLSPlain)
			if f.get(s, "smtp_ssl") == "true" {
				smtpTLS = string(TLSImplicit)
			} else if f.get(s, "smtp_starttls") == "true" {
				smtpTLS = string(TLSStartTLS)
			}
			a.IMAP = ServerConfig{Host: hostPort(f.get(s, "imap_host"), f.get(s, "imap_port")), User: f.get(s, "imap_username"), TLSMode: imapTLS}
			a.SMTP = ServerConfig{Host: hostPort(f.get(s, "smtp_host"), f.get(s, "smtp_port")), User: f.get(s, "smtp_username"), TLSMode: smtpTLS}
		}
		switch provider {
		case "gmail":
			a.Provider = "google"
			if a.IMAP.Host == "" {
				a.IMAP = ServerConfig{Host: "imap.gmail.com:993", User: a.Address, TLSMode: string(TLSImplicit), Auth: "xoauth2"}
				a.SMTP = ServerConfig{Host: "smtp.gmail.com:587", User: a.Address, TLSMode: string(TLSStartTLS), Auth: "xoauth2"}
			}
		case "outlook":
			a.Provider = "microsoft"
			if a.IMAP.Host == "" {
				a.IMAP = ServerConfig{Host: "outlook.office365.com:993", User: a.Address, TLSMode: string(TLSImplicit), Auth: "xoauth2"}
				a.SMTP = ServerConfig{Host: "smtp.office365.com:587", User: a.Address, TLSMode: string(TLSStartTLS), Auth: "xoauth2"}
			}
		}
		if a.Address == "" || a.IMAP.Host == "" {
			continue
		}
		if a.IMAP.User == "" {
			a.IMAP.User = a.Address
		}
		a.Protocol = ProtoIMAP
		a.Name = firstNonEmpty(a.Name, a.Address)
		a.Identities = []Identity{{ID: "geary-" + filepath.Base(filepath.Dir(path)), Name: a.Name, Address: a.Address, Default: true}}
		out = append(out, ImportedAccount{Source: "Geary", Account: a})
	}
	return dedupImported(out)
}

// ---- mutt / neomutt ---------------------------------------------------------

// muttConfig is what comms-mail reads from a muttrc: the few `set`
// variables that describe an account and where its mail is.
type muttConfig struct {
	vars      map[string]string
	mailboxes []string
}

var muttSetRe = regexp.MustCompile(`^set\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)

// parseMuttrc reads path into c, following `source` lines to depth.
func parseMuttrc(path string, c *muttConfig, depth int) {
	f, err := os.Open(expandHome(path))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(stripMuttComment(sc.Text()))
		switch {
		case line == "":
		case strings.HasPrefix(line, "set "):
			if m := muttSetRe.FindStringSubmatch(line); m != nil {
				c.vars[m[1]] = muttValue(m[2])
			}
		case strings.HasPrefix(line, "mailboxes "):
			for _, tok := range muttTokens(strings.TrimPrefix(line, "mailboxes ")) {
				c.mailboxes = append(c.mailboxes, tok)
			}
		case strings.HasPrefix(line, "source ") && depth > 0:
			p := muttValue(strings.TrimPrefix(line, "source "))
			if !strings.HasSuffix(p, "|") {
				if !filepath.IsAbs(expandHome(p)) {
					p = filepath.Join(filepath.Dir(expandHome(path)), p)
				}
				parseMuttrc(p, c, depth-1)
			}
		}
	}
}

// stripMuttComment drops a # comment that is outside quotes.
func stripMuttComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case q != 0 && c == q:
			q = 0
		case q == 0 && (c == '"' || c == '\''):
			q = c
		case q == 0 && c == '#':
			return s[:i]
		}
	}
	return s
}

// muttValue is the first value of a set line, unquoted.
func muttValue(s string) string {
	toks := muttTokens(s)
	if len(toks) == 0 {
		return ""
	}
	return toks[0]
}

func muttTokens(s string) []string {
	var out []string
	s = strings.TrimSpace(s)
	for s != "" {
		if s[0] == '"' || s[0] == '\'' {
			end := strings.IndexByte(s[1:], s[0])
			if end < 0 {
				out = append(out, s[1:])
				break
			}
			out = append(out, s[1:1+end])
			s = strings.TrimSpace(s[end+2:])
			continue
		}
		tok, rest, _ := strings.Cut(s, " ")
		out = append(out, tok)
		s = strings.TrimSpace(rest)
	}
	return out
}

// muttPath expands ~, $HOME and mutt's +/= (relative to folder).
func muttPath(p, folder string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "+") || strings.HasPrefix(p, "=") {
		return filepath.Join(expandHome(folder), p[1:])
	}
	return expandHome(p)
}

func isRemoteURL(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "imap://") || strings.HasPrefix(low, "imaps://") || strings.HasPrefix(low, "pop://") || strings.HasPrefix(low, "pops://")
}

// importMuttSources reads mutt's and neomutt's configuration: the account
// that folder / spoolfile / smtp_url describe, and the local mailboxes.
func importMuttSources() []ImportSource {
	home := homeDir()
	flavours := []struct {
		name  string
		files []string
	}{
		{"mutt", []string{filepath.Join(home, ".muttrc"), filepath.Join(home, ".mutt", "muttrc"), filepath.Join(xdgConfigHome(), "mutt", "muttrc")}},
		{"neomutt", []string{filepath.Join(home, ".neomuttrc"), filepath.Join(xdgConfigHome(), "neomutt", "neomuttrc")}},
	}
	var out []ImportSource
	for _, fl := range flavours {
		for _, rc := range fl.files {
			if _, err := os.Stat(rc); err != nil {
				continue
			}
			c := &muttConfig{vars: map[string]string{}}
			parseMuttrc(rc, c, 3)
			out = append(out, muttSource(fl.name, c))
			break // the first rc a flavour would read
		}
	}
	return out
}

func muttSource(name string, c *muttConfig) ImportSource {
	src := ImportSource{Source: name}
	v := c.vars
	folder := v["folder"]
	if folder == "" {
		folder = "~/Mail"
	}

	// The account: an IMAP/POP folder or spoolfile URL, smtp_url, from.
	remote := ""
	for _, cand := range []string{folder, v["spoolfile"]} {
		if isRemoteURL(cand) {
			remote = cand
			break
		}
	}
	if remote != "" {
		if u, err := url.Parse(remote); err == nil && u.Hostname() != "" {
			var a AccountConfig
			secure := strings.HasSuffix(strings.ToLower(u.Scheme), "s")
			pop := strings.HasPrefix(strings.ToLower(u.Scheme), "pop")
			port, tlsMode := u.Port(), string(TLSStartTLS)
			if secure {
				tlsMode = string(TLSImplicit)
			}
			if port == "" {
				switch {
				case pop && secure:
					port = "995"
				case pop:
					port = "110"
				case secure:
					port = "993"
				default:
					port = "143"
				}
			}
			user := firstNonEmpty(u.User.Username(), v["imap_user"], v["pop_user"])
			in := ServerConfig{Host: hostPort(u.Hostname(), port), User: user, TLSMode: tlsMode}
			if pop {
				a.Protocol, a.POP = ProtoPOP3, in
			} else {
				a.Protocol, a.IMAP = ProtoIMAP, in
			}
			if su, err := url.Parse(v["smtp_url"]); err == nil && su.Hostname() != "" {
				ssecure := strings.EqualFold(su.Scheme, "smtps")
				sport, stls := su.Port(), string(TLSStartTLS)
				if ssecure {
					stls = string(TLSImplicit)
				}
				if sport == "" {
					sport = "587"
					if ssecure {
						sport = "465"
					}
				}
				a.SMTP = ServerConfig{Host: hostPort(su.Hostname(), sport), User: firstNonEmpty(su.User.Username(), user), TLSMode: stls}
			}
			if from := ParseAddrList(v["from"]); len(from) > 0 {
				a.Address, a.Name = from[0].Address, from[0].Name
			}
			if a.Address == "" && strings.Contains(user, "@") {
				a.Address = user
			}
			a.Name = firstNonEmpty(v["realname"], a.Name, a.Address)
			if a.Address != "" {
				a.Identities = []Identity{{ID: name + "-default", Name: a.Name, Address: a.Address, Signature: readSignatureFile(v["signature"]), Default: true}}
				src.Accounts = []ImportedAccount{{Source: name, Account: a}}
			}
		}
	}

	// Local mail: the folder directory, the spool and mbox files, and every
	// local mailbox listed with `mailboxes`.
	var paths []string
	if !isRemoteURL(folder) {
		paths = append(paths, muttPath(folder, folder))
	}
	for _, p := range append([]string{v["spoolfile"], v["mbox"], v["record"]}, c.mailboxes...) {
		if p != "" && !isRemoteURL(p) && !strings.HasPrefix(p, "!") {
			paths = append(paths, muttPath(p, folder))
		}
	}
	seen := map[string]bool{}
	for _, p := range paths {
		for _, st := range scanAnyTree(name, p, "", filepath.Base(p), 8) {
			if !seen[st.Path] {
				seen[st.Path] = true
				src.Mail = append(src.Mail, st)
			}
		}
	}
	return src
}

// ---- Apple Mail -------------------------------------------------------------

func appleMailRoots() []string {
	m, _ := filepath.Glob(filepath.Join(homeDir(), "Library", "Mail", "V*"))
	sort.Sort(sort.Reverse(sort.StringSlice(m))) // newest layout first
	return m
}

// importAppleMail reads Apple Mail's "On My Mac" mailboxes (.mbox folders
// of .emlx files) and, where an older macOS kept it, MailData/Accounts.plist.
// A newer macOS keeps accounts in its system account store, which is not
// readable here; the note says so.
func importAppleMail() ImportSource {
	src := ImportSource{Source: "Apple Mail"}
	roots := appleMailRoots()
	if len(roots) == 0 {
		return src
	}
	seen := map[string]bool{}
	for _, root := range roots {
		for _, st := range scanAnyTree("Apple Mail", filepath.Join(root, "Mailboxes"), "", "On My Mac", 8) {
			if !seen[st.Path] {
				seen[st.Path] = true
				src.Mail = append(src.Mail, st)
			}
		}
		if len(src.Accounts) == 0 {
			src.Accounts = appleAccountsPlist(filepath.Join(root, "MailData", "Accounts.plist"))
		}
	}
	if len(src.Accounts) == 0 {
		src.Note = "This macOS keeps mail accounts in System Settings → Internet Accounts, which cannot be read here — add those accounts by hand."
	}
	return src
}

func appleAccountsPlist(path string) []ImportedAccount {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	v, err := decodePlist(f)
	if err != nil {
		return nil
	}
	root, _ := v.(map[string]any)
	var delivery []map[string]any
	for _, d := range asList(root["DeliveryAccounts"]) {
		if m, ok := d.(map[string]any); ok {
			delivery = append(delivery, m)
		}
	}
	var out []ImportedAccount
	for _, item := range asList(root["MailAccounts"]) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var proto string
		switch asString(m["AccountType"]) {
		case "IMAPAccount", "iToolsAccount":
			proto = ProtoIMAP
		case "POPAccount":
			proto = ProtoPOP3
		default:
			continue
		}
		host := asString(m["Hostname"])
		if host == "" {
			continue
		}
		tlsMode := string(TLSStartTLS)
		if asBool(m["SSLEnabled"]) {
			tlsMode = string(TLSImplicit)
		}
		in := ServerConfig{Host: hostPort(host, asString(m["PortNumber"])), User: asString(m["Username"]), TLSMode: tlsMode}
		var address string
		if emails := asList(m["EmailAddresses"]); len(emails) > 0 {
			address = asString(emails[0])
		}
		name := asString(m["FullUserName"])
		a := AccountConfig{Name: firstNonEmpty(asString(m["AccountName"]), name, address), Address: firstNonEmpty(address, in.User), Protocol: proto}
		if proto == ProtoPOP3 {
			a.POP = in
		} else {
			a.IMAP = in
		}
		ident := asString(m["SMTPIdentifier"])
		for _, d := range delivery {
			if ident == "" || asString(d["Hostname"])+":"+asString(d["Username"]) == ident || asString(d["Hostname"]) == ident {
				stls := string(TLSStartTLS)
				if asBool(d["SSLEnabled"]) {
					stls = string(TLSImplicit)
				}
				a.SMTP = ServerConfig{Host: hostPort(asString(d["Hostname"]), asString(d["PortNumber"])), User: asString(d["Username"]), TLSMode: stls}
				break
			}
		}
		if a.Address != "" {
			a.Identities = []Identity{{ID: "apple-" + strings.ToLower(a.Address), Name: name, Address: a.Address, Default: true}}
		}
		out = append(out, ImportedAccount{Source: "Apple Mail", Account: a})
	}
	return dedupImported(out)
}

// decodePlist reads an XML property list into map[string]any, []any,
// string, int64, float64, bool — enough for Mail's Accounts.plist.
func decodePlist(r io.Reader) (any, error) {
	d := xml.NewDecoder(r)
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return plistValue(d, se)
		}
	}
}

func plistValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		out := map[string]any{}
		var key string
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					var k string
					if err := d.DecodeElement(&k, &t); err != nil {
						return nil, err
					}
					key = k
					continue
				}
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				out[key] = v
			case xml.EndElement:
				return out, nil
			}
		}
	case "array":
		var out []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			case xml.EndElement:
				return out, nil
			}
		}
	case "true":
		_ = d.Skip()
		return true, nil
	case "false":
		_ = d.Skip()
		return false, nil
	default: // string, integer, real, date, data
		var s string
		if err := d.DecodeElement(&s, &se); err != nil {
			return nil, err
		}
		switch se.Name.Local {
		case "integer":
			if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				return n, nil
			}
		case "real":
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				return f, nil
			}
		}
		return s, nil
	}
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	return ""
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "YES") || strings.EqualFold(t, "true") || t == "1"
	case int64:
		return t != 0
	}
	return false
}
