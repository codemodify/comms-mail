package mailcore

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// invite returns the calendar invitation message id carries, with this
// user's place in it filled in, or nil when it has none.
func (s *Server) invite(id MessageID) (*Invite, error) {
	m, ok := s.Store.GetMessage(id)
	if !ok {
		return nil, fmt.Errorf("mail: no message %s", id)
	}
	raw, err := s.Store.GetRaw(id)
	if err != nil {
		return nil, err
	}
	inv, _, ok := FindInvite(raw)
	if !ok {
		return nil, nil
	}
	s.fillInvite(&inv, m)
	return &inv, nil
}

// fillInvite sets which attendee is this user and their answer: the one
// sent from here for this version of the invite, else the invite's own.
func (s *Server) fillInvite(inv *Invite, m Message) {
	inv.MessageID = m.ID
	own := s.ownAddresses()
	for _, a := range inv.Attendees {
		if own[strings.ToLower(a.Email)] {
			inv.You, inv.Answer = a.Email, a.PartStat
			break
		}
	}
	if inv.You == "" && own[strings.ToLower(inv.Organizer.Email)] {
		inv.You = inv.Organizer.Email
	}
	if ex := asExtra(s.Store); ex != nil {
		if a := ex.InviteAnswer(InviteKey(*inv)); a != "" {
			inv.Answer = a
		}
	}
}

// ownAddresses is every address this user sends from, lower-cased.
func (s *Server) ownAddresses() map[string]bool {
	own := map[string]bool{}
	add := func(v string) {
		if a := strings.ToLower(ExtractAddr(v)); strings.Contains(a, "@") {
			own[a] = true
		}
	}
	for _, a := range s.Store.Accounts() {
		add(a.Address)
	}
	for _, id := range s.Store.Identities("") {
		add(id.Address)
	}
	return own
}

// replyIdentity picks who answers: the identity with the invited address,
// else the default identity of the account the invite came to.
func (s *Server) replyIdentity(you, accountID string) (Identity, string) {
	ids := s.Store.Identities("")
	if you != "" {
		for _, id := range ids {
			if strings.EqualFold(ExtractAddr(id.Address), you) {
				if id.AccountID != "" {
					accountID = id.AccountID
				}
				return id, accountID
			}
		}
		for _, a := range s.Store.Accounts() {
			if strings.EqualFold(ExtractAddr(a.Address), you) {
				return Identity{AccountID: a.ID, Name: a.Name, Address: you}, a.ID
			}
		}
	}
	var pick Identity
	for _, id := range ids {
		if id.AccountID == accountID && (pick.ID == "" || id.Default) {
			pick = id
		}
	}
	return pick, accountID
}

// InviteAnswer is how the user answers an invitation.
type InviteAnswer struct {
	PartStat string // ACCEPTED, TENTATIVE, DECLINED — or DECLINECOUNTER to a proposal
	Comment  string // a note to the organizer (or guest)
	// NoSend keeps the answer here without telling the organizer.
	NoSend bool
	// IdentityID answers as this identity (when none of the user's
	// addresses is on the guest list).
	IdentityID string
}

// replyInvite answers the invitation in message id: it mails the organizer
// an iTIP REPLY with this user's PARTSTAT (unless told not to), files it in
// Sent, and records the answer so the invite shows it. To a guest's
// proposal of a new time (COUNTER), DECLINECOUNTER turns it down.
func (s *Server) replyInvite(id MessageID, ans InviteAnswer) (Invite, error) {
	partstat := strings.ToUpper(strings.TrimSpace(ans.PartStat))
	m, ok := s.Store.GetMessage(id)
	if !ok {
		return Invite{}, fmt.Errorf("mail: no message %s", id)
	}
	raw, err := s.Store.GetRaw(id)
	if err != nil {
		return Invite{}, err
	}
	inv, ics, ok := FindInvite(raw)
	if !ok {
		return Invite{}, fmt.Errorf("mail: this message has no invitation")
	}
	s.fillInvite(&inv, m)
	if inv.Method == "COUNTER" {
		if partstat != "DECLINECOUNTER" {
			return Invite{}, fmt.Errorf("mail: a proposed time is accepted by changing the event in your calendar; it can be declined here")
		}
		return s.declineCounter(m, inv, ics, ans)
	}
	switch {
	case inv.Method != "" && inv.Method != "REQUEST":
		return Invite{}, fmt.Errorf("mail: this is not an invitation to answer (%s)", inv.Method)
	case inv.Status == "CANCELLED":
		return Invite{}, fmt.Errorf("mail: this event was cancelled")
	case inv.Organizer.Email == "":
		return Invite{}, fmt.Errorf("mail: the invitation names no organizer to answer")
	}
	switch partstat {
	case PartStatAccepted, PartStatTentative, PartStatDeclined:
	default:
		return Invite{}, fmt.Errorf("mail: cannot answer %q", ans.PartStat)
	}
	you := inv.You
	ident, accountID := s.replyIdentity(you, m.AccountID)
	if ans.IdentityID != "" {
		found := false
		for _, x := range s.Store.Identities("") {
			if x.ID == ans.IdentityID {
				ident, found = x, true
				if x.AccountID != "" {
					accountID = x.AccountID
				}
			}
		}
		if !found {
			return Invite{}, fmt.Errorf("mail: no identity %s", ans.IdentityID)
		}
		if you == "" {
			you = ExtractAddr(ident.Address) // answering as this one adds it
		}
	}
	if you == "" {
		you = ExtractAddr(ident.Address)
	}
	if strings.EqualFold(you, inv.Organizer.Email) {
		return Invite{}, fmt.Errorf("mail: you organized this event")
	}
	record := func() {
		if ex := asExtra(s.Store); ex != nil {
			_ = ex.SetInviteAnswer(InviteKey(inv), partstat)
		}
		inv.You, inv.Answer = you, partstat
	}
	if ans.NoSend {
		record()
		inv.Note = "Kept here — " + organizerNameOf(inv) + " was not told."
		return inv, nil
	}
	if ExtractAddr(ident.Address) == "" {
		return Invite{}, fmt.Errorf("mail: none of your accounts can answer this invitation")
	}
	now := time.Now()
	reply, err := buildInviteReply(ics, you, ident.Name, partstat, ans.Comment, now)
	if err != nil {
		return Invite{}, err
	}
	org := (&mail.Address{Name: inv.Organizer.Name, Address: inv.Organizer.Email}).String()
	msg := s.inviteMail(m, accountID, ident, org, InviteReplySubject(partstat, inv.Summary),
		inviteReplyText(inv, ident.DisplayFrom(), partstat, ans.Comment), now)
	note, err := s.sendInviteMail(accountID, ident, msg, reply, "REPLY")
	if err != nil {
		return Invite{}, err
	}
	record()
	inv.Note = note
	s.broadcast(EventChanged, eventParams{Reason: "send"})
	return inv, nil
}

// declineCounter turns down a guest's proposed new time.
func (s *Server) declineCounter(m Message, inv Invite, ics []byte, ans InviteAnswer) (Invite, error) {
	if len(inv.Attendees) == 0 || inv.Attendees[0].Email == "" {
		return Invite{}, fmt.Errorf("mail: the proposal names no guest to answer")
	}
	guest := inv.Attendees[0]
	ident, accountID := s.replyIdentity(inv.Organizer.Email, m.AccountID)
	if ExtractAddr(ident.Address) == "" {
		return Invite{}, fmt.Errorf("mail: none of your accounts can answer this proposal")
	}
	now := time.Now()
	obj, err := BuildDeclineCounter(ics, ans.Comment, now)
	if err != nil {
		return Invite{}, err
	}
	to := (&mail.Address{Name: guest.Name, Address: guest.Email}).String()
	body := fmt.Sprintf("The new time you proposed for %s was declined.\n", inv.Summary)
	if c := strings.TrimSpace(ans.Comment); c != "" {
		body += "\n" + c + "\n"
	}
	msg := s.inviteMail(m, accountID, ident, to, "Proposal declined: "+inv.Summary, body, now)
	note, err := s.sendInviteMail(accountID, ident, msg, obj, "DECLINECOUNTER")
	if err != nil {
		return Invite{}, err
	}
	if ex := asExtra(s.Store); ex != nil {
		_ = ex.SetInviteAnswer(InviteKey(inv)+"|counter|"+strings.ToLower(guest.Email), "DECLINECOUNTER")
	}
	inv.Answer, inv.Note = "DECLINECOUNTER", note
	s.broadcast(EventChanged, eventParams{Reason: "send"})
	return inv, nil
}

// inviteMail is the message that carries an answer, threaded under m.
func (s *Server) inviteMail(m Message, accountID string, ident Identity, to, subject, body string, now time.Time) Message {
	return Message{
		AccountID:  accountID,
		IdentityID: ident.ID,
		From:       ident.DisplayFrom(),
		To:         to,
		Subject:    subject,
		Body:       body,
		InReplyTo:  m.RFCMessageID,
		References: strings.TrimSpace(m.References + " " + m.RFCMessageID),
		Date:       now,
		Read:       true,
		// An answer is a notice, not a letter: no signature.
		SignatureInBody: true,
	}
}

// sendInviteMail sends msg with obj as its iTIP part and files it in Sent.
// A send that could not happen now but waits in the Outbox is not an
// error; the note says so.
func (s *Server) sendInviteMail(accountID string, ident Identity, msg Message, obj []byte, method string) (string, error) {
	files := []AttachedFile{{Name: "invite.ics", MIME: "text/calendar", Data: obj, Method: method}}
	if ls, ok := s.Store.(*LocalStore); ok && ls.hasConfiguredAccounts() {
		_, err := ls.SendViaSMTP(accountID, ident.ID, msg, files)
		var queued *QueuedError
		if errors.As(err, &queued) {
			return "Not sent yet — it is in the Outbox and goes out when the server answers.", nil
		}
		if err != nil {
			return "", err
		}
		if ls.feat != nil && !ls.feat.Online() {
			return "Working offline — it is in the Outbox.", nil
		}
		return "", nil
	}
	sent, ok := specialFolder(s.Store, accountID, FolderSent)
	if !ok {
		return "", fmt.Errorf("mail: no way to send from %s", accountID)
	}
	_, err := s.Store.Append(sent.ID, msg)
	return "", err
}

func organizerNameOf(inv Invite) string {
	if inv.Organizer.Name != "" {
		return inv.Organizer.Name
	}
	if inv.Organizer.Email != "" {
		return inv.Organizer.Email
	}
	return "the organizer"
}
