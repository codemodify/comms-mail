package mailcore

import (
	"bytes"
	"fmt"
	"mime"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Invite is a calendar invitation a message carries — an iTIP request, or
// the reply or cancellation that follows one (RFC 5546, sent by mail as in
// RFC 6047). Times are absolute; an all-day event's dates are midnight UTC.
type Invite struct {
	// Method is the iTIP method: REQUEST, REPLY, CANCEL, PUBLISH, COUNTER…
	Method      string     `json:"method"`
	UID         string     `json:"uid"`
	Sequence    int        `json:"sequence,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Location    string     `json:"location,omitempty"`
	Description string     `json:"description,omitempty"`
	Start       time.Time  `json:"start"`
	End         time.Time  `json:"end"`
	AllDay      bool       `json:"allDay,omitempty"`
	Repeats     string     `json:"repeats,omitempty"` // the RRULE in words
	Status      string     `json:"status,omitempty"`  // CONFIRMED, TENTATIVE, CANCELLED
	Organizer   Attendee   `json:"organizer"`
	Attendees   []Attendee `json:"attendees,omitempty"`
	// RecurrenceID is set when the invite is about one occurrence of a
	// repeating event: that occurrence's original start, as written.
	RecurrenceID string `json:"recurrenceId,omitempty"`

	// Filled by the daemon for the message the invite came in.
	MessageID MessageID `json:"messageId,omitempty"`
	// You is this user's address among the attendees ("" when none of
	// their addresses is on the list).
	You string `json:"you,omitempty"`
	// Answer is this user's PARTSTAT: the reply they sent from here, else
	// what the invite says.
	Answer string `json:"answer,omitempty"`
	// PartID is the MIME part the calendar object came from (to open it in
	// a calendar application).
	PartID string `json:"partId,omitempty"`
	// Comment is the COMMENT a reply or proposal carries.
	Comment string `json:"comment,omitempty"`
	// Note says what became of an answer that was not sent at once — it
	// waits in the Outbox, or was kept here without being sent.
	Note string `json:"note,omitempty"`
}

// Attendee is an ORGANIZER or ATTENDEE of an event.
type Attendee struct {
	Name     string `json:"name,omitempty"`
	Email    string `json:"email"`
	PartStat string `json:"partstat,omitempty"` // NEEDS-ACTION, ACCEPTED, TENTATIVE, DECLINED, DELEGATED
	Role     string `json:"role,omitempty"`
	RSVP     bool   `json:"rsvp,omitempty"`
}

// Display is "Name <email>", or the address alone.
func (a Attendee) Display() string {
	if a.Name != "" && !strings.EqualFold(a.Name, a.Email) {
		return a.Name + " <" + a.Email + ">"
	}
	return a.Email
}

// The answers an attendee can give from the invite card.
const (
	PartStatAccepted  = "ACCEPTED"
	PartStatTentative = "TENTATIVE"
	PartStatDeclined  = "DECLINED"
)

// InviteKey names one version of one invitation — the event, the
// occurrence, and its SEQUENCE — for the answer sent to it. An organizer's
// update bumps SEQUENCE and asks again.
func InviteKey(inv Invite) string {
	return strings.ToLower(inv.UID) + "|" + inv.RecurrenceID + "|" + strconv.Itoa(inv.Sequence)
}

// NeedsReply reports whether the invite asks this user for an answer.
func (inv Invite) NeedsReply() bool {
	return (inv.Method == "REQUEST" || inv.Method == "") && inv.Status != "CANCELLED" &&
		inv.Organizer.Email != "" && !strings.EqualFold(inv.Organizer.Email, inv.You)
}

// HasInvitePart reports whether a message has a part that may hold an
// invite, so the reader asks for it only then.
func HasInvitePart(m Message) bool {
	for _, p := range m.Parts {
		if isCalendarPart(p) {
			return true
		}
	}
	return false
}

func isCalendarPart(p Part) bool {
	mt := strings.ToLower(p.MIMEType)
	return mt == "text/calendar" || mt == "application/ics" || mt == "text/x-vcalendar" ||
		strings.HasSuffix(strings.ToLower(p.Filename), ".ics")
}

// FindInvite returns the invite a raw message carries, and the iCalendar
// object it came from. An inline text/calendar part wins over an .ics
// attachment (Google and Outlook send both, the same event twice).
func FindInvite(raw []byte) (Invite, []byte, bool) {
	m, err := ParseRFC822(raw, "", "")
	if err != nil {
		return Invite{}, nil, false
	}
	var cands []Part
	for _, p := range m.Parts {
		if isCalendarPart(p) {
			cands = append(cands, p)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return strings.EqualFold(cands[i].MIMEType, "text/calendar") && !strings.EqualFold(cands[j].MIMEType, "text/calendar")
	})
	for _, p := range cands {
		data, ok := sectionBytes(raw, p.ID)
		if !ok {
			continue
		}
		inv, err := ParseInvite(data)
		if err != nil {
			continue
		}
		inv.PartID = p.ID
		return inv, data, true
	}
	return Invite{}, nil, false
}

// ParseInvite reads the event of an iCalendar object. When it holds a
// repeating event and exceptions to it, the event itself is used.
func ParseInvite(data []byte) (Invite, error) {
	cal, err := parseICS(data)
	if err != nil {
		return Invite{}, err
	}
	ev := cal.event()
	if ev == nil {
		return Invite{}, fmt.Errorf("ical: no event")
	}
	inv := Invite{
		Method:      strings.ToUpper(cal.value("METHOD")),
		UID:         ev.value("UID"),
		Summary:     icsText(ev.value("SUMMARY")),
		Location:    icsText(ev.value("LOCATION")),
		Description: icsText(ev.value("DESCRIPTION")),
		Status:      strings.ToUpper(ev.value("STATUS")),
		Repeats:     describeRRule(ev.value("RRULE")),
		Comment:     icsText(ev.value("COMMENT")),
	}
	if inv.UID == "" {
		return Invite{}, fmt.Errorf("ical: event has no UID")
	}
	inv.Sequence, _ = strconv.Atoi(strings.TrimSpace(ev.value("SEQUENCE")))
	inv.RecurrenceID = strings.TrimSpace(ev.value("RECURRENCE-ID"))
	if p, ok := ev.get("DTSTART"); ok {
		inv.Start, inv.AllDay, _ = cal.time(p)
	}
	if p, ok := ev.get("DTEND"); ok {
		inv.End, _, _ = cal.time(p)
	} else if d, ok := parseICSDuration(ev.value("DURATION")); ok {
		inv.End = inv.Start.Add(d)
	} else if inv.AllDay {
		inv.End = inv.Start.AddDate(0, 0, 1)
	} else {
		inv.End = inv.Start
	}
	if p, ok := ev.get("ORGANIZER"); ok {
		inv.Organizer = attendeeOf(p)
	}
	for _, p := range ev.all("ATTENDEE") {
		inv.Attendees = append(inv.Attendees, attendeeOf(p))
	}
	return inv, nil
}

func attendeeOf(p icsProp) Attendee {
	a := Attendee{
		Name:     strings.TrimSpace(p.Params["CN"]),
		Email:    calAddress(p.Value),
		PartStat: strings.ToUpper(p.Params["PARTSTAT"]),
		Role:     strings.ToUpper(p.Params["ROLE"]),
		RSVP:     strings.EqualFold(p.Params["RSVP"], "TRUE"),
	}
	if a.PartStat == "" {
		a.PartStat = "NEEDS-ACTION"
	}
	return a
}

// calAddress is the address of a cal-address value ("mailto:a@b").
func calAddress(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 7 && strings.EqualFold(v[:7], "mailto:") {
		v = v[7:]
	}
	return strings.TrimSpace(v)
}

// BuildInviteReply writes the METHOD:REPLY object that answers an invite
// for one attendee (RFC 5546 section 3.2.3). It echoes what the organizer
// needs to match the answer — UID, SEQUENCE, RECURRENCE-ID — with the
// times in UTC so no VTIMEZONE has to travel with it. An attendee not on
// the list is added: a reply from someone the invite was forwarded to.
func BuildInviteReply(ics []byte, attendee, partstat string, now time.Time) ([]byte, error) {
	return buildInviteReply(ics, attendee, "", partstat, "", now)
}

// buildInviteReply is BuildInviteReply with the attendee's name (when the
// invite does not have it) and a COMMENT to the organizer.
func buildInviteReply(ics []byte, attendee, name, partstat, comment string, now time.Time) ([]byte, error) {
	partstat = strings.ToUpper(strings.TrimSpace(partstat))
	switch partstat {
	case PartStatAccepted, PartStatTentative, PartStatDeclined:
	default:
		return nil, fmt.Errorf("ical: cannot answer %q", partstat)
	}
	cal, err := parseICS(ics)
	if err != nil {
		return nil, err
	}
	ev := cal.event()
	if ev == nil || ev.value("UID") == "" {
		return nil, fmt.Errorf("ical: no event to answer")
	}
	org, ok := ev.get("ORGANIZER")
	if !ok || calAddress(org.Value) == "" {
		return nil, fmt.Errorf("ical: the invitation has no organizer to answer")
	}
	var me icsProp
	found := false
	for _, a := range ev.all("ATTENDEE") {
		if strings.EqualFold(calAddress(a.Value), attendee) {
			me, found = a, true
			break
		}
	}
	if !found {
		me = icsProp{Name: "ATTENDEE", Value: "mailto:" + attendee, Params: map[string]string{}}
		if name != "" {
			me.Params["CN"] = name
		}
	}

	var w icsWriter
	w.line("BEGIN:VCALENDAR")
	w.line("PRODID:-//comms-mail//comms-mail//EN")
	w.line("VERSION:2.0")
	w.line("CALSCALE:GREGORIAN")
	w.line("METHOD:REPLY")
	w.line("BEGIN:VEVENT")
	w.line("UID:" + ev.value("UID"))
	if p, ok := ev.get("RECURRENCE-ID"); ok {
		w.line(cal.utcProp(p))
	}
	seq := strings.TrimSpace(ev.value("SEQUENCE"))
	if seq == "" {
		seq = "0"
	}
	w.line("SEQUENCE:" + seq)
	w.line("DTSTAMP:" + now.UTC().Format("20060102T150405Z"))
	if p, ok := ev.get("DTSTART"); ok {
		w.line(cal.utcProp(p))
	}
	if p, ok := ev.get("DTEND"); ok {
		w.line(cal.utcProp(p))
	} else if d := ev.value("DURATION"); d != "" {
		w.line("DURATION:" + d)
	}
	if s := ev.value("SUMMARY"); s != "" {
		w.line("SUMMARY:" + s) // still escaped as it arrived
	}
	w.line(org.encode("CN", "SENT-BY"))
	me.Params = cloneParams(me.Params)
	me.Params["PARTSTAT"] = partstat
	delete(me.Params, "RSVP")
	w.line(me.encode("CN", "CUTYPE", "ROLE", "PARTSTAT", "DELEGATED-TO", "DELEGATED-FROM"))
	if c := strings.TrimSpace(comment); c != "" {
		w.line("COMMENT:" + icsEscape(c))
	}
	w.line("END:VEVENT")
	w.line("END:VCALENDAR")
	return w.b.Bytes(), nil
}

// BuildDeclineCounter writes the METHOD:DECLINECOUNTER object an organizer
// sends to turn down the new time a guest proposed (RFC 5546 section
// 3.2.8), from the guest's COUNTER.
func BuildDeclineCounter(counter []byte, comment string, now time.Time) ([]byte, error) {
	cal, err := parseICS(counter)
	if err != nil {
		return nil, err
	}
	ev := cal.event()
	if ev == nil || ev.value("UID") == "" {
		return nil, fmt.Errorf("ical: no event in the proposal")
	}
	org, ok := ev.get("ORGANIZER")
	if !ok {
		return nil, fmt.Errorf("ical: the proposal names no organizer")
	}
	var w icsWriter
	w.line("BEGIN:VCALENDAR")
	w.line("PRODID:-//comms-mail//comms-mail//EN")
	w.line("VERSION:2.0")
	w.line("METHOD:DECLINECOUNTER")
	w.line("BEGIN:VEVENT")
	w.line("UID:" + ev.value("UID"))
	if p, ok := ev.get("RECURRENCE-ID"); ok {
		w.line(cal.utcProp(p))
	}
	seq := strings.TrimSpace(ev.value("SEQUENCE"))
	if seq == "" {
		seq = "0"
	}
	w.line("SEQUENCE:" + seq)
	w.line("DTSTAMP:" + now.UTC().Format("20060102T150405Z"))
	w.line(org.encode("CN", "SENT-BY"))
	for _, a := range ev.all("ATTENDEE") {
		w.line(a.encode("CN"))
	}
	if c := strings.TrimSpace(comment); c != "" {
		w.line("COMMENT:" + icsEscape(c))
	}
	w.line("END:VEVENT")
	w.line("END:VCALENDAR")
	return w.b.Bytes(), nil
}

// icsEscape escapes TEXT for a content line: backslash, ; , and newlines.
func icsEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", "").Replace(s)
}

// InviteReplySubject is the subject of a reply: "Accepted: Team sync".
func InviteReplySubject(partstat, summary string) string {
	return partStatVerb(partstat) + ": " + summary
}

func partStatVerb(partstat string) string {
	switch strings.ToUpper(partstat) {
	case PartStatAccepted:
		return "Accepted"
	case PartStatTentative:
		return "Tentatively accepted"
	case PartStatDeclined:
		return "Declined"
	}
	return "Replied"
}

// PartStatWords is how an answer reads in a sentence: "accepted".
func PartStatWords(partstat string) string {
	switch strings.ToUpper(partstat) {
	case PartStatAccepted:
		return "accepted"
	case PartStatTentative:
		return "said maybe to"
	case PartStatDeclined:
		return "declined"
	case "DELEGATED":
		return "delegated"
	}
	return "answered"
}

// ---- iCalendar reading ----------------------------------------------------

type icsProp struct {
	Name   string
	Params map[string]string // upper-case names, values unquoted
	Value  string
}

type icsComp struct {
	Name  string
	Props []icsProp
	Subs  []*icsComp
}

func (c *icsComp) get(name string) (icsProp, bool) {
	for _, p := range c.Props {
		if p.Name == name {
			return p, true
		}
	}
	return icsProp{}, false
}

func (c *icsComp) value(name string) string {
	p, _ := c.get(name)
	return p.Value
}

func (c *icsComp) all(name string) []icsProp {
	var out []icsProp
	for _, p := range c.Props {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

func (c *icsComp) subs(name string) []*icsComp {
	var out []*icsComp
	for _, s := range c.Subs {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

// event is the VEVENT an invite is about: the master of a repeating event
// before any single-occurrence exception, else the first.
func (c *icsComp) event() *icsComp {
	evs := c.subs("VEVENT")
	for _, e := range evs {
		if _, ok := e.get("RECURRENCE-ID"); !ok {
			return e
		}
	}
	if len(evs) > 0 {
		return evs[0]
	}
	return nil
}

// maxICSBytes bounds what the parser reads; an invite is a few KB.
const maxICSBytes = 4 << 20

// parseICS reads an iCalendar stream into its VCALENDAR component.
func parseICS(data []byte) (*icsComp, error) {
	if len(data) > maxICSBytes {
		return nil, fmt.Errorf("ical: object too large")
	}
	var root *icsComp
	var stack []*icsComp
	for _, line := range unfoldICS(data) {
		p, ok := parseICSLine(line)
		if !ok {
			continue
		}
		switch p.Name {
		case "BEGIN":
			c := &icsComp{Name: strings.ToUpper(strings.TrimSpace(p.Value))}
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.Subs = append(top.Subs, c)
			} else if root == nil && c.Name == "VCALENDAR" {
				root = c
			} else {
				continue
			}
			if len(stack) > 16 {
				return nil, fmt.Errorf("ical: nested too deep")
			}
			stack = append(stack, c)
		case "END":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 && root != nil {
				return root, nil
			}
		default:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.Props = append(top.Props, p)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("ical: no VCALENDAR")
	}
	return root, nil
}

// unfoldICS splits content lines, joining folded continuations (a line
// break followed by a space or tab).
func unfoldICS(data []byte) []string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// parseICSLine splits "NAME;P1=v;P2="q:v":value". Quoted parameter values
// may hold ':' and ';'.
func parseICSLine(line string) (icsProp, bool) {
	i := strings.IndexAny(line, ";:")
	if i <= 0 {
		return icsProp{}, false
	}
	p := icsProp{Name: strings.ToUpper(strings.TrimSpace(line[:i])), Params: map[string]string{}}
	rest := line[i:]
	for strings.HasPrefix(rest, ";") {
		rest = rest[1:]
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return icsProp{}, false
		}
		name := strings.ToUpper(strings.TrimSpace(rest[:eq]))
		rest = rest[eq+1:]
		var vals []string
		for {
			var v string
			if strings.HasPrefix(rest, `"`) {
				end := strings.IndexByte(rest[1:], '"')
				if end < 0 {
					return icsProp{}, false
				}
				v, rest = rest[1:1+end], rest[2+end:]
			} else {
				end := strings.IndexAny(rest, ",;:")
				if end < 0 {
					return icsProp{}, false
				}
				v, rest = rest[:end], rest[end:]
			}
			vals = append(vals, v)
			if !strings.HasPrefix(rest, ",") {
				break
			}
			rest = rest[1:]
		}
		p.Params[name] = strings.Join(vals, ",")
	}
	if !strings.HasPrefix(rest, ":") {
		return icsProp{}, false
	}
	p.Value = rest[1:]
	return p, true
}

// icsText undoes TEXT escaping: \n, \, \; and \\.
func icsText(s string) string {
	if !strings.Contains(s, `\`) {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
}

// time reads a DATE or DATE-TIME property: UTC ("…Z"), in a TZID zone, or
// floating (local). DATE values are all-day and come back as midnight UTC.
func (c *icsComp) time(p icsProp) (time.Time, bool, bool) {
	v := strings.TrimSpace(p.Value)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i] // RDATE-style lists: the first
	}
	if strings.EqualFold(p.Params["VALUE"], "DATE") || len(v) == 8 {
		t, err := time.Parse("20060102", v)
		return t, true, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err == nil
	}
	wall, err := time.Parse("20060102T150405", v)
	if err != nil {
		return time.Time{}, false, false
	}
	tzid := strings.TrimSpace(p.Params["TZID"])
	if tzid == "" {
		return inZone(wall, time.Local), false, true
	}
	if loc := lookupZone(tzid); loc != nil {
		return inZone(wall, loc), false, true
	}
	for _, vt := range c.subs("VTIMEZONE") {
		if vt.value("TZID") == tzid {
			off := vtimezoneOffset(vt, wall)
			return wall.Add(-time.Duration(off) * time.Second).UTC(), false, true
		}
	}
	return inZone(wall, time.Local), false, true
}

func inZone(wall time.Time, loc *time.Location) time.Time {
	return time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, loc)
}

// utcProp re-encodes a date or date-time property with its time in UTC, so
// a reply needs no VTIMEZONE. DATE values stay dates.
func (c *icsComp) utcProp(p icsProp) string {
	t, allDay, ok := c.time(p)
	if !ok {
		return p.Name + ":" + p.Value
	}
	if allDay {
		return p.Name + ";VALUE=DATE:" + t.Format("20060102")
	}
	return p.Name + ":" + t.UTC().Format("20060102T150405Z")
}

// lookupZone resolves a TZID: an IANA name (also inside a Mozilla-style
// "/mozilla.org/…/Europe/Berlin" path), or a Windows zone name as Outlook
// and Exchange write them.
func lookupZone(tzid string) *time.Location {
	tzid = strings.Trim(tzid, `"`)
	if loc, err := time.LoadLocation(tzid); err == nil && tzid != "" && tzid != "Local" {
		return loc
	}
	if iana, ok := windowsZones[tzid]; ok {
		if loc, err := time.LoadLocation(iana); err == nil {
			return loc
		}
	}
	if parts := strings.Split(strings.Trim(tzid, "/"), "/"); len(parts) >= 2 {
		for n := 3; n >= 2; n-- {
			if len(parts) >= n {
				if loc, err := time.LoadLocation(strings.Join(parts[len(parts)-n:], "/")); err == nil {
					return loc
				}
			}
		}
	}
	return nil
}

// windowsZones maps the Windows zone names Outlook puts in TZID to IANA
// zones, for the common ones. Anything else falls back to the invite's own
// VTIMEZONE rules.
var windowsZones = map[string]string{
	"UTC":                            "UTC",
	"Coordinated Universal Time":     "UTC",
	"Pacific Standard Time":          "America/Los_Angeles",
	"Mountain Standard Time":         "America/Denver",
	"US Mountain Standard Time":      "America/Phoenix",
	"Central Standard Time":          "America/Chicago",
	"Eastern Standard Time":          "America/New_York",
	"Atlantic Standard Time":         "America/Halifax",
	"Alaskan Standard Time":          "America/Anchorage",
	"Hawaiian Standard Time":         "Pacific/Honolulu",
	"Central Standard Time (Mexico)": "America/Mexico_City",
	"SA Pacific Standard Time":       "America/Bogota",
	"E. South America Standard Time": "America/Sao_Paulo",
	"Argentina Standard Time":        "America/Argentina/Buenos_Aires",
	"GMT Standard Time":              "Europe/London",
	"Greenwich Standard Time":        "Atlantic/Reykjavik",
	"W. Europe Standard Time":        "Europe/Berlin",
	"Central Europe Standard Time":   "Europe/Budapest",
	"Central European Standard Time": "Europe/Warsaw",
	"Romance Standard Time":          "Europe/Paris",
	"E. Europe Standard Time":        "Europe/Chisinau",
	"FLE Standard Time":              "Europe/Kiev",
	"GTB Standard Time":              "Europe/Bucharest",
	"Russian Standard Time":          "Europe/Moscow",
	"Turkey Standard Time":           "Europe/Istanbul",
	"Israel Standard Time":           "Asia/Jerusalem",
	"South Africa Standard Time":     "Africa/Johannesburg",
	"Egypt Standard Time":            "Africa/Cairo",
	"Arabian Standard Time":          "Asia/Dubai",
	"India Standard Time":            "Asia/Kolkata",
	"China Standard Time":            "Asia/Shanghai",
	"Singapore Standard Time":        "Asia/Singapore",
	"Tokyo Standard Time":            "Asia/Tokyo",
	"Korea Standard Time":            "Asia/Seoul",
	"AUS Eastern Standard Time":      "Australia/Sydney",
	"E. Australia Standard Time":     "Australia/Brisbane",
	"New Zealand Standard Time":      "Pacific/Auckland",
}

// vtimezoneOffset is the UTC offset, in seconds, a VTIMEZONE gives a wall
// time: the offset of the STANDARD or DAYLIGHT observance that began last
// before it. Yearly BYMONTH/BYDAY rules ("last Sunday of October") are
// followed; others use their DTSTART alone.
func vtimezoneOffset(vt *icsComp, wall time.Time) int {
	type onset struct {
		at  time.Time
		off int
	}
	var obs []onset
	for _, c := range vt.Subs {
		if c.Name != "STANDARD" && c.Name != "DAYLIGHT" {
			continue
		}
		off, ok := parseUTCOffset(c.value("TZOFFSETTO"))
		if !ok {
			continue
		}
		start, err := time.Parse("20060102T150405", strings.TrimSpace(c.value("DTSTART")))
		if err != nil {
			start = time.Time{}
		}
		rule := parseRRule(c.value("RRULE"))
		for _, y := range []int{wall.Year() - 1, wall.Year()} {
			at, ok := yearlyOnset(start, rule, y)
			if ok {
				obs = append(obs, onset{at, off})
			}
		}
	}
	best := -1
	for i, o := range obs {
		if !o.at.After(wall) && (best < 0 || o.at.After(obs[best].at)) {
			best = i
		}
	}
	if best < 0 {
		if len(obs) > 0 {
			return obs[0].off
		}
		return 0
	}
	return obs[best].off
}

// yearlyOnset is when an observance starts in year y.
func yearlyOnset(start time.Time, rule map[string]string, y int) (time.Time, bool) {
	if rule["FREQ"] != "YEARLY" {
		if start.IsZero() || start.Year() > y {
			return time.Time{}, false
		}
		return start, true
	}
	if start.Year() > y {
		return time.Time{}, false
	}
	month := int(start.Month())
	if m, err := strconv.Atoi(rule["BYMONTH"]); err == nil && m >= 1 && m <= 12 {
		month = m
	}
	day := start.Day()
	if bd := rule["BYDAY"]; bd != "" {
		if d, ok := nthWeekday(y, time.Month(month), bd); ok {
			day = d
		}
	}
	return time.Date(y, time.Month(month), day, start.Hour(), start.Minute(), start.Second(), 0, time.UTC), true
}

// nthWeekday resolves a BYDAY such as "-1SU" (last Sunday) or "2MO".
func nthWeekday(y int, m time.Month, byday string) (int, bool) {
	byday = strings.ToUpper(strings.TrimSpace(strings.Split(byday, ",")[0]))
	if len(byday) < 2 {
		return 0, false
	}
	wd, ok := icsWeekdays[byday[len(byday)-2:]]
	if !ok {
		return 0, false
	}
	n := 1
	if s := byday[:len(byday)-2]; s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v == 0 {
			return 0, false
		}
		n = v
	}
	first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	days := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if n > 0 {
		d := 1 + (int(wd)-int(first.Weekday())+7)%7 + (n-1)*7
		return d, d <= days
	}
	last := time.Date(y, m, days, 0, 0, 0, 0, time.UTC)
	d := days - (int(last.Weekday())-int(wd)+7)%7 + (n+1)*7
	return d, d >= 1
}

var icsWeekdays = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

func parseUTCOffset(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 5 && len(s) != 7 {
		return 0, false
	}
	sign := 1
	switch s[0] {
	case '-':
		sign = -1
	case '+':
	default:
		return 0, false
	}
	h, err1 := strconv.Atoi(s[1:3])
	m, err2 := strconv.Atoi(s[3:5])
	sec := 0
	var err3 error
	if len(s) == 7 {
		sec, err3 = strconv.Atoi(s[5:7])
	}
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	return sign * (h*3600 + m*60 + sec), true
}

// parseICSDuration reads an RFC 5545 duration: P1W, P1DT2H30M, -PT15M.
func parseICSDuration(s string) (time.Duration, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	sign := time.Duration(1)
	switch s[0] {
	case '-':
		sign, s = -1, s[1:]
	case '+':
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") {
		return 0, false
	}
	s = s[1:]
	var d time.Duration
	inTime := false
	num := ""
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
			continue
		case r == 'T':
			inTime = true
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return 0, false
		}
		num = ""
		switch {
		case r == 'W' && !inTime:
			d += time.Duration(n) * 7 * 24 * time.Hour
		case r == 'D' && !inTime:
			d += time.Duration(n) * 24 * time.Hour
		case r == 'H' && inTime:
			d += time.Duration(n) * time.Hour
		case r == 'M' && inTime:
			d += time.Duration(n) * time.Minute
		case r == 'S' && inTime:
			d += time.Duration(n) * time.Second
		default:
			return 0, false
		}
	}
	return sign * d, num == ""
}

func parseRRule(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(s, ";") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[strings.ToUpper(strings.TrimSpace(k))] = strings.ToUpper(strings.TrimSpace(v))
		}
	}
	return out
}

// describeRRule puts a recurrence rule in words: "Every 2 weeks on Mon,
// Wed, until 31 Dec 2026". Rules it cannot say simply read "Repeats".
func describeRRule(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	r := parseRRule(s)
	unit := map[string]string{"DAILY": "day", "WEEKLY": "week", "MONTHLY": "month", "YEARLY": "year"}[r["FREQ"]]
	if unit == "" {
		return "Repeats"
	}
	out := "Every " + unit
	if n, err := strconv.Atoi(r["INTERVAL"]); err == nil && n > 1 {
		out = fmt.Sprintf("Every %d %ss", n, unit)
	}
	if bd := r["BYDAY"]; bd != "" {
		var days []string
		for _, d := range strings.Split(bd, ",") {
			if len(d) >= 2 {
				if wd, ok := icsWeekdays[d[len(d)-2:]]; ok {
					name := wd.String()[:3]
					if pre := d[:len(d)-2]; pre != "" {
						name = ordinal(pre) + " " + name
					}
					days = append(days, name)
				}
			}
		}
		if len(days) > 0 {
			out += " on " + strings.Join(days, ", ")
		}
	}
	if u := r["UNTIL"]; len(u) >= 8 {
		if t, err := time.Parse("20060102", u[:8]); err == nil {
			out += ", until " + t.Format("2 Jan 2006")
		}
	} else if c, err := strconv.Atoi(r["COUNT"]); err == nil && c > 0 {
		out += fmt.Sprintf(", %d times", c)
	}
	return out
}

func ordinal(s string) string {
	switch s {
	case "1", "+1":
		return "1st"
	case "2", "+2":
		return "2nd"
	case "3", "+3":
		return "3rd"
	case "4", "+4":
		return "4th"
	case "-1":
		return "last"
	}
	return s
}

// ---- iCalendar writing ----------------------------------------------------

type icsWriter struct{ b bytes.Buffer }

// line writes one content line folded at 75 octets, never inside a UTF-8
// sequence (RFC 5545 section 3.1).
func (w *icsWriter) line(s string) {
	s = strings.NewReplacer("\r", "", "\n", "").Replace(s)
	width := 75
	for len(s) > width {
		cut := width
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		w.b.WriteString(s[:cut])
		w.b.WriteString("\r\n ")
		s = s[cut:]
		width = 74 // the leading space counts
	}
	w.b.WriteString(s)
	w.b.WriteString("\r\n")
}

// encode writes the property back with the named parameters (when set),
// quoting values that need it.
func (p icsProp) encode(keep ...string) string {
	var b strings.Builder
	b.WriteString(p.Name)
	for _, k := range keep {
		v, ok := p.Params[k]
		if !ok || v == "" {
			continue
		}
		v = strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(v)
		b.WriteString(";" + k + "=")
		if strings.ContainsAny(v, ":;,") {
			b.WriteString(`"` + v + `"`)
		} else {
			b.WriteString(v)
		}
	}
	b.WriteString(":" + p.Value)
	return b.String()
}

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// InviteReplyText is the plain-text body that goes beside a reply's
// calendar part, for mail readers that do not understand it.
func InviteReplyText(inv Invite, who, partstat string) string {
	return inviteReplyText(inv, who, partstat, "")
}

func inviteReplyText(inv Invite, who, partstat, comment string) string {
	name := who
	if a, err := mail.ParseAddress(who); err == nil {
		name = a.Address
		if a.Name != "" {
			name = a.Name
		}
	}
	out := fmt.Sprintf("%s has %s the invitation: %s\n", name, PartStatWords(partstat), inv.Summary)
	if c := strings.TrimSpace(comment); c != "" {
		out += "\n" + c + "\n"
	}
	return out
}

// calendarContentType is the Content-Type of an iMIP part for method.
func calendarContentType(method string) string {
	return mime.FormatMediaType("text/calendar", map[string]string{"method": method, "charset": "utf-8"})
}
