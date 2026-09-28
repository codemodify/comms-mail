package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// fillAndSubmit types text into the prompt window's field and presses its
// primary button.
func fillAndSubmit(t *testing.T, a *app.Application, w *app.Window, text string) {
	t.Helper()
	var field *widgets.TextField
	var ok *widgets.Button
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			field = v
		case *widgets.Button:
			if v.Primary {
				ok = v
			}
		}
	})
	if field == nil || ok == nil {
		t.Fatal("the prompt has no field or no OK button")
	}
	field.SetText(text)
	ok.OnClick()
	a.PumpOnce()
}

// Rename Folder… asks for the name and renames the folder in place; New
// Subfolder… makes one inside the folder it was chosen on.
func TestRenameAndSubfolderFromTheFolderMenu(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	proj := mailcore.Folder{ID: mailcore.FolderAdaProjects, AccountID: mailcore.AcctAda, Name: "Projects", Kind: mailcore.FolderCustom}

	w := askNameWindow(t, a, func() { s.renameFolder(proj) })
	fillAndSubmit(t, a, w, "Clients 2026")
	f, ok, _ := s.cli.GetFolder(mailcore.FolderAdaProjects)
	if !ok || f.Name != "Clients 2026" || !w.Closed() {
		t.Fatalf("after rename: %+v closed=%v", f, w.Closed())
	}

	w = askNameWindow(t, a, func() { s.newSubfolder(f) })
	fillAndSubmit(t, a, w, "Acme")
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

	// A name that is taken is refused and the prompt stays up.
	w = askNameWindow(t, a, func() { s.renameFolder(f) })
	fillAndSubmit(t, a, w, "Archives")
	if f2, _, _ := s.cli.GetFolder(f.ID); f2.Name != "Clients 2026" {
		t.Fatalf("a taken name was accepted: %q", f2.Name)
	}
	if w.Closed() || w.Overlay() == nil {
		t.Fatal("the prompt should stay up and say why")
	}
}

// askNameWindow runs open and returns the window it made.
func askNameWindow(t *testing.T, a *app.Application, open func()) *app.Window {
	t.Helper()
	before := map[*app.Window]bool{}
	for _, w := range a.Windows() {
		before[w] = true
	}
	open()
	a.PumpOnce()
	for _, w := range a.Windows() {
		if !before[w] {
			return w
		}
	}
	t.Fatal("no prompt window opened")
	return nil
}
