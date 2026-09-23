package mail

import (
	"strings"
	"testing"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/a11y"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
)

// Mail's window passes the accessibility audit: every control named, ids
// unique, the folder tree and message list present.
func TestMailIsAccessible(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 1280, Height: 800, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	tree := w.AccessibleTree()
	var lines []string
	for _, p := range a11y.Check(tree) {
		lines = append(lines, p.String())
	}
	if len(lines) > 0 {
		t.Errorf("%d accessibility problems:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	var trees, tables int
	tree.Walk(func(n *a11y.Node) bool {
		switch n.Role {
		case a11y.RoleTree:
			trees++
		case a11y.RoleTable, a11y.RoleList:
			tables++
		}
		return true
	})
	if trees == 0 || tables == 0 {
		t.Fatalf("folder trees %d, message lists %d", trees, tables)
	}
}
