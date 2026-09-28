package mailcore

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/codemodify/paintengine2d"
)

// System tag names. These are first-class Tags (sidebar + Preferences)
// and cannot be removed. Unread / Attachment are the locked pair the
// product requires; Starred is locked too because it is a peer pin.
const (
	TagUnread     = "Unread"
	TagStarred    = "Starred"
	TagAttachment = "Attachment"
)

// DefaultTags is the single Tags store: locked system pins first,
// then Thunderbird-like colored keywords.
func DefaultTags() []Tag {
	return []Tag{
		{Name: TagUnread, Color: "#e74c3c", System: true},
		{Name: TagStarred, Color: "#f1c40f", System: true},
		{Name: TagAttachment, Color: "#7f8c8d", System: true},
		{Name: "Important", Color: "#c0392b"},
		{Name: "Work", Color: "#d35400"},
		{Name: "Personal", Color: "#27ae60"},
		{Name: "To Do", Color: "#2980b9"},
		{Name: "Later", Color: "#8e44ad"},
	}
}

// SystemTags is the locked prefix of DefaultTags.
func SystemTags() []Tag {
	out := DefaultTags()
	return out[:3]
}

// IsSystemTag reports a locked pin (Unread, Starred, Attachment).
func IsSystemTag(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "unread", "starred", "attachment":
		return true
	default:
		return false
	}
}

func cloneTags(in []Tag) []Tag {
	out := make([]Tag, len(in))
	copy(out, in)
	return out
}

func upsertTag(list []Tag, t Tag) []Tag {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return list
	}
	if t.Color == "" {
		t.Color = "#7f8c8d"
	}
	if IsSystemTag(t.Name) {
		t.System = true
		t.Name = canonicalSystemTagName(t.Name)
	}
	for i, x := range list {
		if strings.EqualFold(x.Name, t.Name) {
			if x.System || IsSystemTag(x.Name) {
				t.System = true
				t.Name = x.Name
			}
			list[i] = t
			return list
		}
	}
	return append(list, t)
}

// TagByName finds a tag by its display name.
func TagByName(list []Tag, name string) (Tag, bool) {
	for _, t := range list {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Tag{}, false
}

func removeTag(list []Tag, name string) ([]Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return list, fmt.Errorf("mail: tag name is required")
	}
	if IsSystemTag(name) {
		return list, fmt.Errorf("mail: cannot remove system tag %s", canonicalSystemTagName(name))
	}
	out := list[:0:0]
	found := false
	for _, t := range list {
		if strings.EqualFold(t.Name, name) {
			if t.System {
				return list, fmt.Errorf("mail: cannot remove system tag %s", t.Name)
			}
			found = true
			continue
		}
		out = append(out, t)
	}
	if !found {
		return list, fmt.Errorf("mail: no tag %s", name)
	}
	return out, nil
}

func renameTag(list []Tag, previous string, t Tag) ([]Tag, error) {
	previous = strings.TrimSpace(previous)
	t.Name = strings.TrimSpace(t.Name)
	if previous == "" || t.Name == "" {
		return list, fmt.Errorf("mail: tag name is required")
	}
	if IsSystemTag(previous) {
		return list, fmt.Errorf("mail: cannot rename system tag %s", canonicalSystemTagName(previous))
	}
	if strings.EqualFold(previous, t.Name) {
		return upsertTag(list, t), nil
	}
	if IsSystemTag(t.Name) {
		return list, fmt.Errorf("mail: %s is a system tag", canonicalSystemTagName(t.Name))
	}
	if _, ok := TagByName(list, t.Name); ok {
		return list, fmt.Errorf("mail: tag %s already exists", t.Name)
	}
	out, err := removeTag(list, previous)
	if err != nil {
		return list, err
	}
	return upsertTag(out, t), nil
}

func replaceTagName(tags []string, old, next string) []string {
	if strings.EqualFold(old, next) || strings.TrimSpace(old) == "" || strings.TrimSpace(next) == "" {
		return tags
	}
	out := make([]string, 0, len(tags))
	seen := false
	for _, x := range tags {
		if strings.EqualFold(x, old) {
			if !seen {
				out = append(out, next)
				seen = true
			}
			continue
		}
		out = append(out, x)
	}
	return out
}

func dropTag(tags []string, name string) []string {
	out := tags[:0:0]
	for _, t := range tags {
		if !strings.EqualFold(t, name) {
			out = append(out, t)
		}
	}
	return out
}

func ensureTag(tags []string, name string) []string {
	if HasTag(tags, name) {
		return tags
	}
	return append(append([]string(nil), tags...), name)
}

func canonicalSystemTagName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "unread":
		return TagUnread
	case "starred":
		return TagStarred
	case "attachment":
		return TagAttachment
	default:
		return strings.TrimSpace(name)
	}
}

// mergeTagStore folds a persisted keyword list into the unified Tags
// model (system pins + user keywords). Old Filters/keyword-only stores
// from v0.18.6 pick up Unread / Starred / Attachment without dropping
// user colors.
func mergeTagStore(existing []Tag) []Tag {
	if len(existing) == 0 {
		return DefaultTags()
	}
	have := map[string]Tag{}
	var user []Tag
	for _, t := range existing {
		t.Name = strings.TrimSpace(t.Name)
		if t.Name == "" {
			continue
		}
		key := strings.ToLower(t.Name)
		if IsSystemTag(t.Name) {
			t.System = true
			t.Name = canonicalSystemTagName(t.Name)
			if t.Color == "" {
				if def, ok := TagByName(SystemTags(), t.Name); ok {
					t.Color = def.Color
				}
			}
			have[key] = t
			continue
		}
		t.System = false
		if _, ok := have[key]; ok {
			continue
		}
		have[key] = t
		user = append(user, t)
	}
	out := make([]Tag, 0, 8)
	for _, sys := range SystemTags() {
		if got, ok := have[strings.ToLower(sys.Name)]; ok {
			out = append(out, got)
			continue
		}
		out = append(out, sys)
	}
	return append(out, user...)
}

// applyAutomaticTags keeps Unread / Starred / Attachment keywords in
// lockstep with the message flags. New mail is Unread; parts with a
// filename get Attachment.
func applyAutomaticTags(m *Message) {
	if m == nil {
		return
	}
	if !m.HasAttach {
		for _, p := range m.Parts {
			if strings.TrimSpace(p.Filename) != "" {
				m.HasAttach = true
				break
			}
		}
		if !m.HasAttach && len(m.Attachments) > 0 {
			m.HasAttach = true
		}
	}
	if m.Read {
		m.Tags = dropTag(m.Tags, TagUnread)
	} else {
		m.Tags = ensureTag(m.Tags, TagUnread)
	}
	if m.Starred {
		m.Tags = ensureTag(m.Tags, TagStarred)
	} else {
		m.Tags = dropTag(m.Tags, TagStarred)
	}
	if m.HasAttach {
		m.Tags = ensureTag(m.Tags, TagAttachment)
	} else {
		m.Tags = dropTag(m.Tags, TagAttachment)
	}
}

func syncSystemTagsFromFlags(m *Message) {
	applyAutomaticTags(m)
}

// TagNames is the display names of the given tag ids.
func TagNames(list []Tag) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Name)
	}
	return out
}

// ParseHexColor accepts #rgb or #rrggbb.
func ParseHexColor(s string) paintengine2d.Color {
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return paintengine2d.RGB(0.5, 0.5, 0.55)
	}
	n := func(a, b byte) float32 {
		return float32(unhex(a)*16+unhex(b)) / 255
	}
	return paintengine2d.RGB(n(s[0], s[1]), n(s[2], s[3]), n(s[4], s[5]))
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c - 'a' + 10)
	case c >= 'A' && c <= 'F':
		return int(c - 'A' + 10)
	}
	return 0
}

// Tags on the server are IMAP keywords. Thunderbird keeps its five default
// tags — the same five comms-mail starts with — as $label1…$label5, so
// those are written and read that way and a tag set in either client shows
// in the other. Any other tag is its name, spaces as underscores.
var thunderbirdLabels = []string{"Important", "Work", "Personal", "To Do", "Later"}

// tagKeyword is the IMAP keyword for a tag.
func tagKeyword(tag string) string {
	for i, n := range thunderbirdLabels {
		if strings.EqualFold(tag, n) {
			return fmt.Sprintf("$label%d", i+1)
		}
	}
	return imapSafeKeyword(tag)
}

// bookkeepingKeyword reports keywords clients set for themselves, which are
// not tags: $Forwarded, $MDNSent, $Junk, NonJunk and the like.
func bookkeepingKeyword(kw string) bool {
	low := strings.ToLower(kw)
	if strings.HasPrefix(low, "$") {
		return !strings.HasPrefix(low, "$label") && low != "$important"
	}
	switch low {
	case "junk", "nonjunk", "notjunk", "forwarded", "redirected", "old":
		return true
	}
	return false
}

// keywordTags turns a message's IMAP flags into its tags: $label1…5 and
// $Important into their names, a keyword a known tag stands for (spaces
// written as underscores, any case) into that tag's name, and other
// clients' bookkeeping keywords into nothing.
func keywordTags(flags []string, known []Tag) []string {
	var out []string
	add := func(t string) {
		if t != "" && !listHasFold(out, t) {
			out = append(out, t)
		}
	}
	for _, f := range flags {
		if f == "" || strings.HasPrefix(f, `\`) || bookkeepingKeyword(f) {
			continue
		}
		low := strings.ToLower(f)
		if strings.HasPrefix(low, "$label") {
			if n, err := strconv.Atoi(low[6:]); err == nil && n >= 1 && n <= len(thunderbirdLabels) {
				add(thunderbirdLabels[n-1])
			}
			continue
		}
		if low == "$important" {
			add("Important")
			continue
		}
		name := f
		for _, t := range known {
			if strings.EqualFold(t.Name, f) || strings.EqualFold(imapSafeKeyword(t.Name), f) {
				name = t.Name
				break
			}
		}
		add(name)
	}
	return out
}

func listHasFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// mergeServerTags applies to m the tag changes the server made since it
// last looked — additions and removals between m.Keywords and server —
// leaving tags that exist only here alone, and records server as seen.
func mergeServerTags(m *Message, server []string) {
	var local []string
	for _, t := range m.Tags {
		if IsSystemTag(t) || bookkeepingKeyword(t) {
			continue
		}
		local = append(local, t)
	}
	for _, t := range server {
		if !listHasFold(m.Keywords, t) && !listHasFold(local, t) {
			local = append(local, t) // added elsewhere
		}
	}
	var kept []string
	for _, t := range local {
		if listHasFold(m.Keywords, t) && !listHasFold(server, t) {
			continue // removed elsewhere
		}
		kept = append(kept, t)
	}
	m.Tags = kept
	if len(server) > 0 {
		m.Keywords = append([]string(nil), server...)
	} else {
		m.Keywords = nil
	}
	applyAutomaticTags(m)
}

// normalizeTagsLocked drops other clients' bookkeeping keywords that older
// syncs made into tags, and names $label1…5 as their tags. It reports
// whether anything changed.
func normalizeTags(m *Message) bool {
	changed := false
	var out []string
	for _, t := range m.Tags {
		switch {
		case bookkeepingKeyword(t):
			changed = true
		case strings.HasPrefix(strings.ToLower(t), "$label"):
			changed = true
			for _, n := range keywordTags([]string{t}, nil) {
				if !listHasFold(out, n) {
					out = append(out, n)
				}
			}
		default:
			out = append(out, t)
		}
	}
	if changed {
		m.Tags = out
	}
	return changed
}
