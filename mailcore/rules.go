package mailcore

import (
	"bufio"
	"bytes"
	"fmt"
	"net/textproto"
	"sort"
	"strings"
)

func cloneRules(in []FilterRule) []FilterRule {
	out := make([]FilterRule, len(in))
	for i, r := range in {
		out[i] = r
		if r.Conditions != nil {
			out[i].Conditions = append([]RuleCondition(nil), r.Conditions...)
		}
		if r.Actions != nil {
			out[i].Actions = append([]RuleAction(nil), r.Actions...)
		}
	}
	return out
}

func (r FilterRule) match(m Message) bool {
	if !r.Enabled {
		return false
	}
	if len(r.Conditions) == 0 {
		return false
	}
	for _, c := range r.Conditions {
		ok := c.match(m)
		if r.Any && ok && !scopeCondition(c) {
			return r.scopeMatches(m) // one is enough; scope still applies
		}
		if !r.Any && !ok {
			return false
		}
	}
	return !r.Any
}

// scopeCondition reports conditions that say where a rule applies (an
// account, the Inbox) rather than what it looks for: they hold even when
// the rule matches on any one of the others.
func scopeCondition(c RuleCondition) bool {
	f := strings.ToLower(strings.TrimSpace(c.Field))
	return f == "account" || f == "inbox"
}

func (r FilterRule) scopeMatches(m Message) bool {
	for _, c := range r.Conditions {
		if scopeCondition(c) && !c.match(m) {
			return false
		}
	}
	return true
}

func (c RuleCondition) match(m Message) bool {
	field := strings.ToLower(strings.TrimSpace(c.Field))
	op := strings.ToLower(strings.TrimSpace(c.Op))
	if op == "" {
		op = "contains"
	}
	val := c.Value
	switch field {
	case "from":
		return matchText(m.From, op, val)
	case "to":
		return matchText(m.To+" "+m.Cc+" "+m.Bcc, op, val)
	case "subject":
		return matchText(m.Subject, op, val)
	case "body":
		return matchText(m.Body+" "+m.HTML, op, val)
	case "header":
		// A header the message lacks reads as empty: it contains nothing,
		// is nothing.
		return matchText(m.Headers[CanonicalHeader(c.Header)], op, val)
	case "attachment":
		return m.HasAttach
	case "unread":
		return !m.Read
	case "tag":
		return HasTag(m.Tags, val)
	case "account":
		return strings.EqualFold(m.AccountID, val)
	case "inbox":
		// An account's Inbox is "<account>/inbox" (IMAP and POP alike).
		return strings.HasSuffix(strings.ToLower(string(m.Folder)), "/inbox")
	default:
		return false
	}
}

func matchText(s, op, val string) bool {
	switch op {
	case "is", "equals", "eq":
		return strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(val))
	case "isnot":
		return !strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(val))
	case "notcontains":
		return !containsFold(s, val)
	case "begins":
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), strings.ToLower(val))
	case "ends":
		return strings.HasSuffix(strings.ToLower(strings.TrimSpace(s)), strings.ToLower(val))
	default:
		return containsFold(s, val)
	}
}

func applyRuleActions(s ruleHost, msg *Message, actions []RuleAction) (stop bool, err error) {
	for _, a := range actions {
		switch strings.ToLower(a.Type) {
		case "stop":
			return true, nil
		case "tag":
			if a.Tag != "" && !HasTag(msg.Tags, a.Tag) {
				msg.Tags = append(append([]string(nil), msg.Tags...), a.Tag)
			}
		case "markread", "read":
			msg.Read = true
		case "markunread", "unread":
			msg.Read = false
		case "delete":
			if err := s.deleteOne(msg.ID); err != nil {
				return false, err
			}
			return true, nil
		case "star", "flag":
			msg.Starred = true
		case "move":
			dest := a.Folder
			if dest == "" && a.Path != "" {
				f, ok := s.folderByPath(a.Account, a.Path)
				if !ok {
					continue // not synced yet; the next time it is found
				}
				dest = f
			}
			if dest != "" && dest != msg.Folder {
				if err := s.moveOne(msg.ID, dest); err != nil {
					return false, err
				}
				msg.Folder = dest
			}
		}
	}
	return false, nil
}

type ruleHost interface {
	deleteOne(id MessageID) error
	moveOne(id MessageID, dest FolderID) error
	// folderByPath finds an account's folder by its server path,
	// "/"-separated whatever the server's own delimiter.
	folderByPath(account, path string) (FolderID, bool)
	indexOf(id MessageID) (int, bool)
	messageAt(i int) *Message
}

// nextRuleID is an id no rule has. It used to be the count plus one, which
// after a delete named an existing rule — and PutRule then replaced it.
func nextRuleID(existing []FilterRule) string {
	taken := map[string]bool{}
	for _, r := range existing {
		taken[r.ID] = true
	}
	for n := len(existing) + 1; ; n++ {
		if id := fmt.Sprintf("rule-%02d", n); !taken[id] {
			return id
		}
	}
}

func demoRules() []FilterRule {
	return []FilterRule{
		{
			ID: "rule-01", Name: "Tag invoices as Work", Enabled: true,
			Conditions: []RuleCondition{{Field: "subject", Op: "contains", Value: "Invoice"}},
			Actions:    []RuleAction{{Type: "tag", Tag: "Work"}},
		},
		{
			ID: "rule-02", Name: "File bulk as Junk", Enabled: true, Stop: true,
			Conditions: []RuleCondition{{Field: "subject", Op: "contains", Value: "[bulk]"}},
			Actions:    []RuleAction{{Type: "move", Folder: FolderAdaJunk}, {Type: "stop"}},
		},
	}
}

// folderByPath finds account's folder whose server path (its own delimiter
// read as "/", any case) — or, for a folder that has none, its name — is
// path. A local account's imported folders are matched by the end of their
// name ("Thunderbird · Local Folders/Lists" for "Lists").
func folderByPath(folders []Folder, account, path string) (FolderID, bool) {
	want := strings.Trim(path, "/")
	if want == "" {
		return "", false
	}
	for _, f := range folders {
		if f.AccountID != account || f.Virtual {
			continue
		}
		p := f.Remote
		if p == "" {
			p = f.Name
		}
		if f.Delim != "" && f.Delim != "/" {
			p = strings.ReplaceAll(p, f.Delim, "/")
		}
		if strings.EqualFold(p, want) {
			return f.ID, true
		}
		if account == LocalAccountID && strings.HasSuffix(strings.ToLower(f.Name), "/"+strings.ToLower(want)) {
			return f.ID, true
		}
	}
	return "", false
}

// CanonicalHeader is a header name as messages keep it (List-Id).
func CanonicalHeader(name string) string {
	return textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
}

// validHeaderName reports whether name can be a header's name: printable
// ASCII without spaces or colons (RFC 5322), so it goes into an IMAP
// command as it is.
func validHeaderName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= ' ' || c >= 127 || c == ':' || c == '(' || c == ')' || c == '"' || c == '\\' || c == '[' || c == ']' || c == '{' {
			return false
		}
	}
	return true
}

// ruleHeaderNames are the headers the enabled rules test, canonical and
// sorted.
func ruleHeaderNames(rules []FilterRule) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		for _, c := range r.Conditions {
			if !strings.EqualFold(c.Field, "header") || !validHeaderName(strings.TrimSpace(c.Header)) {
				continue
			}
			if n := CanonicalHeader(c.Header); !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// headersOf reads the named headers out of a raw message (or header
// block), values decoded, several of one name joined with ", ". A name the
// message lacks is there, empty: it was looked for.
func headersOf(raw []byte, names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		end = bytes.Index(raw, []byte("\n\n"))
	}
	if end >= 0 {
		raw = raw[:end]
	}
	h, _ := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(append([]byte(nil), raw...), "\r\n\r\n"...)))).ReadMIMEHeader()
	out := map[string]string{}
	for _, n := range names {
		vs := h.Values(n)
		for i := range vs {
			vs[i] = decodeRFC2047(strings.TrimSpace(vs[i]))
		}
		out[n] = strings.Join(vs, ", ")
	}
	return out
}

// hasHeaders reports whether every one of names has been looked for in m.
func hasHeaders(m Message, names []string) bool {
	for _, n := range names {
		if _, ok := m.Headers[n]; !ok {
			return false
		}
	}
	return true
}

func mergeHeaders(into, from map[string]string) map[string]string {
	if into == nil {
		into = map[string]string{}
	}
	for k, v := range from {
		into[k] = v
	}
	return into
}

// checkRule refuses a rule that could not work: a header test without a
// header's name (it goes into an IMAP command as it is).
func checkRule(r FilterRule) error {
	for _, c := range r.Conditions {
		if strings.EqualFold(c.Field, "header") && !validHeaderName(strings.TrimSpace(c.Header)) {
			return fmt.Errorf("%q is not a header's name (a name like List-Id: letters, digits and dashes, no spaces)", c.Header)
		}
	}
	return nil
}
