package mailui

import (
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// A message that went closes its Write window: no dialog to dismiss after
// every send, as in Thunderbird. It is in Sent.
func TestSentMessageClosesTheWriteWindow(t *testing.T) {
	cli := demoClient(t)
	a, w, fields, body, _, _ := openWriteForAutosave(t, cli, ComposeOptions{})
	fields["To"].SetText("bob@example.com")
	fields["Subject"].SetText("Straight out")
	body.SetText("hello")

	var send func()
	widget.Walk(w.Content(), func(c widget.Component) {
		if tb, ok := c.(*widgets.ToolBar); ok {
			for _, it := range tb.Items() {
				if it.Text == "Send" {
					send = it.OnClick
				}
			}
		}
	})
	if send == nil {
		t.Fatal("no Send on the Write window's toolbar")
	}
	send()
	a.PumpOnce()
	if w.Overlay() != nil {
		t.Fatal("a dialog is up after the message went")
	}
	for _, open := range a.Windows() {
		if open == w {
			t.Fatal("the Write window is still open")
		}
	}
	sent, _ := cli.ListMessages(mailcore.FolderAdaSent, mailcore.Filter{Query: "Straight out"})
	if len(sent) != 1 {
		t.Fatalf("Sent has %d copies", len(sent))
	}
}
