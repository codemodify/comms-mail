package mailcore

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// state is how an imported message came out: read, starred, or not there.
type state struct{ read, starred, present bool }

// importState imports st and reports each message's state by subject.
func importState(t *testing.T, st LocalMailStore) map[string]state {
	t.Helper()
	ls := newImportStore(t)
	if _, err := ls.ImportLocalMail([]LocalMailStore{st}); err != nil {
		t.Fatal(err)
	}
	out := map[string]state{}
	for _, f := range ls.ListFolders(LocalAccountID) {
		for _, m := range ls.ListMessages(f.ID) {
			out[m.Subject] = state{read: m.Read, starred: m.Starred, present: true}
			if HasTag(m.Tags, TagUnread) == m.Read {
				t.Fatalf("%s: Unread tag disagrees with the flag", m.Subject)
			}
		}
	}
	return out
}

func wantStates(t *testing.T, got map[string]state, want map[string]state) {
	t.Helper()
	for subj, w := range want {
		if got[subj] != w {
			t.Fatalf("%s: got %+v, want %+v (all: %+v)", subj, got[subj], w, got)
		}
	}
}

func TestHeaderFlags(t *testing.T) {
	for _, c := range []struct {
		head string
		want mailFlags
	}{
		{"X-Mozilla-Status: 0005\n", mailFlags{known: true, read: true, starred: true}},
		{"X-Mozilla-Status: 0000\nStatus: RO\n", mailFlags{known: true}}, // Thunderbird's own wins
		{"X-Mozilla-Status: 0009\n", mailFlags{known: true, read: true, deleted: true}},
		{"Status: RO\n", mailFlags{known: true, read: true}},
		{"Status: O\n", mailFlags{known: true}}, // old, not read
		{"Status: O\nX-Status: F\n", mailFlags{known: true, starred: true}},
		{"X-Status: AD\n", mailFlags{known: true, deleted: true}},
		{"Subject: none\n", mailFlags{}},
	} {
		raw := "From: a@ex\n" + c.head + "\nStatus: R\n" // a body line never counts
		if got := headerFlags([]byte(raw)); got != c.want {
			t.Fatalf("%q: got %+v want %+v", c.head, got, c.want)
		}
	}
}

func TestImportKeepsMboxState(t *testing.T) {
	dir := t.TempDir()
	mbox := "From - Mon Jan  1 00:00:00 2024\n" + "From: a@ex\nSubject: seen\nMessage-ID: <1@ex>\nStatus: RO\n\nb\n\n" +
		"From - Mon Jan  1 00:00:00 2024\n" + "From: a@ex\nSubject: new\nMessage-ID: <2@ex>\nStatus: O\nX-Status: F\n\nb\n\n" +
		"From - Mon Jan  1 00:00:00 2024\n" + "From: a@ex\nSubject: gone\nMessage-ID: <3@ex>\nX-Mozilla-Status: 0009\n\nb\n\n" +
		"From - Mon Jan  1 00:00:00 2024\n" + "From: a@ex\nSubject: plain\nMessage-ID: <4@ex>\n\nb\n"
	p := filepath.Join(dir, "Inbox")
	mustWrite(t, p, mbox)
	got := importState(t, LocalMailStore{Source: "Test", Name: "Inbox", Path: p, Kind: StoreMbox})
	wantStates(t, got, map[string]state{
		"seen":  {read: true, present: true},
		"new":   {starred: true, present: true},
		"gone":  {},                          // expunged in Thunderbird: not imported
		"plain": {read: true, present: true}, // nothing recorded: old mail, read
	})
}

func TestImportKeepsMaildirState(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "cur", "1.host:2,S"), msg("1@ex", "seen"))
	mustWrite(t, filepath.Join(dir, "cur", "2.host:2,FS"), msg("2@ex", "starred"))
	mustWrite(t, filepath.Join(dir, "cur", "3.host:2,"), msg("3@ex", "unread"))
	mustWrite(t, filepath.Join(dir, "cur", "4.host!2,ST"), msg("4@ex", "trashed"))
	mustWrite(t, filepath.Join(dir, "new", "5.host"), msg("5@ex", "fresh"))
	_ = os.MkdirAll(filepath.Join(dir, "tmp"), 0o700)
	got := importState(t, LocalMailStore{Source: "Test", Name: "Inbox", Path: dir, Kind: StoreMaildir})
	wantStates(t, got, map[string]state{
		"seen":    {read: true, present: true},
		"starred": {read: true, starred: true, present: true},
		"unread":  {present: true},
		"trashed": {},
		"fresh":   {present: true},
	})
}

// Claws Mail keeps an MH folder's state in .claws_mark: little-endian
// version 2, then (number, flags) records; a message with none is new.
func TestImportKeepsClawsMarks(t *testing.T) {
	dir := t.TempDir()
	for i, subj := range []string{"read", "unread", "starred", "deleted", "unmarked"} {
		mustWrite(t, filepath.Join(dir, itoa(i+1)), msg(itoa(i+1)+"@ex", subj))
	}
	mark := binary.LittleEndian.AppendUint32(nil, 2)
	for _, r := range [][2]uint32{{1, 0}, {2, clawsNew | clawsUnread}, {3, clawsMarked}, {4, clawsDeleted}} {
		mark = binary.LittleEndian.AppendUint32(mark, r[0])
		mark = binary.LittleEndian.AppendUint32(mark, r[1])
	}
	if err := os.WriteFile(filepath.Join(dir, ".claws_mark"), mark, 0o600); err != nil {
		t.Fatal(err)
	}
	got := importState(t, LocalMailStore{Source: "Claws Mail", Name: "inbox", Path: dir, Kind: StoreMH})
	wantStates(t, got, map[string]state{
		"read":     {read: true, present: true},
		"unread":   {present: true},
		"starred":  {read: true, starred: true, present: true},
		"deleted":  {},
		"unmarked": {present: true},
	})
}

func TestImportKeepsMHSequences(t *testing.T) {
	dir := t.TempDir()
	for i, subj := range []string{"one", "two", "three", "four"} {
		mustWrite(t, filepath.Join(dir, itoa(i+1)), msg(itoa(i+1)+"@ex", subj))
	}
	mustWrite(t, filepath.Join(dir, ".mh_sequences"), "cur: 4\nunseen: 2-3\nflagged: 1\n")
	got := importState(t, LocalMailStore{Source: "nmh", Name: "inbox", Path: dir, Kind: StoreMH})
	wantStates(t, got, map[string]state{
		"one":   {read: true, starred: true, present: true},
		"two":   {present: true},
		"three": {present: true},
		"four":  {read: true, present: true},
	})
}

// An Apple Mail .emlx keeps its state in the trailing plist's flags: bit
// 0 read, 1 deleted, 4 flagged.
func TestImportKeepsEMLXFlags(t *testing.T) {
	dir := t.TempDir()
	emlx := func(subj string, flags int) string {
		m := msg(subj+"@ex", subj)
		return itoa(len(m)) + "\n" + m + `<?xml version="1.0"?><plist version="1.0"><dict>` +
			"<key>date-sent</key><real>1.7e9</real>\n<key>flags</key>\n\t<integer>" + itoa(flags) + "</integer></dict></plist>"
	}
	box := filepath.Join(dir, "INBOX.mbox", "Messages")
	mustWrite(t, filepath.Join(box, "1.emlx"), emlx("readstar", 1|16))
	mustWrite(t, filepath.Join(box, "2.emlx"), emlx("unread", 0))
	mustWrite(t, filepath.Join(box, "3.emlx"), emlx("deleted", 1|2))
	got := importState(t, LocalMailStore{Source: "Apple Mail", Name: "INBOX", Path: filepath.Join(dir, "INBOX.mbox"), Kind: StoreEMLX})
	wantStates(t, got, map[string]state{
		"readstar": {read: true, starred: true, present: true},
		"unread":   {present: true},
		"deleted":  {},
	})
}
