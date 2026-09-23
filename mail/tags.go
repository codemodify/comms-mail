package mail

import (
	"fmt"
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

func tagByName(list []Tag, name string) (Tag, bool) {
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
	if _, ok := tagByName(list, t.Name); ok {
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
	if hasTag(tags, name) {
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
				if def, ok := tagByName(SystemTags(), t.Name); ok {
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

func tagNames(list []Tag) []string {
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
