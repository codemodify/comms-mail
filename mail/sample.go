package mail

import (
	"fmt"
	"time"
)

// DemoNow is the frozen “today” for sample dates and screenshots
// (Friday, 11 Sep 2026 — matches the dogfood snapshot).
var DemoNow = time.Date(2026, 9, 11, 17, 22, 0, 0, time.UTC)

// Well-known demo folder ids (stable for tests and screenshots).
const (
	AcctAda  = "ada"
	AcctWork = "work"

	FolderAdaInbox    FolderID = "ada/inbox"
	FolderAdaDrafts   FolderID = "ada/drafts"
	FolderAdaSent     FolderID = "ada/sent"
	FolderAdaJunk     FolderID = "ada/junk"
	FolderAdaTrash    FolderID = "ada/trash"
	FolderAdaArchives FolderID = "ada/archives"
	FolderAdaY2025    FolderID = "ada/archives/2025"
	FolderAdaY2026    FolderID = "ada/archives/2026"
	FolderAdaProjects FolderID = "ada/projects"

	FolderWorkInbox   FolderID = "work/inbox"
	FolderWorkDrafts  FolderID = "work/drafts"
	FolderWorkSent    FolderID = "work/sent"
	FolderWorkJunk    FolderID = "work/junk"
	FolderWorkTrash   FolderID = "work/trash"
	FolderWorkClients FolderID = "work/clients"
)

func seedDemo(s *MemoryStore) {
	s.addAccount(Account{ID: AcctAda, Name: "Ada Lovelace", Address: "ada@example.com", Transport: "memory"})
	s.addAccount(Account{ID: AcctWork, Name: "Ada (work)", Address: "ada@codemodify.com", Transport: "memory"})
	s.identities = []Identity{
		{ID: "ada-home", AccountID: AcctAda, Name: "Ada Lovelace", Address: "ada@example.com", Signature: "Ada Lovelace\nMathematical scientist", Default: true},
		{ID: "ada-alias", AccountID: AcctAda, Name: "Ada L.", Address: "ada.lovelace@example.com", Signature: "— Ada"},
		{ID: "ada-work", AccountID: AcctWork, Name: "Ada (work)", Address: "ada@codemodify.com", Signature: "Ada\ncodemodify", Default: true},
	}
	s.tags = DefaultTags()
	s.rules = demoRules()
	s.feat = demoExtras()

	addSpecial := func(acct string, id FolderID, kind FolderKind, parent FolderID, name string) {
		if name == "" {
			name = kind.String()
		}
		s.addFolder(Folder{ID: id, AccountID: acct, Name: name, Kind: kind, Parent: parent})
	}
	addSpecial(AcctAda, FolderAdaInbox, FolderInbox, "", "")
	addSpecial(AcctAda, FolderAdaDrafts, FolderDrafts, "", "")
	addSpecial(AcctAda, FolderAdaSent, FolderSent, "", "")
	addSpecial(AcctAda, FolderAdaJunk, FolderJunk, "", "")
	addSpecial(AcctAda, FolderAdaTrash, FolderTrash, "", "")
	addSpecial(AcctAda, FolderAdaArchives, FolderArchive, "", "")
	s.addFolder(Folder{ID: FolderAdaY2025, AccountID: AcctAda, Name: "2025", Kind: FolderArchive, Parent: FolderAdaArchives})
	s.addFolder(Folder{ID: FolderAdaY2026, AccountID: AcctAda, Name: "2026", Kind: FolderArchive, Parent: FolderAdaArchives})
	s.addFolder(Folder{ID: FolderAdaProjects, AccountID: AcctAda, Name: "Projects", Kind: FolderCustom})

	addSpecial(AcctWork, FolderWorkInbox, FolderInbox, "", "")
	addSpecial(AcctWork, FolderWorkDrafts, FolderDrafts, "", "")
	addSpecial(AcctWork, FolderWorkSent, FolderSent, "", "")
	addSpecial(AcctWork, FolderWorkJunk, FolderJunk, "", "")
	addSpecial(AcctWork, FolderWorkTrash, FolderTrash, "", "")
	s.addFolder(Folder{ID: FolderWorkClients, AccountID: AcctWork, Name: "Clients", Kind: FolderCustom})

	// Pin a distinctive unread welcome at the top of Ada’s Inbox (newest).
	s.addMessage(Message{
		Folder:      FolderAdaInbox,
		AccountID:   AcctAda,
		From:        "Thunderbird Team <hello@thunderbird.example>",
		To:          "Ada Lovelace <ada@example.com>",
		Subject:     "Welcome to Mail on uitoolkit",
		Date:        DemoNow.Add(-34 * time.Minute),
		Read:        false,
		Starred:     true,
		HasAttach:   true,
		Tags:        []string{"Important"},
		Body:        welcomeBody,
		Attachments: []string{"mail-shortcuts.txt", "uitoolkit-v080.png"},
	})

	people := [][2]string{
		{"Bianca Rossi", "bianca@mozilla.example"},
		{"Kai Nakamura", "kai@paintengine.example"},
		{"Iris Patel", "iris@notes.example"},
		{"Morgan Lee", "morgan@lists.example"},
		{"Sofia Álvarez", "sofia@design.example"},
		{"Noah Okonkwo", "noah@ops.example"},
		{"Jules Moreau", "jules@research.example"},
		{"Remy Chen", "remy@clients.example"},
		{"Harper Quinn", "harper@legal.example"},
		{"Diego Santos", "diego@build.example"},
	}
	subjects := []string{
		"Re: retained scene graph notes",
		"TableView column widths",
		"Quick Filter pin states",
		"Wayland eglSwapBuffers flicker",
		"Titillium vs system UI font",
		"Invoice for September",
		"Patch: TreeView unread badge",
		"Lunch tomorrow?",
		"Release checklist v0.7.0",
		"Fwd: paintengine2d v0.9.0",
		"Can you review the splitter?",
		"Draft: conference talk outline",
		"Spam: congratulations, you won",
		"Meeting notes — Thursday",
		"Your package is waiting",
		"RFC: pluggable Store interface",
		"Re: HiDPI metrics on X11",
		"Weekly status",
		"Photos from the office",
		"Please confirm your address",
	}
	bodies := []string{
		"Looks good on UITK_SCENE=auto. Hover damage stays on the row.\n\n— sent from Mail dogfood\n",
		"Can we keep star / attachment as 28px columns and let Subject flex?\nI attached a PNG of the classic 3-pane.\n",
		"Unread + Starred pins should AND with the query, same as Thunderbird.\n",
		"I only see it on the GPU path. UITK_PAINT=cpu is fine.\n",
		"Titillium Web for chrome, JetBrains Mono for the Source tab.\n",
		"Please find the invoice attached (PDF).\nNet 30.\n",
		"Small patch. Tree labels become Inbox (12) when unread > 0.\n",
		"Are you free around 12:30? The usual place.\n",
		"1. Tag v0.7.0\n2. Push the release tag\n3. Update README screenshots\n",
		"Engine tip is 82e4803. Scene recorder + GPU rect batches.\n",
		"The vertical 3-pane default feels right. Classic is the toggle.\n",
		"Talk title: Pure-Go pixels you own.\nStill a draft.\n",
		"Click here to claim your prize!!!\n",
		"Attendees: Ada, Kai, Iris.\nActions in the body below.\n",
		"Left at the front desk. Bring ID.\n",
		"MemoryStore now; IMAP later without rewriting chrome.\n",
		"Scale from Xft.dpi looks correct at 1.5.\n",
		"Shipped the mail example. Tests green with CGO_ENABLED=0.\n",
		"A few shots from Friday. JPEG attached.\n",
		"Is ada@example.com still the right From for invoices?\n",
	}

	add := func(folder FolderID, acct string, i int, unread, star, attach bool, tags []string, delta time.Duration, extra string) {
		p := people[i%len(people)]
		from := fmt.Sprintf("%s <%s>", p[0], p[1])
		to := "Ada Lovelace <ada@example.com>"
		if acct == AcctWork {
			to = "Ada (work) <ada@codemodify.com>"
		}
		subj := subjects[i%len(subjects)]
		if extra != "" {
			subj = extra + " " + subj
		}
		body := bodies[i%len(bodies)]
		if i%7 == 0 {
			body += "\nP.S. This is sample data. go run ./examples/mail\n"
		}
		var atts []string
		if attach {
			atts = []string{fmt.Sprintf("shot-%02d.png", i%20)}
		}
		s.addMessage(Message{
			Folder:      folder,
			AccountID:   acct,
			From:        from,
			To:          to,
			Subject:     subj,
			Date:        DemoNow.Add(delta),
			Read:        !unread,
			Starred:     star,
			HasAttach:   attach,
			Tags:        tags,
			Body:        body,
			Attachments: atts,
		})
	}

	// Ada Inbox — ~55 messages, mixed flags.
	for i := 0; i < 55; i++ {
		unread := i%3 != 0
		star := i%11 == 0
		attach := i%5 == 0
		var tags []string
		switch i % 9 {
		case 0:
			tags = []string{"Work"}
		case 3:
			tags = []string{"Personal"}
		case 6:
			tags = []string{"To Do"}
		}
		// Spread over ~5 weeks, newest first-ish.
		delta := -time.Duration(40+i*7)*time.Hour - time.Duration(i*13)*time.Minute
		add(FolderAdaInbox, AcctAda, i, unread, star, attach, tags, delta, "")
	}

	// Ada Sent
	for i := 0; i < 22; i++ {
		p := people[i%len(people)]
		s.addMessage(Message{
			Folder:    FolderAdaSent,
			AccountID: AcctAda,
			From:      "Ada Lovelace <ada@example.com>",
			To:        fmt.Sprintf("%s <%s>", p[0], p[1]),
			Subject:   "Re: " + subjects[(i+4)%len(subjects)],
			Date:      DemoNow.Add(-time.Duration(i*19+3) * time.Hour),
			Read:      true,
			Body:      "Thanks — I pushed a build.\n\nAda\n",
		})
	}

	// Drafts
	for i := 0; i < 4; i++ {
		s.addMessage(Message{
			Folder:    FolderAdaDrafts,
			AccountID: AcctAda,
			From:      "Ada Lovelace <ada@example.com>",
			To:        people[i][1],
			Subject:   "Draft: " + subjects[(i+2)%len(subjects)],
			Date:      DemoNow.Add(-time.Duration(i*5+2) * time.Hour),
			Read:      true,
			Body:      "Still writing this…\n",
		})
	}

	// Junk / Trash
	for i := 0; i < 8; i++ {
		add(FolderAdaJunk, AcctAda, i+3, true, false, false, nil, -time.Duration(i*26+8)*time.Hour, "[bulk]")
	}
	for i := 0; i < 6; i++ {
		add(FolderAdaTrash, AcctAda, i+8, true, false, i%2 == 0, nil, -time.Duration(i*30+12)*time.Hour, "")
	}

	// Archives + Projects
	for i := 0; i < 8; i++ {
		add(FolderAdaY2025, AcctAda, i+2, false, i == 1, i == 2, []string{"Work"}, -time.Duration(400+i*48)*time.Hour, "2025:")
	}
	for i := 0; i < 7; i++ {
		add(FolderAdaY2026, AcctAda, i+5, false, false, i%3 == 0, nil, -time.Duration(80+i*20)*time.Hour, "")
	}
	for i := 0; i < 9; i++ {
		add(FolderAdaProjects, AcctAda, i+1, i%2 == 0, i == 0, true, []string{"Work"}, -time.Duration(i*14+6)*time.Hour, "uitoolkit:")
	}

	// Work account
	for i := 0; i < 28; i++ {
		unread := i%4 == 0
		add(FolderWorkInbox, AcctWork, i+6, unread, i%10 == 0, i%6 == 0, []string{"Work"}, -time.Duration(i*9+1)*time.Hour, "")
	}
	for i := 0; i < 10; i++ {
		p := people[(i+3)%len(people)]
		s.addMessage(Message{
			Folder:    FolderWorkSent,
			AccountID: AcctWork,
			From:      "Ada (work) <ada@codemodify.com>",
			To:        fmt.Sprintf("%s <%s>", p[0], p[1]),
			Cc:        "team@codemodify.com",
			Subject:   subjects[(i+8)%len(subjects)],
			Date:      DemoNow.Add(-time.Duration(i*11+4) * time.Hour),
			Read:      true,
			Body:      "Tracking this on the work account.\n",
		})
	}
	s.addMessage(Message{
		Folder:    FolderWorkDrafts,
		AccountID: AcctWork,
		From:      "Ada (work) <ada@codemodify.com>",
		To:        "Remy Chen <remy@clients.example>",
		Subject:   "Proposal — Q4 toolkit support",
		Date:      DemoNow.Add(-3 * time.Hour),
		Read:      true,
		Body:      "Remy,\n\nDraft proposal attached in spirit only.\n",
	})
	for i := 0; i < 5; i++ {
		add(FolderWorkJunk, AcctWork, i+12, true, false, false, nil, -time.Duration(i*18+9)*time.Hour, "[promo]")
	}
	for i := 0; i < 4; i++ {
		add(FolderWorkTrash, AcctWork, i+15, true, false, false, nil, -time.Duration(i*22+15)*time.Hour, "")
	}
	for i := 0; i < 11; i++ {
		add(FolderWorkClients, AcctWork, i+4, i%3 != 0, i == 2, i%4 == 0, []string{"Important"}, -time.Duration(i*16+5)*time.Hour, "client:")
	}

	s.addMessage(Message{
		Folder: FolderAdaInbox, AccountID: AcctAda,
		From: "Kai Nakamura <kai@paintengine.example>", To: "Ada Lovelace <ada@example.com>",
		Subject: "Thread: lunch plans", RFCMessageID: "<lunch-1@example.com>",
		Date: DemoNow.Add(-2 * time.Hour), Read: false, Starred: true,
		Body: "Are you free around 12:30?\n",
	})
	s.addMessage(Message{
		Folder: FolderAdaInbox, AccountID: AcctAda,
		From: "Ada Lovelace <ada@example.com>", To: "Kai Nakamura <kai@paintengine.example>",
		Subject: "Re: Thread: lunch plans", RFCMessageID: "<lunch-2@example.com>",
		InReplyTo: "<lunch-1@example.com>", References: "<lunch-1@example.com>",
		Date: DemoNow.Add(-90 * time.Minute), Read: true,
		Body: "Yes — the usual place.\n",
	})
	s.addMessage(Message{
		Folder: FolderAdaInbox, AccountID: AcctAda,
		From: "Kai Nakamura <kai@paintengine.example>", To: "Ada Lovelace <ada@example.com>",
		Subject: "Re: Thread: lunch plans", RFCMessageID: "<lunch-3@example.com>",
		InReplyTo: "<lunch-2@example.com>", References: "<lunch-1@example.com> <lunch-2@example.com>",
		Date: DemoNow.Add(-40 * time.Minute), Read: false,
		Body: "See you there.\n",
	})
	assignThreadIDs(s.messages)
	if s.feat != nil && s.feat.index != nil {
		s.feat.index.rebuild(s.messages)
	}
}

const welcomeBody = `Hi Ada,

This is the uitoolkit Mail dogfood — Thunderbird’s classic 3-pane chrome
on a retained scene (UITK_SCENE=auto), not a Mozilla protocol clone.

What is real in v0.10
  • mailclientd: MemoryStore demo or IMAP+SMTP with an on-disk cache
  • OAuth (Google / Microsoft) + typed password, VIP, smart folders, threading
  • Offline outbox, IDLE/QRESYNC push, text-only Message tab

What is demo
  • This welcome lives in MemoryStore (UITK_MAIL=memory). Point a config
    file at a real IMAP/SMTP account — see docs/mail.md.

— Mail on uitoolkit v0.10.0
`
