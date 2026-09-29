package mailui

import (
	"reflect"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// recipientWindow puts a To field and a Subject field in a headless
// window, with the keyboard in To. asked records the suggestion requests.
func recipientWindow(t *testing.T) (*app.Window, *recipientField, *widgets.TextField, *[]string) {
	t.Helper()
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Write", Width: 600, Height: 400, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	var asked []string
	rf := newRecipientField("To", func(tok string) { asked = append(asked, tok) })
	subject := widgets.NewTextField("", "Subject", nil)
	w.SetContent(widgets.NewColumn(rf, subject))
	a.PumpOnce()
	w.FocusOn(rf.chips.Editor())
	return w, rf, subject, &asked
}

func TestRecipientChipsAndCompletion(t *testing.T) {
	w, rf, _, asked := recipientWindow(t)

	// A finished address becomes a chip; what follows is being typed.
	w.Type("bob@example.org, ja")
	if got := rf.chips.Tokens(); !reflect.DeepEqual(got, []string{"bob@example.org"}) {
		t.Fatalf("chips %q", got)
	}
	if rf.chips.Pending() != "ja" || (*asked)[len(*asked)-1] != "ja" {
		t.Fatalf("pending %q, asked %q", rf.chips.Pending(), *asked)
	}

	// The suggestions float over the window; the keyboard stays in the
	// field, so typing goes on while they are up.
	rf.setSuggestions([]mailcore.Contact{
		{Name: "Jane Doe", Address: "jane@example.com"},
		{Name: "Jack Ma", Address: "jack@example.com"},
	})
	if w.Popup() != widget.Component(rf.pop) || !rf.open {
		t.Fatal("the suggestions are not on the popup layer")
	}
	w.Type("c")
	if rf.chips.Pending() != "jac" || w.Popup() == nil {
		t.Fatalf("typing under the list: pending %q popup %v", rf.chips.Pending(), w.Popup())
	}

	// Down and Return take the second one.
	w.Press(platform.KeyDown)
	if rf.list.Selected != 1 {
		t.Fatalf("Down: selected %d", rf.list.Selected)
	}
	w.Press(platform.KeyReturn)
	if got, want := rf.chips.Tokens(), []string{"bob@example.org", "Jack Ma <jack@example.com>"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after Return: %q, want %q", got, want)
	}
	if rf.chips.Pending() != "" || rf.open || w.Popup() != nil {
		t.Fatalf("after accepting: pending %q open %v popup %v", rf.chips.Pending(), rf.open, w.Popup())
	}
	if got := rf.Text(); got != "bob@example.org, Jack Ma <jack@example.com>" {
		t.Fatalf("Text %q", got)
	}

	// Escape takes the list down and leaves the text.
	w.Type("ja")
	rf.setSuggestions([]mailcore.Contact{{Name: "Jane Doe", Address: "jane@example.com"}})
	w.Press(platform.KeyEscape)
	if rf.open || w.Popup() != nil || rf.chips.Pending() != "ja" {
		t.Fatalf("after Escape: open %v popup %v pending %q", rf.open, w.Popup(), rf.chips.Pending())
	}

	// A contact already on the field is not offered again.
	rf.setSuggestions([]mailcore.Contact{{Name: "Jack Ma", Address: "JACK@example.com"}})
	if rf.open {
		t.Fatal("a contact already on the field was suggested")
	}
}

// A comma inside a quoted name is part of the name.
func TestRecipientQuotedNameKeepsItsComma(t *testing.T) {
	w, rf, _, _ := recipientWindow(t)
	w.Type(`"Doe, Jane" <jane@example.com>, `)
	if got := rf.chips.Tokens(); !reflect.DeepEqual(got, []string{`"Doe, Jane" <jane@example.com>`}) {
		t.Fatalf("chips %q", got)
	}
	// And a suggested contact with one is quoted when it goes in.
	w.Type("sm")
	rf.setSuggestions([]mailcore.Contact{{Name: "Smith, Al", Address: "al@example.com"}})
	w.Press(platform.KeyReturn)
	if got := rf.Text(); got != `"Doe, Jane" <jane@example.com>, "Smith, Al" <al@example.com>` {
		t.Fatalf("Text %q", got)
	}
	if n := len(mailcore.ParseAddrList(rf.Text())); n != 2 {
		t.Fatalf("the header reads as %d addresses", n)
	}
}

// Text that is not an address stays in the editor, where it can be seen
// and fixed; a duplicate is dropped; Backspace takes the last chip back.
func TestRecipientRefusesAndCorrects(t *testing.T) {
	w, rf, subject, _ := recipientWindow(t)
	w.Type("bob,")
	if len(rf.chips.Tokens()) != 0 || rf.chips.Pending() != "bob" {
		t.Fatalf("a non-address became a chip: %q pending %q", rf.chips.Tokens(), rf.chips.Pending())
	}
	if rf.Text() != "bob" {
		t.Fatalf("Text %q — what is typed must still be sent (and refused)", rf.Text())
	}
	rf.chips.Editor().SetText("")

	w.Type("a@example.com, A@EXAMPLE.COM, ")
	if got := rf.chips.Tokens(); !reflect.DeepEqual(got, []string{"a@example.com"}) {
		t.Fatalf("duplicate kept: %q", got)
	}

	w.Press(platform.KeyBackspace)
	if len(rf.chips.Tokens()) != 0 || rf.chips.Pending() != "a@example.com" {
		t.Fatalf("Backspace: chips %q pending %q", rf.chips.Tokens(), rf.chips.Pending())
	}

	// Leaving the field keeps what was typed.
	w.FocusOn(subject)
	if got := rf.chips.Tokens(); !reflect.DeepEqual(got, []string{"a@example.com"}) {
		t.Fatalf("leaving: %q pending %q", got, rf.chips.Pending())
	}
}

// Loading a draft or a reply: addresses become chips, the rest stays text.
func TestRecipientSetText(t *testing.T) {
	_, rf, _, _ := recipientWindow(t)
	rf.SetText(`Bob <bob@example.org>, "Doe, Jane" <jane@example.com>; junk, c@example.net`)
	want := []string{"Bob <bob@example.org>", `"Doe, Jane" <jane@example.com>`, "c@example.net"}
	if got := rf.chips.Tokens(); !reflect.DeepEqual(got, want) {
		t.Fatalf("chips %q, want %q", got, want)
	}
	if rf.chips.Pending() != "junk" {
		t.Fatalf("pending %q", rf.chips.Pending())
	}
	rf.SetText("")
	if rf.Text() != "" {
		t.Fatalf("cleared: %q", rf.Text())
	}
}

func TestSplitRecipients(t *testing.T) {
	for _, c := range []struct {
		in    string
		parts []string
		tail  string
	}{
		{"a@x, b@y", []string{"a@x"}, " b@y"},
		{`"Doe, J" <j@x>; k@y,`, []string{`"Doe, J" <j@x>`, "k@y"}, ""},
		{`"Doe, J`, nil, `"Doe, J`},
		{`x@y (Smith, J), z@w`, []string{"x@y (Smith, J)"}, " z@w"},
		{`"a \" , b" <q@x>,`, []string{`"a \" , b" <q@x>`}, ""},
	} {
		parts, tail := splitRecipients(c.in)
		if !reflect.DeepEqual(parts, c.parts) || tail != c.tail {
			t.Errorf("%q: %q %q, want %q %q", c.in, parts, tail, c.parts, c.tail)
		}
	}
}
