package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// treeNodeFor is the sidebar node carrying data.
func treeNodeFor(s *session, data any) *widgets.TreeNode {
	var found *widgets.TreeNode
	var walk func(ns []*widgets.TreeNode)
	walk = func(ns []*widgets.TreeNode) {
		for _, n := range ns {
			if n.Data == data {
				found = n
			}
			walk(n.Children)
		}
	}
	walk(s.tree.Roots)
	return found
}

// A folder you made is dragged onto another folder of its account and goes
// inside it; dropped on the account it goes back to the top level. Inbox
// does not move, nor does a folder go into itself, into a folder inside
// it, or into another account.
func TestDragAFolderOntoAnother(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.LightLook(), false, AppOptions{})
	defer done()
	alpha, err := s.cli.CreateFolder("ada", "Alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := s.cli.CreateFolder("ada", "Beta", "")
	if err != nil {
		t.Fatal(err)
	}
	s.rebuildTree()
	parentOf := func(id mailcore.FolderID) mailcore.FolderID {
		f, _, _ := s.cli.GetFolder(id)
		return f.Parent
	}
	drop := func(f mailcore.FolderID, onto any) bool {
		d := s.dragFolder(treeNodeFor(s, f))
		if d == nil {
			t.Fatalf("%s cannot be dragged", f)
		}
		ok := s.dropOnFolder(treeNodeFor(s, onto), widget.DropEvent{Payload: d.Payload})
		s.waitIdle()
		s.rebuildTree()
		return ok
	}

	if s.dragFolder(treeNodeFor(s, mailcore.FolderAdaInbox)) != nil {
		t.Fatal("Inbox can be dragged")
	}
	if !drop(alpha.ID, beta.ID) || parentOf(alpha.ID) != beta.ID {
		t.Fatalf("Alpha onto Beta: parent %q", parentOf(alpha.ID))
	}
	if drop(beta.ID, alpha.ID) {
		t.Fatal("Beta went into Alpha, which is inside it")
	}
	if drop(alpha.ID, alpha.ID) {
		t.Fatal("a folder went into itself")
	}
	if drop(alpha.ID, mailcore.FolderWorkInbox) {
		t.Fatal("a folder went into another account")
	}
	if !drop(alpha.ID, "ada") || parentOf(alpha.ID) != "" {
		t.Fatalf("Alpha onto the account: parent %q", parentOf(alpha.ID))
	}
}
