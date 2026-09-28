package mailcore

import (
	"context"
	"errors"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func demoInviteClient(t *testing.T) *Client {
	t.Helper()
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli
}

func sentWith(t *testing.T, cli *Client, subject string) []Message {
	t.Helper()
	out, _ := cli.ListMessages(FolderWorkSent, Filter{Query: subject})
	return out
}

// A comment goes to the organizer in the reply and its text; "don't send"
// keeps the answer here and mails nothing.
func TestInviteAnswerCommentAndNoSend(t *testing.T) {
	cli := demoInviteClient(t)
	inv, err := cli.Invite(DemoInviteID)
	if err != nil || inv.PartID == "" {
		t.Fatalf("invite part %+v %v", inv, err)
	}
	got, err := cli.AnswerInvite(DemoInviteID, InviteAnswer{PartStat: PartStatDeclined, NoSend: true})
	if err != nil || got.Answer != PartStatDeclined || !strings.Contains(got.Note, "not told") {
		t.Fatalf("no-send answer %+v %v", got, err)
	}
	if n := len(sentWith(t, cli, "Declined: Toolkit design review")); n != 0 {
		t.Fatalf("a kept answer was mailed (%d)", n)
	}
	if inv, _ := cli.Invite(DemoInviteID); inv.Answer != PartStatDeclined {
		t.Fatalf("kept answer not remembered: %q", inv.Answer)
	}

	if _, err := cli.AnswerInvite(DemoInviteID, InviteAnswer{PartStat: PartStatTentative, Comment: "Late by 10 min; room 4, right?"}); err != nil {
		t.Fatal(err)
	}
	sent := sentWith(t, cli, "Tentatively accepted: Toolkit design review")
	if len(sent) != 1 {
		t.Fatalf("reply with comment: %+v", sent)
	}
	if m, _, err := cli.GetMessage(sent[0].ID); err != nil || !strings.Contains(m.Body, "Late by 10 min") {
		t.Fatalf("reply with comment: %+v %v", m, err)
	}
}

func TestReplyCarriesCommentAndName(t *testing.T) {
	out, err := buildInviteReply([]byte(demoInviteICS), "new@example.com", "New Person", PartStatAccepted, "See you, bring slides; ok?", DemoNow)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `COMMENT:See you\, bring slides\; ok?`) || !strings.Contains(s, "CN=New Person") {
		t.Fatalf("reply:\n%s", s)
	}
	inv, _ := ParseInvite(out)
	if inv.Comment != "See you, bring slides; ok?" {
		t.Fatalf("comment reads back as %q", inv.Comment)
	}
}

// A guest's proposal of a new time can be declined (DECLINECOUNTER to the
// guest); accepting it is the calendar's job and is refused here.
func TestDeclineCounterProposal(t *testing.T) {
	counter := strings.NewReplacer("METHOD:REQUEST", "METHOD:COUNTER",
		"DTSTART;TZID=Europe/Berlin:20260915T100000", "DTSTART;TZID=Europe/Berlin:20260916T140000",
		"DTEND;TZID=Europe/Berlin:20260915T110000", "DTEND;TZID=Europe/Berlin:20260916T150000").Replace(demoInviteICS)
	// The guest's COUNTER lists only the guest.
	var lines []string
	skip := false
	for _, l := range strings.Split(counter, "\n") {
		if strings.HasPrefix(l, "ATTENDEE") {
			skip = !strings.Contains(l, "CN=Kai Na")
			if skip {
				continue
			}
		} else if strings.HasPrefix(l, " ") && skip {
			continue
		} else {
			skip = false
		}
		lines = append(lines, l)
	}
	counter = strings.Join(lines, "\n")
	inv, err := ParseInvite([]byte(counter))
	if err != nil || inv.Method != "COUNTER" || len(inv.Attendees) != 1 || inv.Attendees[0].Email != "kai@paintengine.example" {
		t.Fatalf("counter %+v %v", inv, err)
	}
	out, err := BuildDeclineCounter([]byte(counter), "That day is full.", DemoNow)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := ParseInvite(out)
	if back.Method != "DECLINECOUNTER" || back.UID != inv.UID || len(back.Attendees) != 1 || back.Comment != "That day is full." {
		t.Fatalf("declinecounter %+v", back)
	}
}

func TestTransientSendErrors(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&textproto.Error{Code: 451, Msg: "try later"}, true},
		{&textproto.Error{Code: 421, Msg: "closing"}, true},
		{&textproto.Error{Code: 550, Msg: "no such user"}, false},
		{&textproto.Error{Code: 535, Msg: "bad login"}, false},
		{errors.New("smtp: refusing to send credentials over an unencrypted connection"), false},
	} {
		if got := transientSendError(c.err); got != c.want {
			t.Fatalf("%v: transient=%v", c.err, got)
		}
	}
}

// A send the network stopped is queued once and reported as queued, not
// failed; retrying it from the Outbox does not queue it a second time.
func TestFailedSendQueuedOnceAndNotDuplicated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "w", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "127.0.0.1:1", TLSMode: string(TLSPlain)},
		SMTP: ServerConfig{Host: "127.0.0.1:1", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.SendViaSMTP("w", "", Message{From: "ada@example.com", To: "bob@example.org", Subject: "hi", Body: "x\n"}, nil)
	var q *QueuedError
	if !errors.As(err, &q) {
		t.Fatalf("an unreachable server should queue the send, got %v", err)
	}
	if n := len(st.ListOutbox()); n != 1 {
		t.Fatalf("outbox %d, want 1", n)
	}
	_, _ = st.FlushOutbox()
	_, _ = st.FlushOutbox()
	if n := len(st.ListOutbox()); n != 1 {
		t.Fatalf("retries queued the message again: %d ops", n)
	}
}
