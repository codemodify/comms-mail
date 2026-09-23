package mail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestMailAppPaints(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	PrepareShot(w, -1)
	a.PumpOnce()

	var tables, trees, areas, bars, menus int
	widget.Walk(mailTree(w), func(c widget.Component) {
		switch c.(type) {
		case *widgets.TableView:
			tables++
		case *widgets.TreeView:
			trees++
		case *widgets.TextArea:
			areas++
		case *widgets.StatusBar:
			bars++
		case *widgets.MenuBar:
			menus++
		}
	})
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TableView); ok && tv.CellText != nil && tv.RowCount > 0 {
			subj := tv.CellText(0, 2)
			t.Logf("row0 subject %q selected=%d rows=%d", subj, tv.Selected, tv.RowCount)
			if subj == "" {
				t.Error("empty first subject")
			}
		}
	})
	if tables < 1 || trees < 1 || areas < 1 || menus < 1 {
		t.Fatalf("chrome table=%d tree=%d area=%d status=%d menu=%d", tables, trees, areas, bars, menus)
	}
	if bars != 0 {
		t.Fatalf("status bar should be hidden, got %d", bars)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "mail.png")
	if err := w.WritePNG(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < 2000 {
		t.Fatalf("png too small: %d", st.Size())
	}
	w.Close()
}

func TestMailHiDPIResizeStable(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Scale: 2, Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()

	var table *widgets.TableView
	var tree *widgets.TreeView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TableView); ok && table == nil {
			table = tv
		}
		if tr, ok := c.(*widgets.TreeView); ok && tree == nil {
			tree = tr
		}
	})
	if table == nil || tree == nil {
		t.Fatal("missing table/tree")
	}
	font0 := table.Look().Font().Size
	row0 := table.Look().Metrics().RowH
	fit := style.FittedRowHeight(table.Look(), table.RowHeight)
	if font0 < 20 || fit < table.Look().Font().Height() {
		t.Fatalf("hidpi font %v row metric %v fitted %v", font0, row0, fit)
	}

	w.Inject(platform.Event{Kind: platform.EventResize, Width: 900, Height: 620})
	a.PumpOnce()
	w.Inject(platform.Event{Kind: platform.EventResize, Width: 1400, Height: 900})
	a.PumpOnce()
	w.Inject(platform.Event{Kind: platform.EventResize, Width: 1100, Height: 700})
	a.PumpOnce()

	if table.Look().Font().Size != font0 {
		t.Fatalf("font exploded %v -> %v", font0, table.Look().Font().Size)
	}
	if table.Look().Metrics().RowH != row0 {
		t.Fatalf("row metric %v -> %v", row0, table.Look().Metrics().RowH)
	}
	if tree.Look().Font().Size != font0 {
		t.Fatalf("tree font exploded %v", tree.Look().Font().Size)
	}
	w.Close()
}

func TestMailSubjectColumnFillsThreadPane(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	var table *widgets.TableView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TableView); ok && len(tv.Columns) >= 5 && table == nil {
			table = tv
		}
	})
	if table == nil {
		t.Fatal("thread table")
	}
	ws := table.ColumnWidths()
	if len(ws) != 5 {
		t.Fatalf("cols %v", ws)
	}
	subj := ws[2]
	if subj < 200 {
		t.Fatalf("topic column too narrow at 1280: %v (all %v) table=%v", subj, ws, table.LocalBounds().Dx())
	}
	sum := float32(0)
	for _, x := range ws {
		sum += x
	}
	// Columns fill the rows area: the table minus the scrollbar gutter, so
	// the When column never runs under the bar.
	if d := sum - table.RowsWidth(); d > 1 || d < -1 {
		t.Fatalf("columns %v sum %v != rows %v (table %v)", ws, sum, table.RowsWidth(), table.LocalBounds().Dx())
	}
	if table.MaxOffset() > 0 && table.RowsWidth() >= table.LocalBounds().Dx() {
		t.Fatalf("rows %v should leave a scrollbar gutter in table %v", table.RowsWidth(), table.LocalBounds().Dx())
	}
	w.Inject(platform.Event{Kind: platform.EventResize, Width: 1100, Height: 720})
	a.PumpOnce()
	ws = table.ColumnWidths()
	if ws[2] < 160 {
		t.Fatalf("topic after resize %v (all %v)", ws[2], ws)
	}
	w.Close()
}

func TestComposeAppPaints(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true})
	w, err := OpenCompose(a, cli, ComposeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	a.PumpOnce()
	var body *widgets.TextArea
	widget.Walk(mailTree(w), func(c widget.Component) {
		if ta, ok := c.(*widgets.TextArea); ok && !ta.ReadOnly {
			body = ta
		}
	})
	if body == nil {
		t.Fatal("compose body should be an editable TextArea")
	}
	if body.ReadOnly {
		t.Fatal("compose must stay editable")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.png")
	if err := w.WritePNG(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < 1000 {
		t.Fatalf("compose png too small: %d", st.Size())
	}
	w.Close()
}

func TestMailPreviewReadOnlyAndListClamp(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	PrepareShot(w, -1)
	a.PumpOnce()

	var table *widgets.TableView
	var views []*widgets.TextArea
	var splits []*widgets.Splitter
	widget.Walk(mailTree(w), func(c widget.Component) {
		switch t := c.(type) {
		case *widgets.TableView:
			if len(t.Columns) >= 5 && table == nil {
				table = t
			}
		case *widgets.TextArea:
			views = append(views, t)
		case *widgets.Splitter:
			splits = append(splits, t)
		}
	})
	if table == nil {
		t.Fatal("thread table")
	}
	if len(views) < 1 {
		t.Fatal("message TextView")
	}
	for _, ta := range views {
		if !ta.ReadOnly {
			t.Fatalf("mail view TextArea must be ReadOnly, got editable %q", ta.Placeholder)
		}
		before := ta.Text
		if ta.TextInput('Z') {
			t.Fatal("preview accepted typing")
		}
		if ta.Text != before {
			t.Fatalf("preview mutated %q -> %q", before, ta.Text)
		}
	}

	for i := 0; i < 80; i++ {
		table.MouseWheel(widget.MouseEvent{Scroll: paintengine2d.Pt(0, 40)})
	}
	if table.OffsetY > table.MaxOffset()+0.5 {
		t.Fatalf("thread list scrolled past end offset=%v max=%v", table.OffsetY, table.MaxOffset())
	}
	if table.MaxOffset() > 0 && table.OffsetY != table.MaxOffset() {
		t.Fatalf("wheel spam should pin to max, offset=%v max=%v", table.OffsetY, table.MaxOffset())
	}

	if len(splits) == 0 {
		t.Fatal("expected splitters")
	}
	for _, s := range splits {
		a, b := s.PaneA(), s.PaneB()
		if a.Overlaps(b) {
			t.Fatalf("splitter panes overlap A=%+v B=%+v", a, b)
		}
		pa, pb := s.PaneA(), s.PaneB()
		var sash, mid paintengine2d.Point
		if s.Vertical {
			sash = paintengine2d.Pt((pa.Max.X+pb.Min.X)*0.5, 8)
			mid = paintengine2d.Pt(s.LocalBounds().Dx()*0.75, 8)
		} else {
			sash = paintengine2d.Pt(8, (pa.Max.Y+pb.Min.Y)*0.5)
			mid = paintengine2d.Pt(8, s.LocalBounds().Dy()*0.75)
		}
		s.MousePress(widget.MouseEvent{Pos: sash, Button: platform.ButtonLeft})
		s.MouseMove(widget.MouseEvent{Pos: mid, Button: platform.ButtonLeft})
		s.MouseRelease(widget.MouseEvent{Pos: mid})
		a, b = s.PaneA(), s.PaneB()
		if a.Overlaps(b) {
			t.Fatalf("after drag overlap A=%+v B=%+v", a, b)
		}
	}
	w.Close()
}

func TestWriteScreenshotsDistinct(t *testing.T) {
	dir := t.TempDir()
	if err := WriteScreenshots(dir); err != nil {
		t.Fatal(err)
	}
	seen := map[int64]string{}
	for _, name := range ScreenshotNames {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() < 200 {
			t.Fatalf("%s too small: %d", name, st.Size())
		}
		if other, ok := seen[st.Size()]; ok {
			t.Fatalf("%s same size as %s (%d) — likely duplicate", name, other, st.Size())
		}
		seen[st.Size()] = name
	}
}

func TestFolderTreeOmitsVirtualSections(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()

	var tree *widgets.TreeView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tr, ok := c.(*widgets.TreeView); ok && tree == nil {
			tree = tr
		}
	})
	if tree == nil {
		t.Fatal("missing tree")
	}
	var walk func([]*widgets.TreeNode)
	walk = func(nodes []*widgets.TreeNode) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if n.Label == "Unified Folders" || n.Label == "Smart Folders" || n.Label == "Categories" {
				t.Fatalf("removed section still in tree: %q", n.Label)
			}
			walk(n.Children)
		}
	}
	walk(tree.Roots)
	assertOutboxPinnedBottom(t, mailTree(w))

	banned := []string{"Unified Inbox", "New Smart Folder", "Smart Folders", "Move sender to Primary"}
	widget.Walk(mailTree(w), func(c widget.Component) {
		mb, ok := c.(*widgets.MenuBar)
		if !ok {
			return
		}
		for _, m := range mb.Menus() {
			for _, it := range m.Items {
				for _, bad := range banned {
					if it.Text == bad {
						t.Fatalf("menu still has %q", bad)
					}
				}
			}
		}
	})
	w.Close()
}

func TestSelectAccountKeepsInboxList(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()

	var tree *widgets.TreeView
	var table *widgets.TableView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tr, ok := c.(*widgets.TreeView); ok && tree == nil {
			tree = tr
		}
		if tv, ok := c.(*widgets.TableView); ok && table == nil {
			table = tv
		}
	})
	if tree == nil || table == nil {
		t.Fatal("missing table/tree")
	}
	if table.RowCount < 1 {
		t.Fatal("inbox should show messages")
	}
	var acct *widgets.TreeNode
	for _, n := range tree.Roots {
		if n == nil {
			continue
		}
		if id, ok := n.Data.(string); ok && id != "" && id != AccountTags {
			acct = n
			break
		}
	}
	if acct == nil {
		t.Fatal("no account node")
	}
	before := table.RowCount
	if tree.OnSelect != nil {
		tree.OnSelect(acct)
	}
	a.PumpOnce()
	if table.RowCount == 0 {
		t.Fatalf("selecting account wiped the list (had %d)", before)
	}
	if !table.Visible() {
		t.Fatal("thread list hidden after account select")
	}
	w.Close()
}

func TestClickUnreadKeepsList(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()

	var table *widgets.TableView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TableView); ok && table == nil {
			table = tv
		}
	})
	if table == nil || table.RowCount < 1 {
		t.Fatal("thread table")
	}
	unread := -1
	for i := 0; i < table.RowCount; i++ {
		if table.CellBold != nil && table.CellBold(i, 2) {
			unread = i
			break
		}
	}
	if unread < 0 {
		unread = 0
	}
	before := table.RowCount
	if table.OnSelect != nil {
		table.OnSelect(unread)
	}
	a.PumpOnce()
	if table.RowCount == 0 {
		t.Fatal("clicking a message wiped the list")
	}
	if table.RowCount != before {
		t.Fatalf("row count %d -> %d after click", before, table.RowCount)
	}
	w.Close()
}

func TestHiddenFromFolderTree(t *testing.T) {
	if !HiddenFromFolderTree(FolderUnifiedInbox) || !HiddenFromFolderTree(FolderUnifiedUnread) {
		t.Fatal("unified")
	}
	if !HiddenFromFolderTree(FolderCatPrimary) || !HiddenFromFolderTree(FolderID("smart/sf-invoices")) {
		t.Fatal("cat/smart")
	}
	if !HiddenFromFolderTree(FolderVIP) {
		t.Fatal("VIP folder should be hidden from the tree")
	}
	if HiddenFromFolderTree(FolderOutbox) || HiddenFromFolderTree(TagFolderID("Work")) {
		t.Fatal("outbox/tag should stay")
	}
}

func TestMailChromeHidesStatusBarAndVIPFolder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	var bars, titles int
	var vipNode bool
	var qf widget.Component
	var split *widgets.Splitter
	widget.Walk(mailTree(w), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.StatusBar:
			bars++
		case *widgets.TitleBar:
			titles++
		case *widgets.Splitter:
			if split == nil {
				split = v
			}
		case *widgets.TreeView:
			var walk func([]*widgets.TreeNode)
			walk = func(nodes []*widgets.TreeNode) {
				for _, n := range nodes {
					if n == nil {
						continue
					}
					if id, ok := n.Data.(FolderID); ok && id == FolderVIP {
						vipNode = true
					}
					if n.Label == "VIP" || strings.HasPrefix(n.Label, "VIP (") {
						vipNode = true
					}
					walk(n.Children)
				}
			}
			walk(v.Roots)
		}
	})
	if bars != 0 {
		t.Fatalf("status bar widgets=%d", bars)
	}
	if titles != 0 {
		t.Fatalf("path/subtitle TitleBar should be gone, got %d", titles)
	}
	if vipNode {
		t.Fatal("VIP folder still in the sidebar tree")
	}
	qf = findQuickFilter(mailTree(w))
	if qf == nil {
		t.Fatal("quick filter field missing")
	}
	if qf.Visible() {
		t.Fatal("quick filter field should stay hidden until the Filter icon or Ctrl+F")
	}
	assertNoUnreadFolderFooter(t, mailTree(w))
	if split == nil || widget.Contains(split, qf) {
		t.Fatal("quick filter should sit on the M chrome row, not inside the splitter")
	}
	assertMailToolChrome(t, mailTree(w), qf)
	w.Close()
}

func TestMailToolBarChrome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	qf := findQuickFilter(mailTree(w))
	if qf == nil {
		t.Fatal("quick filter field missing")
	}
	assertMailToolChrome(t, mailTree(w), qf)
	w.Close()
}

func TestMailFilterIconTogglesField(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	qf := findQuickFilter(mailTree(w))
	var btn *widgets.ToolItem
	widget.Walk(mailTree(w), func(c widget.Component) {
		if v, ok := c.(*widgets.ToolBar); ok {
			if it := filterIconItem(v); it != nil {
				btn = it
			}
		}
	})
	if qf == nil || btn == nil || btn.OnClick == nil {
		t.Fatal("filter icon / field")
	}
	if qf.Visible() {
		t.Fatal("field should start hidden")
	}
	qf.SetText("lunch")
	btn.OnClick()
	a.PumpOnce()
	if !qf.Visible() {
		t.Fatal("Filter icon should reveal the field")
	}
	if qf.Text != "lunch" {
		t.Fatalf("should keep filter text, got %q", qf.Text)
	}
	if qf.Bounds().Dx() <= 100 {
		t.Fatalf("opened field crushed: width=%v", qf.Bounds().Dx())
	}
	if qf.OnEscape == nil {
		t.Fatal("Escape should hide the field")
	}
	qf.OnEscape()
	a.PumpOnce()
	if qf.Visible() {
		t.Fatal("Escape should hide the field")
	}
	if qf.Text != "lunch" {
		t.Fatalf("hide should keep text, got %q", qf.Text)
	}
	w.Close()
}

func TestMailShowFilterPrefHonored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveChromePrefs(ChromePrefs{ShowFilter: true})
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	qf := findQuickFilter(mailTree(w))
	if qf == nil || !qf.Visible() {
		t.Fatal("saved showFilter:true should open the Quick Filter field")
	}
	w.Close()
}

func TestMailCollapsedTagsDoesNotStealOutbox(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	var tree *widgets.TreeView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if v, ok := c.(*widgets.TreeView); ok && tree == nil {
			tree = v
		}
	})
	if tree == nil {
		t.Fatal("folder tree")
	}
	filters := findTreeLabel(tree.Roots, "Tags")
	if filters == nil {
		t.Fatal("Tags")
	}
	tree.Toggle(filters)
	if filters.Expanded {
		t.Fatal("Tags should collapse")
	}
	_, pin := mailSidebarTrees(mailTree(w))
	if pin == nil || pin.OnSelect == nil || len(pin.Roots) == 0 {
		t.Fatal("Outbox pin")
	}
	pin.OnSelect(pin.Roots[0])
	a.PumpOnce()
	filters = findTreeLabel(tree.Roots, "Tags")
	if filters == nil {
		t.Fatal("Tags after rebuild")
	}
	if filters.Expanded {
		t.Fatal("Outbox click re-opened Tags (rebuild lost collapse, or hit-test stole the row)")
	}
	_, pin = mailSidebarTrees(mailTree(w))
	if pin == nil || pin.Selected == nil || !strings.HasPrefix(pin.Selected.Label, "Outbox") {
		label := ""
		if pin != nil && pin.Selected != nil {
			label = pin.Selected.Label
		}
		t.Fatalf("selected %q want Outbox", label)
	}
	w.Close()
}

func findTreeLabel(nodes []*widgets.TreeNode, label string) *widgets.TreeNode {
	var found *widgets.TreeNode
	var walk func([]*widgets.TreeNode)
	walk = func(ns []*widgets.TreeNode) {
		for _, n := range ns {
			if n == nil || found != nil {
				continue
			}
			if n.Label == label {
				found = n
				return
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return found
}

func visibleTreeRows(roots []*widgets.TreeNode) []*widgets.TreeNode {
	var out []*widgets.TreeNode
	var walk func([]*widgets.TreeNode)
	walk = func(nodes []*widgets.TreeNode) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			out = append(out, n)
			if n.Expanded {
				walk(n.Children)
			}
		}
	}
	walk(roots)
	return out
}

func assertMailToolChrome(t *testing.T, root widget.Component, qf widget.Component) {
	t.Helper()
	var split *widgets.Splitter
	var table *widgets.TableView
	var leftBar, rightBar *widgets.ToolBar
	var menuBar *widgets.MenuBar
	var tree *widgets.TreeView
	removed := []string{"Tag", "Archive", "Junk", "Cards", "Classic"}
	widget.Walk(root, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Splitter:
			if split == nil {
				split = v
			}
		case *widgets.MenuBar:
			if menuBar == nil {
				menuBar = v
			}
		case *widgets.TableView:
			if table == nil && len(v.Columns) >= 5 {
				table = v
			}
		case *widgets.TreeView:
			if tree == nil {
				tree = v
			}
		case *widgets.ToolBar:
			texts := toolTexts(v)
			for _, name := range removed {
				if texts[name] {
					t.Fatalf("%q still on a toolbar: %v", name, texts)
				}
			}
			if texts["Fetch"] || texts["Write"] {
				leftBar = v
			} else if texts["Delete"] || filterIconItem(v) != nil {
				rightBar = v
			}
			for _, name := range []string{"Unread", "Starred", "Attachment", "From", "To", "Subject", "Body"} {
				if texts[name] {
					t.Fatalf("filter pin %q still on a toolbar", name)
				}
			}
		case *widgets.ComboBox:
			t.Fatal("Tags ComboBox should be gone from Mail chrome")
		}
	})
	if leftBar == nil {
		t.Fatal("left Fetch/Write toolbar")
	}
	if rightBar == nil {
		t.Fatal("right Filter toolbar")
	}
	if filterIconItem(rightBar) == nil {
		t.Fatal("icon-only Filter button missing on the right toolbar")
	}
	if split == nil || table == nil {
		t.Fatal("splitter/table")
	}
	left := toolTexts(leftBar)
	right := toolTexts(rightBar)
	for _, name := range []string{"Fetch", "Write"} {
		if !left[name] {
			t.Fatalf("left toolbar missing %s: %v", name, left)
		}
		if right[name] {
			t.Fatalf("right toolbar still has %s: %v", name, right)
		}
	}
	if left["Get Messages"] || right["Get Messages"] {
		t.Fatal("Get Messages should be renamed Fetch")
	}
	if right["Delete"] || left["Delete"] {
		t.Fatal("Delete should not sit on either main toolbar")
	}
	var fetch, write *widgets.ToolItem
	for _, it := range leftBar.Items() {
		if it == nil {
			continue
		}
		switch it.Text {
		case "Fetch":
			fetch = it
		case "Write":
			write = it
		}
	}
	if fetch == nil || fetch.Icon != style.IconDownload {
		t.Fatalf("Fetch should use IconDownload, got %+v", fetch)
	}
	if write == nil || write.Icon != style.IconPen {
		t.Fatalf("Write should use IconPen, got %+v", write)
	}
	assertFetchWriteIconsPaint(t, leftBar)
	for _, name := range []string{"Reply", "Forward", "Quick Filter"} {
		if left[name] || right[name] {
			t.Fatalf("toolbar still has %s: left=%v right=%v", name, left, right)
		}
	}
	assertMenubarChromeRow(t, root, menuBar, leftBar, rightBar)
	if widget.Contains(split, leftBar) || widget.Contains(split, rightBar) {
		t.Fatal("toolbars should sit on the M row, not in the thread pane")
	}
	if widget.Contains(split, qf) {
		t.Fatal("quick filter should sit on the M chrome row, not in the thread pane")
	}
	if !filterAfterListActions(root, qf) {
		t.Fatal("Quick Filter field should share the M chrome row with the right toolbar")
	}
	if separateFilterRow(root) {
		t.Fatal("separate Quick Filter row still under the main toolbar")
	}
	if widget.Contains(split, table) {
		to := widget.DeviceOrigin(table)
		mo := widget.DeviceOrigin(rightBar)
		if to.Y+0.5 < mo.Y+rightBar.Bounds().Dy() {
			t.Fatalf("thread list should sit below the M toolbar row: table=%v bar=%v", to, mo)
		}
	}
	if qf.Visible() {
		t.Fatal("quick filter field should be hidden until the Filter icon opens it")
	}
	assertTagsTree(t, tree)
}

func findQuickFilter(root widget.Component) *widgets.TextField {
	var qf *widgets.TextField
	walkAll(root, func(c widget.Component) {
		v, ok := c.(*widgets.TextField)
		if ok && qf == nil && strings.Contains(v.Placeholder, "Quick Filter") {
			qf = v
		}
	})
	return qf
}

func walkAll(c widget.Component, fn func(widget.Component)) {
	if c == nil {
		return
	}
	fn(c)
	for _, ch := range c.Children() {
		walkAll(ch, fn)
	}
}

func filterIconItem(bar *widgets.ToolBar) *widgets.ToolItem {
	if bar == nil {
		return nil
	}
	for _, it := range bar.Items() {
		if it != nil && it.Text == "" && it.Icon == style.IconSearch {
			return it
		}
	}
	return nil
}

func toolTexts(bar *widgets.ToolBar) map[string]bool {
	out := map[string]bool{}
	if bar == nil {
		return out
	}
	for _, it := range bar.Items() {
		if it != nil && it.Text != "" {
			out[it.Text] = true
		}
	}
	return out
}

func filterAfterListActions(root, qf widget.Component) bool {
	var row *widgets.FlexBox
	walkAll(root, func(c widget.Component) {
		f, ok := c.(*widgets.FlexBox)
		if !ok || row != nil {
			return
		}
		var hasList, hasQF bool
		walkAll(f, func(ch widget.Component) {
			if bar, ok := ch.(*widgets.ToolBar); ok {
				texts := toolTexts(bar)
				if filterIconItem(bar) != nil && !texts["Fetch"] && !texts["Write"] {
					hasList = true
				}
			}
			if ch == qf {
				hasQF = true
			}
		})
		if hasList && hasQF {
			row = f
		}
	})
	return row != nil
}

func assertTagsTree(t *testing.T, tree *widgets.TreeView) {
	t.Helper()
	if tree == nil {
		t.Fatal("folder tree")
	}
	var filters *widgets.TreeNode
	for _, n := range tree.Roots {
		if n == nil {
			continue
		}
		if n.Label == "Filters" {
			t.Fatal("sidebar Filters node should be renamed Tags")
		}
		if n.Label == "Tags" || n.Data == AccountTags {
			filters = n
		}
	}
	if filters == nil || filters.Label != "Tags" {
		t.Fatal("Tags missing from the tree")
	}
	want := tagNames(DefaultTags())
	ban := []string{"From", "To", "Subject", "Body"}
	got := make([]string, 0, len(filters.Children))
	seen := map[string]bool{}
	for _, n := range filters.Children {
		if n == nil {
			continue
		}
		label := strings.TrimPrefix(n.Label, "✓ ")
		if i := strings.Index(label, " ("); i > 0 {
			label = label[:i]
		}
		got = append(got, label)
		seen[label] = true
	}
	for _, name := range want {
		if !seen[name] {
			t.Fatalf("Tags tree missing %q (got %v)", name, got)
		}
	}
	for _, name := range ban {
		if seen[name] {
			t.Fatalf("Tags tree still has %q", name)
		}
	}
}

func assertFetchWriteIconsPaint(t *testing.T, bar *widgets.ToolBar) {
	t.Helper()
	if bar == nil {
		t.Fatal("left toolbar")
	}
	fetch := widgets.NewToolBar(widgets.ToolIconBtn(style.IconDownload, "Fetch", nil))
	write := widgets.NewToolBar(widgets.ToolIconBtn(style.IconPen, "Write", nil))
	if bar.Host() != nil {
		fetch.SetHost(bar.Host())
		write.SetHost(bar.Host())
	}
	fetchImg := rasterToolBar(fetch)
	writeImg := rasterToolBar(write)
	fetchInk := toolIconContrast(fetch, fetchImg)
	writeInk := toolIconContrast(write, writeImg)
	if fetchInk < 8 {
		t.Fatalf("Fetch download icon has no ink (%d)", fetchInk)
	}
	if writeInk < 8 {
		t.Fatalf("Write pen icon has no ink (%d)", writeInk)
	}
	if toolIconMaskDiff(fetch, fetchImg, write, writeImg) < 8 {
		t.Fatal("Fetch download and Write pen glyphs should differ")
	}
}

func rasterToolBar(tb *widgets.ToolBar) *paintengine2d.Image {
	sz := tb.Measure(layout.Unbounded())
	h := sz.Y
	if h < 36 {
		h = 36
	}
	tb.Arrange(paintengine2d.XYWH(0, 0, sz.X, h))
	img := paintengine2d.NewImage(int(sz.X)+4, int(h)+4)
	tb.Paint(paintengine2d.NewContext(img))
	return img
}

func toolIconBox(tb *widgets.ToolBar) (x0, y0, x1, y1 int) {
	r := tb.ItemRect(0)
	pad, side, _ := style.ToolButtonChromeFor(tb.Look(), r.Dy())
	x0 = int(r.Min.X + pad)
	y0 = int(r.Min.Y + (r.Dy()-side)*0.5)
	return x0, y0, x0 + int(side), y0 + int(side)
}

func toolIconContrast(tb *widgets.ToolBar, img *paintengine2d.Image) int {
	br, bg, bb := styleRGB8(tb.Look().Palette().SurfaceAlt)
	x0, y0, x1, y1 := toolIconBox(tb)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Width || y >= img.Height {
				continue
			}
			r, g, b, _ := img.PremulAt(x, y)
			if chanDelta(r, br)+chanDelta(g, bg)+chanDelta(b, bb) > 80 {
				n++
			}
		}
	}
	return n
}

func toolIconMaskDiff(a *widgets.ToolBar, ai *paintengine2d.Image, b *widgets.ToolBar, bi *paintengine2d.Image) int {
	ax0, ay0, ax1, ay1 := toolIconBox(a)
	bx0, by0, _, _ := toolIconBox(b)
	w, h := ax1-ax0, ay1-ay0
	diff := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ar, ag, ab, _ := ai.PremulAt(ax0+x, ay0+y)
			br, bg, bb, _ := bi.PremulAt(bx0+x, by0+y)
			if chanDelta(ar, br)+chanDelta(ag, bg)+chanDelta(ab, bb) > 40 {
				diff++
			}
		}
	}
	return diff
}

func chanDelta(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func styleRGB8(c paintengine2d.Color) (r, g, b uint8) {
	return uint8(c.R*255 + 0.5), uint8(c.G*255 + 0.5), uint8(c.B*255 + 0.5)
}

func assertMenubarChromeRow(t *testing.T, root widget.Component, mb *widgets.MenuBar, left, right *widgets.ToolBar) {
	t.Helper()
	if mb == nil || left == nil || right == nil {
		t.Fatal("menu / left / right toolbar")
	}
	menu := widget.DeviceBounds(mb)
	compose := widget.DeviceBounds(left)
	tool := widget.DeviceBounds(right)
	if menu.Max.Y < compose.Min.Y+1 || compose.Max.Y < menu.Min.Y+1 {
		t.Fatalf("M and Fetch/Write toolbar must share one row: menu=%v left=%v", menu, compose)
	}
	if menu.Max.Y < tool.Min.Y+1 || tool.Max.Y < menu.Min.Y+1 {
		t.Fatalf("M and right toolbar must share one row: menu=%v right=%v", menu, tool)
	}
	if menu.Min.X > 16 {
		t.Fatalf("M should sit on the left: %+v", menu)
	}
	if compose.Min.X < menu.Max.X {
		t.Fatalf("Fetch/Write should sit after M: menu=%v left=%v", menu, compose)
	}
	if compose.Min.X > menu.Max.X+24 {
		t.Fatalf("Fetch/Write should sit immediately after M: menu=%v left=%v", menu, compose)
	}
	if tool.Min.X < compose.Max.X+8 {
		t.Fatalf("right toolbar should sit after Fetch/Write: left=%v right=%v", compose, tool)
	}
	rootBox := widget.DeviceBounds(root)
	if tool.Max.X < rootBox.Max.X-24 {
		t.Fatalf("right toolbar should be right-aligned: tool=%v root=%v", tool, rootBox)
	}
	row := parentRow(mb)
	if row == nil || row != parentRow(left) || row != parentRow(right) {
		t.Fatal("M, Fetch/Write, and the right toolbar should share one horizontal row")
	}
}

func parentRow(c widget.Component) *widgets.FlexBox {
	for p := c.Parent(); p != nil; p = p.Parent() {
		f, ok := p.(*widgets.FlexBox)
		if !ok {
			continue
		}
		if f.Spec.Axis == layout.AxisHorizontal {
			return f
		}
	}
	return nil
}

func containsMenuBar(c widget.Component) bool {
	found := false
	widget.Walk(c, func(n widget.Component) {
		if _, ok := n.(*widgets.MenuBar); ok {
			found = true
		}
	})
	return found
}

func separateFilterRow(root widget.Component) bool {
	// The column holding the thread pane: only the M chrome row may sit
	// above the splitter. The chrome row may also be the window's title bar,
	// above the column altogether.
	var col *widgets.FlexBox
	widget.Walk(root, func(c widget.Component) {
		f, ok := c.(*widgets.FlexBox)
		if !ok || col != nil {
			return
		}
		for _, ch := range f.Children() {
			if _, ok := ch.(*widgets.Splitter); ok {
				col = f
			}
		}
	})
	if col == nil {
		return true
	}
	for _, ch := range col.Children() {
		if _, ok := ch.(*widgets.Splitter); ok {
			return false
		}
		if !containsMenuBar(ch) {
			return true
		}
	}
	return true
}

func TestMailChromeHasNoSidebarAccountPicker(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()

	var split *widgets.Splitter
	var tree *widgets.TreeView
	var prefs bool
	widget.Walk(mailTree(w), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Splitter:
			if split == nil {
				split = v
			}
		case *widgets.TreeView:
			if tree == nil {
				tree = v
			}
		case *widgets.Label:
			if v.Title && (v.Text == "Folders" || v.Text == "Account" || v.Text == "Tags") {
				t.Fatalf("sidebar section header still present: %q", v.Text)
			}
		case *widgets.MenuBar:
			for _, m := range v.Menus() {
				for _, it := range m.Items {
					if it == nil {
						continue
					}
					label, _, _ := widgets.ParseMnemonic(it.Text)
					if label == "Preferences" && it.OnClick != nil {
						prefs = true
					}
				}
			}
		}
	})
	if split == nil {
		t.Fatal("splitter")
	}
	widget.Walk(split, func(c widget.Component) {
		if _, ok := c.(*widgets.ComboBox); ok {
			t.Fatal("account ComboBox still in the sidebar splitter")
		}
	})
	if tree == nil {
		t.Fatal("folder tree")
	}
	var acctNode, tagsNode bool
	for _, n := range tree.Roots {
		if n == nil {
			continue
		}
		if id, ok := n.Data.(string); ok && id != "" && id != AccountTags {
			acctNode = true
		}
		if n.Label == "Filters" {
			t.Fatal("sidebar Filters node should be renamed Tags")
		}
		if n.Label == "Tags" || n.Data == AccountTags {
			tagsNode = true
		}
	}
	if !acctNode {
		t.Fatal("account folders missing from the tree")
	}
	if !tagsNode {
		t.Fatal("Tags missing from the tree")
	}
	if !prefs {
		t.Fatal("M → Preferences missing")
	}
	w.Close()
}

func TestMailChromeHasNoActiveFilterBanner(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()

	qf := findQuickFilter(mailTree(w))
	var tree *widgets.TreeView
	var table *widgets.TableView
	widget.Walk(mailTree(w), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TableView:
			if table == nil {
				table = v
			}
		case *widgets.TreeView:
			if tree == nil {
				tree = v
			}
		}
	})
	if qf == nil {
		t.Fatal("quick filter field missing")
	}
	if table == nil {
		t.Fatal("thread table")
	}
	if tree == nil || tree.OnSelect == nil {
		t.Fatal("Tags tree")
	}
	assertNoFilterBanner(t, mailTree(w))

	before := table.RowCount
	qf.SetText("lunch")
	a.PumpOnce()
	assertNoFilterBanner(t, mailTree(w))
	if table.RowCount <= 0 || table.RowCount > before {
		t.Fatalf("quick filter should narrow the list, rows=%d before=%d", table.RowCount, before)
	}

	qf.SetText("")
	a.PumpOnce()
	assertNoFilterBanner(t, mailTree(w))
	if table.RowCount != before {
		t.Fatalf("emptying quick filter should restore the list, rows=%d want %d", table.RowCount, before)
	}

	unread := findFilterPin(tree, "Unread")
	if unread == nil {
		t.Fatal("Unread pin missing from Tags tree")
	}
	tree.OnSelect(unread)
	a.PumpOnce()
	assertNoFilterBanner(t, mailTree(w))
	if table.RowCount <= 0 || table.RowCount > before {
		t.Fatalf("Unread pin should narrow the list, rows=%d before=%d", table.RowCount, before)
	}
	unread = findFilterPin(tree, "Unread")
	if unread == nil {
		t.Fatal("Unread pin missing after toggle")
	}
	tree.OnSelect(unread)
	a.PumpOnce()
	assertNoFilterBanner(t, mailTree(w))
	w.Close()
}

func findFilterPin(tree *widgets.TreeView, name string) *widgets.TreeNode {
	if tree == nil {
		return nil
	}
	var found *widgets.TreeNode
	var walk func([]*widgets.TreeNode)
	walk = func(nodes []*widgets.TreeNode) {
		for _, n := range nodes {
			if n == nil || found != nil {
				continue
			}
			if _, ok := n.Data.(filterPin); ok && strings.TrimPrefix(n.Label, "✓ ") == name {
				found = n
				return
			}
			walk(n.Children)
		}
	}
	walk(tree.Roots)
	return found
}

func assertNoFilterBanner(t *testing.T, root widget.Component) {
	t.Helper()
	widget.Walk(root, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Button:
			if v.Text == "Clear filter" {
				t.Fatal("Clear filter button still above the message list")
			}
		case *widgets.Label:
			if strings.Contains(v.Text, "Filter on") ||
				strings.Contains(v.Text, "No messages match this filter") ||
				v.Text == "This folder is empty." {
				t.Fatalf("active-filter banner still present: %q", v.Text)
			}
		}
	})
}

func TestMailColumnsTopicWhoWhen(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	var table *widgets.TableView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TableView); ok && table == nil {
			table = tv
		}
	})
	if table == nil {
		t.Fatal("thread table")
	}
	want := []string{"★", "📎", "Topic", "Who", "When"}
	if len(table.Columns) != len(want) {
		t.Fatalf("columns %d %v", len(table.Columns), titlesOf(table))
	}
	for i, title := range want {
		if table.Columns[i].Title != title {
			t.Fatalf("col %d %q want %q", i, table.Columns[i].Title, title)
		}
	}
	w.Close()
}

func titlesOf(tv *widgets.TableView) []string {
	out := make([]string, len(tv.Columns))
	for i, c := range tv.Columns {
		out[i] = c.Title
	}
	return out
}

func TestMailStarRendersAfterToggle(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	var table *widgets.TableView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TableView); ok && table == nil {
			table = tv
		}
	})
	if table == nil || table.CellText == nil || table.RowCount < 1 {
		t.Fatal("thread table")
	}
	row := -1
	for i := 0; i < table.RowCount; i++ {
		if table.CellText(i, 0) == "" {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("no unstarred row")
	}
	table.Selected = row
	if table.OnSelect != nil {
		table.OnSelect(row)
	}
	if table.OnContext == nil {
		t.Fatal("thread context menu")
	}
	table.OnContext(row, paintengine2d.Pt(10, 10))
	a.PumpOnce()
	pop, ok := w.Popup().(*widgets.PopupMenu)
	if !ok || pop == nil {
		t.Fatal("context menu Star")
	}
	var star func()
	for _, it := range pop.Items {
		if it != nil && it.Text == "Star" && it.OnClick != nil {
			star = it.OnClick
		}
	}
	if star == nil {
		t.Fatal("context menu Star")
	}
	star()
	a.PumpOnce()
	if got := table.CellText(row, 0); got != "★" {
		t.Fatalf("after star CellText=%q", got)
	}
	img := paintengine2d.NewImage(32, 28)
	ctx := paintengine2d.NewContext(img)
	table.Look().DrawTableCell(ctx, paintengine2d.XYWH(0, 0, 28, 28), style.StateChecked, "★", style.AlignStart, table.Look().Font())
	if ink := cellInk(img, 0, 26); ink < 8 {
		t.Fatalf("star glyph missing in 28px cell, ink=%d", ink)
	}
	star()
	a.PumpOnce()
	if got := table.CellText(row, 0); got != "" {
		t.Fatalf("after unstar CellText=%q", got)
	}
	w.Close()
}

func TestMailAttachmentSelectOpenSaveAs(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()

	hits, opens, saves, saveAll := findAttachChrome(mailTree(w))
	if len(hits) < 2 || len(opens) < 2 || len(saves) < 2 {
		t.Fatalf("per-row chrome hits=%d open=%d saveAs=%d", len(hits), len(opens), len(saves))
	}
	if saveAll == nil || !saveAll.Enabled() || saveAll.OnClick == nil {
		t.Fatal("Save All should sit in the attachment toolbar and be enabled")
	}
	for i, b := range opens {
		if !b.Enabled() {
			t.Fatalf("row Open %d should be enabled without a shared selection", i)
		}
	}
	for i, b := range saves {
		if !b.Enabled() {
			t.Fatalf("row Save As %d should be enabled without a shared selection", i)
		}
	}

	before := globMailOpenDirs()
	if hits[0].OnPress == nil {
		t.Fatal("attachment row press")
	}
	hits[0].OnPress()
	a.PumpOnce()
	if !hits[0].Selected || hits[1].Selected {
		t.Fatal("single click should select that row only")
	}
	if n := globMailOpenDirs(); len(n) != len(before) {
		t.Fatalf("single click opened a part: before=%d after=%d", len(before), len(n))
	}

	hits[0].OnPress()
	a.PumpOnce()
	afterOpen := globMailOpenDirs()
	if len(afterOpen) <= len(before) {
		t.Fatal("double click should Open (messages.openPart cache)")
	}

	beforeBtn := globMailOpenDirs()
	if opens[1].OnClick == nil {
		t.Fatal("row Open")
	}
	opens[1].OnClick()
	a.PumpOnce()
	if n := globMailOpenDirs(); len(n) <= len(beforeBtn) {
		t.Fatal("row Open should call messages.openPart")
	}

	dest := filepath.Join(t.TempDir(), "mail-shortcuts.txt")
	if saves[0].OnClick == nil {
		t.Fatal("Save As")
	}
	saves[0].OnClick()
	a.PumpOnce()
	if err := confirmFileDialog(t, w, dest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("saved file: %v", err)
	}
	if !strings.Contains(string(raw), "mail-shortcuts.txt") {
		t.Fatalf("saved bytes %q", raw)
	}

	dir := t.TempDir()
	saveAll.OnClick()
	a.PumpOnce()
	if err := confirmFileDialog(t, w, dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mail-shortcuts.txt", "uitoolkit-v080.png"} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("Save All %s: %v", name, err)
		}
		if !strings.Contains(string(b), name) {
			t.Fatalf("Save All bytes %s %q", name, b)
		}
	}
	w.Close()
}

func TestAttachFileNameHelpers(t *testing.T) {
	if got := attachFileName("../x/mail-shortcuts.txt"); got != "mail-shortcuts.txt" {
		t.Fatalf("base %q", got)
	}
	if got := attachFileName(".."); got != "attachment" {
		t.Fatalf("dotdot %q", got)
	}
	used := map[string]int{}
	if got := uniqueFileName("a.txt", used); got != "a.txt" {
		t.Fatalf("first %q", got)
	}
	if got := uniqueFileName("a.txt", used); got != "a-2.txt" {
		t.Fatalf("second %q", got)
	}
	dir := t.TempDir()
	got, err := saveAllDir(filepath.Join(dir, "nested-folder"))
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(got); err != nil || !st.IsDir() {
		t.Fatalf("saveAllDir create: %s %v", got, err)
	}
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, err := saveAllDir(file)
	if err != nil || parent != dir {
		t.Fatalf("saveAllDir file -> parent: %q %v", parent, err)
	}
}

func findAttachChrome(root widget.Component) (hits []*attachHit, opens, saves []*widgets.Button, saveAll *widgets.Button) {
	widget.Walk(root, func(c widget.Component) {
		switch v := c.(type) {
		case *attachHit:
			hits = append(hits, v)
		case *widgets.Button:
			switch v.Text {
			case "Open":
				opens = append(opens, v)
			case "Save As":
				saves = append(saves, v)
			case "Save All":
				saveAll = v
			}
		}
	})
	return
}

func confirmFileDialog(t *testing.T, w *app.Window, path string) error {
	t.Helper()
	if w.Overlay() == nil {
		return fmt.Errorf("file dialog overlay")
	}
	var pathField *widgets.TextField
	var confirm *widgets.Button
	widget.Walk(w.Overlay(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TextField:
			if pathField == nil {
				pathField = v
			}
		case *widgets.Button:
			if v.Text == "Save" {
				confirm = v
			}
		}
	})
	if pathField == nil || confirm == nil || confirm.OnClick == nil {
		return fmt.Errorf("dialog path/Save")
	}
	pathField.SetText(path)
	confirm.OnClick()
	return nil
}

func globMailOpenDirs() []string {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "uitk-mail-*"))
	return matches
}

func TestOrderFolderChildrenOutboxLast(t *testing.T) {
	got := orderFolderChildren([]Folder{
		{Name: "Outbox", ID: FolderOutbox, Kind: FolderCustom},
		{Name: "Projects", Kind: FolderCustom},
		{Name: "Inbox", Kind: FolderInbox},
		{Name: "Trash", Kind: FolderTrash},
		{Name: "Junk", Kind: FolderJunk},
		{Name: "Archive", Kind: FolderArchive},
		{Name: "Drafts", Kind: FolderDrafts},
		{Name: "Sent", Kind: FolderSent},
	})
	want := []string{"Inbox", "Drafts", "Sent", "Archive", "Junk", "Trash", "Projects", "Outbox"}
	if len(got) != len(want) {
		t.Fatalf("len %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("pos %d: got %q want %q", i, got[i].Name, want[i])
		}
	}
	named := orderFolderChildren([]Folder{
		{Name: "Outbox", Kind: FolderCustom},
		{Name: "Inbox", Kind: FolderInbox},
		{Name: "Notes", Kind: FolderCustom},
	})
	if named[len(named)-1].Name != "Outbox" {
		t.Fatalf("named Outbox not last: %#v", named)
	}
}

func TestMailMessageSourceShowsRFC822(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	PrepareShot(w, -1)
	a.PumpOnce()

	var tabs *widgets.TabView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TabView); ok && tabs == nil {
			tabs = tv
		}
	})
	if tabs == nil {
		t.Fatal("preview tabs")
	}
	tabs.Select(1)
	a.PumpOnce()
	var sourceTab *widgets.TextArea
	widget.Walk(mailTree(w), func(c widget.Component) {
		if ta, ok := c.(*widgets.TextArea); ok && ta.Mono && ta.ReadOnly {
			sourceTab = ta
		}
	})
	if sourceTab == nil || !strings.Contains(sourceTab.Text, "MIME-Version:") {
		t.Fatalf("Source tab missing RFC822: %q", textPreview(sourceTab))
	}
	if strings.Contains(sourceTab.Text, "X-Flag:") {
		t.Fatal("Source tab used reconstructed headers")
	}

	var mb *widgets.MenuBar
	widget.Walk(mailTree(w), func(c widget.Component) {
		if m, ok := c.(*widgets.MenuBar); ok && mb == nil {
			mb = m
		}
	})
	if mb == nil {
		t.Fatal("menu bar")
	}
	mb.RequestFocus()
	w.Inject(platform.Event{Kind: platform.EventKeyDown, Key: platform.KeyU, Mods: platform.ModCtrl})
	a.PumpOnce()
	var srcWin *app.Window
	for _, win := range a.Windows() {
		if win == w {
			continue
		}
		var body *widgets.TextArea
		widget.Walk(win.Content(), func(c widget.Component) {
			if ta, ok := c.(*widgets.TextArea); ok && ta.Mono && strings.Contains(ta.Text, "MIME-Version:") {
				body = ta
			}
		})
		if body != nil {
			srcWin = win
			break
		}
	}
	if srcWin == nil {
		t.Fatal("source window not opened")
	}
	var body *widgets.TextArea
	widget.Walk(srcWin.Content(), func(c widget.Component) {
		if ta, ok := c.(*widgets.TextArea); ok && ta.Mono {
			body = ta
		}
	})
	if body == nil || !body.ReadOnly || !strings.Contains(body.Text, "MIME-Version:") {
		t.Fatalf("source window body %q", textPreview(body))
	}
	srcWin.Close()
	w.Close()
}

func textPreview(ta *widgets.TextArea) string {
	if ta == nil {
		return "<nil>"
	}
	if len(ta.Text) > 200 {
		return ta.Text[:200]
	}
	return ta.Text
}

func TestMailFolderTreePutsOutboxLast(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	assertOutboxPinnedBottom(t, mailTree(w))
	w.Close()
}

func TestMailMenuHoverDoesNotRebuildTree(t *testing.T) {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	var mb *widgets.MenuBar
	var folder *widgets.TreeView
	widget.Walk(mailTree(w), func(c widget.Component) {
		if m, ok := c.(*widgets.MenuBar); ok && mb == nil {
			mb = m
		}
	})
	folder, _ = mailSidebarTrees(mailTree(w))
	if mb == nil || folder == nil {
		t.Fatal("mail chrome")
	}
	roots := folder.Roots
	mb.Open(0)
	a.PumpOnce()
	pop, ok := w.Popup().(*widgets.PopupMenu)
	if !ok || pop == nil || len(pop.Items) < 3 {
		t.Fatal("M menu")
	}
	for i := 0; i < 3; i++ {
		r := pop.ItemBounds(i)
		o := widget.DeviceOrigin(pop)
		w.Inject(platform.Event{
			Kind: platform.EventMouseMove,
			Pos:  paintengine2d.Pt(o.X+(r.Min.X+r.Max.X)*0.5, o.Y+(r.Min.Y+r.Max.Y)*0.5),
		})
		a.PumpOnce()
	}
	if folder.Roots[0] != roots[0] {
		t.Fatal("menu hover rebuilt the folder tree")
	}
	w.Close()
}

func mailSidebarTrees(root widget.Component) (folder, outbox *widgets.TreeView) {
	widget.Walk(root, func(c widget.Component) {
		tv, ok := c.(*widgets.TreeView)
		if !ok {
			return
		}
		if treeHasOutboxRoot(tv) {
			if outbox == nil {
				outbox = tv
			}
			return
		}
		if folder == nil {
			folder = tv
		}
	})
	return folder, outbox
}

func treeHasOutboxRoot(tv *widgets.TreeView) bool {
	if tv == nil {
		return false
	}
	for _, n := range tv.Roots {
		if n == nil {
			continue
		}
		if id, ok := n.Data.(FolderID); ok && id == FolderOutbox {
			return true
		}
		if strings.HasPrefix(n.Label, "Outbox") {
			return true
		}
	}
	return false
}

func assertOutboxPinnedBottom(t *testing.T, root widget.Component) {
	t.Helper()
	folder, pin := mailSidebarTrees(root)
	if folder == nil || len(folder.Roots) == 0 {
		t.Fatal("folder tree")
	}
	if treeHasOutboxRoot(folder) {
		t.Fatal("Outbox should not sit in the folder tree next to Tags")
	}
	last := folder.Roots[len(folder.Roots)-1]
	if last == nil || last.Label != "Tags" {
		label := ""
		if last != nil {
			label = last.Label
		}
		t.Fatalf("folder tree last root %q, want Tags", label)
	}
	if pin == nil || !treeHasOutboxRoot(pin) {
		t.Fatal("Outbox pin missing")
	}
	if pin.Bounds().Dy() > 48 {
		t.Fatalf("Outbox pin too tall %v (should be one row at the bottom)", pin.Bounds().Dy())
	}
	fo := widget.DeviceOrigin(folder)
	po := widget.DeviceOrigin(pin)
	if po.Y+0.5 < fo.Y+folder.Bounds().Dy()-pin.Bounds().Dy()-8 {
		t.Fatalf("Outbox not at the bottom of the folder pane: folder=%v..%v pin=%v",
			fo.Y, fo.Y+folder.Bounds().Dy(), po.Y)
	}
}

func assertNoUnreadFolderFooter(t *testing.T, root widget.Component) {
	t.Helper()
	walkAll(root, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.StatusBar:
			t.Fatal("status bar should stay gone")
		case *widgets.Label:
			if strings.Contains(strings.ToLower(v.Text), "unread in all folders") ||
				strings.Contains(strings.ToLower(v.Text), "unread folders") {
				t.Fatalf("unread-count footer still present: %q", v.Text)
			}
		}
	})
}

func TestMailMenuBarIsOnlyM(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(MailApp(a, w))
	a.PumpOnce()
	var mb *widgets.MenuBar
	widget.Walk(mailTree(w), func(c widget.Component) {
		if m, ok := c.(*widgets.MenuBar); ok && mb == nil {
			mb = m
		}
	})
	if mb == nil {
		t.Fatal("menu bar")
	}
	menus := mb.Menus()
	if len(menus) != 1 {
		t.Fatalf("menus %d want 1", len(menus))
	}
	title, _, _ := widgets.ParseMnemonic(menus[0].Title)
	if title != "M" {
		t.Fatalf("menu title %q want M", menus[0].Title)
	}
	bannedTitles := []string{"File", "Edit", "View", "Go", "Message", "Tools", "Help"}
	for _, name := range bannedTitles {
		if title == name {
			t.Fatalf("old top-level menu %q", name)
		}
	}
	var labels []string
	var quit, prefs, view, notify, threaded, muted bool
	gone := []string{
		"Mail Toolbar", "Quick Filter Bar",
		"Sort by When", "Sort by Topic", "Sort by Who",
		"Dark", "Light", "Message Source",
	}
	topGone := []string{"Threaded", "Hide muted threads"}
	for _, it := range menus[0].Items {
		if it == nil {
			continue
		}
		label, _, _ := widgets.ParseMnemonic(it.Text)
		labels = append(labels, label)
		for _, g := range gone {
			if label == g {
				t.Fatalf("removed item still on M: %q %v", label, labels)
			}
		}
		for _, g := range topGone {
			if label == g {
				t.Fatalf("%q should live under View, not on M: %v", label, labels)
			}
		}
		switch label {
		case "Quit":
			quit = it.Shortcut == "Ctrl+Q" && it.OnClick != nil
		case "Preferences":
			prefs = it.OnClick != nil
		case "Notify":
			notify = it.HasSubmenu()
			wantNotify := []string{"Notify on new mail", "VIP senders only", "Desktop notifications"}
			gotNotify := menuItemLabels(it.Submenu)
			for _, name := range wantNotify {
				if !containsLabel(gotNotify, name) {
					t.Fatalf("Notify submenu missing %q: %v", name, gotNotify)
				}
			}
			for _, sub := range it.Submenu {
				if sub == nil {
					continue
				}
				if !sub.Checkable {
					t.Fatalf("Notify item %q should be a check", sub.Text)
				}
			}
		case "View":
			view = it.HasSubmenu()
			want := []string{
				"Vertical (3-pane)", "Classic (preview below)",
				"Table view", "Card view",
				"Compact", "Default density", "Relaxed",
				"Threaded", "Hide muted threads",
			}
			got := menuItemLabels(it.Submenu)
			for _, name := range want {
				if !containsLabel(got, name) {
					t.Fatalf("View submenu missing %q: %v", name, got)
				}
			}
			for _, name := range gone {
				if containsLabel(got, name) {
					t.Fatalf("removed item under View: %q %v", name, got)
				}
			}
			for _, sub := range it.Submenu {
				if sub == nil {
					continue
				}
				subLabel, _, _ := widgets.ParseMnemonic(sub.Text)
				switch subLabel {
				case "Threaded":
					threaded = sub.Checkable
				case "Hide muted threads":
					muted = sub.Checkable
				}
			}
		}
	}
	if !view || !notify || !threaded || !muted || !prefs || !quit {
		t.Fatalf("M items view=%v notify=%v threaded=%v muted=%v prefs=%v quit=%v %v", view, notify, threaded, muted, prefs, quit, labels)
	}
	pos := map[string]int{}
	for i, l := range labels {
		if _, ok := pos[l]; !ok {
			pos[l] = i
		}
	}
	if pos["View"] > pos["Notify"] || pos["Notify"] > pos["Preferences"] || pos["Preferences"] > pos["Quit"] {
		t.Fatalf("order %v want View, Notify, Preferences, Quit", labels)
	}
	w.Close()
}

func menuItemLabels(items []*widgets.MenuItem) []string {
	var out []string
	for _, it := range items {
		if it == nil || it.Separator {
			continue
		}
		label, _, _ := widgets.ParseMnemonic(it.Text)
		out = append(out, label)
	}
	return out
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestMailPreferencesTabs(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := OpenPrefs(a, cli, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.PumpOnce()
	var bar *widgets.TabBar
	widget.Walk(mailTree(w), func(c widget.Component) {
		if tv, ok := c.(*widgets.TabBar); ok && bar == nil {
			bar = tv
		}
	})
	if bar == nil {
		t.Fatal("Preferences tabs")
	}
	got := append([]string(nil), bar.Titles...)
	if len(got) != 2 || got[0] != "Accounts" || got[1] != "Tags" {
		t.Fatalf("Preferences tabs %v want Accounts, Tags", got)
	}
	for _, name := range []string{"Appearance", "Notify", "VIP", "Identities", "Filters"} {
		if containsLabel(got, name) {
			t.Fatalf("removed Preferences tab still present: %q %v", name, got)
		}
	}
	var add, edit, remove *widgets.Button
	walkAll(mailTree(w), func(c widget.Component) {
		b, ok := c.(*widgets.Button)
		if !ok {
			return
		}
		switch b.Text {
		case "Add":
			add = b
		case "Edit":
			edit = b
		case "Remove":
			remove = b
		}
	})
	if add == nil || edit == nil || remove == nil {
		t.Fatalf("Tags CRUD buttons add=%v edit=%v remove=%v", add, edit, remove)
	}
	prefs, err := cli.Tags()
	if err != nil {
		t.Fatal(err)
	}
	want := tagNames(DefaultTags())
	gotNames := tagNames(prefs)
	if len(gotNames) != len(want) {
		t.Fatalf("prefs tags %v want %v", gotNames, want)
	}
	for i, name := range want {
		if !strings.EqualFold(gotNames[i], name) {
			t.Fatalf("prefs tags %v want %v", gotNames, want)
		}
	}
	if remove.Enabled() {
		t.Fatal("Remove should be disabled on the locked Unread tag")
	}
	w.Close()
}

func TestMailNotifyMenuPersists(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(Open(a, w, cli, AppOptions{}))
	a.PumpOnce()

	before, err := cli.NotifyPrefs()
	if err != nil {
		t.Fatal(err)
	}
	var notifyOn, vipOnly *widgets.MenuItem
	widget.Walk(mailTree(w), func(c widget.Component) {
		mb, ok := c.(*widgets.MenuBar)
		if !ok {
			return
		}
		for _, m := range mb.Menus() {
			for _, it := range m.Items {
				if it == nil {
					continue
				}
				label, _, _ := widgets.ParseMnemonic(it.Text)
				if label != "Notify" {
					continue
				}
				for _, sub := range it.Submenu {
					if sub == nil {
						continue
					}
					subLabel, _, _ := widgets.ParseMnemonic(sub.Text)
					switch subLabel {
					case "Notify on new mail":
						notifyOn = sub
					case "VIP senders only":
						vipOnly = sub
					}
				}
			}
		}
	})
	if notifyOn == nil || notifyOn.OnClick == nil || vipOnly == nil || vipOnly.OnClick == nil {
		t.Fatal("M → Notify checks")
	}
	if notifyOn.Checked != before.Enabled || vipOnly.Checked != before.VIPOnly {
		t.Fatalf("notify checks enabled=%v vip=%v prefs %+v", notifyOn.Checked, vipOnly.Checked, before)
	}
	notifyOn.OnClick()
	vipOnly.OnClick()
	a.PumpOnce()
	after, err := cli.NotifyPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if after.Enabled == before.Enabled || after.VIPOnly == before.VIPOnly {
		t.Fatalf("notify prefs unchanged: before %+v after %+v", before, after)
	}
	w.Close()
}

func cellInk(img *paintengine2d.Image, x0, x1 int) int {
	n := 0
	for y := 0; y < img.Height; y++ {
		for x := x0; x < x1 && x < img.Width; x++ {
			_, _, _, a := img.PremulAt(x, y)
			if a > 20 {
				n++
			}
		}
	}
	return n
}
