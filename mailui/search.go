package mailui

import (
	"strings"

	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Search is a dialog, opened by the Search button after Write or by
// Ctrl+F: what to look for, in this folder or all of them, and whether to
// ask the mail server too. Search narrows the list to what matches; the
// button stays pressed while it does, and Clear (in the same dialog) takes
// the list back.

// openSearch shows the search dialog over the window.
func (s *session) openSearch() {
	if s.win == nil {
		return
	}
	field := widgets.NewTextField(s.filter.Query, "Subject, people or text", nil)
	all := widgets.NewCheckbox("All folders", s.searchAll, nil)
	server := widgets.NewCheckbox("On the server too", s.srv.on, nil)
	serverNote := wrapLabel("Finds words in mail not downloaded yet; the mail server is asked as well.")

	var ov *widgets.Overlay
	done := func() { widget.DismissOverlay(ov) }
	apply := func() {
		s.setSearch(field.Text, all.Checked, server.Checked)
		done()
	}
	clear := newButton("Clear", func() {
		s.setSearch("", false, server.Checked)
		done()
	})
	clear.Tip = "Show the whole folder again"
	cancel := newButton("Cancel", done)
	search := newButton("Search", apply)
	search.Icon = style.IconSearch
	search.Primary = true
	field.OnSubmit = func(string) { apply() }

	buttons := widgets.NewButtonBox().
		AddButton(clear, widgets.RoleAction).
		AddButton(cancel, widgets.RoleReject).
		AddButton(search, widgets.RoleAccept)
	col := widgets.NewColumn(field, all, server, serverNote, buttons).WithGap(8).WithPad(4)
	card := widgets.NewPanel("Search", col)
	card.Window = true
	card.Raised = true
	card.OnClose = done
	ov = widgets.NewOverlay(card)
	ov.Modal = true
	ov.MinCardW = 420
	ov.InitialFocus = field
	widget.ShowOverlay(s.win.Content(), ov)
	field.SelectAll()
}

// setSearch narrows the list to what matches query — in this folder or in
// all of them, and on the server too when server is set — or shows the
// whole folder again for an empty query.
func (s *session) setSearch(query string, all, server bool) {
	query = strings.TrimSpace(query)
	s.filter.Query = query
	s.searchAll = all && query != ""
	if server != s.srv.on {
		s.srv.on = server
		s.srv.asked, s.srv.key, s.srv.hits = "", "", nil
	}
	s.syncSearchBtn()
	s.refreshList()
	switch {
	case query == "":
		s.mark("Search cleared")
	case s.searchAll:
		s.mark("Searching all folders for “" + query + "”")
	default:
		s.mark("Searching for “" + query + "”")
	}
}

// syncSearchBtn keeps the Search button down while a search narrows the
// list, and says what the search is in its tip.
func (s *session) syncSearchBtn() {
	b := s.searchBtn
	if b == nil {
		return
	}
	q := strings.TrimSpace(s.filter.Query)
	b.Checked = q != ""
	switch {
	case q == "":
		b.SetAction("Search (Ctrl+F)")
	case s.searchAll:
		b.SetAction("Searching all folders for “" + q + "” — click to change or clear")
	default:
		b.SetAction("Searching for “" + q + "” — click to change or clear")
	}
	b.Invalidate()
}
