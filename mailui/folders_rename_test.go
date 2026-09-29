package mailui

import (
	"strings"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// promptField is the text field of the prompt on w's overlay layer, or
// nil when there is no overlay or it has no field (a warning).
func promptField(w *app.Window) *widgets.TextField {
	var f *widgets.TextField
	if o := w.Overlay(); o != nil {
		widget.Walk(o, func(c widget.Component) {
			if v, ok := c.(*widgets.TextField); ok {
				f = v
			}
		})
	}
	return f
}

// pressPrimary presses the default button of what is on w's overlay.
func pressPrimary(t *testing.T, a *app.Application, w *app.Window) {
	t.Helper()
	var ok *widgets.Button
	widget.Walk(w.Overlay(), func(c widget.Component) {
		if b, isB := c.(*widgets.Button); isB && b.Primary {
			ok = b
		}
	})
	if ok == nil {
		t.Fatal("the overlay has no default button")
	}
	ok.OnClick()
	for i := 0; i < 5; i++ {
		a.PumpOnce()
	}
}

// fillAndSubmit opens the prompt, types text into it and presses OK.
func fillAndSubmit(t *testing.T, a *app.Application, w *app.Window, open func(), text string) {
	t.Helper()
	open()
	a.PumpOnce()
	f := promptField(w)
	if f == nil {
		t.Fatal("no prompt with a field over the window")
	}
	f.SetText(text)
	pressPrimary(t, a, w)
}

// Rename Folder… asks for the name and renames the folder in place; New
// Subfolder… makes one inside the folder it was chosen on.
func TestRenameAndSubfolderFromTheFolderMenu(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	proj := mailcore.Folder{ID: mailcore.FolderAdaProjects, AccountID: mailcore.AcctAda, Name: "Projects", Kind: mailcore.FolderCustom}

	fillAndSubmit(t, a, w, func() { s.renameFolder(proj) }, "Clients 2026")
	f, ok, _ := s.cli.GetFolder(mailcore.FolderAdaProjects)
	if !ok || f.Name != "Clients 2026" || w.Overlay() != nil {
		t.Fatalf("after rename: %+v overlay=%v", f, w.Overlay())
	}

	fillAndSubmit(t, a, w, func() { s.newSubfolder(f) }, "Acme")
	folders, _ := s.cli.ListFolders(mailcore.AcctAda)
	found := false
	for _, x := range folders {
		if x.Name == "Acme" && x.Parent == f.ID {
			found = true
		}
	}
	if !found || s.folder == mailcore.FolderAdaProjects {
		t.Fatalf("no subfolder Acme under %s (now showing %s)", f.ID, s.folder)
	}

	// A name that is taken is refused where it was typed: the prompt stays
	// up with the name in it and the server's reason under the field, and
	// the button says what it does.
	fillAndSubmit(t, a, w, func() { s.renameFolder(f) }, "Archives")
	if f2, _, _ := s.cli.GetFolder(f.ID); f2.Name != "Clients 2026" {
		t.Fatalf("a taken name was accepted: %q", f2.Name)
	}
	field := promptField(w)
	if field == nil || field.Text != "Archives" {
		t.Fatal("the prompt closed, or lost the name")
	}
	var labels []string
	var accept string
	widget.Walk(w.Overlay(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Label:
			if v.Visible() && v.Text != "" {
				labels = append(labels, v.Text)
			}
		case *widgets.Button:
			if v.Primary {
				accept = v.Text
			}
		}
	})
	if accept != "Rename" {
		t.Fatalf("the button says %q", accept)
	}
	said := false
	for _, l := range labels {
		said = said || strings.Contains(strings.ToLower(l), "exist") || strings.Contains(strings.ToLower(l), "already")
	}
	if !said {
		t.Fatalf("no reason under the field: %q", labels)
	}
}

// An empty name cannot be accepted.
func TestNamePromptNeedsAName(t *testing.T) {
	s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	s.newFolder()
	a.PumpOnce()
	field := promptField(w)
	if field == nil {
		t.Fatal("no prompt")
	}
	var ok *widgets.Button
	widget.Walk(w.Overlay(), func(c widget.Component) {
		if b, isB := c.(*widgets.Button); isB && b.Primary {
			ok = b
		}
	})
	if ok == nil || ok.Enabled() {
		t.Fatal("OK is enabled with no name")
	}
	field.SetText("x")
	if !ok.Enabled() {
		t.Fatal("OK stayed grey with a name")
	}
}

// A list that mixes folders says where each message is.
func TestMixedListsNameTheFolder(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	s.searchAll = true
	s.filter.Query = "lunch"
	s.refreshList()
	if len(s.rows) == 0 {
		t.Fatal("no search results")
	}
	if s.table.Columns[colWho].Title != "Who · Folder" || !strings.Contains(s.cellText(0, colWho), "  ·  Inbox") {
		t.Fatalf("column %q cell %q", s.table.Columns[colWho].Title, s.cellText(0, colWho))
	}
	s.searchAll = false
	s.filter.Query = ""
	s.refreshList()
	if s.table.Columns[colWho].Title != "Who" || strings.Contains(s.cellText(0, colWho), "·") {
		t.Fatalf("a single folder: %q %q", s.table.Columns[colWho].Title, s.cellText(0, colWho))
	}
}

// Move Folder To offers the top level and every other folder, but not the
// folder itself or what is inside it.
func TestFolderMoveMenu(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	proj, _, _ := s.cli.GetFolder(mailcore.FolderAdaProjects)
	sub, err := s.cli.CreateFolder(mailcore.AcctAda, "Inner", proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, it := range s.folderMoveMenu(proj) {
		texts = append(texts, strings.TrimSpace(it.Text))
	}
	all := strings.Join(texts, "|")
	if !strings.HasPrefix(all, "Top Level|") || strings.Contains(all, "Projects") || strings.Contains(all, sub.Name) || !strings.Contains(all, "Archives") {
		t.Fatalf("menu %q", all)
	}
	s.moveFolder(proj, mailcore.FolderAdaArchives)
	s.waitIdle()
	if f, _, _ := s.cli.GetFolder(proj.ID); f.Parent != mailcore.FolderAdaArchives {
		t.Fatalf("moved %+v", f)
	}
}
