package mail

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

// AppOptions tweak the first build (theme, layout, Quick Filter visibility).
type AppOptions struct {
	Light         bool
	Layout        LayoutMode
	ShowFilter    bool
	ShowStatusBar bool // default off: no reserved bottom strip
	CardView      bool
	Density       style.Density
}

// MailApp starts an in-process mailclientd (MemoryStore) and the UI client.
func MailApp(a *app.Application, win *app.Window) widget.Component {
	sock, _, err := StartDemo(context.Background())
	if err != nil {
		return widgets.NewLabel("mailclientd: " + err.Error())
	}
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		return widgets.NewLabel(err.Error())
	}
	return Open(a, win, cli, AppOptions{ShowFilter: false})
}

// Open builds the Mail chrome against a mailclientd Client (no Store / IMAP).
func Open(a *app.Application, win *app.Window, cli *Client, opts AppOptions) widget.Component {
	s := newSession(a, win, cli, opts)
	root := s.build()
	s.attachTray()
	return root
}

type session struct {
	app  *app.Application
	win  *app.Window
	cli  *Client
	opts AppOptions

	folder       FolderID
	account      string
	central      bool // Account Central instead of the thread list
	selected     []MessageID
	filter       Filter
	sortCol      int
	sortAsc      bool
	online       bool
	rows         []Message
	kind         FolderKind
	backend      string
	attachSel    int
	attNames     []string
	attachClickI int
	attachClickT time.Time

	chromePrefs ChromePrefs
	cardView    bool
	density     style.Density
	tags        []Tag
	threaded    bool
	hideMuted   bool
	muted       map[string]bool

	table                                      *widgets.TableView
	cards                                      *widgets.CardList
	listStack                                  *widgets.Stack
	tree                                       *widgets.TreeView
	outboxTree                                 *widgets.TreeView
	preview                                    *widgets.TextArea
	source                                     *widgets.TextArea
	attachHits                                 []*attachHit
	attachAll                                  *widgets.Button
	attachBar                                  widget.Component
	attachRows                                 *widgets.FlexBox
	attachPane                                 *widgets.FlexBox
	askedEmpty                                 bool
	trayMu                                     sync.Mutex
	tray                                       platform.StatusItem
	notes                                      mailNotifier
	mainBar                                    widget.Component
	listBar                                    *widgets.ToolBar
	hdrFrom, hdrSubj, hdrDate, hdrTo, hdrExtra *widgets.Label
	status                                     *widgets.StatusBar
	qf                                         *widgets.TextField
	qfBtn                                      *widgets.ToolItem
	acctPanel                                  widget.Component
	acctTitle                                  *widgets.Label
	acctBody                                   *widgets.Label
	thread                                     widget.Component
	center                                     *widgets.Stack

	busy      sync.WaitGroup
	refresher *refreshCoalescer
	// pendingRefresh records a daemon event that arrived while no UI loop
	// was pumping (headless / tests). DrainDaemonEvents applies it.
	pendingRefresh atomic.Bool
}

func newSession(a *app.Application, win *app.Window, cli *Client, opts AppOptions) *session {
	s := &session{
		app: a, win: win, cli: cli, opts: opts,
		sortCol: 4, sortAsc: false, online: true,
		attachSel: -1, attachClickI: -1,
	}
	p := loadChromePrefs()
	s.chromePrefs = p
	s.cardView = opts.CardView || p.CardView
	s.opts.ShowFilter = opts.ShowFilter || p.ShowFilter
	s.threaded = p.Threaded
	s.hideMuted = p.HideMute
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
	if inbox, ok := specialFolderClient(cli, s.account, FolderInbox); ok {
		s.folder = inbox.ID
	}
	return s
}

func (s *session) persistChrome() {
	s.chromePrefs.CardView = s.cardView
	s.chromePrefs.ShowFilter = s.opts.ShowFilter
	s.chromePrefs.Threaded = s.threaded
	s.chromePrefs.HideMute = s.hideMuted
	s.chromePrefs.Density = s.density.String()
	if s.opts.Layout == LayoutClassic {
		s.chromePrefs.Layout = "classic"
	} else {
		s.chromePrefs.Layout = "vertical"
	}
	s.chromePrefs.Light = s.opts.Light
	saveChromePrefs(s.chromePrefs)
	// Density / layout live in mailui.json only. look.json is Settings’
	// file; persist must not SaveAppearance or rewrite theme packs.
}

func (s *session) applyLook() {
	ap := style.LoadAppearance()
	if s.app != nil && !s.app.WatchingLook() && s.app.Look() != nil {
		// Screenshot / DarkLook fixtures stay static. WatchLook Mail
		// follows look.json (PreferredLook), never opts.Light.
		ap = style.LookAppearance(s.app.Look())
	}
	s.opts.Light = ap.Effective().Theme == style.ThemeLight
	s.app.SetLook(style.WithDensity(ap.Look(), s.density))
}

// setPalette is View → Dark / Light: flip the starter pack palette,
// keep corners/icons/iconSize, and write look.json only on this explicit toggle.
func (s *session) setPalette(light bool) {
	ap := style.LoadAppearance()
	if light {
		ap = ap.WithPalette(style.ThemeLight)
	} else {
		ap = ap.WithPalette(style.ThemeDark)
	}
	// Picking dark or light by hand stops following the desktop.
	ap.FollowDesktop = false
	_ = style.SaveAppearance(ap)
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
	s.hdrFrom = widgets.NewLabel("")
	s.hdrSubj = widgets.NewTitle("")
	s.hdrDate = widgets.NewLabel("")
	s.hdrTo = widgets.NewLabel("")
	s.hdrExtra = widgets.NewLabel("")
	s.preview = widgets.NewTextView("", "Select a message (plain text)")
	s.preview.MinRows = 8
	s.source = widgets.NewMonoTextView("", "Raw source")
	s.source.MinRows = 8
	s.source.Wrap = false

	s.table = widgets.NewTableView([]widgets.TableColumn{
		{Title: "★", Width: 28, MinWidth: 24, Sortable: true},
		{Title: "📎", Width: 28, MinWidth: 24, Sortable: true},
		{Title: "Topic", MinWidth: 180, Sortable: true},
		{Title: "Who", Width: 148, MinWidth: 110, Sortable: true},
		{Title: "When", Width: 108, MinWidth: 88, Sortable: true},
	}, 0, s.cellText, nil)
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
	s.tree.DisableTypeAhead = true
	s.outboxTree.DisableTypeAhead = true
	s.tree.Sidebar, s.outboxTree.Sidebar = true, true
	s.tree.SetAccessibleName("Folders")
	s.outboxTree.SetAccessibleName("Outbox")
	s.rebuildTree()
	s.wireFolderTree(s.tree)
	s.wireFolderTree(s.outboxTree)

	s.qf = widgets.NewTextField("", "Quick Filter (subject, people, body)", func(q string) {
		s.filter.Query = q
		s.refreshList()
	})
	s.qf.OnEscape = func() { s.showFilter(false) }
	s.qf.SetVisible(s.opts.ShowFilter)

	previewTab := widgets.NewPad(8, s.preview)
	sourceTab := widgets.NewPad(8, s.source)
	tabs := widgets.NewTabView(
		widgets.Tab{Title: "Message", Content: previewTab},
		widgets.Tab{Title: "Source", Content: sourceTab},
	)
	tabs.OnChange = func(i int) {
		if i == 0 {
			s.mark("Message")
		} else {
			s.mark("Source  ·  JetBrains Mono")
		}
	}
	s.attachAll = widgets.NewButton("Save All", s.saveAllAttachments)
	s.attachAll.SetEnabled(false)
	s.attachBar = widgets.NewRow(s.attachAll).WithGap(8)
	s.attachBar.SetVisible(false)
	s.attachRows = widgets.NewColumn().WithGap(4)
	s.attachPane = widgets.NewColumn(s.attachBar, s.attachRows).WithGap(4)
	s.attachPane.SetVisible(false)
	s.applyRowMetrics()
	headCol := widgets.NewColumn(s.hdrSubj, s.hdrFrom, s.hdrTo, s.hdrDate, s.hdrExtra, s.attachPane).WithGap(3).WithPad(10)
	previewCol := widgets.NewColumn(headCol, widgets.NewSeparator(), tabs).WithGap(0)
	previewCol.AddFlex(tabs, 1)

	s.table.SetVisible(!s.cardView)
	s.cards.SetVisible(s.cardView)
	s.listStack = widgets.NewStack(s.table, s.cards)
	thread := widgets.NewColumn(s.listStack).WithGap(0).WithPad(8)
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

	var split *widgets.Splitter
	if s.opts.Layout == LayoutClassic {
		right := widgets.NewSplitter(false, s.center, previewCol)
		right.Ratio = 0.46
		split = widgets.NewSplitter(true, sidebar, right)
		split.Ratio = 0.18
	} else {
		mid := widgets.NewSplitter(true, s.center, previewCol)
		mid.Ratio = 0.58
		split = widgets.NewSplitter(true, sidebar, mid)
		split.Ratio = 0.17
	}

	s.mainBar = s.composeBar()
	s.listBar = s.toolBar()
	// Thunderbird's single chrome row — the menu button, Fetch / Write, free
	// space, the quick filter and its tool bar — is the window's title bar:
	// the caption of the frame uitoolkit draws (caption buttons at the
	// desktop's sides, the free space moves the window), or the first row
	// under the desktop's own frame.
	head := widgets.NewHeaderBar([]widget.Component{s.menuBar(), s.mainBar}, nil, []widget.Component{s.qf, s.listBar})
	var chrome []widget.Component
	if s.win != nil {
		s.win.SetTitleBar(head)
	} else {
		chrome = append(chrome, head)
	}
	chrome = append(chrome, split)
	if s.status != nil {
		chrome = append(chrome, s.status)
	}
	root := widgets.NewColumn(chrome...).WithGap(0)
	root.AddFlex(split, 1)
	s.refreshAll()
	return wrapShortcutsReady(root, s.handleKey, s.maybeAskAddAccount)
}

func (s *session) menuBar() *widgets.MenuBar {
	layoutClassic := s.opts.Layout == LayoutClassic
	return widgets.NewMenuBar(
		widgets.NewMenu("M",
			widgets.Submenu("&View",
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
			),
			widgets.Sep(),
			s.notifyMenuItems(),
			widgets.Sep(),
			widgets.ItemAccel("Preferences", "Ctrl+,", s.openPrefs),
			widgets.Sep(),
			widgets.ItemAccel("&Quit", "Ctrl+Q", func() { s.app.Quit() }),
		),
	)
}

func (s *session) composeBar() *widgets.ToolBar {
	get := widgets.ToolIconBtn(style.IconDownload, "Fetch", s.getMessages)
	get.Tip = "Fetch new messages for this account"
	write := widgets.ToolIconBtn(style.IconPen, "Write", s.write)
	write.Tip = "Write a new message"
	return widgets.NewToolBar(get, write)
}

func (s *session) toolBar() *widgets.ToolBar {
	s.qfBtn = widgets.ToolIconBtn(style.IconSearch, "", s.toggleFilter)
	s.qfBtn.Tip = "Quick Filter"
	s.qfBtn.Toggle = true
	s.qfBtn.Down = s.opts.ShowFilter
	return widgets.NewToolBar(s.qfBtn)
}

func (s *session) notifyPrefs() NotifyPrefs {
	if s.cli == nil {
		return NotifyPrefs{Enabled: true, Desktop: true}
	}
	p, err := s.cli.NotifyPrefs()
	if err != nil {
		return NotifyPrefs{Enabled: true, Desktop: true}
	}
	return p
}

func (s *session) toggleNotify(mut func(*NotifyPrefs)) {
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
			s.toggleNotify(func(n *NotifyPrefs) { n.Enabled = !n.Enabled })
		}),
		widgets.CheckItem("VIP senders only", p.VIPOnly, func() {
			s.toggleNotify(func(n *NotifyPrefs) { n.VIPOnly = !n.VIPOnly })
		}),
		widgets.CheckItem("Desktop notifications", p.Desktop, func() {
			s.toggleNotify(func(n *NotifyPrefs) { n.Desktop = !n.Desktop })
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

func (s *session) messageMenu(from widget.Component, p paintengine2d.Point) {
	widgets.ShowContextMenu(from, p,
		widgets.Item("Reply", s.reply),
		widgets.Item("Forward", s.forward),
		widgets.Sep(),
		widgets.Item("Mark as Read", func() { s.setRead(true) }),
		widgets.Item("Mark as Unread", func() { s.setRead(false) }),
		widgets.Item("Star", s.toggleStar),
		widgets.Sep(),
		widgets.Item("Tag · Important", func() { s.toggleTag("Important") }),
		widgets.Item("Mute Thread", func() { s.muteThread(true) }),
		widgets.Item("Add sender to VIP", s.addVIP),
		widgets.Item("Archive", s.archive),
		widgets.Item("Junk", s.junk),
		widgets.ItemIcon(style.IconCut, "Delete", s.deleteSel),
		widgets.Sep(),
		widgets.ItemIcon(style.IconInfo, "View Source", s.viewSource),
	)
}

// now is the clock the thread list formats against. The seeded demo store
// has a frozen "today" so screenshots stay stable; a real account must use
// the wall clock, or every date reads as a weekday from September 2026.
func (s *session) now() time.Time {
	if s.backend == "memory" {
		return DemoNow
	}
	return time.Now()
}

func (s *session) cellText(row, col int) string {
	if row < 0 || row >= len(s.rows) {
		return ""
	}
	m := s.rows[row]
	switch col {
	case 0:
		if m.Starred {
			return "★"
		}
		return ""
	case 1:
		if m.HasAttach {
			return "📎"
		}
		return ""
	case 2:
		sub := m.Subject
		if strings.TrimSpace(sub) == "" {
			sub = "(no subject)"
		}
		if m.ThreadID != "" && s.muted[m.ThreadID] {
			sub = "🔇 " + sub
		}
		if !m.Read {
			return "● " + sub
		}
		return sub
	case 3:
		return m.Correspondent(s.kind)
	case 4:
		return formatDate(m.Date, s.now())
	default:
		return ""
	}
}

func (s *session) visible() []Message {
	all, err := s.loadVisible()
	if err != nil {
		s.mark(err.Error())
		return nil
	}
	return all
}

func (s *session) loadVisible() ([]Message, error) {
	f, ok, _ := s.cli.GetFolder(s.folder)
	kind := FolderInbox
	if ok {
		kind = f.Kind
	}
	s.kind = kind
	all, err := s.cli.ListMessages(s.folder, s.filter)
	if err != nil {
		return nil, err
	}
	if ids, err := s.cli.MutedThreads(); err == nil {
		s.muted = map[string]bool{}
		for _, id := range ids {
			s.muted[id] = true
		}
	}
	if s.hideMuted && len(s.muted) > 0 {
		var keep []Message
		for _, m := range all {
			if m.ThreadID == "" || !s.muted[m.ThreadID] {
				keep = append(keep, m)
			}
		}
		all = keep
	}
	if s.threaded {
		return groupThreaded(all, kind, s.sortCol, s.sortAsc), nil
	}
	sortMessages(all, s.sortCol, s.sortAsc, kind)
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
		s.selected = []MessageID{s.rows[0].ID}
	}
	if s.primaryIndex() < 0 && len(s.rows) > 0 {
		s.selected = []MessageID{s.rows[0].ID}
	}
	if s.table != nil {
		s.table.RowCount = len(s.rows)
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

	var outbox *Folder
	if vfs, err := s.cli.VirtualFolders(); err == nil {
		for _, f := range vfs {
			if f.ID == FolderOutbox {
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
		byParent := map[FolderID][]Folder{}
		folders, _ := s.cli.ListFolders(acct.ID)
		for _, f := range folders {
			byParent[f.Parent] = append(byParent[f.Parent], f)
		}
		for pid, kids := range byParent {
			byParent[pid] = orderFolderChildren(kids)
		}
		var addKids func(parent *widgets.TreeNode, pid FolderID)
		addKids = func(parent *widgets.TreeNode, pid FolderID) {
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
				n.Expanded = f.Kind == FolderArchive || len(byParent[f.ID]) > 0
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
	filtersNode.Data = AccountTags
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
			n := widgets.NewTreeNode(filterPinLabel(t.Name, on))
			n.Data = pin
			n.Bold = on
			n.Color = ParseHexColor(t.Color)
			filtersNode.Children = append(filtersNode.Children, n)
			continue
		}
		label := t.Name
		fid := TagFolderID(t.Name)
		nUnread := unread[fid]
		if nUnread > 0 {
			label = fmt.Sprintf("%s (%d)", t.Name, nUnread)
		}
		n := widgets.NewTreeNode(label)
		n.Data = fid
		n.Bold = nUnread > 0
		n.Color = ParseHexColor(t.Color)
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

func (s *session) rebuildOutboxPin(outbox *Folder) {
	if s.outboxTree == nil {
		return
	}
	if outbox == nil {
		if vfs, err := s.cli.VirtualFolders(); err == nil {
			for _, f := range vfs {
				if f.ID == FolderOutbox {
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
		if id, ok := n.Data.(FolderID); ok && id != "" {
			s.selectFolder(id)
			return
		}
		if acct, ok := n.Data.(string); ok && acct != "" {
			if acct == AccountTags || acct == AccountUnified || acct == AccountSmart || acct == AccountCategories {
				return
			}
			s.openAccountInbox(acct)
		}
	}
	tv.OnContext = func(n *widgets.TreeNode, p paintengine2d.Point) {
		if n != nil {
			if id, ok := n.Data.(FolderID); ok && id != "" {
				s.folder = id
				s.selected = nil
				s.refreshAll()
			}
		}
		widgets.ShowContextMenu(tv, p,
			widgets.Item("Fetch", s.getMessages),
			widgets.Item("New Folder…", s.newFolder),
			widgets.Item("Remove Account…", s.removeCurrentAccount),
			widgets.Sep(),
			widgets.Item("Empty Trash", s.emptyTrash),
			widgets.Item("Compact Folders", func() { s.mark("Compact Folders (stub)") }),
		)
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
	case FolderID:
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

func isOutboxFolder(f Folder) bool {
	return f.ID == FolderOutbox || strings.EqualFold(f.Name, "Outbox")
}

func folderSidebarRank(f Folder) int {
	if isOutboxFolder(f) {
		return 90
	}
	switch f.Kind {
	case FolderInbox:
		return 0
	case FolderDrafts:
		return 1
	case FolderSent:
		return 2
	case FolderArchive:
		return 3
	case FolderJunk:
		return 4
	case FolderTrash:
		return 5
	default:
		return 40
	}
}

func orderFolderChildren(folders []Folder) []Folder {
	out := append([]Folder(nil), folders...)
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
		s.selected = []MessageID{id}
	}
	s.syncViews()
	if !s.rows[i].Read {
		_ = s.cli.SetFlags(id, FlagPatch{Read: boolPtr(true)})
		s.rows[i].Read = true
		if s.table != nil {
			s.table.Invalidate()
		}
		if s.cards != nil {
			s.cards.Invalidate()
		}
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
	pos := make(map[MessageID]int, len(s.rows))
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
	live := make(map[MessageID]bool, len(s.rows))
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

func (s *session) hasSel(id MessageID) bool {
	for _, x := range s.selected {
		if x == id {
			return true
		}
	}
	return false
}

func (s *session) primary() (Message, bool) {
	if len(s.selected) == 0 {
		return Message{}, false
	}
	m, ok, err := s.cli.GetMessage(s.selected[len(s.selected)-1])
	if err != nil {
		return Message{}, false
	}
	return m, ok
}

func (s *session) loadPreview() {
	m, ok := s.primary()
	if !ok {
		if s.preview != nil {
			s.preview.SetText("")
		}
		if s.source != nil {
			s.source.SetText("")
		}
		if s.hdrSubj != nil {
			s.hdrSubj.SetText("No message selected")
			s.hdrFrom.SetText("")
			s.hdrTo.SetText("")
			s.hdrDate.SetText("")
			s.hdrExtra.SetText("")
		}
		s.attNames = nil
		s.syncAttachPane()
		return
	}
	if s.hdrSubj != nil {
		s.hdrSubj.SetText(m.Subject)
		s.hdrFrom.SetText("From: " + m.From)
		s.hdrTo.SetText("To: " + m.To)
		s.hdrDate.SetText("Date: " + m.Date.Format("Mon, 02 Jan 2006 15:04 MST"))
		extra := ""
		if len(m.Tags) > 0 {
			extra = "Tags: " + strings.Join(m.Tags, ", ")
		}
		if m.HasAttach && extra != "" {
			extra += "  ·  "
		}
		if m.HasAttach {
			extra += fmt.Sprintf("%d attachment(s)", len(m.Attachments))
		}
		s.hdrExtra.SetText(extra)
	}
	s.attNames = append([]string(nil), m.Attachments...)
	s.syncAttachPane()
	if s.preview != nil {
		s.preview.SetText(DisplayBody(m))
	}
	if s.source != nil {
		if raw, err := s.cli.GetSource(m.ID); err == nil {
			s.source.SetText(raw)
		} else {
			s.source.SetText("")
		}
	}
}

func (s *session) refreshStatus() {
	unread, _ := s.cli.UnreadTotal()
	if s.status == nil {
		return
	}
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
	s.status.Set(1, fmt.Sprintf("%s  ·  %d shown", name, len(s.rows)))
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

func (s *session) ids() []MessageID {
	if len(s.selected) > 0 {
		return append([]MessageID(nil), s.selected...)
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

func (s *session) reply() {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	cp := m.Clone()
	_, err := OpenCompose(s.app, s.cli, ComposeOptions{ReplyTo: &cp, OnChange: s.refreshAll})
	if err != nil {
		widgets.Warn(s.win.Content(), "Reply", err.Error(), nil)
	}
}

func (s *session) forward() {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	cp := m.Clone()
	_, err := OpenCompose(s.app, s.cli, ComposeOptions{Forward: &cp, OnChange: s.refreshAll})
	if err != nil {
		widgets.Warn(s.win.Content(), "Forward", err.Error(), nil)
	}
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
		if err != nil {
			widgets.Warn(s.win.Content(), "Fetch", err.Error(), nil)
			s.refreshAll()
			return
		}
		res := v.(SyncResult)
		s.refreshAll()
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
		tid = ThreadIDOf(m)
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
	v, err := s.cli.PutVIP(VIP{Address: extractAddr(m.From), Name: DisplayName(m.From)})
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
	if err := s.cli.SetSenderCategory(extractAddr(m.From), cat); err != nil {
		s.mark(err.Error())
		return
	}
	s.mark("Sender → " + cat)
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
	save := widgets.NewButton("Save", func() {
		sf, err := s.cli.PutSmartFolder(SmartFolder{
			Name:   strings.TrimSpace(name.Text),
			Filter: Filter{Query: strings.TrimSpace(query.Text), Unread: s.filter.Unread, Starred: s.filter.Starred, Attachment: s.filter.Attachment},
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
	cancel := widgets.NewButton("Cancel", func() { win.Close() })
	form := widgets.NewColumn(
		widgets.NewTitle("Saved search"),
		widgets.NewLabel("Name"), name,
		widgets.NewLabel("Query"), query,
		widgets.NewLabel("Unread / starred / attachment pins on the Tags tree are included."),
		widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept),
	).WithGap(8)
	win.SetContent(widgets.NewPad(12, form))
}

func (s *session) openSmartFolders() {
	s.newSmartFolder()
}

func (s *session) setRead(read bool) {
	for _, id := range s.ids() {
		_ = s.cli.SetFlags(id, FlagPatch{Read: boolPtr(read)})
		for i := range s.rows {
			if s.rows[i].ID == id {
				s.rows[i].Read = read
			}
		}
	}
	if s.table != nil {
		s.table.Invalidate()
	}
	if s.cards != nil {
		s.cards.Invalidate()
	}
	s.loadPreview()
	s.refreshStatus()
}

func (s *session) toggleStar() {
	m, ok := s.primary()
	if !ok {
		return
	}
	star := !m.Starred
	for _, id := range s.ids() {
		_ = s.cli.SetFlags(id, FlagPatch{Starred: boolPtr(star)})
		for i := range s.rows {
			if s.rows[i].ID == id {
				s.rows[i].Starred = star
			}
		}
	}
	if s.table != nil {
		s.table.Invalidate()
	}
	if s.cards != nil {
		s.cards.Invalidate()
	}
	s.loadPreview()
	s.refreshStatus()
}

func (s *session) toggleTag(tag string) {
	if len(s.ids()) == 0 {
		s.mark("No selection")
		return
	}
	for _, id := range s.ids() {
		m, ok, err := s.cli.GetMessage(id)
		if err != nil || !ok {
			continue
		}
		next := toggleTag(m.Tags, tag)
		_ = s.cli.SetFlags(id, FlagPatch{Tags: &next})
	}
	s.refreshAll()
	s.mark("Tag " + tag)
}

func (s *session) deleteSel() {
	ids := s.ids()
	if len(ids) == 0 {
		return
	}
	if err := s.cli.Delete(ids); err != nil {
		widgets.Warn(s.win.Content(), "Delete", err.Error(), nil)
		return
	}
	s.selected = nil
	s.refreshAll()
	s.mark("Deleted")
}

func (s *session) junk() {
	ids := s.ids()
	if len(ids) == 0 {
		return
	}
	junk, ok := specialFolderClient(s.cli, s.accountID(), FolderJunk)
	if !ok {
		s.mark("No Junk folder")
		return
	}
	if err := s.cli.Move(ids, junk.ID); err != nil {
		widgets.Warn(s.win.Content(), "Junk", err.Error(), nil)
		return
	}
	s.selected = nil
	s.refreshAll()
	s.mark("Moved to Junk")
}

func (s *session) archive() {
	ids := s.ids()
	if len(ids) == 0 {
		return
	}
	arch, ok := specialFolderClient(s.cli, s.accountID(), FolderArchive)
	if !ok {
		s.mark("No Archives folder")
		return
	}
	if err := s.cli.Move(ids, arch.ID); err != nil {
		widgets.Warn(s.win.Content(), "Archive", err.Error(), nil)
		return
	}
	s.selected = nil
	s.refreshAll()
	s.mark("Archived")
}

func (s *session) emptyTrash() {
	trash, ok := specialFolderClient(s.cli, s.accountID(), FolderTrash)
	if !ok {
		s.mark("No Trash")
		return
	}
	list, _ := s.cli.ListMessages(trash.ID, Filter{})
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
			ids := make([]MessageID, len(list))
			for i, m := range list {
				ids[i] = m.ID
			}
			_ = s.cli.Delete(ids)
			s.selected = nil
			s.refreshAll()
			s.mark("Trash emptied")
		})
}

func (s *session) newFolder() {
	acct := s.accountID()
	folders, _ := s.cli.ListFolders(acct)
	name := fmt.Sprintf("New Folder %d", len(folders)+1)
	f, err := s.cli.CreateFolder(acct, name, "")
	if err != nil {
		widgets.Warn(s.win.Content(), "New Folder", err.Error(), nil)
		return
	}
	s.central = false
	s.folder = f.ID
	s.selected = nil
	s.refreshAll()
	s.mark("Created " + name)
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

func (s *session) goKind(k FolderKind) {
	if f, ok := specialFolderClient(s.cli, s.accountID(), k); ok {
		s.central = false
		s.folder = f.ID
		s.selected = nil
		s.refreshAll()
	}
}

func (s *session) toggleFilter() {
	s.showFilter(!s.opts.ShowFilter)
	s.mark("Quick Filter")
}

func (s *session) showFilter(on bool) {
	s.opts.ShowFilter = on
	s.chromePrefs.ShowFilter = on
	saveChromePrefs(s.chromePrefs)
	if s.qf != nil {
		s.qf.SetVisible(on)
		if on && s.win != nil {
			s.win.RequestFocus(s.qf)
		}
	}
	if s.qfBtn != nil {
		s.qfBtn.Down = on
	}
	if s.win != nil {
		s.win.RequestLayout()
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

func filterPinLabel(name string, on bool) string {
	if on {
		return "✓ " + name
	}
	return name
}

func (s *session) about() {
	widgets.Info(s.win.Content(), "About Mail",
		"Mail — Thunderbird chrome on uitoolkit "+uitoolkit.Version+".\n"+
			"mailclientui talks JSON-RPC to mailclientd (Unix socket).\n"+
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
		if inbox, ok := specialFolderClient(s.cli, s.account, FolderInbox); ok {
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
		snip = snippetOf(m.Body)
	}
	var badges []widgets.CardBadge
	for _, name := range m.Tags {
		col := paintengine2d.RGB(0.5, 0.5, 0.55)
		if t, ok := tagByName(s.tags, name); ok {
			col = ParseHexColor(t.Color)
		}
		badges = append(badges, widgets.CardBadge{Label: name, Color: col})
	}
	return widgets.CardContent{
		Title:    m.Correspondent(s.kind),
		Subtitle: m.Subject,
		Meta:     formatDate(m.Date, s.now()),
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
		if t.System || IsSystemTag(t.Name) {
			continue
		}
		out = append(out, t.Name)
	}
	if len(out) == 0 {
		return demoTags()
	}
	return out
}

const attachActivateWindow = 400 * time.Millisecond

func (s *session) syncAttachPane() {
	has := len(s.attNames) > 0
	s.attachSel = -1
	s.attachClickI = -1
	s.attachClickT = time.Time{}
	s.rebuildAttachRows()
	if s.attachPane != nil {
		s.attachPane.SetVisible(has)
	}
	if s.attachBar != nil {
		s.attachBar.SetVisible(has)
	}
	if s.attachAll != nil {
		s.attachAll.SetEnabled(has)
	}
	s.paintAttachSelection()
	if s.win != nil {
		s.win.RequestLayout()
	}
}

func (s *session) rebuildAttachRows() {
	if s.attachRows == nil {
		return
	}
	s.attachRows.ClearChildren()
	s.attachHits = s.attachHits[:0]
	for i, name := range s.attNames {
		i, name := i, name
		hit := newAttachHit("📎  "+name, func() { s.selectAttachment(i) })
		hit.Drag = func() *widget.Drag { return s.dragAttachment(i) }
		open := widgets.NewButton("Open", func() {
			s.attachSel = i
			s.paintAttachSelection()
			s.openAttachment(i)
		})
		save := widgets.NewButton("Save As", func() {
			s.attachSel = i
			s.paintAttachSelection()
			s.saveAttachment(i)
		})
		row := widgets.NewRow(hit, open, save).WithGap(8)
		row.AddFlex(hit, 1)
		s.attachRows.Add(row)
		s.attachHits = append(s.attachHits, hit)
	}
}

func (s *session) paintAttachSelection() {
	for i, h := range s.attachHits {
		sel := i == s.attachSel
		if h.Selected != sel {
			h.Selected = sel
			h.Invalidate()
		}
	}
}

func (s *session) selectAttachment(i int) {
	now := time.Now()
	activate := i >= 0 && i == s.attachClickI && !s.attachClickT.IsZero() && now.Sub(s.attachClickT) < attachActivateWindow
	s.attachSel = i
	s.attachClickI = i
	s.attachClickT = now
	s.paintAttachSelection()
	if activate {
		s.openAttachment(i)
		return
	}
	if i >= 0 && i < len(s.attNames) {
		s.mark("Attachment: " + s.attNames[i])
	}
}

// attachmentParts is the subset of m.Parts that the attachment rows show,
// in the same order as m.Attachments.
func attachmentParts(m Message) []Part {
	var out []Part
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
func (s *session) attachPartID(m Message, i int) string {
	if i < 0 || i >= len(s.attNames) {
		return ""
	}
	want := s.attNames[i]
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
func (s *session) openAttachment(i int) {
	m, ok := s.primary()
	if !ok || i < 0 || i >= len(s.attNames) {
		return
	}
	pid := s.attachPartID(m, i)
	s.mark("Opening " + s.attNames[i] + "…")
	s.async(func() (any, error) {
		return s.cli.OpenPart(m.ID, pid)
	}, func(v any, err error) {
		if err != nil {
			s.mark("Open: " + err.Error())
			return
		}
		p := v.(PartData)
		if p.Path != "" {
			s.mark("Opened " + p.Path)
			return
		}
		s.mark("Attachment: " + s.attNames[i])
	})
}
func (s *session) saveAttachment(i int) {
	m, ok := s.primary()
	if !ok || i < 0 || i >= len(s.attNames) || s.win == nil {
		return
	}
	name := attachFileName(s.attNames[i])
	s.async(func() (any, error) {
		return s.attachmentBytes(m, i)
	}, func(v any, err error) {
		if err != nil {
			s.mark("Save As: " + err.Error())
			return
		}
		data := v.([]byte)
		widgets.ShowFileDialog(s.win.Content(), widgets.FileDialogOptions{
			Title:      "Save As",
			Mode:       widgets.FileSave,
			Path:       filepath.Join(os.TempDir(), name),
			OnNavigate: mailDirEntries,
			OnPick: func(path string) {
				if path == "" {
					return
				}
				if st, err := os.Stat(path); err == nil && st.IsDir() {
					path = filepath.Join(path, name)
				}
				if err := writeFileAtomic(path, data, 0o600); err != nil {
					s.mark("Save As: " + err.Error())
					return
				}
				s.mark("Saved " + path)
			},
		})
	})
}

// saveAllAttachments picks one folder (FileDialog path treated as a directory:
// an existing file uses its parent; a missing path with no extension is
// created) then writes every attachment for the current message there (0600).
func (s *session) saveAllAttachments() {
	m, ok := s.primary()
	if !ok || len(s.attNames) == 0 || s.win == nil {
		return
	}
	names := append([]string(nil), s.attNames...)
	s.mark("Collecting attachments…")
	s.async(func() (any, error) {
		items := make([]attachBlob, 0, len(names))
		for i := range names {
			data, err := s.attachmentBytes(m, i)
			if err != nil {
				return nil, err
			}
			items = append(items, attachBlob{Name: attachFileName(names[i]), Data: data})
		}
		return items, nil
	}, func(v any, err error) {
		if err != nil {
			s.mark("Save All: " + err.Error())
			return
		}
		items := v.([]attachBlob)
		widgets.ShowFileDialog(s.win.Content(), widgets.FileDialogOptions{
			Title:      "Save All",
			Mode:       widgets.FileSave,
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
					if err := writeFileAtomic(dest, it.Data, 0o600); err != nil {
						s.mark("Save All: " + err.Error())
						return
					}
				}
				s.mark(fmt.Sprintf("Saved %d attachment(s) to %s", len(items), dir))
			},
		})
	})
}
func (s *session) attachmentBytes(m Message, i int) ([]byte, error) {
	pid := s.attachPartID(m, i)
	if pid == "" {
		return nil, fmt.Errorf("no such attachment")
	}
	name := ""
	if i >= 0 && i < len(s.attNames) {
		name = s.attNames[i]
	}
	return s.partBytes(m.ID, pid, name)
}

// partBytes reads one part's bytes: what mailclientd hands back, or the
// file it points at for a part it has already spilled to disk.
//
// It takes the part's id rather than an index, so a caller that means to
// run it off the UI goroutine can settle which part it wants first — the
// index and the names beside it belong to the window (attach_drag.go).
func (s *session) partBytes(id MessageID, pid, name string) ([]byte, error) {
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
	f := h.Look().Font()
	sz := f.Measure(h.Text)
	sz.X += 12
	sz.Y += 6
	return c.Constrain(sz)
}

func (h *attachHit) Arrange(r paintengine2d.Rect) { h.SetBounds(r) }

func (h *attachHit) Paint(ctx *paintengine2d.Context) {
	h.Look().DrawListRow(ctx, h.LocalBounds(), style.RowState(h.Selected, h.Hovered()), h.Text)
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

func attachFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}

func uniqueFileName(name string, used map[string]int) string {
	name = attachFileName(name)
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

func firstAccountID(cli *Client) string {
	accts, err := cli.Accounts()
	if err != nil || len(accts) == 0 {
		return ""
	}
	return accts[0].ID
}

func (s *session) accounts() []Account {
	a, err := s.cli.Accounts()
	if err != nil {
		s.mark(err.Error())
		return nil
	}
	return a
}

func (s *session) backendLabel() string {
	if s.backend == "" {
		return "mailclientd"
	}
	return s.backend
}

func (s *session) showAccountCentral(acct string) {
	s.account = acct
	s.central = true
	if inbox, ok := specialFolderClient(s.cli, acct, FolderInbox); ok {
		s.folder = inbox.ID
	}
	s.selected = nil
	s.refreshAll()
	s.showCenter()
}

func syntheticAccount(id string) bool {
	return id == AccountUnified || id == AccountTags || id == AccountSmart || id == AccountCategories
}

func (s *session) ensureUsableFolder() {
	if s.folder != "" && !HiddenFromFolderTree(s.folder) {
		if _, ok, err := s.cli.GetFolder(s.folder); err == nil && ok {
			return
		}
	}
	if inbox, ok := specialFolderClient(s.cli, s.accountID(), FolderInbox); ok {
		s.folder = inbox.ID
		s.central = false
	}
}

func (s *session) selectFolder(id FolderID) {
	if HiddenFromFolderTree(id) {
		s.openAccountInbox(s.accountID())
		return
	}
	s.central = false
	s.folder = id
	if f, ok, _ := s.cli.GetFolder(id); ok && f.AccountID != "" && !syntheticAccount(f.AccountID) {
		s.account = f.AccountID
	}
	s.selected = nil
	s.refreshAll()
}

func (s *session) openAccountInbox(acct string) {
	if acct != "" {
		s.account = acct
	}
	s.central = false
	s.selected = nil
	if inbox, ok := specialFolderClient(s.cli, s.account, FolderInbox); ok {
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
	s.acctBody = widgets.NewLabel("Use Preferences or Account Central to add or remove stores. Open Inbox or pick a folder in the tree.")
	get := widgets.NewButton("Fetch", s.getMessages)
	write := widgets.NewButton("Write", s.write)
	prefs := widgets.NewButton("Account Settings", s.openPrefs)
	inbox := widgets.NewButton("Open Inbox", func() {
		s.goKind(FolderInbox)
	})
	remove := widgets.NewButton("Remove account…", s.removeCurrentAccount)
	return widgets.NewColumn(s.acctTitle, s.acctBody, widgets.NewSeparator(),
		widgets.NewRow(get, write, inbox, prefs, remove).WithGap(8),
	).WithGap(10).WithPad(16)
}

func (s *session) refreshAccount() {
	if s.acctTitle == nil {
		return
	}
	name, addr, proto := "Account", "", "IMAP"
	for _, a := range s.accounts() {
		if a.ID == s.account {
			name, addr, proto = a.Name, a.Address, ProtocolLabel(a)
			break
		}
	}
	unread, _ := s.cli.UnreadTotal()
	st, _ := s.cli.Status()
	s.acctTitle.SetText(name)
	s.acctBody.SetText(fmt.Sprintf("%s\n\nProtocol: %s\nIdentity for this window.\nUnread (all folders): %d\nDaemon: %s  ·  %s\nSocket: %s\n\nFetch, Write, or open Inbox — retrieve stays in mailclientd.",
		addr, proto, unread, st.Backend, s.backendLabel(), s.cli.Socket))
}

func (s *session) openPrefs() {
	if _, err := OpenPrefs(s.app, s.cli, s.afterAccountsChanged); err != nil {
		widgets.Warn(s.win.Content(), "Preferences", err.Error(), nil)
		return
	}
	s.mark("Preferences")
}

func (s *session) removeCurrentAccount() {
	id := s.account
	var acct Account
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
		if inbox, ok := specialFolderClient(s.cli, s.account, FolderInbox); ok {
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
	if e.Mods.Ctrl() && e.Key == platform.KeyF {
		s.showFilter(true)
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
		return false
	}
	if isHashDelete(e) || e.Key == platform.KeyDelete {
		s.deleteSel()
		return true
	}
	switch e.Key {
	case platform.KeyN:
		s.moveSel(1)
		return true
	case platform.KeyP:
		s.moveSel(-1)
		return true
	case platform.KeyR:
		s.reply()
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
	}
	return false
}

func specialFolderClient(cli *Client, accountID string, kind FolderKind) (Folder, bool) {
	folders, err := cli.ListFolders(accountID)
	if err != nil {
		return Folder{}, false
	}
	for _, f := range folders {
		if f.Kind == kind && f.Parent == "" {
			return f, true
		}
	}
	return Folder{}, false
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
	raw, err := s.cli.GetSource(m.ID)
	if err != nil {
		widgets.Warn(s.win.Content(), "Message Source", err.Error(), nil)
		return
	}
	if _, err := OpenMessageSource(s.app, m, raw); err != nil {
		widgets.Warn(s.win.Content(), "Message Source", err.Error(), nil)
		return
	}
	s.mark("Message Source")
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
