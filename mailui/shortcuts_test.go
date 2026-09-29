package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

// S stars the message and S again takes the star off; J moves it to Junk.
func TestStarAndJunkKeys(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	var id mailcore.MessageID
	for _, m := range s.rows {
		if !m.Starred {
			id = m.ID
			break
		}
	}
	if id == "" {
		t.Fatal("no unstarred message")
	}
	s.selected = []mailcore.MessageID{id}
	starred := func() bool {
		m, _, _ := s.cli.GetMessage(id)
		return m.Starred
	}
	press(s, platform.KeyS, 0)
	s.waitIdle()
	if !starred() {
		t.Fatal("S did not star the message")
	}
	press(s, platform.KeyS, 0)
	s.waitIdle()
	if starred() {
		t.Fatal("S again did not take the star off")
	}

	junk, ok := mailcore.SpecialFolderClient(s.cli, s.accountID(), mailcore.FolderJunk)
	if !ok {
		t.Fatal("no Junk folder")
	}
	inJunk := func() int {
		msgs, _ := s.cli.ListMessages(junk.ID, mailcore.Filter{})
		return len(msgs)
	}
	before := inJunk()
	press(s, platform.KeyJ, 0)
	s.commitUndoNow()
	s.waitIdle()
	for _, m := range s.rows {
		if m.ID == id {
			t.Fatal("J left the message in the list")
		}
	}
	if inJunk() != before+1 {
		t.Fatalf("Junk has %d, had %d", inJunk(), before)
	}
}

// The message menu has no View Source (the Source tab is there), carries
// the keys S and J, and gives its rows icons.
func TestMessageMenu(t *testing.T) {
	s, _, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	s.selected = []mailcore.MessageID{s.rows[0].ID}
	s.messageMenu(s.table, paintengine2d.Pt(10, 10))
	pop, ok := w.Popup().(*widgets.PopupMenu)
	if !ok {
		t.Fatal("no menu")
	}
	byText := map[string]*widgets.MenuItem{}
	icons := 0
	for _, it := range pop.Items {
		if it == nil || it.Separator {
			continue
		}
		byText[it.Text] = it
		if it.Icon != style.IconNone {
			icons++
		}
	}
	if byText["View Source"] != nil {
		t.Fatal("View Source is still in the menu")
	}
	star := byText["Star"]
	if star == nil {
		star = byText["Unstar"]
	}
	if star == nil || star.Shortcut != "S" || star.Icon != style.IconStar {
		t.Fatalf("star row %+v", star)
	}
	if j := byText["Junk"]; j == nil || j.Shortcut != "J" {
		t.Fatalf("junk row %+v", j)
	}
	if d := byText["Delete"]; d == nil || d.Icon == style.IconCut {
		t.Fatal("Delete is shown with scissors")
	}
	if icons < 8 {
		t.Fatalf("only %d rows have icons", icons)
	}
}
