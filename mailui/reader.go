package mailui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// reader shows one message: its header — subject, From, To, Cc, Date, tags
// — the calendar invitation it carries, a row of actions (the selected
// attachment's Open and Save, Save All, and Open HTML for a message that
// has HTML) over its attachments, and three tabs: Message (the text),
// Source (the raw message, fetched when the tab is shown) and Markdown
// (the message rendered, with the remote-images bar).
//
// The reading pane is one reader; a message opened in a tab of its own is
// another, the same thing larger.
type reader struct {
	s    *session
	view *widgets.FlexBox

	subj                      *widgets.Label
	from, to, cc, date, extra *widgets.Label
	// sec is what secretvault says of a signed or encrypted message
	// (security.go).
	sec    *securityPart
	invite *inviteCard
	// retry is offered when the message could not be loaded; onRetry is
	// what it does.
	retry   *widgets.FlexBox
	onRetry func()

	actions                  *widgets.Wrap
	attOpen, attSave, attAll *widgets.ToolButton
	htmlBtn                  *widgets.ToolButton
	attStrip                 widget.Component
	attRows                  *widgets.FlexBox
	attHits                  []*attachHit
	attNames                 []string
	attSel                   int
	attClickI                int
	attClickT                time.Time

	tabs     *widgets.TabView
	text     *widgets.TextArea
	source   *widgets.TextArea
	sourceID mailcore.MessageID
	md       *htmlPane

	// msg is the message showing: the list row until its body is in.
	msg mailcore.Message
	// gen counts the messages shown; a late answer for another is dropped.
	gen uint64
}

// The reader's tabs.
const (
	readerTabText = iota
	readerTabSource
	readerTabMarkdown
)

// attachActivateWindow is how soon a second click on an attachment opens
// it.
const attachActivateWindow = 400 * time.Millisecond

// attachStripHeight is the attachment list's height (1x pixels): about
// three rows, whatever the message carries; more scroll.
const attachStripHeight = 92

func newReader(s *session) *reader {
	r := &reader{s: s, attSel: -1, attClickI: -1}
	r.subj = widgets.NewTitle("")
	r.from = widgets.NewLabel("")
	r.to = widgets.NewLabel("")
	r.cc = widgets.NewLabel("")
	r.cc.SetVisible(false)
	r.date = widgets.NewLabel("")
	r.extra = widgets.NewLabel("")
	r.sec = newSecurityPart()
	r.sec.unlock.OnClick = r.unlockSecretVault
	r.invite = newInviteCard(s)

	// Short labels: the row fits the narrowest reading pane (the header
	// line already says how many attachments there are).
	r.attOpen = widgets.NewToolButton("Open", style.IconOpen, func() { r.openAttachment(r.attSel) })
	r.attOpen.Tip = "Open the selected attachment"
	r.attSave = widgets.NewToolButton("Save", style.IconSave, func() { r.saveAttachment(r.attSel) })
	r.attSave.Tip = "Save the selected attachment as…"
	r.attAll = widgets.NewToolButton("Save All", style.IconSave, r.saveAllAttachments)
	r.attAll.Tip = "Save every attachment in a folder"
	// Nothing of the HTML is drawn in the window: it opens in the browser,
	// as it was sent (views.go).
	r.htmlBtn = widgets.NewToolButton("Open HTML", style.IconExternalLink, r.openHTML)
	r.htmlBtn.Tip = "Open the message as it was sent, in your browser. Its remote images load there, " +
		"which tells the sender you opened it; scripts are blocked."
	// It folds onto a second line in a narrow pane.
	r.actions = widgets.NewWrap(r.attOpen, r.attSave, r.attAll, r.htmlBtn)
	r.actions.Gap = 4
	// The attachments are a strip of fixed height — about three rows —
	// that scrolls: however many a message carries, the text below keeps
	// its room.
	r.attRows = widgets.NewColumn().WithGap(2)
	r.attStrip = widgets.NewHeightBox(attachStripHeight, widgets.NewScrollView(r.attRows))
	r.retry = widgets.NewRow(widgets.NewToolButton("Retry", style.IconRedo, func() {
		if r.onRetry != nil {
			r.onRetry()
		}
	}))
	r.retry.SetVisible(false)

	r.text = widgets.NewTextView("", "Select a message (plain text)")
	r.text.MinRows = 8
	r.source = widgets.NewMonoTextView("", "Raw source")
	r.source.MinRows = 8
	r.source.Wrap = false
	r.md = newHTMLPane(s)
	r.md.rich.Placeholder = "This message has no text."
	r.tabs = widgets.NewTabView(
		widgets.Tab{Title: "Message", Content: widgets.NewPad(8, r.text)},
		widgets.Tab{Title: "Source", Content: widgets.NewPad(8, r.source)},
		widgets.Tab{Title: "Markdown", Content: widgets.NewPad(4, r.md.view)},
	)
	r.tabs.OnChange = func(i int) {
		switch i {
		case readerTabSource:
			s.mark("Source  ·  JetBrains Mono")
			r.loadSource()
		case readerTabMarkdown:
			s.mark("Markdown")
		default:
			s.mark("Message")
		}
	}

	head := widgets.NewColumn(r.subj, r.from, r.to, r.cc, r.date, r.extra, r.sec.view,
		r.invite.view, r.actions, r.attStrip, r.retry).WithGap(3).WithPad(10)
	// The header grows with what the message carries (an invitation,
	// attachments) but always leaves the body room for a few lines: past
	// that it scrolls.
	r.view = widgets.NewColumn(newReserveBox(200, newHeaderScroll(head)), widgets.NewSeparator(), r.tabs).WithGap(0)
	r.view.AddFlex(r.tabs, 1)
	r.syncActions()
	return r
}

// showHeaders shows m's header and attachments. m may be a list row, with
// no body; for the message already showing, the body it has is kept.
func (r *reader) showHeaders(m mailcore.Message) {
	if m.ID != r.msg.ID {
		r.gen++
		r.sourceID = ""
		r.source.SetText("")
		r.sec.clear()
	} else if !hasBody(m) {
		m.Body, m.HTML = r.msg.Body, r.msg.HTML
	}
	r.msg = m
	if r.sec.id != m.ID || r.sec.content == nil || r.sec.content.Subject == "" {
		r.subj.SetText(m.Subject)
	}
	r.from.SetText("From: " + m.From)
	r.to.SetText("To: " + m.To)
	r.cc.SetText("Cc: " + m.Cc)
	r.cc.SetVisible(strings.TrimSpace(m.Cc) != "")
	r.date.SetText("Date: " + m.Date.Format("Mon, 02 Jan 2006 15:04 MST"))
	var extra []string
	var tags []string
	for _, t := range m.Tags {
		if !mailcore.IsSystemTag(t) {
			tags = append(tags, t)
		}
	}
	if len(tags) > 0 {
		extra = append(extra, "Tags: "+strings.Join(tags, ", "))
	}
	if len(m.Attachments) > 0 {
		extra = append(extra, pluralize(len(m.Attachments), "attachment"))
	}
	if did := repliedForwarded(m); did != "" {
		extra = append(extra, did)
	}
	r.extra.SetText(strings.Join(extra, "  ·  "))
	r.attNames = append([]string(nil), m.Attachments...)
	r.syncAttachPane()
}

// showBody puts m, loaded, in the tabs.
func (r *reader) showBody(m mailcore.Message) {
	if m.ID != r.msg.ID {
		r.showHeaders(m)
	}
	r.msg = m
	switch {
	case r.sec.id == m.ID && r.sec.content != nil:
		// Already decrypted: the text stays what secretvault opened.
		r.showContent(*r.sec.content)
	default:
		r.text.Placeholder = "This message has no text"
		r.text.SetText(mailcore.DisplayBody(m))
		md := m
		md.HTML = mailcore.MarkdownToHTML(mailcore.BodyMarkdown(m))
		r.md.show(md)
	}
	if m.Signed || m.Encrypted || m.Autocrypt {
		r.loadSecurity(m)
	}
	r.syncActions()
	if r.tabs.Selected() == readerTabSource {
		r.loadSource()
	}
}

// showPlain puts text in the Message tab and empties the rendering: for
// loading and errors, neither of which is the message.
func (r *reader) showPlain(placeholder, text string) {
	r.text.Placeholder = placeholder
	r.text.SetText(text)
	r.md.clear()
}

// clear shows no message.
func (r *reader) clear() {
	r.gen++
	r.msg = mailcore.Message{}
	r.sourceID = ""
	r.source.SetText("")
	r.subj.SetText("No message selected")
	for _, l := range []*widgets.Label{r.from, r.to, r.cc, r.date, r.extra} {
		l.SetText("")
	}
	r.cc.SetVisible(false)
	r.sec.clear()
	r.attNames = nil
	r.syncAttachPane()
	r.invite.clear()
	r.retry.SetVisible(false)
	r.showPlain("Select a message (plain text)", "")
}

// loadSource fills the Source tab, off the UI goroutine. It runs when the
// tab is shown, not for every message.
func (r *reader) loadSource() {
	id := r.msg.ID
	if id == "" || r.sourceID == id {
		return
	}
	r.sourceID = id
	gen := r.gen
	r.source.Placeholder = "Loading source…"
	r.source.SetText("")
	r.s.async(func() (any, error) {
		return r.s.cli.GetSource(id)
	}, func(v any, err error) {
		if gen != r.gen {
			return
		}
		if err != nil {
			r.sourceID = ""
			r.source.SetText("Couldn't load the source.\n\n" + err.Error())
			return
		}
		r.source.Placeholder = "Raw source"
		r.source.SetText(v.(string))
	})
}

// syncActions shows the actions that apply: the attachments' for a
// message with attachments, Open HTML for one with HTML.
func (r *reader) syncActions() {
	has := len(r.attNames) > 0
	html := strings.TrimSpace(r.msg.HTML) != ""
	for _, b := range []*widgets.ToolButton{r.attOpen, r.attSave, r.attAll} {
		b.SetVisible(has)
	}
	r.htmlBtn.SetVisible(html)
	r.actions.SetVisible(has || html)
	r.attStrip.SetVisible(has)
	r.view.RequestLayout()
}

// openHTML opens the message's HTML part in the browser, as it was sent.
func (r *reader) openHTML() {
	m := r.msg
	if m.ID == "" {
		return
	}
	s := r.s
	s.mark("Opening the HTML in your browser…")
	s.async(func() (any, error) {
		raw, err := s.cli.Raw(m.ID)
		if err != nil {
			return nil, err
		}
		html, ok := mailcore.OriginalHTML(raw)
		if !ok {
			html = m.HTML // no part in the source (an imported message): the window's copy
		}
		imgs, _ := s.cli.InlineImages(m.ID)
		mailcore.RemoveOld(filepath.Join(os.TempDir(), htmlViewPattern), mailcore.PrintedKeep)
		f, err := os.CreateTemp("", htmlViewPattern)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if _, err := f.WriteString(browserPage(html, imgs)); err != nil {
			return nil, err
		}
		if !mailcore.OpenWithDesktop(f.Name()) {
			return f.Name(), errNoBrowser(f.Name())
		}
		return f.Name(), nil
	}, func(_ any, err error) {
		if err != nil {
			widgets.Warn(s.win.Content(), "Open HTML", err.Error(), nil)
			return
		}
		s.mark("Opened in your browser")
	})
}

// syncAttachPane lists the attachments, the first selected: Open and Save
// act on it until another is picked.
func (r *reader) syncAttachPane() {
	r.attSel = -1
	if len(r.attNames) > 0 {
		r.attSel = 0
	}
	r.attClickI = -1
	r.attClickT = time.Time{}
	r.attRows.ClearChildren()
	r.attHits = r.attHits[:0]
	for i, name := range r.attNames {
		i := i
		hit := newAttachHit(name, func() { r.selectAttachment(i) })
		hit.Drag = func() *widget.Drag { return r.dragAttachment(i) }
		r.attRows.Add(hit)
		r.attHits = append(r.attHits, hit)
	}
	r.paintAttachSelection()
	r.syncActions()
}

func (r *reader) paintAttachSelection() {
	for i, h := range r.attHits {
		sel := i == r.attSel
		if h.Selected != sel {
			h.Selected = sel
			h.Invalidate()
		}
	}
}

// selectAttachment is a click on attachment i: a second one soon after
// opens it.
func (r *reader) selectAttachment(i int) {
	now := time.Now()
	activate := i >= 0 && i == r.attClickI && !r.attClickT.IsZero() && now.Sub(r.attClickT) < attachActivateWindow
	r.attSel = i
	r.attClickI = i
	r.attClickT = now
	r.paintAttachSelection()
	if activate {
		r.openAttachment(i)
		return
	}
	if i >= 0 && i < len(r.attNames) {
		r.s.mark("Attachment: " + r.attNames[i])
	}
}

// attachmentParts is the subset of m.Parts that the attachment rows show,
// in the same order as m.Attachments.
func attachmentParts(m mailcore.Message) []mailcore.Part {
	var out []mailcore.Part
	for _, p := range m.Parts {
		if strings.TrimSpace(p.Filename) != "" {
			out = append(out, p)
		}
	}
	return out
}

// attachPartID maps attachment row i to its MIME section id.
//
// Row i indexes m.Attachments (filenames); m.Parts also holds the text
// parts, so indexing Parts directly used to fetch the message body for
// "Save As report.pdf". Match by filename first, then by position among the
// parts that actually have a filename.
func (r *reader) attachPartID(m mailcore.Message, i int) string {
	if i < 0 || i >= len(r.attNames) {
		return ""
	}
	want := r.attNames[i]
	parts := attachmentParts(m)
	for _, p := range parts {
		if p.Filename == want && p.ID != "" {
			return p.ID
		}
	}
	if i < len(parts) && parts[i].ID != "" {
		return parts[i].ID
	}
	// MemoryStore / demo messages have no BODYSTRUCTURE parts.
	return fmt.Sprintf("att-%d", i+1)
}

func (r *reader) openAttachment(i int) {
	s, m := r.s, r.msg
	if m.ID == "" || i < 0 || i >= len(r.attNames) {
		return
	}
	pid := r.attachPartID(m, i)
	name := r.attNames[i]
	s.mark("Opening " + name + "…")
	s.async(func() (any, error) {
		return s.cli.OpenPart(m.ID, pid)
	}, func(v any, err error) {
		if err != nil {
			s.mark("Open: " + err.Error())
			return
		}
		p := v.(mailcore.PartData)
		if p.Path != "" {
			s.mark("Opened " + p.Path)
			return
		}
		s.mark("Attachment: " + name)
	})
}

func (r *reader) saveAttachment(i int) {
	s, m := r.s, r.msg
	if m.ID == "" || i < 0 || i >= len(r.attNames) || s.win == nil {
		return
	}
	raw := r.attNames[i]
	name := mailcore.AttachFileName(raw)
	pid := r.attachPartID(m, i)
	s.async(func() (any, error) {
		return s.partBytes(m.ID, pid, raw)
	}, func(v any, err error) {
		if err != nil {
			s.mark("Save As: " + err.Error())
			return
		}
		data := v.([]byte)
		widgets.ShowFileDialog(s.win.Content(), widgets.FileDialogOptions{
			Title:      "Save As",
			Mode:       widgets.FileSave,
			Path:       os.TempDir(),
			Name:       name,
			OnNavigate: mailDirEntries,
			OnPick: func(path string) {
				if path == "" {
					return
				}
				if st, err := os.Stat(path); err == nil && st.IsDir() {
					path = filepath.Join(path, name)
				}
				if err := mailcore.WriteFileAtomic(path, data, 0o600); err != nil {
					s.mark("Save As: " + err.Error())
					return
				}
				s.mark("Saved " + path)
			},
		})
	})
}

// saveAllAttachments picks one folder (a path that is a file uses its
// parent; a missing one with no extension is created) then writes every
// attachment of the message there (0600).
func (r *reader) saveAllAttachments() {
	s, m := r.s, r.msg
	if m.ID == "" || len(r.attNames) == 0 || s.win == nil {
		return
	}
	names := append([]string(nil), r.attNames...)
	pids := make([]string, len(names))
	for i := range names {
		pids[i] = r.attachPartID(m, i)
	}
	s.mark("Collecting attachments…")
	s.async(func() (any, error) {
		items := make([]attachBlob, 0, len(names))
		for i := range names {
			data, err := s.partBytes(m.ID, pids[i], names[i])
			if err != nil {
				return nil, err
			}
			items = append(items, attachBlob{Name: mailcore.AttachFileName(names[i]), Data: data})
		}
		return items, nil
	}, func(v any, err error) {
		if err != nil {
			s.mark("Save All: " + err.Error())
			return
		}
		items := v.([]attachBlob)
		widgets.ShowFileDialog(s.win.Content(), widgets.FileDialogOptions{
			Title:      "Save all attachments in",
			Mode:       widgets.FileOpenFolder,
			Path:       os.TempDir(),
			OnNavigate: mailDirEntries,
			OnPick: func(path string) {
				dir, err := saveAllDir(path)
				if err != nil {
					s.mark("Save All: " + err.Error())
					return
				}
				used := map[string]int{}
				for _, it := range items {
					dest := filepath.Join(dir, uniqueFileName(it.Name, used))
					if err := mailcore.WriteFileAtomic(dest, it.Data, 0o600); err != nil {
						s.mark("Save All: " + err.Error())
						return
					}
				}
				s.mark(fmt.Sprintf("Saved %d attachment(s) to %s", len(items), dir))
			},
		})
	})
}
