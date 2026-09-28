package mailcore

import (
	"sort"
	"strings"
	"time"
)

// Contact is one person the address book knows: an address, the best display
// name seen for it, how often it has appeared, and when it was last seen.
// The book is built from the messages already in the cache — everyone you
// have corresponded with — so recipient fields can complete as you type
// without a server round trip or a separate contacts store.
type Contact struct {
	Name    string    `json:"name,omitempty"`
	Address string    `json:"address"`
	Count   int       `json:"count"`
	Last    time.Time `json:"last"`
}

// Display is the contact as it goes into a recipient field: `Name <addr>`,
// or the bare address when there is no name.
func (c Contact) Display() string {
	if strings.TrimSpace(c.Name) == "" {
		return c.Address
	}
	return c.Name + " <" + c.Address + ">"
}

// buildContacts collects an address book from msgs. From, To and Cc all
// count, so both the people who write to you and the people you write to are
// suggested; the best (non-empty) name seen for an address is kept, and its
// most recent date.
func buildContacts(msgs []Message) []Contact {
	by := map[string]*Contact{}
	note := func(field string, when time.Time) {
		for _, a := range ParseAddrList(field) {
			addr := strings.ToLower(strings.TrimSpace(a.Address))
			if addr == "" || !strings.Contains(addr, "@") {
				continue
			}
			c := by[addr]
			if c == nil {
				c = &Contact{Address: strings.TrimSpace(a.Address)}
				by[addr] = c
			}
			c.Count++
			if name := strings.TrimSpace(a.Name); name != "" && c.Name == "" {
				c.Name = name
			}
			if when.After(c.Last) {
				c.Last = when
			}
		}
	}
	for _, m := range msgs {
		note(m.From, m.Date)
		note(m.To, m.Date)
		note(m.Cc, m.Date)
	}
	out := make([]Contact, 0, len(by))
	for _, c := range by {
		out = append(out, *c)
	}
	sortContacts(out)
	return out
}

// sortContacts ranks the book: most seen first, then most recent, then by
// address so the order is stable.
func sortContacts(cs []Contact) {
	sort.Slice(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if !a.Last.Equal(b.Last) {
			return a.Last.After(b.Last)
		}
		return strings.ToLower(a.Address) < strings.ToLower(b.Address)
	})
}

// suggestContacts returns up to limit contacts matching query, best first.
// An empty query returns the most-corresponded-with. A match is a
// case-insensitive substring of the address or the name; a match at the
// start of the address, the name, or a word of the name ranks above one in
// the middle, so typing "jo" offers John before someone whose address
// merely contains "jo".
func suggestContacts(book []Contact, query string, limit int) []Contact {
	q := strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = 8
	}
	if q == "" {
		if len(book) > limit {
			return append([]Contact(nil), book[:limit]...)
		}
		return append([]Contact(nil), book...)
	}
	type scored struct {
		c    Contact
		rank int // 0 best
	}
	var hits []scored
	for _, c := range book {
		addr := strings.ToLower(c.Address)
		name := strings.ToLower(c.Name)
		rank := -1
		switch {
		case strings.HasPrefix(addr, q) || wordPrefix(name, q):
			rank = 0
		case strings.HasPrefix(name, q):
			rank = 1
		case strings.Contains(addr, q) || strings.Contains(name, q):
			rank = 2
		}
		if rank >= 0 {
			hits = append(hits, scored{c, rank})
		}
	}
	// Stable: book is already ranked, so equal-rank hits keep that order.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	out := make([]Contact, 0, limit)
	for _, h := range hits {
		out = append(out, h.c)
		if len(out) == limit {
			break
		}
	}
	return out
}

// wordPrefix reports whether any whitespace-separated word of s starts with
// prefix, so "jane" matches "Mary Jane Watson".
func wordPrefix(s, prefix string) bool {
	if prefix == "" {
		return false
	}
	for _, w := range strings.Fields(s) {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}

// contactsFromAddrs is a tiny address book from raw header values, for a
// store that has no message slice of its own to build from.
func contactsFromAddrs(values ...string) []Contact {
	msgs := make([]Message, 0, len(values))
	for _, v := range values {
		msgs = append(msgs, Message{From: v})
	}
	return buildContacts(msgs)
}
