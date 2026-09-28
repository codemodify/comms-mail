package mailcore

import (
	"bufio"
	"database/sql"
	"encoding/xml"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Contacts from other mail clients' address books. The address book here is
// built from the mail itself; importing adds the people the other client
// knew but this machine has no mail from yet. Only names and addresses are
// read.

// readThunderbirdAddressBooks reads abook.sqlite (Personal), history.sqlite
// (Collected Addresses) and any abook-N.sqlite of every Thunderbird
// profile: the properties table, one row per card and field.
func readThunderbirdAddressBooks() []Contact {
	var out []Contact
	for _, prefs := range ThunderbirdProfiles() {
		dir := filepath.Dir(prefs)
		files, _ := filepath.Glob(filepath.Join(dir, "abook*.sqlite"))
		files = append(files, filepath.Join(dir, "history.sqlite"))
		for _, f := range files {
			out = append(out, readTBAddressBook(f)...)
		}
	}
	return dedupContacts(out)
}

// openReadOnlySQLite opens another program's database without writing to
// it or waiting on its lock (immutable: it is not changed while read).
func openReadOnlySQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}
	return sql.Open("sqlite", u.String())
}

func readTBAddressBook(path string) []Contact {
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`SELECT card, name, value FROM properties WHERE name IN ('PrimaryEmail','SecondEmail','DisplayName','FirstName','LastName')`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type card struct{ primary, second, display, first, last string }
	cards := map[string]*card{}
	var order []string
	for rows.Next() {
		var id, name, value string
		if rows.Scan(&id, &name, &value) != nil {
			continue
		}
		c := cards[id]
		if c == nil {
			c = &card{}
			cards[id] = c
			order = append(order, id)
		}
		switch name {
		case "PrimaryEmail":
			c.primary = value
		case "SecondEmail":
			c.second = value
		case "DisplayName":
			c.display = value
		case "FirstName":
			c.first = value
		case "LastName":
			c.last = value
		}
	}
	var out []Contact
	for _, id := range order {
		c := cards[id]
		name := strings.TrimSpace(c.display)
		if name == "" {
			name = strings.TrimSpace(c.first + " " + c.last)
		}
		for _, a := range []string{c.primary, c.second} {
			if a = strings.TrimSpace(a); strings.Contains(a, "@") {
				out = append(out, Contact{Name: name, Address: a})
			}
		}
	}
	return out
}

// readEvolutionAddressBooks reads the vCards in Evolution's local address
// books (contacts.db, table folder_id).
func readEvolutionAddressBooks() []Contact {
	var out []Contact
	for _, root := range existingDirs(filepath.Join(xdgDataHome(), "evolution", "addressbook"),
		flatpakDir("org.gnome.Evolution", "data/evolution/addressbook")) {
		dbs, _ := filepath.Glob(filepath.Join(root, "*", "contacts.db"))
		for _, p := range dbs {
			db, err := openReadOnlySQLite(p)
			if err != nil {
				continue
			}
			rows, err := db.Query(`SELECT vcard FROM folder_id`)
			if err == nil {
				for rows.Next() {
					var v sql.NullString
					if rows.Scan(&v) == nil && v.Valid {
						out = append(out, parseVCards(v.String)...)
					}
				}
				rows.Close()
			}
			db.Close()
		}
	}
	return dedupContacts(out)
}

// readVCardDirs reads .vcf files — KAddressBook keeps its local book as a
// directory of them.
func readVCardDirs() []Contact {
	var out []Contact
	for _, dir := range existingDirs(filepath.Join(xdgDataHome(), "contacts"),
		filepath.Join(xdgDataHome(), "kaddressbook"),
		filepath.Join(homeDir(), ".kde", "share", "apps", "kabc")) {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !hasSuffixFold(d.Name(), ".vcf") {
				return nil
			}
			if b, err := os.ReadFile(p); err == nil && len(b) < 4<<20 {
				out = append(out, parseVCards(string(b))...)
			}
			return nil
		})
	}
	return dedupContacts(out)
}

// parseVCards reads the names and addresses of the vCards in text: FN (or
// N) and every EMAIL, unfolding continued lines.
func parseVCards(text string) []Contact {
	var out []Contact
	var name, nParts string
	var emails []string
	flush := func() {
		n := strings.TrimSpace(name)
		if n == "" {
			n = nParts
		}
		for _, e := range emails {
			out = append(out, Contact{Name: n, Address: e})
		}
		name, nParts, emails = "", "", nil
	}
	for _, line := range unfoldICS([]byte(text)) {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		prop := strings.ToUpper(key)
		if i := strings.IndexByte(prop, ';'); i >= 0 {
			prop = prop[:i]
		}
		if i := strings.LastIndexByte(prop, '.'); i >= 0 {
			prop = prop[i+1:] // item1.EMAIL
		}
		val = icsText(val)
		switch prop {
		case "BEGIN":
			name, nParts, emails = "", "", nil
		case "FN":
			name = val
		case "N":
			parts := strings.Split(val, ";")
			if len(parts) >= 2 {
				nParts = strings.TrimSpace(parts[1] + " " + parts[0])
			}
		case "EMAIL":
			if v := strings.TrimSpace(val); strings.Contains(v, "@") {
				emails = append(emails, v)
			}
		case "END":
			flush()
		}
	}
	return out
}

// readClawsAddressBook reads Claws Mail's addrbook-*.xml: a person's cn
// and each address's email.
func readClawsAddressBook() []Contact {
	var out []Contact
	for _, dir := range clawsDirs() {
		files, _ := filepath.Glob(filepath.Join(dir, "addrbook", "addrbook-*.xml"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var book struct {
				People []struct {
					CN        string `xml:"cn,attr"`
					First     string `xml:"first-name,attr"`
					Last      string `xml:"last-name,attr"`
					Addresses []struct {
						Email string `xml:"email,attr"`
					} `xml:"address-list>address"`
				} `xml:"person"`
			}
			if xml.Unmarshal(b, &book) != nil {
				continue
			}
			for _, p := range book.People {
				name := strings.TrimSpace(p.CN)
				if name == "" {
					name = strings.TrimSpace(p.First + " " + p.Last)
				}
				for _, a := range p.Addresses {
					if strings.Contains(a.Email, "@") {
						out = append(out, Contact{Name: name, Address: strings.TrimSpace(a.Email)})
					}
				}
			}
		}
	}
	return dedupContacts(out)
}

// readMuttAliases reads `alias key Name <address>` lines from a muttrc and
// the files it sources (and its alias_file).
func readMuttAliases(rc string) []Contact {
	var out []Contact
	seen := map[string]bool{}
	var read func(p string, depth int)
	read = func(p string, depth int) {
		p = expandHome(p)
		if seen[p] || depth < 0 {
			return
		}
		seen[p] = true
		f, err := os.Open(p)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(stripMuttComment(sc.Text()))
			switch {
			case strings.HasPrefix(line, "alias "):
				fields := strings.Fields(line)
				if len(fields) < 3 {
					continue
				}
				rest := strings.TrimSpace(line[strings.Index(line, fields[1])+len(fields[1]):])
				for _, a := range ParseAddrList(rest) {
					if strings.Contains(a.Address, "@") {
						out = append(out, Contact{Name: a.Name, Address: a.Address})
					}
				}
			case strings.HasPrefix(line, "source "):
				q := muttValue(strings.TrimPrefix(line, "source "))
				if !filepath.IsAbs(expandHome(q)) {
					q = filepath.Join(filepath.Dir(p), q)
				}
				read(q, depth-1)
			case strings.HasPrefix(line, "set alias_file"):
				if m := muttSetRe.FindStringSubmatch(line); m != nil {
					read(muttValue(m[2]), depth-1)
				}
			}
		}
	}
	read(rc, 3)
	return dedupContacts(out)
}

// dedupContacts keeps one contact per address, with the first name seen.
func dedupContacts(in []Contact) []Contact {
	by := map[string]int{}
	var out []Contact
	for _, c := range in {
		key := strings.ToLower(strings.TrimSpace(c.Address))
		if key == "" {
			continue
		}
		if i, ok := by[key]; ok {
			if out[i].Name == "" {
				out[i].Name = c.Name
			}
			continue
		}
		by[key] = len(out)
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Address) < strings.ToLower(out[j].Address) })
	return out
}

// ImportContacts adds contacts to the address book kept here, beside what
// the mail itself gives it, and reports how many were new.
func (f *featureHost) ImportContacts(cs []Contact) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	have := map[string]int{}
	for i, c := range f.saved {
		have[strings.ToLower(c.Address)] = i
	}
	added := 0
	for _, c := range cs {
		key := strings.ToLower(strings.TrimSpace(c.Address))
		if key == "" || !strings.Contains(key, "@") {
			continue
		}
		if i, ok := have[key]; ok {
			if f.saved[i].Name == "" {
				f.saved[i].Name = c.Name
			}
			continue
		}
		have[key] = len(f.saved)
		f.saved = append(f.saved, Contact{Name: strings.TrimSpace(c.Name), Address: strings.TrimSpace(c.Address)})
		added++
	}
	f.book = nil
	return added
}

// bookLocked is the address book suggestions draw on: the one built from
// the mail, then imported contacts it does not have.
func (f *featureHost) bookLocked() []Contact {
	if f.book != nil {
		return f.book
	}
	out := append([]Contact(nil), f.contacts...)
	have := map[string]int{}
	for i, c := range out {
		have[strings.ToLower(c.Address)] = i
	}
	for _, c := range f.saved {
		if i, ok := have[strings.ToLower(c.Address)]; ok {
			if out[i].Name == "" {
				out[i].Name = c.Name
			}
			continue
		}
		out = append(out, c)
	}
	f.book = out
	return out
}
