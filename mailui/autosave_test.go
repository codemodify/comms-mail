package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// openWriteForAutosave builds a Write window headless and hands back its
// autosave step and close action.
func openWriteForAutosave(t *testing.T, cli *mailcore.Client, opts ComposeOptions) (a *app.Application, w *app.Window, fields map[string]*widgets.TextField, body *widgets.TextArea, autosave, closeWin func()) {
	t.Helper()
	composeSeam = func(as, cl func()) { autosave, closeWin = as, cl }
	t.Cleanup(func() { composeSeam = nil })
	a = uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Write", Width: 760, Height: 640, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	w.SetContent(ComposeApp(a, w, cli, opts))
	a.PumpOnce()
	fields = map[string]*widgets.TextField{}
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			fields[v.Placeholder] = v
		case *widgets.TextArea:
			if !v.ReadOnly {
				body = v
			}
		}
	})
	if autosave == nil || closeWin == nil || body == nil || fields["Subject"] == nil {
		t.Fatal("the Write window did not expose its parts")
	}
	return
}

func draftsWith(t *testing.T, cli *mailcore.Client, subject string) []mailcore.Message {
	t.Helper()
	all, err := cli.ListMessages(mailcore.FolderAdaDrafts, mailcore.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var out []mailcore.Message
	for _, m := range all {
		if m.Subject == subject {
			out = append(out, m)
		}
	}
	return out
}

func answerOverlay(t *testing.T, a *app.Application, w *app.Window, button string) {
	t.Helper()
	a.PumpOnce()
	ov := w.Overlay()
	if ov == nil {
		t.Fatalf("no dialog to answer %q", button)
	}
	clicked := false
	widget.Walk(ov, func(c widget.Component) {
		if b, ok := c.(*widgets.Button); ok && b.Text == button && b.OnClick != nil && !clicked {
			clicked = true
			b.OnClick()
		}
	})
	if !clicked {
		t.Fatalf("the dialog has no %q", button)
	}
	a.PumpOnce()
}

// A changed message is saved to Drafts on its own — one draft, updated in
// place — and nothing is saved while it is unchanged. Closing it and
// answering No throws the autosaved draft away.
func TestWriteWindowAutosavesDrafts(t *testing.T) {
	cli := demoClient(t)
	a, w, fields, body, autosave, closeWin := openWriteForAutosave(t, cli, ComposeOptions{})
	const subj = "Autosave keeps this"

	autosave()
	if n := len(draftsWith(t, cli, "")); n != 0 {
		t.Fatalf("an untouched message was saved (%d)", n)
	}
	fields["Subject"].SetText(subj)
	body.SetText("first words\n" + body.Text)
	autosave()
	d := draftsWith(t, cli, subj)
	if len(d) != 1 {
		t.Fatalf("drafts after the first autosave: %d", len(d))
	}
	body.SetText("more words\n" + body.Text)
	autosave()
	autosave() // unchanged: no second save
	d = draftsWith(t, cli, subj)
	if len(d) != 1 {
		t.Fatalf("autosave made %d drafts, want the same one updated", len(d))
	}
	if full, ok, _ := cli.GetMessage(d[0].ID); !ok || full.Body != body.Text {
		t.Fatalf("the draft holds %q, want %q", full.Body, body.Text)
	}

	closeWin()
	answerOverlay(t, a, w, "No")
	if n := len(draftsWith(t, cli, subj)); n != 0 {
		t.Fatalf("No should throw away the draft autosave made (%d left)", n)
	}
	if !w.Closed() {
		t.Fatal("the window stayed open")
	}
}

// A draft the writer saved, or opened, is theirs: No on close keeps it.
func TestWriteWindowKeepsAnOpenedDraft(t *testing.T) {
	cli := demoClient(t)
	const subj = "A draft I opened"
	id, err := cli.SaveDraft(mailcore.AcctAda, mailcore.Message{Subject: subj, To: "kai@paintengine.example", Body: "v1\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	m, _, _ := cli.GetMessage(id)
	a, w, _, body, autosave, closeWin := openWriteForAutosave(t, cli, ComposeOptions{Draft: &m})
	body.SetText("v2\n")
	autosave()
	d := draftsWith(t, cli, subj)
	if len(d) != 1 || d[0].ID != id {
		t.Fatalf("autosave of an opened draft: %+v", d)
	}
	body.SetText("v3\n")
	closeWin()
	answerOverlay(t, a, w, "No")
	d = draftsWith(t, cli, subj)
	if len(d) != 1 {
		t.Fatalf("No on an opened draft removed it (%d)", len(d))
	}
	if full, _, _ := cli.GetMessage(id); full.Body != "v2\n" {
		t.Fatalf("the draft holds %q, want the autosaved v2", full.Body)
	}

}

// The window's close button asks before a changed message is lost; Yes
// saves it as a draft.
func TestWriteWindowCloseButtonAsks(t *testing.T) {
	cli := demoClient(t)
	a, w, fields, body, _, _ := openWriteForAutosave(t, cli, ComposeOptions{})
	const subj = "Closed with the X"
	fields["Subject"].SetText(subj)
	body.SetText("unsaved\n")
	w.Inject(platform.Event{Kind: platform.EventClose})
	a.PumpOnce()
	if w.Closed() {
		t.Fatal("the close button closed a changed message without asking")
	}
	answerOverlay(t, a, w, "Yes")
	if !w.Closed() {
		t.Fatal("Yes should save and close")
	}
	if n := len(draftsWith(t, cli, subj)); n != 1 {
		t.Fatalf("Yes saved %d drafts", n)
	}

	// An untouched window closes at once.
	a2, w2, _, _, _, _ := openWriteForAutosave(t, cli, ComposeOptions{})
	w2.Inject(platform.Event{Kind: platform.EventClose})
	a2.PumpOnce()
	if !w2.Closed() {
		t.Fatal("an untouched Write window asked before closing")
	}
}
