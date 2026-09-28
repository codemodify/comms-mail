package mailui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// ComposeOptions configure a Write / Reply / Forward / Draft window.
type ComposeOptions struct {
	ReplyTo *mailcore.Message
	// ReplyAll widens a reply from the sender to everyone on the message.
	ReplyAll bool
	Forward  *mailcore.Message
	Draft    *mailcore.Message
	OnChange func() // refresh the 3-pane after send / save
}

// OpenCompose opens a second Window. Headless apps still get a surface.
func OpenCompose(a *app.Application, cli *mailcore.Client, opts ComposeOptions) (*app.Window, error) {
	title := "Write: (no subject)"
	if opts.ReplyTo != nil && strings.TrimSpace(opts.ReplyTo.Subject) != "" {
		title = "Write: Re: " + stripRe(opts.ReplyTo.Subject)
	} else if opts.Forward != nil && strings.TrimSpace(opts.Forward.Subject) != "" {
		title = "Write: Fwd: " + strings.TrimSpace(opts.Forward.Subject)
	} else if opts.Draft != nil && strings.TrimSpace(opts.Draft.Subject) != "" {
		title = "Write: " + strings.TrimSpace(opts.Draft.Subject)
	}
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 760, Height: 640, MinWidth: 520, MinHeight: 400,
	})
	if err != nil {
		return nil, err
	}
	win.SetContent(ComposeApp(a, win, cli, opts))
	return win, nil
}

// ComposeApp is the Write window: From / To / Cc / Bcc / Subject / body.
func ComposeApp(a *app.Application, win *app.Window, cli *mailcore.Client, opts ComposeOptions) widget.Component {
	idents := sendIdentities(cli)
	fromItems := make([]string, 0, len(idents))
	for _, id := range idents {
		fromItems = append(fromItems, id.DisplayFrom())
	}
	if len(fromItems) == 0 {
		fromItems = []string{"(no identity)"}
	}

	to0, cc0, bcc0, subj0, body0 := "", "", "", "", ""
	fromIdx := 0
	draftID := mailcore.MessageID("")
	// Threading headers for the outgoing message. Without them a reply
	// starts a new conversation in the recipient's client.
	inReplyTo, references := "", ""
	forwardOf := mailcore.MessageID("")
	if opts.Draft != nil {
		d := opts.Draft
		to0, cc0, bcc0, subj0, body0 = d.To, d.Cc, d.Bcc, d.Subject, d.Body
		draftID = d.ID
		fromIdx = indexFrom(fromItems, d.From)
		inReplyTo, references = d.InReplyTo, d.References
	} else if opts.ReplyTo != nil {
		m := opts.ReplyTo
		if opts.ReplyAll {
			to0, cc0 = mailcore.ReplyAllRecipients(*m, selfAddrs(idents))
		} else {
			to0 = replyToAddr(*m)
		}
		subj0 = "Re: " + stripRe(m.Subject)
		body0 = quoteBody(*m)
		fromIdx = indexFrom(fromItems, m.To+", "+m.Cc)
		inReplyTo, references = mailcore.ReplyThreadHeaders(*m)
	} else if opts.Forward != nil {
		m := opts.Forward
		forwardOf = m.ID // marked forwarded once this is sent
		subj0 = "Fwd: " + strings.TrimSpace(m.Subject)
		body0 = forwardBody(*m)
		fromIdx = indexFrom(fromItems, m.To)
	}
	// The signature goes in where it can be seen and edited: under what is
	// written, above the quote. A draft already has whatever it was saved
	// with.
	curSig := ""
	if opts.Draft == nil && fromIdx >= 0 && fromIdx < len(idents) {
		curSig = signatureBlock(idents[fromIdx].Signature)
		body0 = curSig + body0
	}

	status := widgets.NewStatusBar("Write a message.", "Offline demo", "v"+uitoolkit.Version)
	from := widgets.NewComboBox(fromItems, fromIdx, nil)
	// To / Cc / Bcc complete addresses from the address book as you type.
	newRcpt := func(placeholder, initial string) *recipientField {
		var rf *recipientField
		var gen uint64
		rf = newRecipientField(placeholder, func(token string) {
			gen++
			g := gen
			runAsync(a, func() (any, error) {
				return cli.SuggestContacts(token, maxSuggestRows)
			}, func(v any, err error) {
				if err != nil || g != gen {
					return
				}
				rf.setSuggestions(v.([]mailcore.Contact))
			})
		})
		if initial != "" {
			rf.SetText(initial)
		}
		return rf
	}
	to := newRcpt("To", to0)
	cc := newRcpt("Cc", cc0)
	bcc := newRcpt("Bcc", bcc0)
	subject := widgets.NewTextField(subj0, "Subject", func(s string) {
		t := strings.TrimSpace(s)
		if t == "" {
			t = "(no subject)"
		}
		win.SetTitle("Write: " + t)
	})
	body := widgets.NewTextArea(body0, "Compose in Titillium Web. Source-style quotes stay readable.", nil)
	body.MinRows = 10
	body.Wrap = true
	// Choosing another From swaps that identity's signature for the old
	// one, when the old one is still there as it was put in.
	from.OnChange = func(i int) {
		if i < 0 || i >= len(idents) {
			return
		}
		next := signatureBlock(idents[i].Signature)
		switch {
		case curSig != "" && strings.Contains(body.Text, curSig):
			body.SetText(strings.Replace(body.Text, curSig, next, 1))
		case curSig == "" && next != "" && opts.Draft == nil:
			body.SetText(next + body.Text)
		default:
			return
		}
		curSig = next
	}

	var attachPaths []string
	accountID := func() string {
		i := from.Selected
		if i >= 0 && i < len(idents) {
			if idents[i].AccountID != "" {
				return idents[i].AccountID
			}
			return idents[i].ID
		}
		if len(idents) > 0 {
			return idents[0].AccountID
		}
		return ""
	}
	identityID := func() string {
		i := from.Selected
		if i >= 0 && i < len(idents) {
			return idents[i].ID
		}
		return ""
	}
	fromText := func() string {
		if from.Selected >= 0 && from.Selected < len(fromItems) {
			return fromItems[from.Selected]
		}
		return ""
	}

	collect := func() mailcore.Message {
		return mailcore.Message{
			From:       fromText(),
			To:         to.Text(),
			Cc:         cc.Text(),
			Bcc:        bcc.Text(),
			Subject:    subject.Text,
			Body:       body.Text,
			InReplyTo:  inReplyTo,
			References: references,
			Read:       true,
			// The body carries the signature, or the writer took it out.
			SignatureInBody: true,
		}
	}

	// Both of these are daemon round trips (a send can take a while): run
	// them off the UI goroutine and apply the result back on it.
	//
	// Drafts are also saved on their own: every autosaveEvery, a message
	// changed since it was last saved is written to Drafts (the same draft
	// each time), so a crash or a lost window costs at most that much.
	lastSaved := collect() // what is on disk (or what the window opened with)
	var saving, again, sent, autoOnly bool
	sentDraft := mailcore.MessageID("")
	var saveNow func(auto bool)
	saveNow = func(auto bool) {
		acct := accountID()
		if acct == "" {
			if !auto {
				widgets.Warn(win.Content(), "Save Draft", "No account is configured.", nil)
			}
			return
		}
		if saving {
			// One save at a time, or the second would make a second draft
			// before the first reported its id. A click waits its turn.
			again = again || !auto
			return
		}
		msg := collect()
		did := draftID
		saving = true
		if !auto {
			status.Set(0, "Saving draft…")
		}
		runAsync(a, func() (any, error) {
			return cli.SaveDraft(acct, msg, did)
		}, func(v any, err error) {
			saving = false
			if err != nil {
				if auto {
					status.Set(0, "Autosave failed: "+err.Error())
				} else {
					widgets.Warn(win.Content(), "Save Draft", err.Error(), nil)
				}
				return
			}
			id := v.(mailcore.MessageID)
			if sent {
				// Sent (or thrown away) while this save was in flight: the
				// send could not know this draft to remove it.
				if id != sentDraft {
					runAsync(a, func() (any, error) { return nil, cli.Delete([]mailcore.MessageID{id}) }, nil)
				}
				return
			}
			if did == "" {
				autoOnly = auto // a draft only autosave made, not the writer
			} else if !auto {
				autoOnly = false
			}
			draftID, lastSaved = id, msg
			if auto {
				status.Set(0, "Draft autosaved at "+time.Now().Format("15:04"))
			} else {
				status.Set(0, "Saved draft")
				autoOnly = false
				if opts.OnChange != nil {
					opts.OnChange()
				}
			}
			if again {
				again = false
				saveNow(false)
			}
		})
	}
	saveDraft := func() { saveNow(false) }
	dirty := func() bool { return !sameDraft(collect(), lastSaved) }
	autosave := func() {
		if !sent && dirty() {
			saveNow(true)
		}
	}

	send := func() {
		if strings.TrimSpace(to.Text()) == "" {
			widgets.Warn(win.Content(), "Send", "Please enter a To: address.", nil)
			return
		}
		acct := accountID()
		msg := collect()
		ident := identityID()
		did := draftID
		status.Set(0, "Sending…")
		sent, sentDraft = true, did
		runAsync(a, func() (any, error) {
			// Attachments are read here and shipped as bytes: comms-maild
			// does not open paths on a client's behalf.
			files, err := mailcore.ReadAttachments(attachPaths)
			if err != nil {
				return nil, err
			}
			return cli.SendForward(acct, ident, msg, did, files, forwardOf)
		}, func(_ any, err error) {
			var queued *mailcore.QueuedError
			if errors.As(err, &queued) {
				// Not lost and not to be sent again: it waits in the Outbox.
				if opts.OnChange != nil {
					opts.OnChange()
				}
				widgets.Info(win.Content(), "Not sent yet",
					"The server could not be reached, so the message is in the Outbox and will be sent when it answers.\n\n"+queued.Reason,
					func() { win.Close() })
				return
			}
			if err != nil {
				sent = false
				status.Set(0, "Send failed")
				widgets.Warn(win.Content(), "Send", err.Error(), nil)
				return
			}
			if opts.OnChange != nil {
				opts.OnChange()
			}
			widgets.Info(win.Content(), "Sent",
				"Message handed to comms-maild: submitted over SMTP and filed in Sent\n(queued in the Outbox when offline).",
				func() { win.Close() })
		})
	}

	// closeWin asks before closing a message with unsaved work, or one only
	// autosave kept: No throws that draft away.
	closeWin := func() {
		if sent || (!dirty() && !autoOnly) {
			win.Close()
			return
		}
		widgets.Confirm(win.Content(), "Close write window?",
			"Save this message as a draft?",
			func(yes bool) {
				if yes {
					saveDraft()
					win.Close()
					return
				}
				// A save still in flight lands after this; drop what it
				// makes unless it is the draft this window was opened on.
				keep := draftID
				if autoOnly {
					keep = ""
				}
				sent, sentDraft = true, keep
				if autoOnly && draftID != "" {
					id := draftID
					runAsync(a, func() (any, error) { return nil, cli.Delete([]mailcore.MessageID{id}) }, func(any, error) {
						if opts.OnChange != nil {
							opts.OnChange()
						}
					})
				}
				win.Close()
			})
	}
	if win != nil {
		// The window's close button asks too.
		win.SetOnCloseRequest(func() bool {
			if sent || (!dirty() && !autoOnly) {
				return true
			}
			closeWin()
			return false
		})
		if appLooping(a) {
			go func() {
				tick := time.NewTicker(autosaveEvery)
				defer tick.Stop()
				for range tick.C {
					if win.Closed() {
						return
					}
					a.Post(func() {
						if !win.Closed() {
							autosave()
						}
					})
				}
			}()
		}
	}
	if composeSeam != nil {
		composeSeam(autosave, closeWin)
	}

	menubar := widgets.NewMenuBar(
		widgets.NewMenu("&File",
			widgets.ItemAccel("&Send Now", "Ctrl+Enter", send),
			widgets.ItemAccel("Save as &Draft", "Ctrl+S", saveDraft),
			widgets.Sep(),
			widgets.Item("Close", closeWin),
		),
		widgets.NewMenu("&Edit",
			widgets.ItemAccel("Select &All", "Ctrl+A", func() {
				body.SetSelection(0, len([]rune(body.Text)))
			}),
			&widgets.MenuItem{Text: "Undo", Shortcut: "Ctrl+Z", Disabled: true},
		),
		widgets.NewMenu("&View",
			widgets.Item("Body as plain text", func() { status.Set(0, "Plain text (demo)") }),
		),
		widgets.NewMenu("&Insert",
			widgets.Item("File…", func() {
				widgets.Info(win.Content(), "Attach",
					"FileDialog is a stub in v1. Mark HasAttach on a Store message later.", nil)
			}),
		),
		widgets.NewMenu("&Help",
			widgets.Item("About Write", func() {
				widgets.Info(win.Content(), "Write",
					"Compose window on uitoolkit.\nUI: Titillium Web.\nSend files to Sent in the demo Store.", nil)
			}),
		),
	)

	sendBtn := widgets.ToolIconBtn(style.IconNew, "Send", send)
	sendBtn.Tip = "Send Now (demo: file in Sent)"
	draftBtn := widgets.ToolIconBtn(style.IconSave, "Save", saveDraft)
	draftBtn.Tip = "Save as Draft"
	attachBtn := widgets.ToolIconBtn(style.IconOpen, "Attach", func() {
		widgets.ShowFileDialog(win.Content(), widgets.FileDialogOptions{
			Title: "Attach file",
			Path:  ".",
			OnNavigate: func(path string) []widgets.FileInfo {
				ents, err := os.ReadDir(path)
				if err != nil {
					return nil
				}
				var out []widgets.FileInfo
				for _, e := range ents {
					out = append(out, widgets.FileInfo{Name: e.Name(), Dir: e.IsDir()})
				}
				return out
			},
			OnPick: func(path string) {
				attachPaths = append(attachPaths, path)
				status.Set(0, "Attached "+path)
			},
		})
	})
	attachBtn.Tip = "Attach file (stub)"
	tools := widgets.NewToolBar(sendBtn, draftBtn, widgets.ToolDivider(), attachBtn)

	// One label column, so the fields line up (right-aligned labels under
	// Mac looks).
	form := widgets.NewForm()
	form.RowGap = 6
	form.AddRow("From", from)
	form.AddRow("To", to)
	form.AddRow("Cc", cc)
	form.AddRow("Bcc", bcc)
	form.AddRow("Subject", subject)
	fields := widgets.NewPad(10, form)

	chrome := widgets.NewTitleBar("Write", "compose  ·  comms-maild  ·  v"+uitoolkit.Version)
	bodyPad := widgets.NewPad(8, body)
	root := widgets.NewColumn(menubar, tools, chrome, fields, bodyPad, status).WithGap(0)
	root.AddFlex(bodyPad, 1)
	_ = a
	// Files dragged in from a file manager are attached; text dropped on a
	// field goes into that field (the fields take text themselves).
	return widgets.NewDropZone(root, func(paths []string) {
		attachPaths = append(attachPaths, paths...)
		if len(paths) == 1 {
			status.Set(0, "Attached "+paths[0])
		} else {
			status.Set(0, fmt.Sprintf("Attached %d files", len(paths)))
		}
	})
}

// autosaveEvery is how often the Write window saves a changed message to
// Drafts on its own.
var autosaveEvery = 10 * time.Second

// composeSeam, when set (tests), is handed each Write window's autosave
// step and its close action.
var composeSeam func(autosave, close func())

// sameDraft reports whether two states of a message would save the same.
func sameDraft(a, b mailcore.Message) bool {
	return a.From == b.From && a.To == b.To && a.Cc == b.Cc && a.Bcc == b.Bcc &&
		a.Subject == b.Subject && a.Body == b.Body
}

// replyToAddr honours Reply-To when the sender set one.
// sendIdentities is every From the writer can pick: the identities the
// daemon has, and for an account with none of its own, the account itself.
func sendIdentities(cli *mailcore.Client) []mailcore.Identity {
	idents, _ := cli.Identities("")
	has := map[string]bool{}
	for _, id := range idents {
		has[id.AccountID] = true
	}
	accts, _ := cli.Accounts()
	for _, a := range accts {
		// The imported-mail account has no server and cannot send.
		if a.ID == mailcore.LocalAccountID {
			continue
		}
		if !has[a.ID] {
			idents = append(idents, mailcore.Identity{ID: a.ID, AccountID: a.ID, Name: a.Name, Address: a.Address, Default: true})
		}
	}
	return idents
}

// signatureBlock is sig as it goes into a body: after a blank line, under
// the "-- " delimiter mail clients use to recognise and trim signatures.
func signatureBlock(sig string) string {
	sig = strings.TrimRight(sig, "\n")
	if strings.TrimSpace(sig) == "" {
		return ""
	}
	return "\n\n-- \n" + sig + "\n"
}

// selfAddrs is every address the writer sends as, for Reply All to leave out.
func selfAddrs(idents []mailcore.Identity) []string {
	out := make([]string, 0, len(idents))
	for _, id := range idents {
		out = append(out, id.Address)
	}
	return out
}

func replyToAddr(m mailcore.Message) string {
	if r := strings.TrimSpace(m.ReplyTo); r != "" {
		return mailcore.FirstAddr(r)
	}
	return mailcore.FirstAddr(m.From)
}

func stripRe(s string) string {
	s = strings.TrimSpace(s)
	for {
		low := strings.ToLower(s)
		if strings.HasPrefix(low, "re:") {
			s = strings.TrimSpace(s[3:])
			continue
		}
		if strings.HasPrefix(low, "fwd:") {
			s = strings.TrimSpace(s[4:])
			continue
		}
		if strings.HasPrefix(low, "fw:") {
			s = strings.TrimSpace(s[3:])
			continue
		}
		return s
	}
}

func quoteBody(m mailcore.Message) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n\nOn %s, %s wrote:\n", m.Date.Format("Mon 2 Jan 2006 15:04"), mailcore.DisplayName(m.From)))
	for _, line := range strings.Split(m.Body, "\n") {
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func forwardBody(m mailcore.Message) string {
	return fmt.Sprintf("\n\n-------- Forwarded Message --------\nFrom: %s\nDate: %s\nSubject: %s\nTo: %s\n\n%s",
		m.From, m.Date.Format("Mon 2 Jan 2006 15:04 MST"), m.Subject, m.To, m.Body)
}

func indexFrom(items []string, from string) int {
	from = strings.TrimSpace(from)
	if from == "" {
		return 0
	}
	want := strings.ToLower(mailcore.DisplayName(from))
	addr := strings.ToLower(from)
	for i, it := range items {
		low := strings.ToLower(it)
		if low == addr || strings.Contains(low, want) {
			return i
		}
	}
	return 0
}
