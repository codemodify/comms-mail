package mail

import (
	"strings"
	"unicode"
)

// ThreadIDOf groups a message into a conversation.
// Prefer In-Reply-To / References; otherwise a normalized subject key.
func ThreadIDOf(m Message) string {
	if m.ThreadID != "" {
		return m.ThreadID
	}
	if mid := firstAngle(m.InReplyTo); mid != "" {
		return "mid:" + strings.ToLower(mid)
	}
	if refs := strings.Fields(m.References); len(refs) > 0 {
		if mid := firstAngle(refs[0]); mid != "" {
			return "mid:" + strings.ToLower(mid)
		}
	}
	if mid := firstAngle(m.RFCMessageID); mid != "" {
		return "mid:" + strings.ToLower(mid)
	}
	return "subj:" + normalizeSubject(m.Subject)
}

func firstAngle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			return s[i+1 : i+j]
		}
	}
	if !strings.ContainsAny(s, " \t") {
		return strings.Trim(s, "<>")
	}
	return ""
}

func normalizeSubject(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	for {
		n := s
		if strings.HasPrefix(n, "re:") || strings.HasPrefix(n, "fw:") {
			n = strings.TrimSpace(n[3:])
			s = n
			continue
		}
		if strings.HasPrefix(n, "fwd:") {
			n = strings.TrimSpace(n[4:])
			s = n
			continue
		}
		break
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "empty"
	}
	return b.String()
}

func assignThreadIDs(msgs []Message) {
	byRFC := map[string]int{}
	for i, m := range msgs {
		if mid := firstAngle(m.RFCMessageID); mid != "" {
			byRFC[strings.ToLower(mid)] = i
		}
	}
	parentOf := func(m Message) string {
		if mid := firstAngle(m.InReplyTo); mid != "" {
			return strings.ToLower(mid)
		}
		fields := strings.Fields(m.References)
		if len(fields) == 0 {
			return ""
		}
		return strings.ToLower(firstAngle(fields[0]))
	}
	rootOf := make([]string, len(msgs))
	for i, m := range msgs {
		seen := map[int]bool{}
		cur := i
		for cur >= 0 && !seen[cur] {
			seen[cur] = true
			p := parentOf(msgs[cur])
			if p == "" {
				break
			}
			next, ok := byRFC[p]
			if !ok {
				rootOf[i] = "mid:" + p
				cur = -1
				break
			}
			cur = next
		}
		if cur >= 0 && rootOf[i] == "" {
			if mid := firstAngle(msgs[cur].RFCMessageID); mid != "" {
				rootOf[i] = "mid:" + strings.ToLower(mid)
			} else {
				rootOf[i] = ThreadIDOf(msgs[cur])
			}
		}
		if rootOf[i] == "" {
			rootOf[i] = ThreadIDOf(m)
		}
	}
	for i := range msgs {
		msgs[i].ThreadID = rootOf[i]
	}
}

func groupThreaded(msgs []Message, kind FolderKind, col int, asc bool) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	type grp struct {
		id   string
		msgs []Message
		last Message
	}
	order := []string{}
	by := map[string]*grp{}
	for _, m := range msgs {
		id := m.ThreadID
		if id == "" {
			id = ThreadIDOf(m)
			m.ThreadID = id
		}
		g, ok := by[id]
		if !ok {
			g = &grp{id: id}
			by[id] = g
			order = append(order, id)
		}
		g.msgs = append(g.msgs, m)
		if g.last.ID == "" || m.Date.After(g.last.Date) {
			g.last = m
		}
	}
	heads := make([]Message, 0, len(order))
	for _, id := range order {
		heads = append(heads, by[id].last)
	}
	sortMessages(heads, col, asc, kind)
	var out []Message
	for _, h := range heads {
		id := h.ThreadID
		if id == "" {
			id = ThreadIDOf(h)
		}
		g := by[id]
		sortMessages(g.msgs, 4, true, kind) // oldest first inside thread
		for i, m := range g.msgs {
			if i > 0 {
				m.Subject = "  ↳ " + strings.TrimPrefix(m.Subject, "  ↳ ")
			} else if len(g.msgs) > 1 {
				m.Subject = m.Subject + "  (" + itoa(len(g.msgs)) + ")"
			}
			out = append(out, m)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
