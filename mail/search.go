package mail

import (
	"strings"
	"unicode"
)

// searchIndex is a process-local inverted index (subject/from/to/body).
// Fast enough for dogfood + typical personal caches; rebuild is cheap.
type searchIndex struct {
	tokens map[string][]MessageID
}

func newSearchIndex() *searchIndex {
	return &searchIndex{tokens: map[string][]MessageID{}}
}

func (idx *searchIndex) rebuild(msgs []Message) {
	if idx == nil {
		return
	}
	idx.tokens = map[string][]MessageID{}
	for _, m := range msgs {
		idx.add(m)
	}
}

func (idx *searchIndex) add(m Message) {
	if idx == nil || m.ID == "" {
		return
	}
	idx.remove(m.ID)
	seen := map[string]bool{}
	for _, tok := range tokenizeMail(m) {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		idx.tokens[tok] = append(idx.tokens[tok], m.ID)
	}
}

func (idx *searchIndex) remove(id MessageID) {
	if idx == nil {
		return
	}
	for tok, ids := range idx.tokens {
		out := ids[:0]
		for _, x := range ids {
			if x != id {
				out = append(out, x)
			}
		}
		if len(out) == 0 {
			delete(idx.tokens, tok)
		} else {
			idx.tokens[tok] = out
		}
	}
}

// query returns candidate ids (AND of tokens). nil means "no query / scan all".
func (idx *searchIndex) query(q string) []MessageID {
	if idx == nil {
		return nil
	}
	toks := tokenize(q)
	if len(toks) == 0 {
		return nil
	}
	var hit []MessageID
	for i, tok := range toks {
		ids := idx.tokens[tok]
		if i == 0 {
			hit = append([]MessageID(nil), ids...)
			continue
		}
		set := map[MessageID]bool{}
		for _, id := range ids {
			set[id] = true
		}
		out := hit[:0]
		for _, id := range hit {
			if set[id] {
				out = append(out, id)
			}
		}
		hit = out
	}
	return hit
}

func tokenizeMail(m Message) []string {
	var b strings.Builder
	b.WriteString(m.Subject)
	b.WriteByte(' ')
	b.WriteString(m.From)
	b.WriteByte(' ')
	b.WriteString(m.To)
	b.WriteByte(' ')
	b.WriteString(m.Cc)
	b.WriteByte(' ')
	body := m.Body
	if len(body) > 8192 {
		body = body[:8192]
	}
	b.WriteString(body)
	return tokenize(b.String())
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() < 2 {
			cur.Reset()
			return
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func searchMessages(all []Message, q SearchQuery, idx *searchIndex) []Message {
	var cand []Message
	if strings.TrimSpace(q.Filter.Query) != "" && idx != nil {
		ids := idx.query(q.Filter.Query)
		if ids != nil {
			set := map[MessageID]bool{}
			for _, id := range ids {
				set[id] = true
			}
			for _, m := range all {
				if set[m.ID] {
					cand = append(cand, m)
				}
			}
		} else {
			cand = all
		}
	} else {
		cand = all
	}
	var out []Message
	for _, m := range cand {
		if q.AccountID != "" && m.AccountID != q.AccountID {
			continue
		}
		if q.Filter.Match(m) {
			out = append(out, m.Clone())
		}
	}
	return out
}
