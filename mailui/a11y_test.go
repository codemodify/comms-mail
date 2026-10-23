package mailui

import (
	"testing"

	"goframework.io/desktop-ui"
	"goframework.io/desktop-ui/a11y"
	"goframework.io/desktop-ui/a11y/a11ytest"
	"goframework.io/desktop-ui/platform"
	"goframework.io/desktop-ui/style"
)

// Mail's window passes the accessibility audit: every control named, ids
// unique, the folder tree and message list present.
func TestMailIsAccessible(t *testing.T) {
	a := ui.New(ui.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	tree := a11ytest.Audit(t, "mail", w.AccessibleTree())
	if a11ytest.Find(tree, a11y.RoleTree, "Folders") == nil {
		t.Error("no tree called Folders")
	}
	if a11ytest.Find(tree, a11y.RoleTable, "Messages") == nil && a11ytest.Find(tree, a11y.RoleList, "Messages") == nil {
		t.Error("no table or list called Messages")
	}
}
