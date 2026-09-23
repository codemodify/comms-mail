package mail

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
func (f Filter) Match(m Message) bool {
	if f.Unread && m.Read {
		return false
	}
	if f.Starred && !m.Starred {
		return false
	}
	if f.Attachment && !m.HasAttach {
		return false
	}
	if f.Tag != "" && !hasTag(m.Tags, f.Tag) {
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
		if containsFold(m.Body, q) {
			return true
		}
	}
	return false
}

func containsFold(s, q string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(q))
}

func sortMessages(msgs []Message, col int, asc bool, kind FolderKind) {
	less := func(i, j int) bool {
		a, b := msgs[i], msgs[j]
		var ok bool
		switch col {
		case 0:
			ok = boolLess(a.Starred, b.Starred)
		case 1:
			ok = boolLess(a.HasAttach, b.HasAttach)
		case 2:
			ok = strings.ToLower(a.Subject) < strings.ToLower(b.Subject)
		case 3:
			ok = strings.ToLower(a.Correspondent(kind)) < strings.ToLower(b.Correspondent(kind))
		case 4:
			ok = a.Date.Before(b.Date)
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
	case 0:
		return a.Starred == b.Starred
	case 1:
		return a.HasAttach == b.HasAttach
	case 2:
		return a.Subject == b.Subject
	case 3:
		return a.Correspondent(kind) == b.Correspondent(kind)
	case 4:
		return a.Date.Equal(b.Date)
	default:
		return a.Date.Equal(b.Date)
	}
}
