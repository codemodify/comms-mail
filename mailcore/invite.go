package mailcore

import (
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

// replyInvite answers the invitation in message id: it mails the organizer
// an iTIP REPLY with this user's PARTSTAT, files it in Sent, and records
// the answer so the invite shows it.
func (s *Server) replyInvite(id MessageID, partstat string) (Invite, error) {
	partstat = strings.ToUpper(strings.TrimSpace(partstat))
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
	switch {
	case inv.Method != "" && inv.Method != "REQUEST":
		return Invite{}, fmt.Errorf("mail: this is not an invitation to answer (%s)", inv.Method)
	case inv.Status == "CANCELLED":
		return Invite{}, fmt.Errorf("mail: this event was cancelled")
	case inv.Organizer.Email == "":
		return Invite{}, fmt.Errorf("mail: the invitation names no organizer to answer")
	}
	ident, accountID := s.replyIdentity(inv.You, m.AccountID)
	if ExtractAddr(ident.Address) == "" {
		return Invite{}, fmt.Errorf("mail: none of your accounts can answer this invitation")
	}
	you := inv.You
	if you == "" {
		you = ExtractAddr(ident.Address)
	}
	if strings.EqualFold(you, inv.Organizer.Email) {
		return Invite{}, fmt.Errorf("mail: you organized this event")
	}
	now := time.Now()
	reply, err := BuildInviteReply(ics, you, partstat, now)
	if err != nil {
		return Invite{}, err
	}
	org := (&mail.Address{Name: inv.Organizer.Name, Address: inv.Organizer.Email}).String()
	msg := Message{
		AccountID:  accountID,
		IdentityID: ident.ID,
		From:       ident.DisplayFrom(),
		To:         org,
		Subject:    InviteReplySubject(partstat, inv.Summary),
		Body:       InviteReplyText(inv, ident.DisplayFrom(), partstat),
		InReplyTo:  m.RFCMessageID,
		References: strings.TrimSpace(m.References + " " + m.RFCMessageID),
		Date:       now,
		Read:       true,
		// The reply is a notice, not a letter: no signature.
		SignatureInBody: true,
	}
	files := []AttachedFile{{Name: "reply.ics", MIME: "text/calendar", Data: reply, Method: "REPLY"}}
	if ls, ok := s.Store.(*LocalStore); ok && ls.hasConfiguredAccounts() {
		if _, err := ls.SendViaSMTP(accountID, ident.ID, msg, files); err != nil {
			return Invite{}, err
		}
	} else if sent, ok := specialFolder(s.Store, accountID, FolderSent); ok {
		if _, err := s.Store.Append(sent.ID, msg); err != nil {
			return Invite{}, err
		}
	} else {
		return Invite{}, fmt.Errorf("mail: no way to send from %s", accountID)
	}
	if ex := asExtra(s.Store); ex != nil {
		_ = ex.SetInviteAnswer(InviteKey(inv), partstat)
	}
	inv.You, inv.Answer = you, partstat
	s.broadcast(EventChanged, eventParams{Reason: "send"})
	return inv, nil
}
