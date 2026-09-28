package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
)

// typeInto sets the field's text with the caret at the end, as typing
// leaves it (SetText alone keeps the caret at 0).
func typeInto(rf *recipientField, text string) {
	rf.field.SetText(text)
	n := runeCount(text)
	rf.field.SetSelection(n, n)
}

func TestRecipientFieldCompletesTheCurrentToken(t *testing.T) {
	var asked string
	rf := newRecipientField("To", func(tok string) { asked = tok })

	// Typing a second recipient: the token is what follows the last comma.
	typeInto(rf, "bob@example.org, ja")
	rf.onEdit()
	if asked != "ja" {
		t.Fatalf("asked for %q, want the current token \"ja\"", asked)
	}

	rf.setSuggestions([]mailcore.Contact{
		{Name: "Jane Doe", Address: "jane@example.com"},
		{Name: "Jack Ma", Address: "jack@example.com"},
	})
	if !rf.open || rf.list.Count != 2 {
		t.Fatalf("suggestions not shown: open=%v count=%d", rf.open, rf.list.Count)
	}

	// Down then Return accepts the second suggestion, replacing only the
	// "ja" token and keeping the first recipient.
	rf.KeyPress(widget.KeyEvent{Key: platform.KeyDown})
	if rf.list.Selected != 1 {
		t.Fatalf("Down did not move the selection: %d", rf.list.Selected)
	}
	if !rf.KeyPress(widget.KeyEvent{Key: platform.KeyReturn}) {
		t.Fatal("Return should accept when the list is open")
	}
	if got, want := rf.Text(), "bob@example.org, Jack Ma <jack@example.com>, "; got != want {
		t.Fatalf("after accept: %q, want %q", got, want)
	}
	if rf.open {
		t.Fatal("the list should close after accepting")
	}
	// Keys do nothing once it is closed (they belong to the field).
	if rf.KeyPress(widget.KeyEvent{Key: platform.KeyDown}) {
		t.Fatal("a closed field should not consume Down")
	}
}

func TestRecipientFieldEmptyTokenClosesList(t *testing.T) {
	rf := newRecipientField("To", func(string) {})
	typeInto(rf, "al")
	rf.setSuggestions([]mailcore.Contact{{Address: "a@b.c"}})
	if !rf.open {
		t.Fatal("should be open")
	}
	// Text ending in a comma+space: the current token is empty, so the
	// suggestion list must not stay up.
	typeInto(rf, "a@b.c, ")
	rf.setSuggestions([]mailcore.Contact{{Address: "x@y.z"}})
	if rf.open {
		t.Fatal("an empty current token should keep the list closed")
	}
}
