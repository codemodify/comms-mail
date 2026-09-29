package mailcore

import "strings"

// Filter is the Thunderbird Quick Filter: a query plus Tags-tree pins.
type Filter struct {
	Query       string `json:"query,omitempty"`
	Unread      bool   `json:"unread,omitempty"`
	Starred     bool   `json:"starred,omitempty"`
	Attachment  bool   `json:"attachment,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Sender      bool   `json:"sender,omitempty"`
	Recipients  bool   `json:"recipients,omitempty"`
	SubjectOnly bool   `json:"subjectOnly,omitempty"`
	Body        bool   `json:"body,omitempty"`
}

// Active reports whether any pin or query is on (the list may look empty).
func (f Filter) Active() bool {
	return !filterEmpty(f)
}

// Match reports whether m passes the Quick Filter pins and query.
func (f Filter) Match(m Message) bool { return f.MatchText(m, nil) }

// MatchText is Match with the text test given: inText reports whether the
// query is in m's text (the search index answers it for LocalStore). nil
// looks in m.Body.
func (f Filter) MatchText(m Message, inText func(Message) bool) bool {
	if f.Unread && m.Read {
		return false
	}
	if f.Starred && !m.Starred {
		return false
	}
	if f.Attachment && !m.HasAttach {
		return false
	}
	if f.Tag != "" && !HasTag(m.Tags, f.Tag) {
		return false
	}
	q := strings.TrimSpace(f.Query)
	if q == "" {
		return true
	}
	// Thunderbird searches the checked scopes; default is all headers + body.
	useDefault := !f.Sender && !f.Recipients && !f.SubjectOnly && !f.Body
	if useDefault || f.SubjectOnly {
		if containsFold(m.Subject, q) {
			return true
		}
	}
	if useDefault || f.Sender {
		if containsFold(m.From, q) {
			return true
		}
	}
	if useDefault || f.Recipients {
		if containsFold(m.To, q) || containsFold(m.Cc, q) || containsFold(m.Bcc, q) {
			return true
		}
	}
	if useDefault || f.Body {
		if inText != nil {
			return inText(m)
		}
		if containsFold(m.Body, q) {
			return true
		}
	}
	return false
}

func containsFold(s, q string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(q))
}

// What a message list sorts by: SortMessages's col.
const (
	SortStarred = 0
	SortAttach  = 1
	SortSubject = 2
	SortWho     = 3
	SortDate    = 4
	// SortStatus puts unread first, then forwarded, then replied.
	SortStatus = 5
)

// statusRank orders the status column: what still needs reading, then
// what was passed on, then what was answered.
func statusRank(m Message) int {
	switch {
	case !m.Read:
		return 3
	case m.Forwarded:
		return 2
	case m.Answered:
		return 1
	}
	return 0
}

// SortMessages orders msgs by the given column and direction.
func SortMessages(msgs []Message, col int, asc bool, kind FolderKind) {
	less := func(i, j int) bool {
		a, b := msgs[i], msgs[j]
		var ok bool
		switch col {
		case SortStarred:
			ok = boolLess(a.Starred, b.Starred)
		case SortAttach:
			ok = boolLess(a.HasAttach, b.HasAttach)
		case SortSubject:
			ok = strings.ToLower(a.Subject) < strings.ToLower(b.Subject)
		case SortWho:
			ok = strings.ToLower(a.Correspondent(kind)) < strings.ToLower(b.Correspondent(kind))
		case SortDate:
			ok = a.Date.Before(b.Date)
		case SortStatus:
			ok = statusRank(a) < statusRank(b)
		default:
			ok = a.Date.Before(b.Date)
		}
		if !asc {
			return !ok && !equalCol(a, b, col, kind)
		}
		return ok
	}
	// Stable insertion so screenshot order is deterministic.
	for i := 1; i < len(msgs); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			msgs[j], msgs[j-1] = msgs[j-1], msgs[j]
		}
	}
}

func boolLess(a, b bool) bool { return !a && b }

func equalCol(a, b Message, col int, kind FolderKind) bool {
	switch col {
	case SortStarred:
		return a.Starred == b.Starred
	case SortAttach:
		return a.HasAttach == b.HasAttach
	case SortSubject:
		return a.Subject == b.Subject
	case SortWho:
		return a.Correspondent(kind) == b.Correspondent(kind)
	case SortDate:
		return a.Date.Equal(b.Date)
	case SortStatus:
		return statusRank(a) == statusRank(b)
	default:
		return a.Date.Equal(b.Date)
	}
}
