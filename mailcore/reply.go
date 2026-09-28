package mailcore

import (
	"net/mail"
	"strings"
)

// ParseAddrList splits an address header into its addresses, keeping
// display names. It uses the RFC 5322 grammar, so a quoted name with a comma
// in it ("Doe, Jane" <jane@example.com>) stays one address; a value the
// grammar rejects falls back to splitting on commas.
func ParseAddrList(s string) []*mail.Address {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if list, err := mail.ParseAddressList(s); err == nil {
		return list
	}
	var out []*mail.Address
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if a, err := mail.ParseAddress(part); err == nil {
			out = append(out, a)
			continue
		}
		if addr := ExtractAddr(part); addr != "" {
			out = append(out, &mail.Address{Address: addr})
		}
	}
	return out
}

// FormatAddr is an address as a person would type it: `Name <addr>`, the
// name quoted when it holds a character the header grammar reserves.
// Unlike mail.Address.String it does not RFC 2047-encode the name; the
// message builder does that when the header is written.
func FormatAddr(a *mail.Address) string {
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return a.Address
	}
	if strings.ContainsAny(name, `,;:<>()[]@"\`) {
		name = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	return name + " <" + a.Address + ">"
}

// ReplyAllRecipients is who a Reply All goes to: the sender (or Reply-To)
// and everyone else on To, with the original Cc kept as Cc. The writer's
// own addresses (self) and duplicates are left out.
func ReplyAllRecipients(m Message, self []string) (to, cc string) {
	seen := map[string]bool{}
	for _, a := range self {
		if a = strings.ToLower(ExtractAddr(a)); a != "" {
			seen[a] = true
		}
	}
	take := func(list []*mail.Address) []string {
		var out []string
		for _, a := range list {
			key := strings.ToLower(strings.TrimSpace(a.Address))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, FormatAddr(a))
		}
		return out
	}
	first := m.ReplyTo
	if strings.TrimSpace(first) == "" {
		first = m.From
	}
	toList := take(ParseAddrList(first))
	toList = append(toList, take(ParseAddrList(m.To))...)
	ccList := take(ParseAddrList(m.Cc))
	return strings.Join(toList, ", "), strings.Join(ccList, ", ")
}
