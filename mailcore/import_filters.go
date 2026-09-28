package mailcore

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Thunderbird's message filters, imported as rules. Each incoming server
// keeps its filters in msgFilterRules.dat in its folder of the profile; they
// run on new mail in that account's Inbox, so each imported rule is scoped
// to its account and the Inbox. A filter that tests or does something rules
// here cannot is left out, and the import says which and why — never
// imported half, which could move mail it should not.

// ThunderbirdFilter is one filter as Thunderbird wrote it.
type ThunderbirdFilter struct {
	Name    string      `json:"name"`
	Enabled bool        `json:"enabled"`
	Match   string      `json:"match"` // AND, OR, ALL
	Conds   [][3]string `json:"conds,omitempty"`
	Actions [][2]string `json:"actions,omitempty"` // action, value
}

// FilterSet is the filters of one incoming server.
type FilterSet struct {
	Host    string              `json:"host"`
	User    string              `json:"user,omitempty"`
	Local   bool                `json:"local,omitempty"` // Local Folders
	Filters []ThunderbirdFilter `json:"filters"`
}

// FilterImport is what an import of filters did.
type FilterImport struct {
	Added   int      `json:"added"`
	Skipped []string `json:"skipped,omitempty"`
}

// readThunderbirdFilters reads the filters of every incoming server of every
// Thunderbird profile.
func readThunderbirdFilters() []FilterSet {
	var out []FilterSet
	for _, prefsPath := range ThunderbirdProfiles() {
		prof := filepath.Dir(prefsPath)
		p, err := parsePrefsFile(prefsPath)
		if err != nil {
			continue
		}
		for key, val := range p {
			if !strings.HasPrefix(key, "mail.server.") || !strings.HasSuffix(key, ".directory-rel") {
				continue
			}
			sp := strings.TrimSuffix(key, "directory-rel")
			dir := strings.Replace(val, "[ProfD]", prof+string(filepath.Separator), 1)
			typ := strings.ToLower(p[sp+"type"])
			if typ != "imap" && typ != "pop3" && typ != "none" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, "msgFilterRules.dat"))
			if err != nil {
				continue
			}
			filters := parseThunderbirdFilters(string(b))
			if len(filters) == 0 {
				continue
			}
			out = append(out, FilterSet{Host: p[sp+"hostname"], User: p[sp+"userName"], Local: typ == "none", Filters: filters})
		}
	}
	return out
}

// parseThunderbirdFilters reads a msgFilterRules.dat: key="value" lines,
// a filter beginning at each name=.
func parseThunderbirdFilters(text string) []ThunderbirdFilter {
	var out []ThunderbirdFilter
	var cur *ThunderbirdFilter
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		val = unquoteTB(val)
		switch key {
		case "name":
			out = append(out, ThunderbirdFilter{Name: val, Enabled: true})
			cur = &out[len(out)-1]
		case "enabled":
			if cur != nil {
				cur.Enabled = val == "yes"
			}
		case "action":
			if cur != nil {
				cur.Actions = append(cur.Actions, [2]string{val, ""})
			}
		case "actionValue":
			if cur != nil && len(cur.Actions) > 0 {
				cur.Actions[len(cur.Actions)-1][1] = val
			}
		case "condition":
			if cur != nil {
				cur.Match, cur.Conds = parseTBCondition(val)
			}
		}
	}
	return out
}

// unquoteTB undoes a value's quotes and \" escapes.
func unquoteTB(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(v)
}

// parseTBCondition reads `AND (from,contains,x) AND (subject,is,"a, b")`
// into its joiner and (field, op, value) clauses; "ALL" matches everything.
func parseTBCondition(v string) (string, [][3]string) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "ALL") {
		return "ALL", nil
	}
	match := "AND"
	var conds [][3]string
	for i := 0; i < len(v); {
		for i < len(v) && v[i] == ' ' {
			i++
		}
		switch {
		case strings.HasPrefix(v[i:], "AND "):
			i += 4
		case strings.HasPrefix(v[i:], "OR "):
			match = "OR"
			i += 3
		}
		for i < len(v) && v[i] == ' ' {
			i++
		}
		if i >= len(v) || v[i] != '(' {
			break
		}
		i++
		var parts []string
		var b strings.Builder
		quoted := false
		for ; i < len(v); i++ {
			c := v[i]
			switch {
			case c == '\\' && i+1 < len(v):
				i++
				b.WriteByte(v[i])
			case c == '"':
				quoted = !quoted
			case c == ',' && !quoted && len(parts) < 2:
				parts = append(parts, b.String())
				b.Reset()
			case c == ')' && !quoted:
				parts = append(parts, b.String())
				i++
				goto done
			default:
				b.WriteByte(c)
			}
		}
	done:
		if len(parts) == 3 {
			conds = append(conds, [3]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), parts[2]})
		}
	}
	return match, conds
}

// tbField and tbOp translate a Thunderbird clause; "" means rules here
// cannot test it.
func tbField(f string) string {
	switch strings.ToLower(f) {
	case "from":
		return "from"
	case "to", "cc", "to or cc", "all addresses":
		return "to"
	case "subject":
		return "subject"
	case "body":
		return "body"
	}
	return ""
}

func tbOp(o string) string {
	switch strings.ToLower(o) {
	case "contains":
		return "contains"
	case "doesn't contain":
		return "notcontains"
	case "is":
		return "is"
	case "isn't":
		return "isnot"
	case "begins with":
		return "begins"
	case "ends with":
		return "ends"
	}
	return ""
}

// tbFolderPath is the server path of a Thunderbird folder URI
// (imap://user@host/Archives/2023 → "Archives/2023").
func tbFolderPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	p, err := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/"))
	if err != nil {
		return ""
	}
	return p
}

// tbRule turns a filter into a rule for account, or says why it cannot.
func tbRule(f ThunderbirdFilter, account string, known []Tag) (FilterRule, string) {
	r := FilterRule{Name: "Thunderbird: " + f.Name, Enabled: f.Enabled, Any: f.Match == "OR",
		Conditions: []RuleCondition{{Field: "account", Value: account}, {Field: "inbox"}}}
	for _, c := range f.Conds {
		field, op := tbField(c[0]), tbOp(c[1])
		if field == "" {
			return r, fmt.Sprintf("tests %q, which rules here cannot", c[0])
		}
		if op == "" {
			return r, fmt.Sprintf("uses %q, which rules here cannot", c[1])
		}
		r.Conditions = append(r.Conditions, RuleCondition{Field: field, Op: op, Value: c[2]})
	}
	if f.Match == "ALL" {
		r.Conditions = append(r.Conditions, RuleCondition{Field: "from", Op: "contains", Value: ""})
	}
	for _, a := range f.Actions {
		switch a[0] {
		case "Move to folder":
			p := tbFolderPath(a[1])
			if p == "" {
				return r, "moves to a folder whose address cannot be read"
			}
			r.Actions = append(r.Actions, RuleAction{Type: "move", Account: account, Path: p})
		case "Mark read":
			r.Actions = append(r.Actions, RuleAction{Type: "markRead"})
		case "Mark unread":
			r.Actions = append(r.Actions, RuleAction{Type: "markUnread"})
		case "Mark flagged":
			r.Actions = append(r.Actions, RuleAction{Type: "star"})
		case "AddTag":
			if tags := keywordTags([]string{a[1]}, known); len(tags) > 0 {
				r.Actions = append(r.Actions, RuleAction{Type: "tag", Tag: tags[0]})
			}
		case "Delete":
			r.Actions = append(r.Actions, RuleAction{Type: "delete"})
		case "Stop execution":
			r.Stop = true
			r.Actions = append(r.Actions, RuleAction{Type: "stop"})
		case "Change priority", "JunkScore", "Fetch body from Pop3Server":
			// Bookkeeping with no counterpart here; the rest of the filter holds.
		default:
			return r, fmt.Sprintf("does %q, which rules here cannot", a[0])
		}
	}
	if len(r.Actions) == 0 {
		return r, "does nothing rules here can do"
	}
	return r, ""
}

func parsePrefsFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseThunderbirdPrefs(f), nil
}

// ImportFilters adds the filters of each server as rules, scoped to the
// account here that has that server (Local Folders: the imported-mail
// account). Filters already imported (by name) are not added again.
func (s *LocalStore) ImportFilters(sets []FilterSet) FilterImport {
	var res FilterImport
	s.mu.Lock()
	cfgs := append([]AccountConfig(nil), s.cfg.Accounts...)
	tags := append([]Tag(nil), s.tags...)
	have := map[string]bool{}
	for _, r := range s.rules {
		have[r.Name] = true
	}
	s.mu.Unlock()
	for _, set := range sets {
		account := LocalAccountID
		if !set.Local {
			account = ""
			for _, c := range cfgs {
				in := c.Incoming()
				host := in.Host
				if h, _, ok := strings.Cut(host, ":"); ok {
					host = h
				}
				if strings.EqualFold(host, set.Host) && (set.User == "" || strings.EqualFold(in.User, set.User) || strings.EqualFold(c.Address, set.User)) {
					account = c.ID
					if account == "" {
						account = slug(c.Address)
					}
					break
				}
			}
		}
		for _, f := range set.Filters {
			if account == "" {
				res.Skipped = append(res.Skipped, fmt.Sprintf("%q: its account (%s) is not set up here", f.Name, set.Host))
				continue
			}
			r, why := tbRule(f, account, tags)
			if why != "" {
				res.Skipped = append(res.Skipped, fmt.Sprintf("%q %s", f.Name, why))
				continue
			}
			if have[r.Name] {
				continue
			}
			if _, err := s.PutRule(r); err != nil {
				res.Skipped = append(res.Skipped, fmt.Sprintf("%q: %v", f.Name, err))
				continue
			}
			have[r.Name] = true
			res.Added++
		}
	}
	return res
}
