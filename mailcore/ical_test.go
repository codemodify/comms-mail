package mailcore

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseInviteDemo(t *testing.T) {
	inv, err := ParseInvite([]byte(demoInviteICS))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Method != "REQUEST" || inv.UID != "design-review-20260915@clients.example" {
		t.Fatalf("method/uid %q %q", inv.Method, inv.UID)
	}
	if inv.Summary != "Toolkit design review" || inv.Location != "Room 4, second floor" {
		t.Fatalf("summary/location %q %q", inv.Summary, inv.Location)
	}
	if inv.Description != "Walk through the invite card and the reply flow.\nBring questions." {
		t.Fatalf("description %q", inv.Description)
	}
	// 10:00 in Berlin summer time is 08:00 UTC.
	if want := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC); !inv.Start.Equal(want) || !inv.End.Equal(want.Add(time.Hour)) {
		t.Fatalf("times %v – %v", inv.Start.UTC(), inv.End.UTC())
	}
	if inv.Organizer.Email != "remy@clients.example" || inv.Organizer.Name != "Remy Chen" {
		t.Fatalf("organizer %+v", inv.Organizer)
	}
	if len(inv.Attendees) != 3 {
		t.Fatalf("attendees %+v", inv.Attendees)
	}
	ada, kai, grace := inv.Attendees[0], inv.Attendees[1], inv.Attendees[2]
	if ada.Email != "ada@codemodify.com" || ada.PartStat != "NEEDS-ACTION" || !ada.RSVP || ada.Name != "Ada (work)" {
		t.Fatalf("folded attendee %+v", ada)
	}
	if kai.Name != "Kai Nakamura" || kai.PartStat != PartStatAccepted {
		t.Fatalf("kai %+v", kai)
	}
	// A fold's continuation keeps its own leading space: "Grace" + " Hopper".
	if grace.Name != "Grace Hopper" || grace.Role != "OPT-PARTICIPANT" {
		t.Fatalf("grace %+v", grace)
	}
}

func TestParseInviteForms(t *testing.T) {
	const allDay = "BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nBEGIN:VEVENT\r\nUID:a1\r\n" +
		"DTSTART;VALUE=DATE:20261224\r\nDURATION:P2D\r\nSUMMARY:Holidays\r\n" +
		"ORGANIZER:MAILTO:boss@example.com\r\n" +
		"RRULE:FREQ=YEARLY;UNTIL=20301224\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	inv, err := ParseInvite([]byte(allDay))
	if err != nil {
		t.Fatal(err)
	}
	if !inv.AllDay || !inv.Start.Equal(time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)) ||
		!inv.End.Equal(time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("all-day %v %v – %v", inv.AllDay, inv.Start, inv.End)
	}
	if inv.Organizer.Email != "boss@example.com" {
		t.Fatalf("MAILTO: prefix %q", inv.Organizer.Email)
	}
	if inv.Repeats != "Every year, until 24 Dec 2030" {
		t.Fatalf("repeats %q", inv.Repeats)
	}

	// Outlook: a Windows zone name, and a quoted CN holding ':' and ';'.
	const outlook = "BEGIN:VCALENDAR\nMETHOD:REQUEST\nBEGIN:VEVENT\nUID:o1\n" +
		"DTSTART;TZID=Pacific Standard Time:20260115T090000\n" +
		"DTEND;TZID=Pacific Standard Time:20260115T093000\n" +
		"ORGANIZER;CN=\"Doe; Jane: PM\":mailto:jane@corp.example\n" +
		"RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;COUNT=10\n" +
		"END:VEVENT\nEND:VCALENDAR\n"
	inv, err = ParseInvite([]byte(outlook))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 1, 15, 17, 0, 0, 0, time.UTC); !inv.Start.Equal(want) {
		t.Fatalf("windows zone start %v", inv.Start.UTC())
	}
	if inv.Organizer.Name != "Doe; Jane: PM" || inv.Organizer.Email != "jane@corp.example" {
		t.Fatalf("quoted CN %+v", inv.Organizer)
	}
	if inv.Repeats != "Every 2 weeks on Mon, Wed, 10 times" {
		t.Fatalf("repeats %q", inv.Repeats)
	}
}

// A zone the system does not know is read from the invite's VTIMEZONE,
// summer and winter.
func TestParseInviteVTimezone(t *testing.T) {
	const tz = "BEGIN:VCALENDAR\nMETHOD:REQUEST\n" +
		"BEGIN:VTIMEZONE\nTZID:Custom Europe\n" +
		"BEGIN:STANDARD\nDTSTART:16010101T030000\nTZOFFSETFROM:+0200\nTZOFFSETTO:+0100\nRRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=10\nEND:STANDARD\n" +
		"BEGIN:DAYLIGHT\nDTSTART:16010101T020000\nTZOFFSETFROM:+0100\nTZOFFSETTO:+0200\nRRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=3\nEND:DAYLIGHT\n" +
		"END:VTIMEZONE\n" +
		"BEGIN:VEVENT\nUID:%s\nDTSTART;TZID=Custom Europe:%s\nEND:VEVENT\nEND:VCALENDAR\n"
	for _, c := range []struct {
		wall string
		want time.Time
	}{
		{"20260701T120000", time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)},
		{"20260115T120000", time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)},
		{"20261226T120000", time.Date(2026, 12, 26, 11, 0, 0, 0, time.UTC)},
	} {
		inv, err := ParseInvite([]byte(fmt.Sprintf(tz, "u1", c.wall)))
		if err != nil {
			t.Fatal(err)
		}
		if !inv.Start.Equal(c.want) {
			t.Fatalf("%s: got %v want %v", c.wall, inv.Start.UTC(), c.want)
		}
	}
}

func TestParseInviteRejects(t *testing.T) {
	for _, bad := range []string{
		"",
		"not a calendar",
		"BEGIN:VCALENDAR\nEND:VCALENDAR\n",
		"BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:no uid\nEND:VEVENT\nEND:VCALENDAR\n",
		"BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID;X=\"unterminated:x\nEND:VEVENT\nEND:VCALENDAR\n",
	} {
		if _, err := ParseInvite([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	deep := "BEGIN:VCALENDAR\n" + strings.Repeat("BEGIN:X\n", 40)
	if _, err := ParseInvite([]byte(deep)); err == nil {
		t.Fatal("accepted a deep nest")
	}
}

func TestBuildInviteReply(t *testing.T) {
	now := time.Date(2026, 9, 12, 9, 30, 0, 0, time.UTC)
	out, err := BuildInviteReply([]byte(demoInviteICS), "ADA@codemodify.com", "accepted", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(out), "\r\n") {
		if len(l) > 75 {
			t.Fatalf("unfolded line (%d): %q", len(l), l)
		}
	}
	s := string(out)
	for _, want := range []string{
		"METHOD:REPLY\r\n", "UID:design-review-20260915@clients.example\r\n", "SEQUENCE:0\r\n",
		"DTSTAMP:20260912T093000Z\r\n", "DTSTART:20260915T080000Z\r\n", "DTEND:20260915T090000Z\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("reply lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "RSVP") || strings.Contains(s, "kai@") || strings.Contains(s, "TZID") {
		t.Fatalf("reply carries what it should not:\n%s", s)
	}
	inv, err := ParseInvite(out)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Method != "REPLY" || len(inv.Attendees) != 1 || inv.Attendees[0].PartStat != PartStatAccepted ||
		inv.Attendees[0].Email != "ada@codemodify.com" || inv.Organizer.Email != "remy@clients.example" {
		t.Fatalf("reply reads back as %+v", inv)
	}
	if inv.Summary != "Toolkit design review" {
		t.Fatalf("summary %q", inv.Summary)
	}

	// Someone the invite was forwarded to is added as an attendee.
	out, err = BuildInviteReply([]byte(demoInviteICS), "new@example.com", PartStatDeclined, now)
	if err != nil {
		t.Fatal(err)
	}
	if inv, _ := ParseInvite(out); len(inv.Attendees) != 1 || inv.Attendees[0].Email != "new@example.com" ||
		inv.Attendees[0].PartStat != PartStatDeclined {
		t.Fatalf("crasher reply %+v", inv.Attendees)
	}
	if _, err := BuildInviteReply([]byte(demoInviteICS), "ada@codemodify.com", "DELEGATED", now); err == nil {
		t.Fatal("answered with DELEGATED")
	}
}

// The reply goes out as text and text/calendar;method=REPLY alternatives,
// which FindInvite (and the organizer's calendar) reads back.
func TestBuildRFC822WithCalendar(t *testing.T) {
	ics, _ := BuildInviteReply([]byte(demoInviteICS), "ada@codemodify.com", PartStatTentative, DemoNow)
	msg := Message{From: "ada@codemodify.com", To: "remy@clients.example", Subject: "Tentatively accepted: x", Body: "maybe\n", Date: DemoNow}
	cal := AttachedFile{Name: "reply.ics", MIME: "text/calendar", Data: ics, Method: "REPLY"}
	raw, err := BuildRFC822Strict(msg, Identity{}, []AttachedFile{cal})
	if err != nil {
		t.Fatal(err)
	}
	head := string(raw[:bytes.Index(raw, []byte("\r\n\r\n"))])
	if !strings.Contains(head, "Content-Type: multipart/alternative") {
		t.Fatalf("top level is not alternative:\n%s", head)
	}
	if !strings.Contains(string(raw), "Content-Type: text/calendar; charset=utf-8; method=REPLY") {
		t.Fatalf("no calendar part:\n%s", raw)
	}
	m, err := ParseRFC822(raw, "", "")
	if err != nil || m.Body != "maybe\n" && m.Body != "maybe\r\n" && strings.TrimSpace(m.Body) != "maybe" {
		t.Fatalf("body %q %v", m.Body, err)
	}
	if m.HasAttach {
		t.Fatal("the calendar part counted as an attachment")
	}
	inv, _, ok := FindInvite(raw)
	if !ok || inv.Method != "REPLY" || inv.Attendees[0].PartStat != PartStatTentative {
		t.Fatalf("read back %+v %v", inv, ok)
	}

	// With a real attachment too: mixed{alternative{text, calendar}, file}.
	raw, err = BuildRFC822Strict(msg, Identity{}, []AttachedFile{cal, {Name: "a.txt", MIME: "text/plain", Data: []byte("x")}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = ParseRFC822(raw, "", "")
	if len(m.Attachments) != 1 || m.Attachments[0] != "a.txt" {
		t.Fatalf("attachments %v", m.Attachments)
	}
	if _, _, ok := FindInvite(raw); !ok {
		t.Fatal("no invite in the mixed message")
	}
	cal.Method = "REPLY\r\nX-Evil: 1"
	if _, err := BuildRFC822Strict(msg, Identity{}, []AttachedFile{cal}); err == nil {
		t.Fatal("a method with CRLF went into a header")
	}
}

// An invite's calendar part is not the message body.
func TestCalendarPartNotBody(t *testing.T) {
	raw := "From: a@b\r\nSubject: s\r\nContent-Type: text/calendar; method=REQUEST\r\n\r\n" + demoInviteICS
	m, err := ParseRFC822([]byte(raw), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Body, "BEGIN:VCALENDAR") || !HasInvitePart(m) {
		t.Fatalf("body %q parts %+v", m.Body, m.Parts)
	}
	if _, _, ok := FindInvite([]byte(raw)); !ok {
		t.Fatal("single-part invite not found")
	}
}

func TestInviteRPC(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	m, ok, err := cli.GetMessage(DemoInviteID)
	if err != nil || !ok || !HasInvitePart(m) || strings.Contains(m.Body, "VCALENDAR") {
		t.Fatalf("demo invite %+v %v %v", m.Parts, ok, err)
	}
	inv, err := cli.Invite(DemoInviteID)
	if err != nil || inv == nil {
		t.Fatalf("invite %v %v", inv, err)
	}
	if inv.You != "ada@codemodify.com" || inv.Answer != "NEEDS-ACTION" || !inv.NeedsReply() {
		t.Fatalf("you/answer %q %q", inv.You, inv.Answer)
	}

	got, err := cli.ReplyInvite(DemoInviteID, PartStatAccepted)
	if err != nil || got.Answer != PartStatAccepted {
		t.Fatalf("reply %+v %v", got, err)
	}
	inv, _ = cli.Invite(DemoInviteID)
	if inv.Answer != PartStatAccepted {
		t.Fatalf("answer not kept: %q", inv.Answer)
	}
	sent, _ := cli.ListMessages(FolderWorkSent, Filter{Query: "Accepted: Toolkit design review"})
	if len(sent) != 1 {
		t.Fatalf("sent copies %d", len(sent))
	}
	r := sent[0]
	if !strings.Contains(r.To, "remy@clients.example") || r.InReplyTo != "<invite-design-review@clients.example>" ||
		!strings.Contains(r.From, "ada@codemodify.com") {
		t.Fatalf("reply headers %+v", r)
	}

	// A message without an invite has none; a reply to it is refused.
	inbox, _ := cli.ListMessages(FolderAdaInbox, Filter{})
	if inv, err := cli.Invite(inbox[0].ID); err != nil || inv != nil {
		t.Fatalf("plain message invite %v %v", inv, err)
	}
	if _, err := cli.ReplyInvite(inbox[0].ID, PartStatAccepted); err == nil {
		t.Fatal("replied to a message with no invite")
	}
	if _, err := cli.ReplyInvite(DemoInviteID, "maybe"); err == nil {
		t.Fatal("bad partstat accepted")
	}
}

func TestInviteCancelAndReplyNeedNoAnswer(t *testing.T) {
	cancel := strings.Replace(demoInviteICS, "METHOD:REQUEST", "METHOD:CANCEL", 1)
	cancel = strings.Replace(cancel, "STATUS:CONFIRMED", "STATUS:CANCELLED", 1)
	inv, err := ParseInvite([]byte(cancel))
	if err != nil || inv.NeedsReply() {
		t.Fatalf("cancel %+v %v", inv, err)
	}
	reply, _ := BuildInviteReply([]byte(demoInviteICS), "ada@codemodify.com", PartStatAccepted, DemoNow)
	inv, _ = ParseInvite(reply)
	inv.You = "remy@clients.example"
	if inv.NeedsReply() {
		t.Fatal("a REPLY asked for an answer")
	}
	if ct := calendarContentType("REPLY"); !strings.Contains(ct, "method=REPLY") {
		t.Fatal(ct)
	} else if _, p, err := mime.ParseMediaType(ct); err != nil || p["method"] != "REPLY" {
		t.Fatal(ct, err)
	}
}

// An answer sent from here is still shown after the daemon restarts.
func TestInviteAnswerSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st := newTestLocalStore(t, dir)
	key := InviteKey(Invite{UID: "U1", Sequence: 2})
	if err := st.SetInviteAnswer(key, PartStatDeclined); err != nil {
		t.Fatal(err)
	}
	again := newTestLocalStore(t, dir)
	if got := again.InviteAnswer(key); got != PartStatDeclined {
		t.Fatalf("after restart %q", got)
	}
	// The organizer's update (a new SEQUENCE) asks again.
	if got := again.InviteAnswer(InviteKey(Invite{UID: "U1", Sequence: 3})); got != "" {
		t.Fatalf("new sequence already answered: %q", got)
	}
}

// On a real account the answer takes the SMTP send path; offline it waits
// in the Outbox with its calendar part, to go out as an iTIP reply later.
func TestInviteReplyQueuedOffline(t *testing.T) {
	st := newTestLocalStore(t, t.TempDir())
	st.mu.Lock()
	st.cfg.Accounts = append(st.cfg.Accounts, AccountConfig{
		ID: "w", Address: "ada@codemodify.com",
		IMAP: ServerConfig{Host: "imap.invalid"}, SMTP: ServerConfig{Host: "smtp.invalid"},
	})
	st.accounts = append(st.accounts, Account{ID: "w", Address: "ada@codemodify.com"})
	st.identities = append(st.identities, Identity{ID: "w-id", AccountID: "w", Name: "Ada", Address: "ada@codemodify.com", Default: true})
	st.Folders = append(st.Folders,
		Folder{ID: "w/inbox", AccountID: "w", Name: "Inbox", Kind: FolderInbox},
		Folder{ID: "w/sent", AccountID: "w", Name: "Sent", Kind: FolderSent})
	st.mu.Unlock()

	demo := NewDemoStore()
	raw, err := demo.GetRaw(DemoInviteID)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ParseRFC822(raw, "w/inbox", "w")
	m.ID = "w/inbox:1"
	st.mu.Lock()
	st.Messages = append(st.Messages, m)
	st.mu.Unlock()
	st.writeRaw(m, raw)
	st.SetOnline(false)

	srv := NewServer(st, filepath.Join(t.TempDir(), "s.sock"))
	inv, err := srv.replyInvite(m.ID, InviteAnswer{PartStat: PartStatTentative})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Answer != PartStatTentative || inv.You != "ada@codemodify.com" {
		t.Fatalf("invite %+v", inv)
	}
	ops := st.ListOutbox()
	if len(ops) != 1 || ops[0].Kind != "send" || len(ops[0].Attachments) != 1 {
		t.Fatalf("outbox %+v", ops)
	}
	op := ops[0]
	if op.Attachments[0].Method != "REPLY" || op.Message.To != `"Remy Chen" <remy@clients.example>` ||
		op.Message.Subject != "Tentatively accepted: Toolkit design review" {
		t.Fatalf("queued %+v / %+v", op.Message, op.Attachments[0].Method)
	}
	out, err := BuildRFC822Strict(*op.Message, Identity{}, op.Attachments)
	if err != nil {
		t.Fatal(err)
	}
	got, _, ok := FindInvite(out)
	if !ok || got.Method != "REPLY" || got.Attendees[0].PartStat != PartStatTentative {
		t.Fatalf("the queued message reads back as %+v", got)
	}
	if st.InviteAnswer(InviteKey(inv)) != PartStatTentative {
		t.Fatal("answer not recorded")
	}
}
