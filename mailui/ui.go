// Package mailui is comms-mail's desktop UI: the three-pane window, the
// compose, account, preferences and filter dialogs, the tray item and the
// screenshot harness. It is built on github.com/codemodify/uitoolkit.
//
// It talks to the daemon only through a mailcore.Client over a Unix
// socket, and never speaks IMAP, POP3 or SMTP itself.
package mailui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// LayoutMode is Thunderbird’s View → Layout.
type LayoutMode int

const (
	// LayoutVertical is folder | thread list | preview (3 columns).
	LayoutVertical LayoutMode = iota
	// LayoutClassic is folder | (thread list above preview).
	LayoutClassic
)

// filterPin is a sidebar Tags-tree toggle (not a folder).
type filterPin string

const (
	pinUnread     filterPin = "unread"
	pinStarred    filterPin = "starred"
	pinAttachment filterPin = "attachment"
)

// AppOptions tweak the first build (theme, layout, list view).
type AppOptions struct {
	Light         bool
	Layout        LayoutMode
	ShowStatusBar bool // default off: no reserved bottom strip
	CardView      bool
	Density       style.Density
}

// MailApp starts an in-process comms-maild (MemoryStore) and the UI client.
func MailApp(a *app.Application, win *app.Window) widget.Component {
	sock, _, err := mailcore.StartDemo(context.Background())
	if err != nil {
		return widgets.NewLabel("comms-maild: " + err.Error())
	}
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		return widgets.NewLabel(err.Error())
	}
	return Open(a, win, cli, AppOptions{})
}

// Open builds the Mail chrome against a comms-maild Client (no Store / IMAP).
func Open(a *app.Application, win *app.Window, cli *mailcore.Client, opts AppOptions) widget.Component {
	noWindowMenu(a)
	s := newSession(a, win, cli, opts)
	root := s.build()
	s.attachTray()
	s.checkVault()
	if a.WatchingLook() {
		keepOwnTheme(a)
	}
	return root
}

type session struct {
	// vaultAsked: the window has checked for a locked daemon or passwords
	// in plain text (checkVault); unlockWin is the unlock prompt, while up.
	vaultAsked bool
	unlockWin  *app.Window

	app  *app.Application
	win  *app.Window
	cli  *mailcore.Client
	opts AppOptions

	folder   mailcore.FolderID
	account  string
	central  bool // Account Central instead of the thread list
	selected []mailcore.MessageID
	filter   mailcore.Filter
	sortCol  int
	sortAsc  bool
	online   bool
	rows     []mailcore.Message
	kind     mailcore.FolderKind
	backend  string

	chromePrefs ChromePrefs
	cardView    bool
	density     style.Density
	tags        []mailcore.Tag
	threaded    bool
	hideMuted   bool
	muted       map[string]bool

	table      *widgets.TableView
	cards      *widgets.CardList
	listStack  *widgets.Stack
	tree       *widgets.TreeView
	outboxTree *widgets.TreeView
	// rd is the reading pane (reader.go).
	rd         *reader
	askedEmpty bool
	trayMu     sync.Mutex
	tray       platform.StatusItem
	notes      mailNotifier
	newMail    bool // the tray shows the new-mail logo (trayMu)
	// The title bar (titlebar.go): empty over the folder pane (its
	// StartWidth, from where the pages land), then the app menu's button
	// and Fetch / Write / Search, then the tabs.
	head                          *widgets.HeaderBar
	appBtn                        *widgets.MenuButton
	fetchBtn, writeBtn, searchBtn *widgets.IconButton
	status                        *widgets.StatusBar
	searchAll                     bool
	acctPanel                     widget.Component
	acctTitle                     *widgets.Label
	acctBody                      *widgets.Label
	thread                        widget.Component
	center                        *widgets.Stack

	busy      sync.WaitGroup
	refresher *refreshCoalescer

	// previewGen counts preview loads. A body that arrives for an older one
	// is dropped, so clicking down the list shows the last click rather
	// than whichever reply was slowest.
	previewGen uint64
	// shown is the primary message as fully loaded for the preview, when
	// shownOK; what reply, forward and the attachments act on.
	shown   mailcore.Message
	shownOK bool
	// loadingID is the message whose body is being fetched for the preview.
	// A refresh that lands meanwhile (marking it read sends one) waits for
	// that fetch rather than starting a second.
	loadingID mailcore.MessageID
	// inviteCompact folds every invitation card's guest list and options
	// away.
	inviteCompact bool
	// images holds the pictures HTML mail showed (cid: parts by message,
	// remote ones by URL) and the senders whose remote images load
	// without asking.
	images     map[string]*paintengine2d.Image
	imgSenders map[string]bool
	// folderNames names each folder, for the rows of a list that mixes
	// folders (search, unified views).
	folderNames map[mailcore.FolderID]string
	// srv is the "On server" search: its toggle and its latest answer;
	// srvAdded is how many rows it added to the list showing.
	srv      serverSearch
	srvAdded int

	// undo is the move or delete waiting out its undo window; its messages
	// are hidden from the list until it is committed or undone.
	undo      *pendingRemoval
	tagUndo   *tagUndo // the last tag change, while it can be taken back
	hidden    map[mailcore.MessageID]bool
	undoBar   widget.Component
	undoLabel *widgets.Label

	// tabs is the strip in the title bar: Mail (mainPage), then a tab per
	// opened message (tabs.go). pages holds what they show.
	tabs     *widgets.BrowserTabs
	pages    *widgets.Stack
	mainPage widget.Component
	// pendingRefresh records a daemon event that arrived while no UI loop
	// was pumping (headless / tests). DrainDaemonEvents applies it.
	pendingRefresh atomic.Bool
}

func newSession(a *app.Application, win *app.Window, cli *mailcore.Client, opts AppOptions) *session {
	s := &session{
		app: a, win: win, cli: cli, opts: opts,
		sortCol: colWhen, sortAsc: false, online: true,
	}
	s.watchFocus(win)
	// Pages printed and attachments dragged out by an earlier run, once
	// old (mailcore.RemoveOld).
	go func() {
		mailcore.RemoveOld(filepath.Join(os.TempDir(), printPattern), mailcore.PrintedKeep)
		mailcore.RemoveOld(filepath.Join(os.TempDir(), dragPattern+"*"), mailcore.DraggedKeep)
	}()
	p := loadChromePrefs()
	s.chromePrefs = p
	s.cardView = opts.CardView || p.CardView
	s.threaded = p.Threaded
	s.hideMuted = p.HideMute
	s.inviteCompact = p.InviteLess
	s.density = opts.Density
	if s.density == style.DensityDefault && p.Density != "" {
		s.density = p.density()
	}
	if st, err := cli.Status(); err == nil {
		s.backend = st.Backend
		s.online = st.Online
	}
	if tags, err := cli.Tags(); err == nil {
		s.tags = tags
	}
	s.account = firstAccountID(cli)
	if inbox, ok := mailcore.SpecialFolderClient(cli, s.account, mailcore.FolderInbox); ok {
		s.folder = inbox.ID
	}
	return s
}

func (s *session) persistChrome() {
	s.chromePrefs.CardView = s.cardView
	s.chromePrefs.Threaded = s.threaded
	s.chromePrefs.HideMute = s.hideMuted
	s.chromePrefs.Density = s.density.String()
	if s.opts.Layout == LayoutClassic {
		s.chromePrefs.Layout = "classic"
	} else {
		s.chromePrefs.Layout = "vertical"
	}
	s.chromePrefs.Light = s.opts.Light
	s.chromePrefs.InviteLess = s.inviteCompact
	// The theme is Settings › Appearance's, which writes the file itself:
	// the session's copy may be older.
	s.chromePrefs.Theme = ownTheme()
	saveChromePrefs(s.chromePrefs)
	// Density / layout live in mailui.json only. look.json is Settings’
	// file; persist must not SaveAppearance or rewrite theme packs.
}

func (s *session) applyLook() {
	ap := effectiveAppearance()
	if s.app != nil && !s.app.WatchingLook() && s.app.Look() != nil {
		// Screenshot / DarkLook fixtures stay static. WatchLook Mail
		// follows look.json (PreferredLook), never opts.Light.
		ap = style.LookAppearance(s.app.Look())
	}
	s.opts.Light = ap.Effective().Theme == style.ThemeLight
	s.app.SetLook(style.WithDensity(ap.Look(), s.density))
}

// setPalette makes comms-mail's own theme the plain light or dark one
// (look.json, shared with every uitoolkit app, is not touched).
func (s *session) setPalette(light bool) {
	theme := style.ThemeDark
	if light {
		theme = style.ThemeLight
	}
	p := loadChromePrefs()
	p.Theme = style.StarterName(theme)
	saveChromePrefs(p)
	s.chromePrefs.Theme = p.Theme
	s.opts.Light = light
	s.rebuild()
}

func (s *session) rebuild() {
	s.persistChrome()
	s.applyLook()
	s.win.SetContent(s.build())
}

func (s *session) build() widget.Component {
	s.applyLook()
	if s.opts.ShowStatusBar {
		s.status = widgets.NewStatusBar("Ready.", "", "Offline demo", "v"+uitoolkit.Version)
	} else {
		s.status = nil
	}
	s.table = widgets.NewTableView([]widgets.TableColumn{
		colStar:   {Icon: style.IconStar, Width: 28, MinWidth: 24, Sortable: true},
		colAttach: {Icon: style.IconAttach, Width: 28, MinWidth: 24, Sortable: true},
		colStatus: {Icon: style.IconMail, Width: 28, MinWidth: 24, Sortable: true},
		colTopic:  {Title: "Topic", MinWidth: 180, Sortable: true},
		colWho:    {Title: "Who", Width: 148, MinWidth: 110, Sortable: true},
		colWhen:   {Title: "When", Width: 108, MinWidth: 88, Sortable: true},
	}, 0, s.cellText, nil)
	s.table.CellIcon = s.cellIcon
	// Mail-client selection: Ctrl toggles, Shift extends, Ctrl+A selects the
	// folder; bulk actions then act on every selected message.
	s.table.Mode = widgets.SelectExtended
	// Letters are commands here (n / p / r / f / c / m), not a search.
	s.table.DisableTypeAhead = true
	s.table.OnSelectionChange = func(rows []int) { s.viewSelection(rows, s.table.Selected) }
	s.table.CellBold = func(row, col int) bool {
		if row < 0 || row >= len(s.rows) {
			return false
		}
		return !s.rows[row].Read
	}
	s.table.OnSort = func(col int, asc bool) {
		s.sortCol, s.sortAsc = col, asc
		s.refreshList()
	}
	// Inside the selection the menu acts on every selected message; on a row
	// outside it, on that row alone (the table's right-click does the same,
	// this covers callers that open the menu directly).
	s.table.OnContext = func(i int, p paintengine2d.Point) {
		if i >= 0 && i < len(s.rows) && !s.table.IsSelected(i) {
			s.clickRow(i, false)
		}
		s.messageMenu(s.table, p)
	}
	s.table.OnDrag = s.dragMessages
	// Double click or Return opens the message in a tab of its own.
	s.table.OnActivate = func(row int) {
		if row >= 0 && row < len(s.rows) && s.primaryIndex() != row {
			s.clickRow(row, false)
		}
		s.openInTab()
	}
	s.cards = widgets.NewCardList(0, s.cardAt, nil)
	s.table.SetAccessibleName("Messages")
	s.cards.SetAccessibleName("Messages")
	s.cards.Mode = widgets.SelectExtended
	s.cards.OnSelectionChange = func(rows []int) { s.viewSelection(rows, s.cards.Selected) }
	s.cards.OnContext = func(i int, p paintengine2d.Point) {
		if i >= 0 && i < len(s.rows) && !s.cards.IsSelected(i) {
			s.clickRow(i, false)
		}
		s.messageMenu(s.cards, p)
	}

	s.tree = widgets.NewTreeView()
	s.outboxTree = widgets.NewTreeView()
	// The folder pane is the window's sidebar, drawn as one where the
	// look has a sidebar style (Aqua's source list, Adwaita's, …).
	s.tree.Sidebar = true
	s.outboxTree.Sidebar = true
	s.tree.DisableTypeAhead = true
	s.outboxTree.DisableTypeAhead = true
	s.tree.Sidebar, s.outboxTree.Sidebar = true, true
	s.tree.SetAccessibleName("Folders")
	s.outboxTree.SetAccessibleName("Outbox")
	s.rebuildTree()
	s.wireFolderTree(s.tree)
	s.wireFolderTree(s.outboxTree)
	s.tree.DropMimes = []string{mimeMessageIDs, mimeFolder}
	s.tree.DropActions = platform.DragMove
	s.tree.OnDropNode = s.dropOnFolder
	s.tree.OnDrag = s.dragFolder

	// The reading pane: the message's header, invitation, actions and
	// attachments over Message, Source and Markdown (reader.go).
	s.rd = newReader(s)
	s.rd.onRetry = s.loadPreview
	s.loadImageSenders()
	s.applyRowMetrics()
	previewCol := s.rd.view

	s.table.SetVisible(!s.cardView)
	s.cards.SetVisible(s.cardView)
	s.listStack = widgets.NewStack(s.table, s.cards)
	s.undoLabel = widgets.NewLabel("")
	s.undoBar = widgets.NewRow(s.undoLabel, widgets.NewSpacer(), newButton("Undo", s.undoLast)).WithGap(8)
	s.undoBar.SetVisible(s.undo != nil)
	if s.win != nil {
		// Closing the window must not lose a move still in its undo window.
		s.win.SetOnCloseRequest(func() bool {
			s.commitUndoNow()
			return true
		})
	}
	thread := widgets.NewColumn(s.listStack, s.undoBar).WithGap(4).WithPad(8)
	thread.AddFlex(s.listStack, 1)
	s.thread = thread
	s.acctPanel = s.buildAccountCentral()
	s.acctPanel.SetVisible(false)
	s.center = widgets.NewStack(thread, s.acctPanel)

	s.applyRowMetrics()
	sidebar := widgets.NewColumn(
		s.tree,
		widgets.NewSeparator(),
		s.outboxTree,
	).WithGap(0).WithPad(6)
	sidebar.AddFlex(s.tree, 1)

	// The folder pane runs the window's height on the left, with nothing
	// over it. Right of it are the pages the tabs switch between — Mail,
	// the list and the reading pane, and a page per message opened in a
	// tab (tabs.go).
	var right widget.Component
	sideRatio := float32(0.17)
	if s.opts.Layout == LayoutClassic {
		r := widgets.NewSplitter(widgets.SplitRows, s.center, previewCol)
		r.Ratio = 0.46
		right, sideRatio = r, 0.18
	} else {
		m := widgets.NewSplitter(widgets.SplitColumns, s.center, previewCol)
		m.Ratio = 0.58
		right = m
	}
	s.setupTabs(right)
	pages := widgets.NewColumn(s.pages).WithGap(0)
	pages.AddFlex(s.pages, 1)
	if s.status != nil {
		pages.Add(s.status)
	}
	split := widgets.NewSplitter(widgets.SplitColumns, sidebar, newEdgeWatch(pages, s.alignTitle))
	split.Ratio = sideRatio

	// The window's title bar: empty over the folder pane, then the menu,
	// Fetch, Write and Search and the tabs, starting where the pages do.
	// It is the caption of the frame uitoolkit draws (caption buttons at
	// the desktop's sides, free space — the empty part included — moves
	// the window), or the first row under the desktop's own frame.
	head := widgets.NewHeaderBar([]widget.Component{s.titleButtons()}, s.tabs, nil)
	s.head = head
	var chrome []widget.Component
	if s.win != nil {
		s.win.SetTitleBar(head)
		// The tabs are the title bar under every pack, as Thunderbird's
		// are: an era that draws its own title strip (Windows 95, KDE 1,
		// BeOS's tab) would otherwise put this row under it.
		s.win.SetCaptionStyle(style.CaptionMerged)
	} else {
		chrome = append(chrome, head)
	}
	chrome = append(chrome, split)
	root := widgets.NewColumn(chrome...).WithGap(0)
	root.AddFlex(split, 1)
	s.refreshAll()
	// Once laid out (the look known), the window's minimum width follows
	// what the panes need, then a first run offers to add an account.
	return wrapShortcutsReady(root, s.handleKey, func(c widget.Component) {
		fitMinWidth(s.win, c)
		s.maybeAskAddAccount(c)
	})
}

// appMenuItems is the app menu: View, Notify, Settings, Quit. Its button
// is the first in the title bar (titlebar.go).
func (s *session) appMenuItems() []*widgets.MenuItem {
	layoutClassic := s.opts.Layout == LayoutClassic
	return []*widgets.MenuItem{
		withIcon(style.IconEye, widgets.Submenu("&View",
			widgets.RadioItem("&Vertical (3-pane)", "layout", !layoutClassic, func() {
				s.opts.Layout = LayoutVertical
				s.rebuild()
			}),
			widgets.RadioItem("&Classic (preview below)", "layout", layoutClassic, func() {
				s.opts.Layout = LayoutClassic
				s.rebuild()
			}),
			widgets.Sep(),
			widgets.RadioItem("&Table view", "list", !s.cardView, func() { s.setCardView(false) }),
			widgets.RadioItem("C&ard view", "list", s.cardView, func() { s.setCardView(true) }),
			widgets.Sep(),
			widgets.RadioItem("&Compact", "density", s.density == style.DensityCompact, func() { s.setDensity(style.DensityCompact) }),
			widgets.RadioItem("&Default density", "density", s.density == style.DensityDefault, func() { s.setDensity(style.DensityDefault) }),
			widgets.RadioItem("&Relaxed", "density", s.density == style.DensityRelaxed, func() { s.setDensity(style.DensityRelaxed) }),
			widgets.Sep(),
			widgets.CheckItem("&Threaded", s.threaded, func() {
				s.threaded = !s.threaded
				s.persistChrome()
				s.refreshList()
			}),
			widgets.CheckItem("Hide muted threads", s.hideMuted, func() {
				s.hideMuted = !s.hideMuted
				s.persistChrome()
				s.refreshList()
			}),
		)),
		widgets.Sep(),
		withIcon(style.IconBell, s.notifyMenuItems()),
		widgets.Sep(),
		iconItem(style.IconSettings, "Settings", "Ctrl+,", s.openPrefs),
		widgets.Sep(),
		iconItem(style.IconQuit, "&Quit", "Ctrl+Q", s.quit),
	}
}

// quit leaves, after the move or delete waiting on its undo bar goes
// through.
func (s *session) quit() {
	s.commitUndoNow()
	s.app.Quit()
}

func (s *session) notifyPrefs() mailcore.NotifyPrefs {
	if s.cli == nil {
		return mailcore.NotifyPrefs{Enabled: true, Desktop: true}
	}
	p, err := s.cli.NotifyPrefs()
	if err != nil {
		return mailcore.NotifyPrefs{Enabled: true, Desktop: true}
	}
	return p
}

func (s *session) toggleNotify(mut func(*mailcore.NotifyPrefs)) {
	p := s.notifyPrefs()
	mut(&p)
	if s.cli != nil {
		_, _ = s.cli.PutNotifyPrefs(p)
	}
}

func (s *session) notifyMenuItems() *widgets.MenuItem {
	p := s.notifyPrefs()
	return widgets.Submenu("Notify",
		widgets.CheckItem("Notify on new mail", p.Enabled, func() {
			s.toggleNotify(func(n *mailcore.NotifyPrefs) { n.Enabled = !n.Enabled })
		}),
		widgets.CheckItem("VIP senders only", p.VIPOnly, func() {
			s.toggleNotify(func(n *mailcore.NotifyPrefs) { n.VIPOnly = !n.VIPOnly })
		}),
		widgets.CheckItem("Desktop notifications", p.Desktop, func() {
			s.toggleNotify(func(n *mailcore.NotifyPrefs) { n.Desktop = !n.Desktop })
		}),
	)
}

func (s *session) tagPopup(from widget.Component, p paintengine2d.Point) {
	names := s.tagNames()
	items := make([]*widgets.MenuItem, 0, len(names))
	for _, t := range names {
		tag := t
		items = append(items, widgets.Item(tag, func() { s.toggleTag(tag) }))
	}
	widgets.ShowContextMenu(from, p, items...)
}

// messageMenu is the right-click menu on a message; every row has an
// icon.
func (s *session) messageMenu(from widget.Component, p paintengine2d.Point) {
	star := "Star"
	if m, ok := s.primary(); ok && m.Starred {
		star = "Unstar"
	}
	widgets.ShowContextMenu(from, p,
		iconItem(style.IconOpen, "Open in New Tab", "E", s.openInTab),
		widgets.Sep(),
		iconItem(style.IconReply, "Reply", "R", s.reply),
		iconItem(style.IconReplyAll, "Reply All", "Shift+R", s.replyAll),
		iconItem(style.IconForward, "Forward", "F", s.forward),
		widgets.Sep(),
		iconItem(style.IconCheck, "Mark as Read", "M", func() { s.setRead(true) }),
		iconItem(style.IconDot, "Mark as Unread", "", func() { s.setRead(false) }),
		iconItem(style.IconStar, star, "S", s.toggleStar),
		widgets.Sep(),
		&widgets.MenuItem{Text: "Tag", Shortcut: "T", Icon: style.IconTag, Submenu: s.tagMenuItems()},
		iconItem(style.IconMute, "Mute Thread", "", func() { s.muteThread(true) }),
		iconItem(style.IconUser, "Add sender to VIP", "", s.addVIP),
		iconItem(style.IconArchive, "Archive", "A", s.archive),
		&widgets.MenuItem{Text: "Move to", Icon: style.IconFolder, Submenu: s.moveMenu()},
		iconItem(style.IconJunk, "Junk", "J", s.junk),
		iconItem(style.IconTrash, "Delete", "D", s.deleteSel),
		widgets.Sep(),
		iconItem(style.IconPrint, "Print…", "Ctrl+P", s.printMessage),
		iconItem(style.IconSave, "Save As…", "", s.saveMessageAs),
	)
}

// withIcon gives a menu row (a submenu, say) an icon.
func withIcon(icon style.ToolIcon, it *widgets.MenuItem) *widgets.MenuItem {
	it.Icon = icon
	return it
}

// iconItem is a menu row with an icon and, when there is one, its key.
func iconItem(icon style.ToolIcon, text, shortcut string, on func()) *widgets.MenuItem {
	return &widgets.MenuItem{Icon: icon, Text: text, Shortcut: shortcut, OnClick: on}
}

// now is the clock the thread list formats against. The seeded demo store
// has a frozen "today" so screenshots stay stable; a real account must use
// the wall clock, or every date reads as a weekday from September 2026.
func (s *session) now() time.Time {
	if s.backend == "memory" {
		return mailcore.DemoNow
	}
	return time.Now()
}

// mixedFolders reports whether the list shows messages from more than one
// folder — a search of every folder, a unified or tag view — so each row
// says where its message is.
func (s *session) mixedFolders() bool {
	if s.searchAll && strings.TrimSpace(s.filter.Query) != "" {
		return true
	}
	return mailcore.IsVirtual(s.folder)
}

// The message list's columns. The first three are marks — icons, with no
// text — and colSort is what each one sorts by.
const (
	colStar = iota
	colAttach
	colStatus
	colTopic
	colWho
	colWhen
)

var colSort = [...]int{
	colStar:   mailcore.SortStarred,
	colAttach: mailcore.SortAttach,
	colStatus: mailcore.SortStatus,
	colTopic:  mailcore.SortSubject,
	colWho:    mailcore.SortWho,
	colWhen:   mailcore.SortDate,
}

// starColor is a starred message's star.
var starColor = paintengine2d.RGB(0.95, 0.68, 0.1)

func (s *session) cellText(row, col int) string {
	if row < 0 || row >= len(s.rows) {
		return ""
	}
	m := s.rows[row]
	switch col {
	case colTopic:
		sub := m.Subject
		if strings.TrimSpace(sub) == "" {
			sub = "(no subject)"
		}
		return sub
	case colWho:
		who := m.Correspondent(s.kind)
		if s.mixedFolders() {
			if name := s.folderNames[m.Folder]; name != "" {
				who += "  ·  " + name
			}
		}
		return who
	case colWhen:
		return mailcore.FormatDate(m.Date, s.now())
	default:
		return ""
	}
}

// cellIcon is a row's marks: the filled star, the paperclip, the status
// (the unread dot, else forwarded or replied) and the muted bell before
// the topic. Unread is also the row in bold.
func (s *session) cellIcon(row, col int) (style.ToolIcon, paintengine2d.Color) {
	var none paintengine2d.Color
	if row < 0 || row >= len(s.rows) {
		return style.IconNone, none
	}
	m := s.rows[row]
	switch col {
	case colStar:
		if m.Starred {
			return style.IconStarFilled, starColor
		}
	case colAttach:
		if m.HasAttach {
			return style.IconAttach, none
		}
	case colStatus:
		switch {
		case !m.Read:
			return style.IconDot, none
		case m.Forwarded:
			return style.IconForward, none
		case m.Answered:
			return style.IconReply, none
		}
	case colTopic:
		if m.ThreadID != "" && s.muted[m.ThreadID] {
			return style.IconMute, none
		}
	}
	return style.IconNone, none
}

func (s *session) visible() []mailcore.Message {
	all, err := s.loadVisible()
	if err != nil {
		s.mark(err.Error())
		return nil
	}
	return all
}

func (s *session) loadVisible() ([]mailcore.Message, error) {
	f, ok, _ := s.cli.GetFolder(s.folder)
	kind := mailcore.FolderInbox
	if ok {
		kind = f.Kind
	}
	s.kind = kind
	searching := s.searchAll && strings.TrimSpace(s.filter.Query) != ""
	var all []mailcore.Message
	var err error
	if searching {
		// Every folder, over the daemon's index (all synced headers, and
		// the bodies already downloaded).
		all, err = s.cli.Search(mailcore.SearchQuery{Filter: s.filter})
		kind = mailcore.FolderInbox // date-sorted, flat
	} else {
		all, err = s.cli.ListMessages(s.folder, s.filter)
	}
	if err != nil {
		return nil, err
	}
	var fromServer int
	all, fromServer = s.mergeServerHits(all)
	s.srvAdded = fromServer
	s.maybeSearchServer()
	if ids, err := s.cli.MutedThreads(); err == nil {
		s.muted = map[string]bool{}
		for _, id := range ids {
			s.muted[id] = true
		}
	}
	if s.hideMuted && len(s.muted) > 0 {
		var keep []mailcore.Message
		for _, m := range all {
			if m.ThreadID == "" || !s.muted[m.ThreadID] {
				keep = append(keep, m)
			}
		}
		all = keep
	}
	if len(s.hidden) > 0 {
		keep := all[:0]
		for _, m := range all {
			if !s.hidden[m.ID] {
				keep = append(keep, m)
			}
		}
		all = keep
	}
	if s.threaded && !searching {
		return mailcore.GroupThreaded(all, kind, colSort[s.sortCol], s.sortAsc), nil
	}
	mailcore.SortMessages(all, colSort[s.sortCol], s.sortAsc, kind)
	return all, nil
}

func (s *session) refreshAll() {
	s.ensureUsableFolder()
	s.rebuildTree()
	s.refreshList()
	s.refreshAccount()
	s.showCenter()
	s.refreshStatus()
}

func (s *session) refreshList() {
	s.ensureUsableFolder()
	rows, err := s.loadVisible()
	if err != nil {
		s.mark(err.Error())
	} else {
		s.rows = rows
	}
	// Drop selected ids that are no longer in the list (moved, deleted, or
	// re-keyed by a server-side MOVE): acting on them would target nothing,
	// or — before UIDs were re-keyed — the wrong message.
	s.pruneSelection()
	if len(s.selected) == 0 && len(s.rows) > 0 {
		s.selected = []mailcore.MessageID{s.rows[0].ID}
	}
	if s.primaryIndex() < 0 && len(s.rows) > 0 {
		s.selected = []mailcore.MessageID{s.rows[0].ID}
	}
	if s.table != nil {
		s.table.RowCount = len(s.rows)
		if len(s.table.Columns) > colWho {
			s.table.Columns[colWho].Title = "Who"
			if s.mixedFolders() {
				s.table.Columns[colWho].Title = "Who · Folder"
			}
		}
		s.table.SortCol = s.sortCol
		s.table.SortAsc = s.sortAsc
		s.table.SetVisible(!s.cardView)
		s.table.Invalidate()
	}
	if s.cards != nil {
		s.cards.Count = len(s.rows)
		s.cards.SetVisible(s.cardView)
		s.cards.Invalidate()
	}
	s.syncViews()
	s.loadPreview()
	s.refreshStatus()
}

func (s *session) rebuildTree() {
	if s.tree == nil {
		return
	}
	if tags, err := s.cli.Tags(); err == nil {
		s.tags = tags
	}
	was := treeExpandState(s.tree.Roots)
	var roots []*widgets.TreeNode
	var selected *widgets.TreeNode
	// One RPC for every folder's unread count instead of one per node.
	unread, _, _ := s.cli.UnreadAll()

	var outbox *mailcore.Folder
	if vfs, err := s.cli.VirtualFolders(); err == nil {
		for _, f := range vfs {
			if f.ID == mailcore.FolderOutbox {
				cp := f
				outbox = &cp
			}
		}
	}

	acctUnread := 0
	for _, acct := range s.accounts() {
		node := widgets.NewTreeNode(acct.Address)
		node.Data = acct.ID
		applyTreeExpand(node, was, true)
		byParent := map[mailcore.FolderID][]mailcore.Folder{}
		folders, _ := s.cli.ListFolders(acct.ID)
		for _, f := range folders {
			byParent[f.Parent] = append(byParent[f.Parent], f)
			if s.folderNames == nil {
				s.folderNames = map[mailcore.FolderID]string{}
			}
			s.folderNames[f.ID] = f.Name
		}
		for pid, kids := range byParent {
			byParent[pid] = orderFolderChildren(kids)
		}
		var addKids func(parent *widgets.TreeNode, pid mailcore.FolderID)
		addKids = func(parent *widgets.TreeNode, pid mailcore.FolderID) {
			for _, f := range byParent[pid] {
				label := f.Name
				nUnread := unread[f.ID]
				if nUnread > 0 {
					label = fmt.Sprintf("%s (%d)", f.Name, nUnread)
					acctUnread += nUnread
				}
				n := widgets.NewTreeNode(label)
				n.Data = f.ID
				n.Bold = nUnread > 0
				n.Expanded = f.Kind == mailcore.FolderArchive || len(byParent[f.ID]) > 0
				applyTreeExpand(n, was, n.Expanded)
				addKids(n, f.ID)
				parent.Children = append(parent.Children, n)
				if f.ID == s.folder && !s.central {
					selected = n
				}
			}
		}
		addKids(node, "")
		node.Bold = acctUnread > 0
		if s.central && s.account == acct.ID {
			selected = node
		}
		roots = append(roots, node)
		acctUnread = 0
	}

	filtersNode := widgets.NewTreeNode("Tags")
	filtersNode.Data = mailcore.AccountTags
	applyTreeExpand(filtersNode, was, true)
	for _, t := range s.tags {
		if pin, ok := systemFilterPin(t.Name); ok {
			on := false
			switch pin {
			case pinUnread:
				on = s.filter.Unread
			case pinStarred:
				on = s.filter.Starred
			case pinAttachment:
				on = s.filter.Attachment
			}
			n := widgets.NewTreeNode(t.Name)
			n.Data = pin
			if on {
				n.Icon = style.IconCheck
			}
			n.Color = mailcore.ParseHexColor(t.Color)
			filtersNode.Children = append(filtersNode.Children, n)
			continue
		}
		label := t.Name
		fid := mailcore.TagFolderID(t.Name)
		nUnread := unread[fid]
		if nUnread > 0 {
			label = fmt.Sprintf("%s (%d)", t.Name, nUnread)
		}
		n := widgets.NewTreeNode(label)
		n.Data = fid
		n.Bold = nUnread > 0
		n.Color = mailcore.ParseHexColor(t.Color)
		filtersNode.Children = append(filtersNode.Children, n)
		if fid == s.folder && !s.central {
			selected = n
		}
	}
	roots = append(roots, filtersNode)

	s.tree.SetRoots(roots)
	s.tree.Selected = selected
	s.tree.Invalidate()
	s.rebuildOutboxPin(outbox)
}

func (s *session) rebuildOutboxPin(outbox *mailcore.Folder) {
	if s.outboxTree == nil {
		return
	}
	if outbox == nil {
		if vfs, err := s.cli.VirtualFolders(); err == nil {
			for _, f := range vfs {
				if f.ID == mailcore.FolderOutbox {
					cp := f
					outbox = &cp
					break
				}
			}
		}
	}
	if outbox == nil {
		s.outboxTree.SetRoots(nil)
		return
	}
	label := outbox.Name
	nUnread, _ := s.cli.Unread(outbox.ID)
	if nUnread > 0 {
		label = fmt.Sprintf("%s (%d)", outbox.Name, nUnread)
	}
	n := widgets.NewTreeNode(label)
	n.Data = outbox.ID
	n.Bold = nUnread > 0
	s.outboxTree.SetRoots([]*widgets.TreeNode{n})
	if outbox.ID == s.folder && !s.central {
		s.outboxTree.Selected = n
		if s.tree != nil {
			s.tree.Selected = nil
			s.tree.Invalidate()
		}
	} else {
		s.outboxTree.Selected = nil
	}
	s.outboxTree.Invalidate()
}

func (s *session) wireFolderTree(tv *widgets.TreeView) {
	if tv == nil {
		return
	}
	tv.OnSelect = func(n *widgets.TreeNode) {
		if n == nil {
			return
		}
		s.syncFolderTreeSelection(tv)
		if pin, ok := n.Data.(filterPin); ok {
			s.toggleFilterPin(pin)
			return
		}
		if id, ok := n.Data.(mailcore.FolderID); ok && id != "" {
			s.selectFolder(id)
			return
		}
		if acct, ok := n.Data.(string); ok && acct != "" {
			if acct == mailcore.AccountTags || acct == mailcore.AccountUnified || acct == mailcore.AccountSmart || acct == mailcore.AccountCategories {
				return
			}
			s.openAccountInbox(acct)
		}
	}
	tv.OnContext = func(n *widgets.TreeNode, p paintengine2d.Point) {
		var folder mailcore.Folder
		haveFolder := false
		if n != nil {
			if id, ok := n.Data.(mailcore.FolderID); ok && id != "" {
				s.folder = id
				s.selected = nil
				s.refreshAll()
				if f, ok, _ := s.cli.GetFolder(id); ok {
					folder, haveFolder = f, true
				}
			}
		}
		items := []*widgets.MenuItem{
			iconItem(style.IconDownload, "Fetch", "", s.getMessages),
			iconItem(style.IconFolder, "New Folder…", "", s.newFolder),
		}
		if haveFolder && !folder.Virtual {
			f := folder
			if !f.NoSelect {
				items = append(items, iconItem(style.IconCheck, "Mark Folder Read", "", func() { s.markFolderRead(f.ID) }))
			}
			items = append(items, iconItem(style.IconFolder, "New Subfolder…", "", func() { s.newSubfolder(f) }))
			if folder.Kind == mailcore.FolderCustom {
				items = append(items,
					iconItem(style.IconPen, "Rename Folder…", "", func() { s.renameFolder(f) }),
					&widgets.MenuItem{Text: "Move Folder To", Icon: style.IconFolder, Submenu: s.folderMoveMenu(f)},
					iconItem(style.IconTrash, "Delete Folder…", "", func() { s.confirmDeleteFolder(f) }))
			}
			if !f.NoSelect {
				items = append(items, widgets.Item("Compact Folder", func() { s.compactFolder(f) }))
			}
		}
		items = append(items,
			widgets.Sep(),
			iconItem(style.IconUser, "Remove Account…", "", s.removeCurrentAccount),
			iconItem(style.IconTrash, "Empty Trash", "", s.emptyTrash),
		)
		widgets.ShowContextMenu(tv, p, items...)
	}
}

func (s *session) syncFolderTreeSelection(from *widgets.TreeView) {
	if s.tree != nil && s.tree != from {
		s.tree.Selected = nil
		s.tree.Invalidate()
	}
	if s.outboxTree != nil && s.outboxTree != from {
		s.outboxTree.Selected = nil
		s.outboxTree.Invalidate()
	}
}

func systemFilterPin(name string) (filterPin, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "unread":
		return pinUnread, true
	case "starred":
		return pinStarred, true
	case "attachment":
		return pinAttachment, true
	default:
		return "", false
	}
}

func treeNodeKey(n *widgets.TreeNode) (string, bool) {
	if n == nil {
		return "", false
	}
	switch d := n.Data.(type) {
	case mailcore.FolderID:
		return "folder:" + string(d), true
	case string:
		return "id:" + d, true
	default:
		return "", false
	}
}

func treeExpandState(roots []*widgets.TreeNode) map[string]bool {
	out := map[string]bool{}
	var walk func([]*widgets.TreeNode)
	walk = func(nodes []*widgets.TreeNode) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if k, ok := treeNodeKey(n); ok {
				out[k] = n.Expanded
			}
			walk(n.Children)
		}
	}
	walk(roots)
	return out
}

func applyTreeExpand(n *widgets.TreeNode, was map[string]bool, def bool) {
	if n == nil {
		return
	}
	if k, ok := treeNodeKey(n); ok {
		if v, ok := was[k]; ok {
			n.Expanded = v
			return
		}
	}
	n.Expanded = def
}

func isOutboxFolder(f mailcore.Folder) bool {
	return f.ID == mailcore.FolderOutbox || strings.EqualFold(f.Name, "Outbox")
}

func folderSidebarRank(f mailcore.Folder) int {
	if isOutboxFolder(f) {
		return 90
	}
	switch f.Kind {
	case mailcore.FolderInbox:
		return 0
	case mailcore.FolderDrafts:
		return 1
	case mailcore.FolderSent:
		return 2
	case mailcore.FolderArchive:
		return 3
	case mailcore.FolderJunk:
		return 4
	case mailcore.FolderTrash:
		return 5
	default:
		return 40
	}
}

func orderFolderChildren(folders []mailcore.Folder) []mailcore.Folder {
	out := append([]mailcore.Folder(nil), folders...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := folderSidebarRank(out[i]), folderSidebarRank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *session) clickRow(i int, add bool) {
	if i < 0 || i >= len(s.rows) {
		return
	}
	id := s.rows[i].ID
	if add {
		if !s.hasSel(id) {
			s.selected = append(s.selected, id)
		}
	} else {
		s.selected = []mailcore.MessageID{id}
	}
	s.syncViews()
	if !s.rows[i].Read {
		s.setFlagsLater([]mailcore.MessageID{id}, mailcore.FlagPatch{Read: mailcore.BoolPtr(true)})
	}
	s.loadPreview()
	s.refreshStatus()
}

// viewSelection mirrors the selection of the message table or card list
// (cur is that view's current row). One row behaves as a click (preview,
// mark read); several are all selected with the current row last, so the
// preview follows the keyboard and nothing is marked read.
func (s *session) viewSelection(rows []int, cur int) {
	if len(rows) == 1 {
		s.clickRow(rows[0], false)
		return
	}
	s.selected = s.selected[:0]
	curSelected := false
	for _, r := range rows {
		if r < 0 || r >= len(s.rows) {
			continue
		}
		if r == cur {
			curSelected = true
			continue
		}
		s.selected = append(s.selected, s.rows[r].ID)
	}
	if curSelected {
		s.selected = append(s.selected, s.rows[cur].ID)
	}
	s.syncViews()
	s.loadPreview()
	s.refreshStatus()
}

// syncViews shows s.selected in the message table and the card list, the
// primary (last) as their current row.
func (s *session) syncViews() {
	if s.table == nil && s.cards == nil {
		return
	}
	pos := make(map[mailcore.MessageID]int, len(s.rows))
	for i, m := range s.rows {
		pos[m.ID] = i
	}
	idx := make([]int, 0, len(s.selected))
	for _, id := range s.selected {
		if i, ok := pos[id]; ok {
			idx = append(idx, i)
		}
	}
	if s.table != nil {
		s.table.SetSelectedRows(idx)
	}
	if s.cards != nil {
		s.cards.SetSelectedRows(idx)
	}
}

// pruneSelection keeps only ids that are still visible in the thread list.
func (s *session) pruneSelection() {
	if len(s.selected) == 0 {
		return
	}
	live := make(map[mailcore.MessageID]bool, len(s.rows))
	for _, m := range s.rows {
		live[m.ID] = true
	}
	keep := s.selected[:0]
	for _, id := range s.selected {
		if live[id] {
			keep = append(keep, id)
		}
	}
	s.selected = keep
}

func (s *session) primaryIndex() int {
	if len(s.selected) == 0 {
		return -1
	}
	want := s.selected[len(s.selected)-1]
	for i, m := range s.rows {
		if m.ID == want {
			return i
		}
	}
	return -1
}

func (s *session) hasSel(id mailcore.MessageID) bool {
	for _, x := range s.selected {
		if x == id {
			return true
		}
	}
	return false
}

// primary is the message the window is acting on: the open message tab's,
// else the list's (listPrimary). It answers from what the window already
// has and never asks the daemon, so a keystroke or a menu never waits on
// the network. What needs the body goes through withFull.
func (s *session) primary() (mailcore.Message, bool) {
	if mt, ok := s.activeTab(); ok {
		return mt.msg, true
	}
	return s.listPrimary()
}

// listPrimary is the list's primary message — the last one selected — from
// the loaded preview, else the list row. The preview pane shows this one
// whichever tab is in front.
func (s *session) listPrimary() (mailcore.Message, bool) {
	if len(s.selected) == 0 {
		return mailcore.Message{}, false
	}
	id := s.selected[len(s.selected)-1]
	if s.shownOK && s.shown.ID == id {
		return s.shown, true
	}
	for _, m := range s.rows {
		if m.ID == id {
			return m, true
		}
	}
	return mailcore.Message{}, false
}

// withFull calls fn on the UI goroutine with the primary message, body
// included, fetching it off the UI goroutine first when the preview has
// not loaded it yet.
func (s *session) withFull(what string, fn func(mailcore.Message)) {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	if hasBody(m) {
		fn(m)
		return
	}
	s.mark(what + ": loading message…")
	id := m.ID
	s.async(func() (any, error) {
		return s.getMessage(id)
	}, func(v any, err error) {
		if err != nil {
			widgets.Warn(s.win.Content(), what, err.Error(), nil)
			return
		}
		fn(v.(mailcore.Message))
	})
}

func (s *session) getMessage(id mailcore.MessageID) (mailcore.Message, error) {
	m, ok, err := s.cli.GetMessage(id)
	if err == nil && !ok {
		err = fmt.Errorf("the message is no longer there")
	}
	return m, err
}

func hasBody(m mailcore.Message) bool { return m.Body != "" || m.HTML != "" }

// loadingNoteDelay is how long a message may take to load before the
// reading pane says it is loading.
const loadingNoteDelay = 150 * time.Millisecond

// loadPreview shows the primary message. The headers come from the list
// row at once; a body not yet downloaded is fetched off the UI goroutine
// with "Loading message…" in its place, so a click on a new message never
// freezes the window, however slow or broken the network.
func (s *session) loadPreview() {
	if m, ok := s.listPrimary(); ok && m.ID == s.loadingID {
		// A fetch for this message is already in flight (a refresh landed
		// mid-load); let it finish rather than start another.
		s.showHeaders(m)
		return
	}
	s.previewGen++
	gen := s.previewGen
	s.loadingID = ""
	s.shownOK = false
	rd := s.rd
	rd.retry.SetVisible(false)
	m, ok := s.listPrimary()
	if !ok {
		rd.clear()
		return
	}
	s.showHeaders(m)
	rd.invite.show(m)
	// A list row carries no text (a large folder's list stays small), so
	// the message is fetched: fast and local for a cached one, a download
	// for one not yet fetched. A message read before in this window shows
	// at once from the client's copy. "Loading message…" waits a moment,
	// so a local fetch does not flash it.
	if cm, ok := s.cli.CachedMessage(m.ID); ok && !hasBody(m) {
		m.Body, m.HTML = cm.Body, cm.HTML
	}
	hadBody := hasBody(m)
	id := m.ID
	if hadBody {
		s.showBody(m)
	} else {
		rd.showPlain("", "")
		time.AfterFunc(loadingNoteDelay, func() {
			s.post(func() {
				if gen == s.previewGen && s.loadingID == id {
					rd.showPlain("Loading message…", "")
				}
			})
		})
	}
	s.loadingID = id
	s.async(func() (any, error) {
		return s.getMessage(id)
	}, func(v any, err error) {
		if gen != s.previewGen {
			return
		}
		s.loadingID = ""
		if err != nil {
			if !hadBody {
				rd.showPlain("", "Couldn't load this message.\n\n"+err.Error())
				rd.retry.SetVisible(true)
			}
			return
		}
		full := v.(mailcore.Message)
		s.showHeaders(full)
		s.showBody(full)
		rd.invite.show(full)
	})
}

// showHeaders shows m's header in the reading pane.
func (s *session) showHeaders(m mailcore.Message) { s.rd.showHeaders(m) }

// showBody puts a loaded message in the reading pane and makes it the one
// the window acts on.
func (s *session) showBody(m mailcore.Message) {
	s.shown, s.shownOK = m, true
	s.rd.showBody(m)
}

// setFlagsLater shows a flag change in the list at once and tells the
// daemon off the UI goroutine; when the daemon refuses, the list goes back
// to what the daemon has.
func (s *session) setFlagsLater(ids []mailcore.MessageID, patch mailcore.FlagPatch) {
	want := make(map[mailcore.MessageID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	for i := range s.rows {
		if !want[s.rows[i].ID] {
			continue
		}
		if patch.Read != nil {
			s.rows[i].Read = *patch.Read
		}
		if patch.Starred != nil {
			s.rows[i].Starred = *patch.Starred
		}
		if patch.Tags != nil {
			s.rows[i].Tags = append([]string(nil), (*patch.Tags)...)
		}
	}
	if s.shownOK && want[s.shown.ID] {
		if patch.Read != nil {
			s.shown.Read = *patch.Read
		}
		if patch.Starred != nil {
			s.shown.Starred = *patch.Starred
		}
		if patch.Tags != nil {
			s.shown.Tags = append([]string(nil), (*patch.Tags)...)
		}
	}
	s.tabsFollow(want, patch)
	s.invalidateList()
	s.async(func() (any, error) {
		for _, id := range ids {
			if err := s.cli.SetFlags(id, patch); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}, func(_ any, err error) {
		if err != nil {
			s.mark("Couldn't update the message: " + err.Error())
			s.refreshList()
			return
		}
		s.refreshStatus()
	})
}

func (s *session) invalidateList() {
	if s.table != nil {
		s.table.Invalidate()
	}
	if s.cards != nil {
		s.cards.Invalidate()
	}
}

func (s *session) refreshStatus() {
	if s.status == nil {
		return
	}
	unread, _ := s.cli.UnreadTotal()
	name := string(s.folder)
	if f, ok, _ := s.cli.GetFolder(s.folder); ok {
		name = f.Name
		if f.Virtual {
			name = "Virtual · " + f.Name
		}
	}
	sel := ""
	if n := len(s.selected); n > 1 {
		sel = fmt.Sprintf("  ·  %d selected", n)
	}
	s.status.Set(0, fmt.Sprintf("%d unread%s", unread, sel))
	shown := fmt.Sprintf("%s  ·  %d shown", name, len(s.rows))
	if s.srvAdded > 0 {
		shown += fmt.Sprintf(" (%d from the server)", s.srvAdded)
	}
	s.status.Set(1, shown)
	line := s.backendLabel()
	if ops, err := s.cli.Outbox(); err == nil && len(ops) > 0 {
		line += fmt.Sprintf(" · %d queued", len(ops))
	}
	if s.online {
		s.status.Set(2, "Online · "+line)
	} else {
		s.status.Set(2, "Offline · "+line)
	}
	s.status.Set(3, "v"+uitoolkit.Version)
}

func (s *session) mark(msg string) {
	if s.status != nil {
		s.status.Set(0, msg)
	}
}

// ids is what an action applies to: the open message tab's message, else
// the list's selection.
func (s *session) ids() []mailcore.MessageID {
	if mt, ok := s.activeTab(); ok {
		return []mailcore.MessageID{mt.msg.ID}
	}
	if len(s.selected) > 0 {
		return append([]mailcore.MessageID(nil), s.selected...)
	}
	return nil
}

func (s *session) write() {
	_, err := OpenCompose(s.app, s.cli, ComposeOptions{OnChange: s.refreshAll})
	if err != nil {
		widgets.Warn(s.win.Content(), "Write", err.Error(), nil)
		return
	}
	s.mark("Write")
}

// decrypted is m as it reads: what secretvault decrypted of it, where a
// reader showing it holds that, and whether it was encrypted. Replying to
// or forwarding an encrypted message quotes that, and starts encrypted.
func (s *session) decrypted(m mailcore.Message) (mailcore.Message, bool) {
	readers := []*reader{s.rd}
	if mt, ok := s.activeTab(); ok {
		readers = append([]*reader{mt.rd}, readers...)
	}
	for _, r := range readers {
		if r != nil && r.sec.id == m.ID && r.sec.content != nil {
			c := *r.sec.content
			out := m.Clone()
			out.Body, out.HTML = c.Body, c.HTML
			if c.Subject != "" {
				out.Subject = c.Subject
			}
			return out, true
		}
	}
	return m, m.Encrypted
}

func (s *session) reply() {
	s.withFull("Reply", func(m mailcore.Message) {
		cp, enc := s.decrypted(m)
		_, err := OpenCompose(s.app, s.cli, ComposeOptions{ReplyTo: &cp, Encrypt: enc, OnChange: s.refreshAll})
		if err != nil {
			widgets.Warn(s.win.Content(), "Reply", err.Error(), nil)
		}
	})
}

func (s *session) replyAll() {
	s.withFull("Reply All", func(m mailcore.Message) {
		cp, enc := s.decrypted(m)
		_, err := OpenCompose(s.app, s.cli, ComposeOptions{ReplyTo: &cp, ReplyAll: true, Encrypt: enc, OnChange: s.refreshAll})
		if err != nil {
			widgets.Warn(s.win.Content(), "Reply All", err.Error(), nil)
		}
	})
}

func (s *session) forward() {
	s.withFull("Forward", func(m mailcore.Message) {
		cp, enc := s.decrypted(m)
		_, err := OpenCompose(s.app, s.cli, ComposeOptions{Forward: &cp, Encrypt: enc, OnChange: s.refreshAll})
		if err != nil {
			widgets.Warn(s.win.Content(), "Forward", err.Error(), nil)
		}
	})
}

// getMessages runs a full sync off the UI goroutine. The window stays live
// while it runs and the daemon's mail.changed events refresh the list as
// folders complete.
func (s *session) getMessages() {
	acct := s.accountID()
	s.mark("Fetching " + acct + "…")
	s.async(func() (any, error) {
		return s.cli.Sync(acct)
	}, func(v any, err error) {
		if mailcore.IsLocked(err) {
			s.promptUnlock()
			return
		}
		if err != nil {
			widgets.Warn(s.win.Content(), "Fetch", err.Error(), nil)
			s.refreshAll()
			return
		}
		res := v.(mailcore.SyncResult)
		s.refreshAll()
		if strings.Contains(res.Error, mailcore.ErrLocked.Error()) {
			s.promptUnlock()
			return
		}
		if res.Error != "" {
			s.mark("Sync: " + res.Error)
			return
		}
		if res.New == 0 {
			s.mark("No new messages on " + acct)
			return
		}
		s.mark(fmt.Sprintf("Downloaded %d message(s)", res.New))
	})
}
func (s *session) toggleOnline() {
	s.online = !s.online
	online := s.online
	s.refreshStatus()
	s.async(func() (any, error) {
		return s.cli.SetOnline(online)
	}, func(v any, err error) {
		if err != nil {
			s.mark(err.Error())
			return
		}
		s.refreshStatus()
		if online {
			s.mark(fmt.Sprintf("Online · flushed %d queued op(s)", v.(int)))
			s.refreshAll()
			return
		}
		s.mark("Working offline · send/move/delete/flag queue in the Outbox")
	})
}
func (s *session) muteThread(muted bool) {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	tid := m.ThreadID
	if tid == "" {
		tid = mailcore.ThreadIDOf(m)
	}
	if err := s.cli.MuteThread(tid, muted); err != nil {
		s.mark(err.Error())
		return
	}
	if muted {
		s.mark("Muted thread")
	} else {
		s.mark("Unmuted thread")
	}
	s.refreshList()
	s.rebuildTree()
}

func (s *session) addVIP() {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	v, err := s.cli.PutVIP(mailcore.VIP{Address: mailcore.ExtractAddr(m.From), Name: mailcore.DisplayName(m.From)})
	if err != nil {
		s.mark(err.Error())
		return
	}
	s.mark("VIP: " + v.Address)
	s.rebuildTree()
}

func (s *session) recategorize(cat string) {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	if err := s.cli.SetSenderCategory(mailcore.ExtractAddr(m.From), cat); err != nil {
		s.mark(err.Error())
		return
	}
	s.mark("Sender filed under " + cat)
	s.refreshAll()
}

func (s *session) newSmartFolder() {
	win, err := s.app.NewWindow(platform.WindowOptions{
		Title: "Smart Folder", Width: 420, Height: 280, MinWidth: 320, MinHeight: 220,
	})
	if err != nil {
		s.mark(err.Error())
		return
	}
	name := widgets.NewTextField("", "Folder name", nil)
	query := widgets.NewTextField(s.filter.Query, "Search query", nil)
	save := newButton("Save", func() {
		sf, err := s.cli.PutSmartFolder(mailcore.SmartFolder{
			Name:   strings.TrimSpace(name.Text),
			Filter: mailcore.Filter{Query: strings.TrimSpace(query.Text), Unread: s.filter.Unread, Starred: s.filter.Starred, Attachment: s.filter.Attachment},
		})
		if err != nil {
			widgets.Warn(win.Content(), "Smart Folder", err.Error(), nil)
			return
		}
		s.folder = sf.FolderIDFor()
		s.central = false
		s.refreshAll()
		s.mark("Smart folder: " + sf.Name)
		win.Close()
	})
	save.Primary = true
	cancel := newButton("Cancel", func() { win.Close() })
	// The fields scroll in a short window; Save and Cancel stay in view.
	fields := widgets.NewScrollView(widgets.NewColumn(
		widgets.NewTitle("Saved search"),
		widgets.NewLabel("Name"), name,
		widgets.NewLabel("Query"), query,
		wrapLabel("Unread / starred / attachment pins on the Tags tree are included."),
	).WithGap(8))
	form := widgets.NewColumn(fields,
		widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept)).WithGap(8)
	form.AddFlex(fields, 1)
	setContent(win, widgets.NewPad(12, form))
}

func (s *session) openSmartFolders() {
	s.newSmartFolder()
}

func (s *session) setRead(read bool) {
	s.setFlagsLater(s.ids(), mailcore.FlagPatch{Read: mailcore.BoolPtr(read)})
}

func (s *session) toggleStar() {
	m, ok := s.primary()
	if !ok {
		return
	}
	s.setFlagsLater(s.ids(), mailcore.FlagPatch{Starred: mailcore.BoolPtr(!m.Starred)})
}

// toggleTag flips tag on every selected message, each from the tags its
// row already shows.
func (s *session) toggleTag(tag string) {
	ids := s.ids()
	if len(ids) == 0 {
		s.mark("No selection")
		return
	}
	s.commitUndo() // a move or delete waiting on the bar goes through
	before := map[mailcore.MessageID][]string{}
	added := false
	for _, id := range ids {
		if m, ok := s.messageByID(id); ok {
			before[id] = append([]string(nil), m.Tags...)
			next := mailcore.ToggleTag(m.Tags, tag)
			added = added || mailcore.HasTag(next, tag)
			s.setFlagsLater([]mailcore.MessageID{id}, mailcore.FlagPatch{Tags: &next})
		}
	}
	if s.shownOK {
		s.showHeaders(s.shown)
	}
	label := "Tagged " + tag
	if !added {
		label = "Took off " + tag
	}
	if len(before) > 1 {
		label = fmt.Sprintf("%s · %d messages", label, len(before))
	}
	s.offerTagUndo(before, label)
	s.mark(label)
}

// tagUndo is a tag change that can be taken back: each message's tags
// from before it.
type tagUndo struct {
	before map[mailcore.MessageID][]string
	label  string
	timer  *time.Timer
}

// offerTagUndo puts a tag change on the undo bar for the undo window.
func (s *session) offerTagUndo(before map[mailcore.MessageID][]string, label string) {
	s.dropTagUndo()
	if len(before) == 0 {
		return
	}
	u := &tagUndo{before: before, label: label}
	s.tagUndo = u
	if s.undoLabel != nil {
		s.undoLabel.SetText(label)
	}
	if s.undoBar != nil {
		s.undoBar.SetVisible(true)
	}
	if appLooping(s.app) {
		u.timer = time.AfterFunc(undoWindow, func() {
			s.app.Post(func() {
				if s.tagUndo == u {
					s.dropTagUndo()
				}
			})
		})
	}
}

// dropTagUndo takes a tag change off the undo bar (it stays done).
func (s *session) dropTagUndo() {
	u := s.tagUndo
	if u == nil {
		return
	}
	s.tagUndo = nil
	if u.timer != nil {
		u.timer.Stop()
	}
	if s.undoBar != nil && s.undo == nil {
		s.undoBar.SetVisible(false)
	}
}

// undoWindow is how long a move or delete can be taken back before it
// reaches the daemon.
var undoWindow = 6 * time.Second

// pendingRemoval is a move or delete held back for its undo window.
type pendingRemoval struct {
	ids   []mailcore.MessageID
	what  string // "Archive", "Delete", …: the title of an error
	done  string // "Archived": what the bar and the status say
	call  func() error
	timer *time.Timer
}

// messageByID is id as the window has it: its open tab, else its row.
func (s *session) messageByID(id mailcore.MessageID) (mailcore.Message, bool) {
	if mt, ok := s.activeTab(); ok && mt.msg.ID == id {
		return mt.msg, true
	}
	for _, m := range s.rows {
		if m.ID == id {
			return m, true
		}
	}
	return mailcore.Message{}, false
}

// removeLater takes ids out of the list at once and offers Undo. The
// daemon is told only when the undo window closes (or the next move, a
// quit or closing the window cuts it short), so undoing never has to
// chase a message the server has already renumbered. Without a live loop
// (headless, tests) the call is made at once.
func (s *session) removeLater(ids []mailcore.MessageID, what, done string, call func() error) {
	s.commitUndo()
	s.dropTagUndo() // the bar now offers this instead
	s.closeTabsFor(ids)
	gone := make(map[mailcore.MessageID]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}
	next := -1
	keep := s.rows[:0:0]
	for i, m := range s.rows {
		if gone[m.ID] {
			if next < 0 {
				next = i
			}
			continue
		}
		keep = append(keep, m)
	}
	s.rows = keep
	s.selected = nil
	// The row below the first one removed takes its place, the way a mail
	// client moves on to the next message.
	if next >= 0 && len(s.rows) > 0 {
		if next >= len(s.rows) {
			next = len(s.rows) - 1
		}
		s.selected = []mailcore.MessageID{s.rows[next].ID}
	}
	s.showRows()

	p := &pendingRemoval{ids: ids, what: what, done: done, call: call}
	if !appLooping(s.app) {
		s.runRemoval(p)
		return
	}
	if s.hidden == nil {
		s.hidden = map[mailcore.MessageID]bool{}
	}
	for _, id := range ids {
		s.hidden[id] = true
	}
	s.undo = p
	label := done
	if len(ids) > 1 {
		label = fmt.Sprintf("%s · %d messages", done, len(ids))
	}
	if s.undoLabel != nil {
		s.undoLabel.SetText(label)
	}
	if s.undoBar != nil {
		s.undoBar.SetVisible(true)
	}
	p.timer = time.AfterFunc(undoWindow, func() {
		s.app.Post(func() {
			if s.undo == p {
				s.commitUndo()
			}
		})
	})
}

// commitUndo sends the pending move or delete to the daemon now.
func (s *session) commitUndo() {
	p := s.takeUndo()
	if p == nil {
		return
	}
	s.runRemoval(p)
}

// commitUndoNow sends the pending move or delete and waits for it: for a
// quit or a closing window, which will not be around for the reply.
func (s *session) commitUndoNow() {
	p := s.takeUndo()
	if p == nil {
		return
	}
	if err := p.call(); err != nil {
		s.mark(p.what + ": " + err.Error())
	}
}

// undoLast puts the pending move or delete's messages back in the list;
// the daemon never heard of it.
func (s *session) undoLast() {
	if u := s.tagUndo; u != nil && s.undo == nil {
		// The last change was a tag: put every message's tags back.
		s.dropTagUndo()
		for id, tags := range u.before {
			tags := tags
			s.setFlagsLater([]mailcore.MessageID{id}, mailcore.FlagPatch{Tags: &tags})
		}
		if s.shownOK {
			if tags, ok := u.before[s.shown.ID]; ok {
				s.shown.Tags = tags
				s.showHeaders(s.shown)
			}
		}
		s.mark("Undone: " + u.label)
		return
	}
	p := s.takeUndo()
	if p == nil {
		return
	}
	for _, id := range p.ids {
		delete(s.hidden, id)
	}
	s.selected = append([]mailcore.MessageID(nil), p.ids...)
	s.refreshList()
	s.mark("Undone: " + p.done)
}

func (s *session) takeUndo() *pendingRemoval {
	p := s.undo
	if p == nil {
		return nil
	}
	s.undo = nil
	if p.timer != nil {
		p.timer.Stop()
	}
	if s.undoBar != nil {
		s.undoBar.SetVisible(false)
	}
	return p
}

func (s *session) runRemoval(p *pendingRemoval) {
	if len(p.ids) > 1 {
		s.mark(p.what + " " + pluralize(len(p.ids), "message") + "…") // the daemon then counts them off
	} else {
		s.mark(p.what + "…")
	}
	s.async(func() (any, error) {
		return nil, p.call()
	}, func(_ any, err error) {
		for _, id := range p.ids {
			delete(s.hidden, id)
		}
		if err != nil {
			widgets.Warn(s.win.Content(), p.what, err.Error(), nil)
		} else {
			s.mark(p.done)
		}
		s.refreshAll()
	})
}

// moveTo moves the selected messages to dest, with Undo.
func (s *session) moveTo(ids []mailcore.MessageID, dest mailcore.Folder) {
	if len(ids) == 0 {
		return
	}
	s.removeLater(ids, "Move", "Moved to "+dest.Name, func() error { return s.cli.Move(ids, dest.ID) })
}

// moveMenu is the Move to submenu: every folder of the primary message's
// account, nested as in the sidebar, the folder it is in greyed out.
func (s *session) moveMenu() []*widgets.MenuItem {
	acct := s.accountID()
	if m, ok := s.primary(); ok && m.AccountID != "" {
		acct = m.AccountID
	}
	folders, _ := s.cli.ListFolders(acct)
	byParent := map[mailcore.FolderID][]mailcore.Folder{}
	for _, f := range folders {
		if f.Virtual {
			continue
		}
		byParent[f.Parent] = append(byParent[f.Parent], f)
	}
	var items []*widgets.MenuItem
	var walk func(parent mailcore.FolderID, depth int)
	walk = func(parent mailcore.FolderID, depth int) {
		for _, f := range orderFolderChildren(byParent[parent]) {
			f := f
			it := iconItem(style.IconFolder, strings.Repeat("    ", depth)+f.Name, "", func() { s.moveTo(s.ids(), f) })
			it.Disabled = f.ID == s.folder
			items = append(items, it)
			walk(f.ID, depth+1)
		}
	}
	walk("", 0)
	if len(items) == 0 {
		items = append(items, &widgets.MenuItem{Text: "(no folders)", Disabled: true})
	}
	return items
}

// folderMoveMenu lists where folder f can go: the top level, or under any
// other folder of its account but itself and those inside it.
func (s *session) folderMoveMenu(f mailcore.Folder) []*widgets.MenuItem {
	folders, _ := s.cli.ListFolders(f.AccountID)
	byParent := map[mailcore.FolderID][]mailcore.Folder{}
	for _, x := range folders {
		if !x.Virtual {
			byParent[x.Parent] = append(byParent[x.Parent], x)
		}
	}
	top := iconItem(style.IconFolder, "Top Level", "", func() { s.moveFolder(f, "") })
	top.Disabled = f.Parent == ""
	items := []*widgets.MenuItem{top, widgets.Sep()}
	var walk func(parent mailcore.FolderID, depth int)
	walk = func(parent mailcore.FolderID, depth int) {
		for _, x := range orderFolderChildren(byParent[parent]) {
			if x.ID == f.ID {
				continue // not into itself, nor anything inside it
			}
			x := x
			it := iconItem(style.IconFolder, strings.Repeat("    ", depth)+x.Name, "", func() { s.moveFolder(f, x.ID) })
			it.Disabled = x.ID == f.Parent
			items = append(items, it)
			walk(x.ID, depth+1)
		}
	}
	walk("", 0)
	return items
}

// moveFolder puts f under parent, off the UI goroutine.
func (s *session) moveFolder(f mailcore.Folder, parent mailcore.FolderID) {
	s.async(func() (any, error) {
		return s.cli.MoveFolder(f.ID, parent)
	}, func(_ any, err error) {
		if err != nil {
			widgets.Warn(s.win.Content(), "Move Folder", err.Error(), nil)
			return
		}
		s.refreshAll()
		s.mark("Moved " + f.Name)
	})
}

// compactFolder removes what is marked deleted in f, on the server.
func (s *session) compactFolder(f mailcore.Folder) {
	s.mark("Compacting " + f.Name + "…")
	s.async(func() (any, error) {
		return nil, s.cli.CompactFolder(f.ID)
	}, func(_ any, err error) {
		if err != nil {
			widgets.Warn(s.win.Content(), "Compact Folder", err.Error(), nil)
			return
		}
		s.refreshAll()
		s.mark("Compacted " + f.Name)
	})
}

// mimeMessageIDs marks a drag of messages inside this window; the ids ride
// in the drag's in-process payload.
const mimeMessageIDs = "application/x-comms-mail-message-ids"

// dragMessages is what dragging the selected rows carries: their ids, to
// drop on a folder in the sidebar.
func (s *session) dragMessages(rows []int) *widget.Drag {
	var ids []mailcore.MessageID
	var subjects []string
	for _, r := range rows {
		if r >= 0 && r < len(s.rows) {
			ids = append(ids, s.rows[r].ID)
			subjects = append(subjects, s.rows[r].Subject)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return &widget.Drag{
		Types: []string{mimeMessageIDs, "text/plain"},
		Data: func(mime string) ([]byte, bool) {
			if mime == "text/plain" {
				return []byte(strings.Join(subjects, "\n")), true
			}
			return nil, false
		},
		Payload:   ids,
		Source:    s.table,
		Local:     true,
		Actions:   platform.DragMove,
		Preferred: platform.DragMove,
	}
}

// mimeFolder marks a drag of a folder inside the tree; the folder rides in
// the drag's in-process payload.
const mimeFolder = "application/x-comms-mail-folder"

// dragFolder is what dragging a folder in the tree carries: the folder, to
// drop on another folder of its account (it goes inside it) or on the
// account (it goes to the top level). Only a folder you made moves, as in
// Move Folder To; Inbox, Sent and the others stay where they are.
func (s *session) dragFolder(n *widgets.TreeNode) *widget.Drag {
	fid, ok := n.Data.(mailcore.FolderID)
	if !ok || s.cli == nil {
		return nil
	}
	f, ok, err := s.cli.GetFolder(fid)
	if err != nil || !ok || f.Virtual || f.Kind != mailcore.FolderCustom {
		return nil
	}
	return &widget.Drag{
		Types: []string{mimeFolder, "text/plain"},
		Data: func(mime string) ([]byte, bool) {
			if mime == "text/plain" {
				return []byte(f.Name), true
			}
			return nil, false
		},
		Payload:   f,
		Source:    s.tree,
		Local:     true,
		Actions:   platform.DragMove,
		Preferred: platform.DragMove,
	}
}

// dropFolderOn moves folder f under the node it was dropped on: inside a
// folder of its account, or to the top level on the account itself. Not
// into itself, nor into a folder inside it.
func (s *session) dropFolderOn(f mailcore.Folder, n *widgets.TreeNode) bool {
	var parent mailcore.FolderID
	switch d := n.Data.(type) {
	case string:
		if d != f.AccountID || d == mailcore.AccountTags || f.Parent == "" {
			return false
		}
	case mailcore.FolderID:
		t, ok, err := s.cli.GetFolder(d)
		if err != nil || !ok || t.Virtual || t.AccountID != f.AccountID || t.ID == f.ID || t.ID == f.Parent {
			return false
		}
		folders, _ := s.cli.ListFolders(f.AccountID)
		byID := map[mailcore.FolderID]mailcore.Folder{}
		for _, x := range folders {
			byID[x.ID] = x
		}
		for p, hops := t.Parent, 0; p != "" && hops < 64; p, hops = byID[p].Parent, hops+1 {
			if p == f.ID {
				return false // a folder inside f
			}
		}
		parent = t.ID
	default:
		return false
	}
	s.moveFolder(f, parent)
	return true
}

// dropOnFolder moves messages dragged from the list onto the folder under
// the drop, or a folder dragged in the tree (dropFolderOn).
func (s *session) dropOnFolder(n *widgets.TreeNode, e widget.DropEvent) bool {
	if n == nil {
		return false
	}
	if f, ok := e.Payload.(mailcore.Folder); ok {
		return s.dropFolderOn(f, n)
	}
	ids, ok := e.Payload.([]mailcore.MessageID)
	if !ok || len(ids) == 0 || n == nil {
		return false
	}
	fid, ok := n.Data.(mailcore.FolderID)
	if !ok || fid == s.folder {
		return false
	}
	f, ok, err := s.cli.GetFolder(fid)
	if err != nil || !ok || f.Virtual {
		return false
	}
	s.moveTo(ids, f)
	return true
}

// showRows puts s.rows in the table and card list and the primary in the
// preview, without asking the daemon for the list again.
func (s *session) showRows() {
	if s.table != nil {
		s.table.RowCount = len(s.rows)
		s.table.Invalidate()
	}
	if s.cards != nil {
		s.cards.Count = len(s.rows)
		s.cards.Invalidate()
	}
	s.syncViews()
	s.loadPreview()
	s.refreshStatus()
}

func (s *session) deleteSel() {
	ids := s.ids()
	if len(ids) == 0 {
		return
	}
	s.removeLater(ids, "Delete", "Deleted", func() error { return s.cli.Delete(ids) })
}

func (s *session) junk() {
	ids := s.ids()
	if len(ids) == 0 {
		return
	}
	junk, ok := mailcore.SpecialFolderClient(s.cli, s.accountID(), mailcore.FolderJunk)
	if !ok {
		s.mark("No Junk folder")
		return
	}
	s.removeLater(ids, "Junk", "Moved to Junk", func() error { return s.cli.Move(ids, junk.ID) })
}

func (s *session) archive() {
	ids := s.ids()
	if len(ids) == 0 {
		return
	}
	arch, ok := mailcore.SpecialFolderClient(s.cli, s.accountID(), mailcore.FolderArchive)
	if !ok {
		s.mark("No Archives folder")
		return
	}
	s.removeLater(ids, "Archive", "Archived", func() error { return s.cli.Move(ids, arch.ID) })
}

func (s *session) emptyTrash() {
	trash, ok := mailcore.SpecialFolderClient(s.cli, s.accountID(), mailcore.FolderTrash)
	if !ok {
		s.mark("No Trash")
		return
	}
	list, _ := s.cli.ListMessages(trash.ID, mailcore.Filter{})
	if len(list) == 0 {
		s.mark("Trash is empty")
		return
	}
	widgets.Confirm(s.win.Content(), "Empty Trash?",
		fmt.Sprintf("Permanently delete %d messages? (demo store only)", len(list)),
		func(yes bool) {
			if !yes {
				s.mark("Kept trash")
				return
			}
			ids := make([]mailcore.MessageID, len(list))
			for i, m := range list {
				ids[i] = m.ID
			}
			s.mark("Emptying Trash…")
			s.async(func() (any, error) {
				return nil, s.cli.Delete(ids)
			}, func(_ any, err error) {
				if err != nil {
					widgets.Warn(s.win.Content(), "Empty Trash", err.Error(), nil)
				} else {
					s.mark("Trash emptied")
				}
				s.selected = nil
				s.refreshAll()
			})
		})
}

// markFolderRead marks every message in a folder read, off the UI goroutine.
func (s *session) markFolderRead(id mailcore.FolderID) {
	s.mark("Marking read…")
	s.async(func() (any, error) {
		return nil, s.cli.MarkFolderRead(id)
	}, func(_ any, err error) {
		if err != nil {
			widgets.Warn(s.win.Content(), "Mark Folder Read", err.Error(), nil)
			return
		}
		s.refreshAll()
		s.mark("Folder marked read")
	})
}

// confirmDeleteFolder asks before deleting a user folder and its messages.
func (s *session) confirmDeleteFolder(f mailcore.Folder) {
	widgets.Confirm(s.win.Content(), "Delete folder?",
		fmt.Sprintf("Delete %q and its messages from the server? This cannot be undone.", f.Name),
		func(yes bool) {
			if !yes {
				return
			}
			s.async(func() (any, error) {
				return nil, s.cli.DeleteFolder(f.ID)
			}, func(_ any, err error) {
				if err != nil {
					widgets.Warn(s.win.Content(), "Delete Folder", err.Error(), nil)
					return
				}
				if s.folder == f.ID {
					s.central = false
					s.ensureUsableFolder()
				}
				s.selected = nil
				s.refreshAll()
				s.mark("Deleted " + f.Name)
			})
		})
}

// newFolder asks for a name and makes a top-level folder in the current
// account.
func (s *session) newFolder() {
	s.createFolderNamed(s.accountID(), "", "New Folder")
}

// newSubfolder asks for a name and makes a folder inside parent.
func (s *session) newSubfolder(parent mailcore.Folder) {
	s.createFolderNamed(parent.AccountID, parent.ID, "New Folder in “"+parent.Name+"”")
}

func (s *session) createFolderNamed(acct string, parent mailcore.FolderID, title string) {
	var made mailcore.Folder
	askName(s.win.Content(), s.app, title, "Folder name", "", "Create", func(name string) error {
		f, err := s.cli.CreateFolder(acct, name, parent)
		made = f
		return err
	}, func(name string) {
		s.central = false
		s.folder = made.ID
		s.selected = nil
		s.refreshAll()
		s.mark("Created " + name)
	})
}

// renameFolder asks for a folder's new name. The folder keeps its messages,
// its place in the tree, and — when it is the one showing — the view.
func (s *session) renameFolder(f mailcore.Folder) {
	askName(s.win.Content(), s.app, "Rename Folder", "New name for “"+f.Name+"”", f.Name, "Rename", func(name string) error {
		if name == f.Name {
			return nil
		}
		_, err := s.cli.RenameFolder(f.ID, name)
		return err
	}, func(name string) {
		s.refreshAll()
		s.mark("Renamed " + f.Name + " to " + name)
	})
}

func (s *session) selectAll() {
	primary := s.primaryIndex()
	s.selected = s.selected[:0]
	for i, m := range s.rows {
		if i != primary {
			s.selected = append(s.selected, m.ID)
		}
	}
	if primary >= 0 {
		s.selected = append(s.selected, s.rows[primary].ID)
	}
	s.syncViews()
	s.refreshStatus()
	s.mark(fmt.Sprintf("%d selected", len(s.selected)))
}

func (s *session) moveSel(delta int) {
	if len(s.rows) == 0 {
		return
	}
	i := s.primaryIndex()
	i += delta
	if i < 0 {
		i = 0
	}
	if i >= len(s.rows) {
		i = len(s.rows) - 1
	}
	s.clickRow(i, false)
}

func (s *session) nextUnread() {
	start := s.primaryIndex() + 1
	for i := 0; i < len(s.rows); i++ {
		j := (start + i) % len(s.rows)
		if !s.rows[j].Read {
			s.clickRow(j, false)
			return
		}
	}
	s.mark("No unread in this folder")
}

func (s *session) goKind(k mailcore.FolderKind) {
	if f, ok := mailcore.SpecialFolderClient(s.cli, s.accountID(), k); ok {
		s.central = false
		s.folder = f.ID
		s.selected = nil
		s.refreshAll()
	}
}

func (s *session) toggleFilterPin(p filterPin) {
	switch p {
	case pinUnread:
		s.filter.Unread = !s.filter.Unread
	case pinStarred:
		s.filter.Starred = !s.filter.Starred
	case pinAttachment:
		s.filter.Attachment = !s.filter.Attachment
	default:
		return
	}
	s.rebuildTree()
	s.refreshList()
}

func (s *session) about() {
	widgets.Info(s.win.Content(), "About Mail",
		"Mail — Thunderbird chrome on uitoolkit "+uitoolkit.Version+".\n"+
			"comms-mail talks JSON-RPC to comms-maild (Unix socket).\n"+
			"No IMAP/SMTP in this process. Empty until you add an account.\n"+
			"MemoryStore dogfood: UITK_MAIL=memory.\n\n"+
			"Message view is plain text only (HTML is stripped).\n"+
			"UI: Titillium Web. Source tab: JetBrains Mono.\n"+
			"See docs/mail.md",
		nil)
}

func (s *session) maybeAskAddAccount(from widget.Component) {
	if s.askedEmpty {
		return
	}
	accts, err := s.cli.Accounts()
	if err != nil || len(accts) > 0 {
		return
	}
	s.askedEmpty = true
	widgets.Confirm(from, "Mail", FirstRunPrompt, func(yes bool) {
		if yes {
			s.openAddAccount()
		}
	})
}

func (s *session) openAddAccount() {
	if _, err := OpenAddAccount(s.app, s.cli, func() {
		s.account = firstAccountID(s.cli)
		if inbox, ok := mailcore.SpecialFolderClient(s.cli, s.account, mailcore.FolderInbox); ok {
			s.folder = inbox.ID
		}
		s.refreshAll()
		s.mark("Account saved — Fetch to connect")
	}); err != nil {
		widgets.Warn(s.win.Content(), "Add account", err.Error(), nil)
		return
	}
	s.mark("Add account")
}

func (s *session) cardAt(i int) widgets.CardContent {
	if i < 0 || i >= len(s.rows) {
		return widgets.CardContent{}
	}
	m := s.rows[i]
	snip := m.Snippet
	if snip == "" {
		snip = mailcore.SnippetOf(m.Body)
	}
	var badges []widgets.CardBadge
	for _, name := range m.Tags {
		col := paintengine2d.RGB(0.5, 0.5, 0.55)
		if t, ok := mailcore.TagByName(s.tags, name); ok {
			col = mailcore.ParseHexColor(t.Color)
		}
		badges = append(badges, widgets.CardBadge{Label: name, Color: col})
	}
	meta := mailcore.FormatDate(m.Date, s.now())
	if s.mixedFolders() && s.folderNames[m.Folder] != "" {
		meta = s.folderNames[m.Folder] + "  ·  " + meta
	}
	return widgets.CardContent{
		Title:    m.Correspondent(s.kind),
		Subtitle: m.Subject,
		Meta:     meta,
		Snippet:  snip,
		Badges:   badges,
		Bold:     !m.Read,
		Starred:  m.Starred,
	}
}

func (s *session) setCardView(on bool) {
	s.cardView = on
	s.persistChrome()
	if s.table != nil {
		s.table.SetVisible(!on)
	}
	if s.cards != nil {
		s.cards.SetVisible(on)
	}
	if s.win != nil {
		s.win.RequestLayout()
	}
	s.refreshList()
	if on {
		s.mark("Card view")
	} else {
		s.mark("Table view")
	}
}

func (s *session) setDensity(d style.Density) {
	s.density = d
	s.persistChrome()
	s.rebuild()
}

func (s *session) applyRowMetrics() {
	rh, cardH, treeH := densityRows(s.density)
	if s.table != nil {
		s.table.RowHeight = rh
	}
	if s.cards != nil {
		s.cards.CardHeight = cardH
	}
	if s.tree != nil {
		s.tree.RowHeight = treeH
	}
}

func densityRows(d style.Density) (table, card, tree float32) {
	switch d {
	case style.DensityCompact:
		return 22, 56, 20
	case style.DensityRelaxed:
		return 36, 88, 32
	default:
		return 28, 68, 24
	}
}

func (s *session) tagNames() []string {
	if len(s.tags) == 0 {
		if tags, err := s.cli.Tags(); err == nil {
			s.tags = tags
		}
	}
	out := make([]string, 0, len(s.tags))
	for _, t := range s.tags {
		if t.System || mailcore.IsSystemTag(t.Name) {
			continue
		}
		out = append(out, t.Name)
	}
	if len(out) == 0 {
		return demoTags()
	}
	return out
}

// partBytes reads one part's bytes: what comms-maild hands back, or the
// file it points at for a part it has already spilled to disk.
//
// It takes the part's id rather than an index, so a caller that means to
// run it off the UI goroutine can settle which part it wants first — the
// index and the names beside it belong to the window (attach_drag.go).
func (s *session) partBytes(id mailcore.MessageID, pid, name string) ([]byte, error) {
	p, err := s.cli.GetPart(id, pid)
	if err != nil {
		return nil, err
	}
	if len(p.Data) == 0 && p.Path != "" {
		return os.ReadFile(p.Path)
	}
	if len(p.Data) == 0 {
		return nil, fmt.Errorf("attachment %q is empty", name)
	}
	return p.Data, nil
}
func newAttachHit(text string, on func()) *attachHit {
	h := &attachHit{Text: text, OnPress: on}
	h.Init(h)
	return h
}

func (h *attachHit) Measure(c layout.Constraints) paintengine2d.Point {
	lk := h.Look()
	f := lk.Font()
	sz := f.Measure(h.Text)
	sz.X += 3*style.Dip(lk, 6) + f.Height()
	sz.Y += 6
	return c.Constrain(sz)
}

func (h *attachHit) Arrange(r paintengine2d.Rect) { h.SetBounds(r) }

// Paint is the look's list row with a paperclip icon before the name.
func (h *attachHit) Paint(ctx *paintengine2d.Context) {
	lk := h.Look()
	b := h.LocalBounds()
	st := style.RowState(h.Selected, h.Hovered())
	lk.DrawListRow(ctx, b, st, "")
	fg := lk.Palette().Text
	if st.Checked() {
		if on := lk.Palette().TextOnAccent; on != (paintengine2d.Color{}) {
			fg = on
		}
	}
	f := lk.Font()
	pad := style.Dip(lk, 6)
	sz := f.Height()
	y := b.Min.Y + (b.Dy()-sz)*0.5
	style.DrawToolIcon(ctx, paintengine2d.XYWH(b.Min.X+pad, y, sz, sz), style.IconAttach, fg, style.IconSetOf(lk))
	x := b.Min.X + 2*pad + sz
	text := h.Text
	if room := b.Max.X - pad - x; f.Advance(text) > room {
		text = f.Fit(text, room)
	}
	f.Draw(ctx, text, paintengine2d.Pt(x, b.Min.Y+(b.Dy()-f.Height())*0.5), fg)
}

func (h *attachHit) MousePress(widget.MouseEvent) bool {
	if h.OnPress != nil {
		h.OnPress()
	}
	return true
}

type attachBlob struct {
	Name string
	Data []byte
}

// attachHit is the clickable name on an attachment row (select; double-click
// opens; dragging it out hands the file to another application).
type attachHit struct {
	widget.Base
	Text     string
	Selected bool
	OnPress  func()
	// Drag is what a press on the row drags out (attach_drag.go).
	Drag func() *widget.Drag
}

func uniqueFileName(name string, used map[string]int) string {
	name = mailcore.AttachFileName(name)
	if used[name] == 0 {
		used[name] = 1
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if used[cand] == 0 {
			used[cand] = 1
			return cand
		}
	}
}

func saveAllDir(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	st, err := os.Stat(path)
	if err == nil {
		if st.IsDir() {
			return path, nil
		}
		return filepath.Dir(path), nil
	}
	if filepath.Ext(path) != "" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		return dir, nil
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func mailDirEntries(path string) []widgets.FileInfo {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	out := make([]widgets.FileInfo, 0, len(ents))
	for _, e := range ents {
		out = append(out, widgets.FileInfo{Name: e.Name(), Dir: e.IsDir()})
	}
	return out
}

func (s *session) openFilters() {
	if _, err := OpenFilters(s.app, s.cli); err != nil {
		widgets.Warn(s.win.Content(), "Tags", err.Error(), nil)
		return
	}
	s.mark("Tags")
}

func (s *session) accountID() string {
	if s.account != "" {
		return s.account
	}
	if f, ok, _ := s.cli.GetFolder(s.folder); ok {
		return f.AccountID
	}
	return firstAccountID(s.cli)
}

func firstAccountID(cli *mailcore.Client) string {
	accts, err := cli.Accounts()
	if err != nil || len(accts) == 0 {
		return ""
	}
	return accts[0].ID
}

func (s *session) accounts() []mailcore.Account {
	a, err := s.cli.Accounts()
	if err != nil {
		s.mark(err.Error())
		return nil
	}
	return a
}

func (s *session) backendLabel() string {
	if s.backend == "" {
		return "comms-maild"
	}
	return s.backend
}

func (s *session) showAccountCentral(acct string) {
	s.account = acct
	s.central = true
	if inbox, ok := mailcore.SpecialFolderClient(s.cli, acct, mailcore.FolderInbox); ok {
		s.folder = inbox.ID
	}
	s.selected = nil
	s.refreshAll()
	s.showCenter()
}

func syntheticAccount(id string) bool {
	return id == mailcore.AccountUnified || id == mailcore.AccountTags || id == mailcore.AccountSmart || id == mailcore.AccountCategories
}

func (s *session) ensureUsableFolder() {
	if s.folder != "" && !mailcore.HiddenFromFolderTree(s.folder) {
		if _, ok, err := s.cli.GetFolder(s.folder); err == nil && ok {
			return
		}
	}
	if inbox, ok := mailcore.SpecialFolderClient(s.cli, s.accountID(), mailcore.FolderInbox); ok {
		s.folder = inbox.ID
		s.central = false
	}
}

func (s *session) selectFolder(id mailcore.FolderID) {
	if mailcore.HiddenFromFolderTree(id) {
		s.openAccountInbox(s.accountID())
		return
	}
	s.central = false
	s.folder = id
	if s.searchAll {
		s.searchAll = false
		s.syncSearchBtn()
	}
	if f, ok, _ := s.cli.GetFolder(id); ok && f.AccountID != "" && !syntheticAccount(f.AccountID) {
		s.account = f.AccountID
	}
	s.selected = nil
	s.refreshAll()
	// Watched while it shows: new mail in it arrives at once.
	s.async(func() (any, error) { return nil, s.cli.FocusFolder(id) }, nil)
}

func (s *session) openAccountInbox(acct string) {
	if acct != "" {
		s.account = acct
	}
	s.central = false
	s.selected = nil
	if inbox, ok := mailcore.SpecialFolderClient(s.cli, s.account, mailcore.FolderInbox); ok {
		s.folder = inbox.ID
	}
	s.refreshAll()
}

func (s *session) showCenter() {
	if s.thread != nil {
		s.thread.SetVisible(!s.central)
	}
	if s.acctPanel != nil {
		s.acctPanel.SetVisible(s.central)
	}
	if s.win != nil {
		s.win.RequestLayout()
	}
}

func (s *session) buildAccountCentral() widget.Component {
	s.acctTitle = widgets.NewTitle("Account Central")
	s.acctBody = widgets.NewLabel("Use Settings or Account Central to add or remove stores. Open Inbox or pick a folder in the tree.")
	get := newButton("Fetch", s.getMessages)
	write := newButton("Write", s.write)
	prefs := newButton("Account Settings", s.openPrefs)
	inbox := newButton("Open Inbox", func() {
		s.goKind(mailcore.FolderInbox)
	})
	remove := newButton("Remove account…", s.removeCurrentAccount)
	return widgets.NewColumn(s.acctTitle, s.acctBody, widgets.NewSeparator(),
		foldRow(get, write, inbox, prefs, remove),
	).WithGap(10).WithPad(16)
}

func (s *session) refreshAccount() {
	if s.acctTitle == nil {
		return
	}
	name, addr, proto := "Account", "", "IMAP"
	for _, a := range s.accounts() {
		if a.ID == s.account {
			name, addr, proto = a.Name, a.Address, mailcore.ProtocolLabel(a)
			break
		}
	}
	unread, _ := s.cli.UnreadTotal()
	st, _ := s.cli.Status()
	s.acctTitle.SetText(name)
	s.acctBody.SetText(fmt.Sprintf("%s\n\nProtocol: %s\nIdentity for this window.\nUnread (all folders): %d\nDaemon: %s  ·  %s\nSocket: %s\n\nFetch, Write, or open Inbox — retrieve stays in comms-maild.",
		addr, proto, unread, st.Backend, s.backendLabel(), s.cli.Socket))
}

func (s *session) openPrefs() {
	if _, err := OpenPrefs(s.app, s.cli, s.afterAccountsChanged); err != nil {
		widgets.Warn(s.win.Content(), "Settings", err.Error(), nil)
		return
	}
	s.mark("Settings")
}

func (s *session) removeCurrentAccount() {
	id := s.account
	var acct mailcore.Account
	for _, a := range s.accounts() {
		if a.ID == id {
			acct = a
			break
		}
	}
	if acct.ID == "" {
		list := s.accounts()
		if len(list) > 0 {
			acct = list[0]
		}
	}
	if acct.ID == "" {
		widgets.Warn(s.win.Content(), "Remove account", "There is no account to remove.", nil)
		return
	}
	confirmRemoveAccount(s.win.Content(), acct, func() {
		if err := s.cli.DeleteAccount(acct.ID); err != nil {
			widgets.Warn(s.win.Content(), "Remove account", err.Error(), nil)
			return
		}
		s.afterAccountsChanged()
		s.mark("Account removed")
	})
}

func (s *session) afterAccountsChanged() {
	s.loadImageSenders() // Settings may have taken a trusted sender back
	accts := s.accounts()
	still := false
	for _, a := range accts {
		if a.ID == s.account {
			still = true
			break
		}
	}
	if !still {
		s.account = firstAccountID(s.cli)
		s.selected = nil
		if inbox, ok := mailcore.SpecialFolderClient(s.cli, s.account, mailcore.FolderInbox); ok {
			s.folder = inbox.ID
			s.central = false
		} else {
			s.folder = ""
		}
	}
	s.refreshAll()
	if len(accts) == 0 {
		s.askedEmpty = false
		s.maybeAskAddAccount(s.win.Content())
	}
}

func (s *session) handleKey(e widget.KeyEvent) bool {
	if e.Mods.Ctrl() && e.Key == platform.KeyU {
		s.viewSource()
		return true
	}
	if e.Mods.Ctrl() && e.Key == platform.KeyP {
		s.printMessage()
		return true
	}
	if e.Mods.Ctrl() && e.Key == platform.KeyF {
		s.openSearch()
		return true
	}
	// F10 is the app menu, as it was for the menu bar it used to be in
	// (its button carries its items' own keys, Ctrl+Q and Ctrl+,).
	if e.Key == platform.KeyF10 && !e.Mods.Ctrl() && !e.Mods.Alt() {
		s.openAppMenu()
		return true
	}
	if isTextFocus(s.win.Focus()) {
		return false
	}
	if e.Mods.Ctrl() || e.Mods.Alt() {
		if e.Mods.Ctrl() && e.Key == platform.KeyComma {
			s.openPrefs()
			return true
		}
		if e.Mods.Ctrl() && e.Mods.Shift() && e.Key == platform.KeyR {
			s.replyAll()
			return true
		}
		if e.Mods.Ctrl() && !e.Mods.Shift() && e.Key == platform.KeyZ && s.undo != nil {
			s.undoLast()
			return true
		}
		// Ctrl+Tab, Ctrl+Shift+Tab, Ctrl+W: the tabs in the title bar.
		if s.tabs != nil && s.tabs.Shortcut(e) {
			return true
		}
		return false
	}
	if isHashDelete(e) || e.Key == platform.KeyDelete {
		s.deleteSel()
		return true
	}
	_, onTab := s.activeTab()
	switch e.Key {
	case platform.KeyN:
		if !onTab {
			s.moveSel(1)
		}
		return true
	case platform.KeyP:
		if !onTab {
			s.moveSel(-1)
		}
		return true
	case platform.KeyD:
		s.deleteSel()
		return true
	case platform.KeyT:
		s.showTagMenu()
		return true
	case platform.KeyE:
		s.openInTab()
		return true
	case platform.KeyR:
		if e.Mods.Shift() {
			s.replyAll()
		} else {
			s.reply()
		}
		return true
	case platform.KeyA:
		s.archive()
		return true
	case platform.KeyF:
		s.forward()
		return true
	case platform.KeyC:
		s.write()
		return true
	case platform.KeyM:
		s.setRead(true)
		return true
	case platform.KeyS:
		s.toggleStar()
		return true
	case platform.KeyJ:
		s.junk()
		return true
	}
	return false
}

func demoTags() []string {
	return []string{"Important", "Work", "Personal", "To Do", "Later"}
}

func (s *session) viewSource() {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	s.mark("Message Source: loading…")
	s.async(func() (any, error) {
		return s.cli.GetSource(m.ID)
	}, func(v any, err error) {
		if err != nil {
			widgets.Warn(s.win.Content(), "Message Source", err.Error(), nil)
			return
		}
		if _, err := OpenMessageSource(s.app, m, v.(string)); err != nil {
			widgets.Warn(s.win.Content(), "Message Source", err.Error(), nil)
			return
		}
		s.mark("Message Source")
	})
}

// PrepareShot selects Ada’s Inbox welcome message and optionally opens File.
// WalkWindow visits w's title bar (Mail's chrome row lives there) and then
// its content.
func WalkWindow(w *app.Window, fn func(widget.Component)) {
	if tb := w.TitleBar(); tb != nil {
		widget.Walk(tb, fn)
	}
	widget.Walk(w.Content(), fn)
}

func PrepareShot(w *app.Window, openMenu int) {
	WalkWindow(w, func(c widget.Component) {
		if tv, ok := c.(*widgets.TableView); ok && len(tv.Columns) >= 5 {
			tv.Selected = 0
			if tv.OnSelect != nil {
				tv.OnSelect(0)
			}
			tv.Invalidate()
		}
		if cl, ok := c.(*widgets.CardList); ok && cl.Count > 0 {
			cl.Selected = 0
			if cl.OnSelect != nil {
				cl.OnSelect(0)
			}
			cl.Invalidate()
		}
		if mb, ok := c.(*widgets.MenuBar); ok && openMenu >= 0 {
			mb.Open(openMenu)
		}
	})
}

// PrepareShotCards forces card view for screenshots.
func PrepareShotCards(w *app.Window) {
	WalkWindow(w, func(c widget.Component) {
		if cl, ok := c.(*widgets.CardList); ok {
			cl.SetVisible(true)
			if cl.Count > 0 {
				// Select the first card as a click would (preview, read).
				cl.SetSelectedRows([]int{0})
				if cl.OnSelectionChange != nil {
					cl.OnSelectionChange([]int{0})
				}
			}
			cl.Invalidate()
		}
		if tv, ok := c.(*widgets.TableView); ok && len(tv.Columns) >= 5 {
			tv.SetVisible(false)
		}
	})
}

// repliedForwarded says what the user did with m: "You replied", "You
// forwarded", both, or "".
func repliedForwarded(m mailcore.Message) string {
	switch {
	case m.Answered && m.Forwarded:
		return "You replied and forwarded"
	case m.Answered:
		return "You replied"
	case m.Forwarded:
		return "You forwarded"
	}
	return ""
}
