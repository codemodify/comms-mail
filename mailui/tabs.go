package mailui

import (
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Messages open in tabs, the way the uitoolkit Files sample opens folders:
// a browser-style strip in the window's title bar, after the menu and
// Fetch / Write / Search, over the pages it switches — the folder pane
// stays beside them. The first tab is Mail — the list and the reading pane
// — and cannot be closed; E, a double click or Return on a row opens that
// message in a tab of its own, or brings its tab forward when it is
// already open. The keys that act on a message (R,
// Shift+R, F, A, D, T, M, Delete) act on the tab's message while it shows.

// messageTab is what a message tab holds: the message, in a reader of its
// own — the reading pane, larger (reader.go).
type messageTab struct {
	msg  mailcore.Message
	full bool // msg has its body
	view widget.Component
	rd   *reader
}

// setupTabs makes the strip with the Mail tab showing main, and reopens the
// message tabs a rebuild (a layout or density change) would otherwise drop.
func (s *session) setupTabs(main widget.Component) {
	var reopen []mailcore.Message
	sel := 0
	if s.tabs != nil {
		sel = s.tabs.Selected()
		for i := 1; i < s.tabs.Len(); i++ {
			if mt, ok := s.tabs.Tab(i).Data.(*messageTab); ok {
				reopen = append(reopen, mt.msg)
			}
		}
	}
	s.mainPage = main
	s.pages = widgets.NewStack(main)
	s.tabs = widgets.NewBrowserTabs()
	s.tabs.AddTab(widgets.BrowserTab{Title: "Mail", NoClose: true})
	s.tabs.OnSelect = s.showTab
	s.tabs.OnClose = s.closeTab
	s.tabs.OnContextMenu = func(i int, at paintengine2d.Point) bool {
		if i <= 0 {
			return false
		}
		widgets.ShowContextMenu(s.tabs, at,
			iconItem(style.IconClose, "Close Tab", "Ctrl+W", func() { s.tabs.CloseTab(i) }),
			iconItem(style.IconClose, "Close Other Tabs", "", func() { s.closeTabsExcept(i) }),
		)
		return true
	}
	for _, m := range reopen {
		s.addMessageTab(m)
	}
	if sel < 0 || sel >= s.tabs.Len() {
		sel = 0
	}
	s.tabs.Select(sel)
	s.showTab(sel)
}

// showTab shows tab i's page and hides the rest.
func (s *session) showTab(i int) {
	if s.tabs == nil {
		return
	}
	var show widget.Component = s.mainPage
	if mt, ok := s.tabs.Tab(i).Data.(*messageTab); ok && i > 0 {
		show = mt.view
	}
	for _, c := range s.pages.Children() {
		c.SetVisible(c == show)
	}
	s.pages.Invalidate()
}

func (s *session) closeTab(i int) {
	if i <= 0 || i >= s.tabs.Len() {
		return
	}
	if mt, ok := s.tabs.Tab(i).Data.(*messageTab); ok {
		s.pages.Remove(mt.view)
	}
	s.tabs.RemoveTab(i)
	s.showTab(s.tabs.Selected())
}

func (s *session) closeTabsExcept(keep int) {
	for i := s.tabs.Len() - 1; i > 0; i-- {
		if i != keep {
			s.closeTab(i)
		}
	}
}

// closeTabsFor closes the tabs of messages that have left the folder
// (archived, deleted, moved).
func (s *session) closeTabsFor(ids []mailcore.MessageID) {
	if s.tabs == nil {
		return
	}
	gone := map[mailcore.MessageID]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	for i := s.tabs.Len() - 1; i > 0; i-- {
		if mt, ok := s.tabs.Tab(i).Data.(*messageTab); ok && gone[mt.msg.ID] {
			s.closeTab(i)
		}
	}
}

// activeTab is the message tab showing, if one is.
func (s *session) activeTab() (*messageTab, bool) {
	if s.tabs == nil {
		return nil, false
	}
	i := s.tabs.Selected()
	if i <= 0 {
		return nil, false
	}
	mt, ok := s.tabs.Tab(i).Data.(*messageTab)
	return mt, ok
}

// openInTab opens the list's primary message in a tab of its own, or
// brings its tab forward.
func (s *session) openInTab() {
	if _, onTab := s.activeTab(); onTab {
		return
	}
	m, ok := s.listPrimary()
	if !ok {
		s.mark("No message")
		return
	}
	for i := 1; i < s.tabs.Len(); i++ {
		if mt, ok := s.tabs.Tab(i).Data.(*messageTab); ok && mt.msg.ID == m.ID {
			s.tabs.Select(i)
			s.showTab(i)
			return
		}
	}
	i := s.addMessageTab(m)
	s.tabs.Select(i)
	s.showTab(i)
}

// addMessageTab adds a tab for m after the last one and returns its index.
// The body loads off the UI goroutine when m does not carry it.
func (s *session) addMessageTab(m mailcore.Message) int {
	mt := &messageTab{msg: m, full: hasBody(m)}
	mt.rd = newReader(s)
	mt.view = mt.rd.view
	mt.view.SetVisible(false)
	s.pages.Add(mt.view)

	title := strings.TrimSpace(m.Subject)
	if title == "" {
		title = "(no subject)"
	}
	s.tabs.AddTab(widgets.BrowserTab{Title: title, Tip: title + "\n" + m.From, Data: mt})

	rd := mt.rd
	rd.showHeaders(m)
	rd.invite.show(m)
	if mt.full {
		rd.showBody(m)
	} else {
		rd.showPlain("Loading message…", "")
	}
	// The full message (a list row carries no HTML and no parts' text) is
	// fetched, as for the reading pane.
	load := func() {
		rd.retry.SetVisible(false)
		id := mt.msg.ID
		s.async(func() (any, error) {
			return s.getMessage(id)
		}, func(v any, err error) {
			if err != nil {
				if !mt.full {
					rd.showPlain("", "Couldn't load this message.\n\n"+err.Error())
					rd.retry.SetVisible(true)
				}
				return
			}
			full := v.(mailcore.Message)
			// Flags and tags changed in the window since it opened stay.
			full.Read, full.Starred, full.Tags = mt.msg.Read, mt.msg.Starred, mt.msg.Tags
			mt.msg, mt.full = full, true
			rd.showHeaders(full)
			rd.showBody(full)
			rd.invite.show(full)
		})
	}
	rd.onRetry = load
	if !mt.full || m.HTML == "" {
		load()
	}
	return s.tabs.Len() - 1
}

// tabsFollow applies a flag or tag change to the tabs of those messages.
func (s *session) tabsFollow(want map[mailcore.MessageID]bool, patch mailcore.FlagPatch) {
	if s.tabs == nil {
		return
	}
	for i := 1; i < s.tabs.Len(); i++ {
		mt, ok := s.tabs.Tab(i).Data.(*messageTab)
		if !ok || !want[mt.msg.ID] {
			continue
		}
		if patch.Read != nil {
			mt.msg.Read = *patch.Read
		}
		if patch.Starred != nil {
			mt.msg.Starred = *patch.Starred
		}
		if patch.Tags != nil {
			mt.msg.Tags = append([]string(nil), (*patch.Tags)...)
			mt.rd.showHeaders(mt.msg)
		}
	}
}

// tagMenuItems are the user's tags as check items for ids: ticked when the
// primary message has the tag, toggling it on every one of ids.
func (s *session) tagMenuItems() []*widgets.MenuItem {
	m, _ := s.primary()
	has := map[string]bool{}
	for _, t := range m.Tags {
		has[t] = true
	}
	var items []*widgets.MenuItem
	for _, t := range s.tagNames() {
		if mailcore.IsSystemTag(t) {
			continue
		}
		name := t
		items = append(items, widgets.CheckItem(name, has[name], func() { s.toggleTag(name) }))
	}
	if len(items) == 0 {
		items = append(items, &widgets.MenuItem{Text: "(no tags — add them in Settings)", Disabled: true})
	}
	return items
}

// showTagMenu is T: the tag menu under the message it acts on — the
// selected row, or the top of the open tab.
func (s *session) showTagMenu() {
	if len(s.ids()) == 0 {
		s.mark("No selection")
		return
	}
	var from widget.Component
	var at paintengine2d.Point
	if mt, ok := s.activeTab(); ok {
		from = mt.rd.text
		r := widget.LocalToWindow(mt.rd.text, mt.rd.text.LocalBounds())
		at = paintengine2d.Pt(r.Min.X+8, r.Min.Y+8)
	} else if i := s.primaryIndex(); i >= 0 && s.table != nil && s.table.Visible() {
		from = s.table
		r := widget.LocalToWindow(s.table, s.table.RowBounds(i))
		at = paintengine2d.Pt(r.Min.X+24, r.Max.Y)
	} else if s.cards != nil {
		from = s.cards
		r := widget.LocalToWindow(s.cards, s.cards.LocalBounds())
		at = paintengine2d.Pt(r.Min.X+24, r.Min.Y+24)
	}
	if from == nil {
		return
	}
	widgets.ShowContextMenu(from, at, s.tagMenuItems()...)
}
