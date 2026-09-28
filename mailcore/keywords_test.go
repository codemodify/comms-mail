package mailcore

import (
	"strings"
	"testing"
)

func TestKeywordTags(t *testing.T) {
	known := []Tag{{Name: "To Do"}, {Name: "Client A"}}
	got := keywordTags([]string{`\Seen`, "$label1", "$label4", "$Forwarded", "NonJunk", "$MDNSent", "Client_A", "$Important", "custom"}, known)
	if strings.Join(got, "|") != "Important|To Do|Client A|custom" {
		t.Fatalf("tags %q", got)
	}
	for tag, kw := range map[string]string{"Important": "$label1", "later": "$label5", "Client A": "Client_A"} {
		if got := tagKeyword(tag); got != kw {
			t.Fatalf("%s → %s, want %s", tag, got, kw)
		}
	}
}

// A tag change made in another client arrives; a tag that exists only
// here (a filter rule's) is left alone.
func TestMergeServerTags(t *testing.T) {
	m := Message{Read: true, Tags: []string{"Work", "Rule tag"}, Keywords: []string{"Work"}}
	mergeServerTags(&m, []string{"Work", "Important"}) // Important added elsewhere
	if strings.Join(m.Tags, "|") != "Work|Rule tag|Important" {
		t.Fatalf("after an addition %q", m.Tags)
	}
	mergeServerTags(&m, []string{"Important"}) // Work removed elsewhere
	if strings.Join(m.Tags, "|") != "Rule tag|Important" || strings.Join(m.Keywords, "|") != "Important" {
		t.Fatalf("after a removal %q (keywords %q)", m.Tags, m.Keywords)
	}
	// A server without keywords never had any: nothing here is removed.
	n := Message{Tags: []string{"Local"}}
	mergeServerTags(&n, nil)
	if !HasTag(n.Tags, "Local") || !HasTag(n.Tags, TagUnread) {
		t.Fatalf("keywordless server %q", n.Tags)
	}
}

func TestNormalizeTags(t *testing.T) {
	m := Message{Tags: []string{"$Forwarded", "$label2", "NonJunk", "Mine"}}
	if !normalizeTags(&m) || strings.Join(m.Tags, "|") != "Work|Mine" {
		t.Fatalf("normalized %q", m.Tags)
	}
	if normalizeTags(&m) {
		t.Fatal("a second pass changed something")
	}
}

// Tagged in Thunderbird on another machine ($label2 is Work), the message
// shows the tag here after a sync; untagged there, it goes — while a tag
// only this machine has stays.
func TestTagsFromAnotherClientSyncDown(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1}})
	fs.flags = map[uint32]string{1: `\Seen $Forwarded`}
	st := newFolderStore(t, fs)
	id := MessageID("home/inbox:1")
	if m, _ := st.CachedMessage(id); len(m.Tags) != 0 {
		t.Fatalf("a bookkeeping keyword became a tag: %q", m.Tags)
	}
	st.mu.Lock()
	i, _ := st.indexLocked(id)
	st.Messages[i].Tags = append(st.Messages[i].Tags, "Mine") // a rule's, say
	st.mu.Unlock()

	fs.mu.Lock()
	fs.flags[1] = `\Seen $Forwarded $label2`
	fs.mu.Unlock()
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	m, _ := st.CachedMessage(id)
	if !HasTag(m.Tags, "Work") || !HasTag(m.Tags, "Mine") {
		t.Fatalf("after tagging elsewhere %q", m.Tags)
	}
	fs.mu.Lock()
	fs.flags[1] = `\Seen`
	fs.mu.Unlock()
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	m, _ = st.CachedMessage(id)
	if HasTag(m.Tags, "Work") || !HasTag(m.Tags, "Mine") {
		t.Fatalf("after untagging elsewhere %q", m.Tags)
	}
}

// Mail another client marked deleted leaves the lists here; undeleted
// before it is expunged, it comes back.
func TestDeletedElsewhereHidesAndUndeleteReturns(t *testing.T) {
	fs := newFolderServer(t, "/", map[string][]uint32{"INBOX": {1, 2}})
	st := newFolderStore(t, fs)
	if n := len(st.ListMessages("home/inbox")); n != 2 {
		t.Fatalf("start %d", n)
	}
	fs.mu.Lock()
	fs.flags = map[uint32]string{2: `\Seen \Deleted`}
	fs.mu.Unlock()
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.CachedMessage("home/inbox:2"); ok {
		t.Fatal("a message marked deleted is still listed")
	}
	fs.mu.Lock()
	fs.flags[2] = `\Seen`
	fs.mu.Unlock()
	if _, err := st.Sync("home"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.CachedMessage("home/inbox:2"); !ok {
		t.Fatal("an undeleted message did not come back")
	}
}
