package mailcore

import (
	"strings"
	"unicode/utf8"
)

// Search looks for the query as Filter.Match does — a case-insensitive
// substring of the subject, the addresses or the text. A message's text is
// its display text (DisplayBody), so words in HTML-only mail are found too.
//
// LocalStore answers the text part from a full-text index in mail.db
// (sqlstore.go), kept as messages are saved; the rest is a scan of the
// headers in memory, which is cheap. MemoryStore scans.

// minIndexedQuery is the shortest query the index answers: it holds
// trigrams, so one or two characters would need a scan of every text.
const minIndexedQuery = 3

func searchMessages(all []Message, q SearchQuery, inText func(Message) bool) []Message {
	var out []Message
	for _, m := range all {
		if q.AccountID != "" && m.AccountID != q.AccountID {
			continue
		}
		if q.Filter.MatchText(m, inText) {
			out = append(out, m.Clone())
		}
	}
	return out
}

// textMatcherLocked answers "is q in the text of m" for a search: from the
// index for the messages it holds, by reading the text of those not yet
// saved to it. A query too short for the index looks in the text part only
// (m.Body), as before there was an index, so a first keystroke never makes
// every HTML message be rendered to text. The caller holds s.mu.
func (s *LocalStore) textMatcherLocked(q string) func(Message) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	if utf8.RuneCountInString(q) < minIndexedQuery {
		return func(m Message) bool { return containsFold(m.Body, q) }
	}
	c := s.sqlc
	var hits map[MessageID]bool
	if c != nil {
		var err error
		if hits, err = c.textHits(q); err != nil {
			c = nil // no index: read every text instead
		}
	}
	return func(m Message) bool {
		if hits[m.ID] {
			return true
		}
		if c != nil && c.text[m.ID] == textStamp(&m) {
			return false // indexed, and the index says no
		}
		return messageHasBody(m) && containsFold(DisplayBody(m), q)
	}
}
