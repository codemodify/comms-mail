package mailui

import (
	"fmt"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/widgets"
)

// inviteCard shows the calendar invitation a message carries, above its
// body: what, when, where and who, and this user's answer, with Accept /
// Maybe / Decline when the invite asks for one. The answer goes to the
// organizer as an iTIP reply — with a note, or not at all when "Tell the
// organizer" is off. A cancellation, or a guest's reply to an invite this
// user sent, shows what it says with no buttons; a guest's proposal of a
// new time can be declined. Open in Calendar hands the event to the
// desktop's calendar. Less / More folds the guest list and the options
// away, for every card, so the message body keeps its room.
type inviteCard struct {
	s                      *session
	view                   *widgets.Panel
	what, when, who        *widgets.Label
	state                  *widgets.Label
	btns                   *widgets.FlexBox
	accept, maybe, decline *widgets.Button
	declineCounter         *widgets.Button
	openCal, more          *widgets.Button
	opts                   *widgets.FlexBox // tell / answer as / note
	tell                   *widgets.Checkbox
	note                   *widgets.TextField
	asLabel                *widgets.Label
	as                     *widgets.ComboBox
	idents                 []mailcore.Identity
	id                     mailcore.MessageID // the message shown or loading
	inv                    mailcore.Invite
	gen                    uint64
	now                    func() time.Time
	sending                bool
}

func newInviteCard(s *session) *inviteCard {
	c := &inviteCard{s: s, now: time.Now}
	// The title has a line of its own: a wrapping label that flexes in a
	// row beside buttons is measured one line high and its wrapped lines
	// are clipped (uitoolkit-gaps.md #11).
	c.what = wrapLabel("")
	c.what.Title = true
	c.when = wrapLabel("")
	c.who = wrapLabel("")
	c.state = wrapLabel("")
	c.accept = widgets.NewButton("Accept", func() { c.answer(mailcore.PartStatAccepted) })
	c.maybe = widgets.NewButton("Maybe", func() { c.answer(mailcore.PartStatTentative) })
	c.decline = widgets.NewButton("Decline", func() { c.answer(mailcore.PartStatDeclined) })
	c.declineCounter = widgets.NewButton("Decline Proposal", func() { c.answer("DECLINECOUNTER") })
	c.btns = widgets.NewRow(c.accept, c.maybe, c.decline, c.declineCounter).WithGap(8)
	c.openCal = widgets.NewButton("Open in Calendar", c.openInCalendar)
	c.openCal.Tip = "Hand the event to your calendar application"
	c.more = widgets.NewButton("Less", func() {
		s.inviteCompact = !s.inviteCompact
		s.inviteLayoutChanged()
	})
	c.tell = widgets.NewCheckbox("Tell the organizer", true, nil)
	c.note = widgets.NewTextField("", "Add a note (optional)", nil)
	c.asLabel = widgets.NewLabel("Answer as")
	c.as = widgets.NewComboBox(nil, 0, nil)
	c.opts = widgets.NewRow(c.tell, c.asLabel, c.as, c.note).WithGap(8)
	c.opts.AddFlex(c.note, 1)
	// The question sits beside the buttons, to keep the card short: it
	// takes room from the message body.
	foot := widgets.NewRow(c.state, c.btns).WithGap(12)
	foot.AddFlex(c.state, 1)
	tools := widgets.NewRow(c.openCal, c.more).WithGap(8)
	c.view = widgets.NewPanel("", c.what, c.when, c.who, c.opts, foot, tools)
	c.view.Content().WithGap(4)
	c.view.SetAccessibleName("Calendar invitation")
	c.view.SetVisible(false)
	return c
}

// show asks the daemon for m's invitation when m has a calendar part, and
// hides the card otherwise. Showing the message already shown (the list
// row, then the fully loaded message) keeps what is there.
func (c *inviteCard) show(m mailcore.Message) {
	if m.ID != "" && m.ID == c.id {
		return
	}
	c.clear()
	if !mailcore.HasInvitePart(m) || c.s.cli == nil {
		return
	}
	c.id = m.ID
	gen := c.gen
	id := m.ID
	c.s.async(func() (any, error) {
		inv, err := c.s.cli.Invite(id)
		if err != nil || inv == nil {
			return nil, err
		}
		idents, _ := c.s.cli.Identities("")
		return inviteLoad{inv: *inv, idents: idents}, nil
	}, func(v any, err error) {
		if gen != c.gen || err != nil || v == nil {
			return
		}
		l := v.(inviteLoad)
		c.idents = l.idents
		c.note.SetText("")
		c.tell.SetChecked(true)
		c.set(l.inv)
	})
}

type inviteLoad struct {
	inv    mailcore.Invite
	idents []mailcore.Identity
}

// clear hides the card and drops a load in flight.
func (c *inviteCard) clear() {
	c.gen++
	c.id = ""
	c.inv = mailcore.Invite{}
	c.sending = false
	c.view.SetVisible(false)
}

// set fills the card from inv.
func (c *inviteCard) set(inv mailcore.Invite) {
	c.inv = inv
	summary := strings.TrimSpace(inv.Summary)
	if summary == "" {
		summary = "(untitled event)"
	}
	compact := c.s.inviteCompact
	c.what.SetText(inviteHeading(inv) + ": " + summary)
	c.when.SetText(inviteWhen(inv, time.Local))
	c.who.SetText(inviteWho(inv))
	c.who.SetVisible(!compact)
	state := inviteState(inv, c.now())
	if inv.Note != "" {
		state += " " + inv.Note
	}
	c.state.SetText(state)
	c.more.Text = "Less"
	if compact {
		c.more.Text = "More"
	}
	c.more.RequestLayout()
	c.openCal.SetVisible(inv.PartID != "" && inv.Method != "CANCEL" && inv.Method != "REPLY" &&
		inv.Method != "COUNTER" && inv.Method != "DECLINECOUNTER")

	needs := inv.NeedsReply()
	counter := inv.Method == "COUNTER" && inv.Answer != "DECLINECOUNTER"
	c.btns.SetVisible(needs || counter)
	for _, b := range []struct {
		btn *widgets.Button
		ps  string
	}{{c.accept, mailcore.PartStatAccepted}, {c.maybe, mailcore.PartStatTentative}, {c.decline, mailcore.PartStatDeclined}} {
		// The answer already given is not offered again.
		b.btn.SetVisible(needs)
		b.btn.SetEnabled(needs && !c.sending && inv.Answer != b.ps)
		b.btn.Primary = b.ps == mailcore.PartStatAccepted && inv.Answer != b.ps
	}
	c.declineCounter.SetVisible(counter)
	c.declineCounter.SetEnabled(counter && !c.sending)

	// Options: whether to tell the organizer, who answers when none of
	// the user's addresses is invited, and a note.
	pickIdentity := needs && inv.You == "" && len(c.idents) > 1
	if pickIdentity && len(c.as.Items) != len(c.idents) {
		items := make([]string, len(c.idents))
		for i, id := range c.idents {
			items[i] = id.DisplayFrom()
		}
		c.as.Items, c.as.Selected = items, 0
	}
	c.as.SetVisible(pickIdentity)
	c.asLabel.SetVisible(pickIdentity)
	c.tell.SetVisible(needs)
	c.opts.SetVisible(!compact && (needs || counter))
	c.view.SetVisible(true)
	c.view.RequestLayout()
	c.view.Invalidate()
}

// answer sends ps (or, with Tell the organizer off, only keeps it) and
// shows the result.
func (c *inviteCard) answer(ps string) {
	if c.id == "" || c.sending || c.s.cli == nil {
		return
	}
	id, inv := c.id, c.inv
	gen := c.gen
	ans := mailcore.InviteAnswer{PartStat: ps, Comment: strings.TrimSpace(c.note.Text)}
	if ps != "DECLINECOUNTER" && c.tell.Visible() && !c.tell.Checked {
		ans.NoSend = true
	}
	if c.as.Visible() && c.as.Selected >= 0 && c.as.Selected < len(c.idents) {
		ans.IdentityID = c.idents[c.as.Selected].ID
	}
	c.sending = true
	c.set(inv)
	switch {
	case ans.NoSend:
		c.state.SetText("Keeping your answer…")
	case ps == "DECLINECOUNTER":
		c.state.SetText("Telling the guest…")
	default:
		c.state.SetText("Sending your answer to " + organizerName(inv) + "…")
	}
	c.s.async(func() (any, error) {
		return c.s.cli.AnswerInvite(id, ans)
	}, func(v any, err error) {
		if gen != c.gen {
			return
		}
		c.sending = false
		if err != nil {
			c.set(inv)
			widgets.Warn(c.s.win.Content(), "Invitation", "Your answer was not sent.\n\n"+err.Error(), nil)
			return
		}
		got := v.(mailcore.Invite)
		c.note.SetText("")
		c.set(got)
		switch {
		case ps == "DECLINECOUNTER":
			c.s.mark("Declined the proposed time for “" + got.Summary + "”")
		case ans.NoSend:
			c.s.mark(fmt.Sprintf("You %s “%s” — %s was not told.", mailcore.PartStatWords(ps), got.Summary, organizerName(got)))
		default:
			c.s.mark(fmt.Sprintf("You %s “%s” — %s was told.", mailcore.PartStatWords(ps), got.Summary, organizerName(got)))
		}
		c.s.inviteAnswered(c, got)
	})
}

// openInCalendar hands the invite's calendar object to the desktop, whose
// calendar application imports it.
func (c *inviteCard) openInCalendar() {
	inv := c.inv
	if inv.MessageID == "" || inv.PartID == "" {
		return
	}
	c.s.async(func() (any, error) {
		return c.s.cli.OpenPart(inv.MessageID, inv.PartID)
	}, func(_ any, err error) {
		if err != nil {
			widgets.Warn(c.s.win.Content(), "Open in Calendar", err.Error(), nil)
			return
		}
		c.s.mark("Opened “" + inv.Summary + "” in your calendar application")
	})
}

// inviteLayoutChanged re-lays every card after Less / More.
func (s *session) inviteLayoutChanged() {
	for _, c := range s.inviteCards() {
		if c.inv.UID != "" {
			c.set(c.inv)
		}
	}
}

func (s *session) inviteCards() []*inviteCard {
	cards := []*inviteCard{s.invite}
	if s.tabs != nil {
		for i := 1; i < s.tabs.Len(); i++ {
			if mt, ok := s.tabs.Tab(i).Data.(*messageTab); ok {
				cards = append(cards, mt.invite)
			}
		}
	}
	return cards
}

// inviteAnswered updates the other cards showing the same invitation (the
// reading pane and a message tab).
func (s *session) inviteAnswered(from *inviteCard, inv mailcore.Invite) {
	for _, c := range s.inviteCards() {
		if c != nil && c != from && c.id == inv.MessageID && c.inv.UID != "" {
			c.set(inv)
		}
	}
}

// inviteHeading names what kind of calendar message this is.
func inviteHeading(inv mailcore.Invite) string {
	switch {
	case inv.Method == "CANCEL" || inv.Status == "CANCELLED":
		return "Cancelled event"
	case inv.Method == "REPLY":
		return "Invitation reply"
	case inv.Method == "COUNTER":
		return "New time proposed"
	case inv.Method == "DECLINECOUNTER":
		return "Proposal declined"
	case inv.Method == "PUBLISH":
		return "Event"
	case inv.Sequence > 0:
		return "Updated invitation"
	}
	return "Invitation"
}

// inviteWhen is the event's time in loc — "Tue 15 Sep 2026, 10:00 – 11:00
// CEST", or an all-day date or range — and place, and how it repeats.
func inviteWhen(inv mailcore.Invite, loc *time.Location) string {
	if inv.Start.IsZero() {
		return "Time not given"
	}
	var when string
	if inv.AllDay {
		start, end := inv.Start.UTC(), inv.End.UTC().AddDate(0, 0, -1)
		if !end.After(start) {
			when = start.Format("Mon 2 Jan 2006") + " (all day)"
		} else {
			when = start.Format("Mon 2 Jan") + " – " + end.Format("Mon 2 Jan 2006") + " (all day)"
		}
	} else {
		start, end := inv.Start.In(loc), inv.End.In(loc)
		switch {
		case !end.After(start):
			when = start.Format("Mon 2 Jan 2006, 15:04 MST")
		case sameDay(start, end):
			when = start.Format("Mon 2 Jan 2006, 15:04") + " – " + end.Format("15:04 MST")
		default:
			when = start.Format("Mon 2 Jan 2006, 15:04") + " – " + end.Format("Mon 2 Jan 2006, 15:04 MST")
		}
	}
	if loc := strings.TrimSpace(inv.Location); loc != "" {
		when += "  ·  " + loc
	}
	if inv.Repeats != "" {
		when += "\n" + inv.Repeats
	}
	if inv.RecurrenceID != "" {
		when += "\n(one occurrence of a repeating event)"
	}
	return when
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// inviteWho is the organizer and the guests with how each answered.
func inviteWho(inv mailcore.Invite) string {
	var b strings.Builder
	if inv.Organizer.Email != "" {
		b.WriteString("Organizer: " + organizerName(inv))
	}
	if inv.Method == "REPLY" || len(inv.Attendees) == 0 {
		return b.String()
	}
	var names []string
	for _, a := range inv.Attendees {
		n := a.Name
		if n == "" {
			n = a.Email
		}
		if strings.EqualFold(a.Email, inv.You) {
			n += " (you)"
		}
		names = append(names, n+partStatMark(a.PartStat))
	}
	const show = 6
	if len(names) > show {
		counts := map[string]int{}
		for _, a := range inv.Attendees {
			counts[a.PartStat]++
		}
		names = append(names[:show], fmt.Sprintf("and %d more (%d yes, %d maybe, %d no)", len(names)-show,
			counts[mailcore.PartStatAccepted], counts[mailcore.PartStatTentative], counts[mailcore.PartStatDeclined]))
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString("Guests: " + strings.Join(names, ", "))
	return b.String()
}

// partStatMark is an attendee's answer after their name. Words, not
// check marks: the toolkit's font has no ✓ / ✗ (uitoolkit-gaps.md #2).
func partStatMark(ps string) string {
	switch ps {
	case mailcore.PartStatAccepted:
		return " (yes)"
	case mailcore.PartStatTentative:
		return " (maybe)"
	case mailcore.PartStatDeclined:
		return " (no)"
	}
	return ""
}

func organizerName(inv mailcore.Invite) string {
	if inv.Organizer.Name != "" {
		return inv.Organizer.Name
	}
	if inv.Organizer.Email != "" {
		return inv.Organizer.Email
	}
	return "the organizer"
}

// inviteState is the line under the details: this user's answer, what a
// cancellation or reply says, or the question.
func inviteState(inv mailcore.Invite, now time.Time) string {
	var s string
	switch {
	case inv.Method == "CANCEL" || inv.Status == "CANCELLED":
		s = organizerName(inv) + " cancelled this event."
	case inv.Method == "REPLY":
		if len(inv.Attendees) == 0 {
			return "A reply to your invitation."
		}
		a := inv.Attendees[0]
		who := a.Name
		if who == "" {
			who = a.Email
		}
		return fmt.Sprintf("%s %s your invitation.", who, mailcore.PartStatWords(a.PartStat))
	case inv.Method == "COUNTER":
		who := "A guest"
		if len(inv.Attendees) > 0 {
			who = firstNonEmptyStr(inv.Attendees[0].Name, inv.Attendees[0].Email)
		}
		if inv.Answer == "DECLINECOUNTER" {
			return "You declined " + who + "'s proposed time."
		}
		return who + " proposes the time above. To take it, change the event in your calendar; Decline Proposal tells them no."
	case inv.Method == "DECLINECOUNTER":
		return organizerName(inv) + " declined the time you proposed."
	case inv.Method == "PUBLISH":
		s = "Shared for your information — no answer is needed."
	case inv.You != "" && strings.EqualFold(inv.You, inv.Organizer.Email):
		s = "You organized this event."
	default:
		switch inv.Answer {
		case mailcore.PartStatAccepted:
			s = "You accepted."
		case mailcore.PartStatTentative:
			s = "You said maybe."
		case mailcore.PartStatDeclined:
			s = "You declined."
		default:
			s = "Will you attend?"
		}
		if inv.Answer != "" && inv.Answer != "NEEDS-ACTION" && inv.NeedsReply() {
			s += " You can change your answer."
		}
		if inv.You == "" && inv.NeedsReply() {
			s += " (None of your addresses is on the guest list; answering adds you.)"
		}
	}
	end := inv.End
	if end.IsZero() {
		end = inv.Start
	}
	if inv.Repeats == "" && !end.IsZero() && end.Before(now) {
		s += " This event is over."
	}
	return s
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
