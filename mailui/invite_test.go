package mailui

import (
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
)

// The demo invite shows as a card over the preview; Accept sends the reply
// and the card says so, and a message with no invite hides it.
func TestInviteCardInPreview(t *testing.T) {
	s, _, _, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{})
	defer done()
	s.invite.now = func() time.Time { return mailcore.DemoNow }
	s.selectFolder(mailcore.FolderWorkInbox)
	s.selected = []mailcore.MessageID{mailcore.DemoInviteID}
	s.loadPreview()

	c := s.invite
	if !c.view.Visible() || c.id != mailcore.DemoInviteID {
		t.Fatalf("card visible=%v for %q", c.view.Visible(), c.id)
	}
	if c.what.Text != "Invitation: Toolkit design review" {
		t.Fatalf("heading %q", c.what.Text)
	}
	if !strings.HasSuffix(c.when.Text, "  ·  Room 4, second floor") {
		t.Fatalf("when/where %q", c.when.Text)
	}
	for _, want := range []string{"Organizer: Remy Chen\n", "Guests: Ada (work) (you), Kai Nakamura (yes), Grace Hopper (maybe)"} {
		if !strings.Contains(c.who.Text, want) {
			t.Fatalf("who lacks %q:\n%s", want, c.who.Text)
		}
	}
	if c.state.Text != "Will you attend?" || !c.btns.Visible() || !c.accept.Enabled() || !c.accept.Primary {
		t.Fatalf("state %q buttons %v", c.state.Text, c.btns.Visible())
	}
	// The calendar part is not the body.
	if strings.Contains(s.preview.Text, "BEGIN:VCALENDAR") {
		t.Fatal("the calendar object shows as the body")
	}

	c.accept.OnClick()
	s.waitIdle()
	if c.inv.Answer != mailcore.PartStatAccepted || !strings.HasPrefix(c.state.Text, "You accepted.") {
		t.Fatalf("after Accept: %q %q", c.inv.Answer, c.state.Text)
	}
	if c.accept.Enabled() || !c.decline.Enabled() || !c.maybe.Enabled() {
		t.Fatal("the answer given should not be offered again; the others should")
	}
	sent, err := s.cli.ListMessages(mailcore.FolderWorkSent, mailcore.Filter{Query: "Accepted: Toolkit design review"})
	if err != nil || len(sent) != 1 {
		t.Fatalf("reply in Sent: %d %v", len(sent), err)
	}

	// Opened in a tab, the same invite shows the answer; declining there
	// updates the preview's card too.
	press(s, platform.KeyE, 0)
	mt, ok := s.activeTab()
	if !ok || !mt.invite.view.Visible() || mt.invite.inv.Answer != mailcore.PartStatAccepted {
		t.Fatalf("tab card: %v", mt)
	}
	mt.invite.decline.OnClick()
	s.waitIdle()
	if c.inv.Answer != mailcore.PartStatDeclined || !strings.HasPrefix(c.state.Text, "You declined.") {
		t.Fatalf("preview card after declining in the tab: %q", c.state.Text)
	}

	s.tabs.Select(0)
	s.showTab(0)
	rows := s.visible()
	for _, m := range rows {
		if m.ID != mailcore.DemoInviteID {
			s.selected = []mailcore.MessageID{m.ID}
			break
		}
	}
	s.loadPreview()
	if c.view.Visible() {
		t.Fatal("a message without an invite shows the card")
	}
}

func TestInviteWhenAndState(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip(err)
	}
	start := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	inv := mailcore.Invite{Method: "REQUEST", Start: start, End: start.Add(time.Hour)}
	if got := inviteWhen(inv, berlin); got != "Tue 15 Sep 2026, 10:00 – 11:00 CEST" {
		t.Fatalf("same day %q", got)
	}
	inv.End = start.Add(26 * time.Hour)
	if got := inviteWhen(inv, berlin); got != "Tue 15 Sep 2026, 10:00 – Wed 16 Sep 2026, 12:00 CEST" {
		t.Fatalf("two days %q", got)
	}
	day := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	allDay := mailcore.Invite{AllDay: true, Start: day, End: day.AddDate(0, 0, 1), Repeats: "Every year"}
	if got := inviteWhen(allDay, berlin); got != "Thu 24 Dec 2026 (all day)\nEvery year" {
		t.Fatalf("all day %q", got)
	}
	allDay.End = day.AddDate(0, 0, 3)
	if got := inviteWhen(allDay, berlin); !strings.HasPrefix(got, "Thu 24 Dec – Sat 26 Dec 2026 (all day)") {
		t.Fatalf("all-day range %q", got)
	}

	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cancel := mailcore.Invite{Method: "CANCEL", Organizer: mailcore.Attendee{Name: "Remy", Email: "r@x"}, Start: start, End: start}
	if got := inviteState(cancel, now); got != "Remy cancelled this event." || inviteHeading(cancel) != "Cancelled event" {
		t.Fatalf("cancel %q %q", got, inviteHeading(cancel))
	}
	reply := mailcore.Invite{Method: "REPLY", Attendees: []mailcore.Attendee{{Name: "Kai", Email: "k@x", PartStat: "TENTATIVE"}}}
	if got := inviteState(reply, now); got != "Kai said maybe to your invitation." {
		t.Fatalf("reply %q", got)
	}
	mine := mailcore.Invite{Method: "REQUEST", You: "r@x", Organizer: mailcore.Attendee{Email: "r@x"}, Start: start, End: start}
	if got := inviteState(mine, now); got != "You organized this event." {
		t.Fatalf("organizer %q", got)
	}
	past := mailcore.Invite{Method: "REQUEST", You: "a@x", Answer: "ACCEPTED", Organizer: mailcore.Attendee{Email: "r@x"}, Start: start, End: start}
	if got := inviteState(past, start.Add(time.Hour)); !strings.HasSuffix(got, "This event is over.") {
		t.Fatalf("past %q", got)
	}
	if inviteHeading(mailcore.Invite{Method: "REQUEST", Sequence: 2}) != "Updated invitation" {
		t.Fatal("an update should say so")
	}
}
