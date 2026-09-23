package mail

import "strings"

// Synthetic account / folder ids for virtual views.
const (
	AccountUnified = "unified"
	AccountTags    = "tags"

	FolderUnifiedInbox   FolderID = "virtual/inbox"
	FolderUnifiedUnread  FolderID = "virtual/unread"
	FolderUnifiedStarred FolderID = "virtual/starred"
)

// IsVirtual reports Unified / tag / smart / category folder ids.
func IsVirtual(id FolderID) bool {
	s := string(id)
	return strings.HasPrefix(s, "virtual/") || strings.HasPrefix(s, "tag/") || strings.HasPrefix(s, "smart/")
}

// HiddenFromFolderTree is Unified Inbox/Unread/Starred, Smart Folders,
// Categories, and the VIP smart folder — those are no longer in the folder
// tree. Outbox and tag views stay. VIP sender APIs are unchanged.
func HiddenFromFolderTree(id FolderID) bool {
	switch id {
	case FolderUnifiedInbox, FolderUnifiedUnread, FolderUnifiedStarred, FolderVIP:
		return true
	}
	if SmartFolderID(id) != "" {
		return true
	}
	return categoryFromFolder(id) != ""
}

// TagFolderID is the virtual folder for a named tag.
func TagFolderID(name string) FolderID {
	return FolderID("tag/" + name)
}

// TagNameFromFolder returns the tag for tag/<name> folders.
func TagNameFromFolder(id FolderID) string {
	s := string(id)
	if strings.HasPrefix(s, "tag/") {
		return s[len("tag/"):]
	}
	return ""
}

func defaultVirtualFolders() []Folder {
	base := []Folder{
		{ID: FolderUnifiedInbox, AccountID: AccountUnified, Name: "Unified Inbox", Kind: FolderInbox, Virtual: true, MatchKind: FolderInbox},
		{ID: FolderUnifiedUnread, AccountID: AccountUnified, Name: "Unread", Kind: FolderCustom, Virtual: true},
		{ID: FolderUnifiedStarred, AccountID: AccountUnified, Name: "Starred", Kind: FolderCustom, Virtual: true},
	}
	return append(base, extraVirtualFolders(featureSnap{})...)
}

func virtualFolderByID(id FolderID) (Folder, bool) {
	if name := TagNameFromFolder(id); name != "" {
		return Folder{ID: id, AccountID: AccountTags, Name: name, Kind: FolderCustom, Virtual: true, Tag: name}, true
	}
	if sid := SmartFolderID(id); sid != "" {
		return Folder{ID: id, AccountID: AccountSmart, Name: sid, Kind: FolderCustom, Virtual: true}, true
	}
	for _, f := range defaultVirtualFolders() {
		if f.ID == id {
			return f, true
		}
	}
	return Folder{}, false
}

func matchVirtual(id FolderID, m Message, folder Folder, kind FolderKind, snap featureSnap) bool {
	switch id {
	case FolderUnifiedInbox:
		return kind == FolderInbox && !folder.Virtual
	case FolderUnifiedUnread:
		return !m.Read && !snap.muted[m.ThreadID]
	case FolderUnifiedStarred:
		return m.Starred
	case FolderVIP:
		return isVIPAddr(m.From, snap.vips)
	case FolderOutbox:
		for _, op := range snap.outbox {
			if op.Kind == "send" && (op.MessageID == m.ID || (op.Message != nil && op.Message.ID == m.ID)) {
				return true
			}
		}
		return false
	}
	if cat := categoryFromFolder(id); cat != "" {
		got := m.Category
		if got == "" {
			got = messageCategory(m, snap)
		}
		return got == cat
	}
	if sid := SmartFolderID(id); sid != "" {
		for _, sf := range snap.smart {
			if sf.ID != sid {
				continue
			}
			if sf.AccountID != "" && m.AccountID != sf.AccountID {
				return false
			}
			if sf.FolderID != "" && m.Folder != sf.FolderID {
				return false
			}
			return sf.Filter.Match(m)
		}
		return false
	}
	if tag := TagNameFromFolder(id); tag != "" {
		if IsSystemTag(tag) {
			switch strings.ToLower(tag) {
			case "unread":
				return !m.Read || hasTag(m.Tags, TagUnread)
			case "starred":
				return m.Starred || hasTag(m.Tags, TagStarred)
			case "attachment":
				return m.HasAttach || hasTag(m.Tags, TagAttachment)
			}
		}
		return hasTag(m.Tags, tag)
	}
	return false
}
