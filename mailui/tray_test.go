package mailui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

func TestMailTrayFakeClickRaises(t *testing.T) {
	mailcore.IsolateTestEnvTB(t)
	t.Setenv("UITK_TRAY", "fake")
	sock, stop, err := mailcore.StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	rec := &recNotifier{}
	old := newMailNotifier
	newMailNotifier = func(*app.Application) mailNotifier { return rec }
	defer func() { newMailNotifier = old }()

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 640, Height: 400, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(Open(a, w, cli, AppOptions{}))
	a.PumpOnce()

	var tray *platform.FakeStatusItem
	for _, it := range a.StatusItems() {
		if f, ok := it.(*platform.FakeStatusItem); ok {
			tray = f
		}
	}
	if tray == nil {
		t.Fatal("Open should create a fake StatusItem when UITK_TRAY=fake")
	}
	// The tray wears comms-mail's logo, as a picture: with a theme name
	// too, the tray host would draw its theme's icon instead.
	icon := tray.Icon()
	if icon.Name != "" || icon.Image == nil || icon.Image.Width != trayIconSize {
		t.Fatalf("tray icon %q, image %v: want the logo", icon.Name, icon.Image != nil)
	}
	if _, _, _, a := icon.Image.PremulAt(trayIconSize/2, trayIconSize/2); a == 0 {
		t.Fatal("the tray icon is blank")
	}
	w.Hide()
	if w.Visible() {
		t.Fatal("close-to-tray hide")
	}
	tray.Click()
	a.DrainPosted() // tray callbacks are queued for the UI goroutine
	if !w.Visible() {
		t.Fatal("tray click should show Mail")
	}
	w.Hide()
	tray.ClickMenu(0)
	a.DrainPosted()
	if !w.Visible() {
		t.Fatal("Show Mail menu should raise")
	}
	w.Hide()
	tray.ContextClick(600, 10)
	if w.Visible() {
		t.Fatal("tray context click must not raise Mail (left-click does)")
	}
	for _, win := range a.Windows() {
		if win != nil && win != w && !win.Closed() && win.Visible() {
			t.Fatal("HostMenu Mail must not open a toolkit popup window")
		}
	}
	if tray.MenuChrome() != platform.HostMenu {
		t.Fatalf("Mail tray chrome %v want HostMenu", tray.MenuChrome())
	}
	w.Hide()
	tray.ClickNotify()
	a.DrainPosted()
	if !w.Visible() {
		t.Fatal("notify-click should show Mail")
	}
}

func TestMailNotifyEventSendsNotification(t *testing.T) {
	mailcore.IsolateTestEnvTB(t)
	t.Setenv("UITK_TRAY", "fake")
	sock, stop, err := mailcore.StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := mailcore.DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	rec := &recNotifier{}
	old := newMailNotifier
	newMailNotifier = func(*app.Application) mailNotifier { return rec }
	defer func() { newMailNotifier = old }()

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 640, Height: 400, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(Open(a, w, cli, AppOptions{}))
	a.PumpOnce()

	var tray *platform.FakeStatusItem
	for _, it := range a.StatusItems() {
		if f, ok := it.(*platform.FakeStatusItem); ok {
			tray = f
		}
	}
	if tray == nil {
		t.Fatal("missing tray")
	}
	accts, err := cli.Accounts()
	if err != nil || len(accts) == 0 {
		t.Fatalf("accounts %v %d", err, len(accts))
	}
	if _, err := cli.Fetch(accts[0].ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rec.count() > 0 {
			rec.mu.Lock()
			n := rec.sent[0]
			rec.mu.Unlock()
			if n.Title == "" || n.ID != "new-mail" {
				t.Fatalf("notification %+v", n)
			}
			// A desktop notification of its own, not the tray's toast.
			if len(tray.Notes) != 0 {
				t.Fatalf("the tray toasted too: %+v", tray.Notes)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("mail.notify should send a desktop notification")
}

func TestMailTrayNativeNeverPanics(t *testing.T) {
	mailcore.IsolateTestEnvTB(t)
	os.Unsetenv("UITK_TRAY")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Mail StatusItem panicked: %v", r)
		}
	}()
	// Same options attachTray uses (without a headless Application, which
	// would force a stub and hide the Plasma GetLayout path).
	item, err := platform.NewStatusItem(platform.StatusItemOptions{
		ID:      "comms-mail",
		Title:   "Mail",
		Tooltip: "Mail",
		Icon:    app.StatusIconFromTool(style.IconMail, style.DarkLook(), 22),
		Menu: []platform.StatusMenuItem{
			{Text: "Show Mail"},
			{Separator: true},
			{Text: "Quit"},
		},
		OnClick:       func() {},
		OnNotifyClick: func() {},
	})
	if err != nil || item == nil {
		t.Fatalf("NewStatusItem %v %v", item, err)
	}
	t.Cleanup(func() { _ = item.Close() })
	if item.Backend() == "" {
		t.Fatal("empty backend")
	}
}

func TestFormatNewMailNoticeUsesSender(t *testing.T) {
	s := mailcore.NewDemoStore()
	accts := s.Accounts()
	if len(accts) == 0 {
		t.Fatal("demo accounts")
	}
	title, body := mailcore.FormatNewMailNotice(s, accts[0].ID, 1, false)
	if title == "" || body == "" {
		t.Fatalf("empty notice %q %q", title, body)
	}
	if title == "New mail" && strings.Contains(body, "new message") {
		t.Fatalf("demo inbox should yield sender/subject, got %q %q", title, body)
	}
}

// New mail puts the logo in a seal in the tray until the window is looked
// at: it takes the focus, or is opened from the tray or the notification.
// Painted without the focus, it has not been seen.
func TestTrayShowsNewMailUntilSeen(t *testing.T) {
	t.Setenv("UITK_TRAY", "fake")
	item, err := platform.NewStatusItem(platform.StatusItemOptions{Icon: trayIcon(false)})
	tray, _ := item.(*platform.FakeStatusItem)
	if err != nil || tray == nil {
		t.Fatalf("fake tray %T %v", item, err)
	}
	t.Cleanup(func() { _ = tray.Close() })

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 320, Height: 200, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{app: a, win: w, tray: tray, notes: &recNotifier{}}
	root := wrapShortcutsReady(widgets.NewColumn(), nil, nil)
	root.painted = s.mailSeen
	w.SetContent(root)
	a.PumpOnce()

	// The seal's rim is where the plain logo is clear, above its disc.
	sealed := func() bool {
		img := tray.Icon().Image
		if img == nil || img.Width != trayIconSize {
			t.Fatalf("tray icon %v: want a %d px picture", img != nil, trayIconSize)
		}
		_, _, _, al := img.PremulAt(trayIconSize/2, 3)
		return al != 0
	}
	if sealed() {
		t.Fatal("the tray starts with the new-mail logo")
	}

	w.Inject(platform.Event{Kind: platform.EventFocusOut})
	a.PumpOnce()
	s.onDaemonEvent(mailcore.Event{Method: mailcore.EventNotify, Title: "Ada Lovelace"})
	a.PumpOnce()
	if !sealed() || !s.newMailWaiting() {
		t.Fatal("new mail, behind another window: the tray keeps the plain logo")
	}
	root.Invalidate()
	a.PumpOnce()
	a.DrainPosted()
	if !sealed() {
		t.Fatal("a paint without the focus counted as seeing the mail")
	}
	w.Inject(platform.Event{Kind: platform.EventFocusIn})
	a.PumpOnce()
	a.DrainPosted() // what the paint posted, as the run loop's next turn would
	if sealed() || s.newMailWaiting() {
		t.Fatal("the window took the focus: the tray still shows new mail")
	}

	w.Hide()
	s.onDaemonEvent(mailcore.Event{Method: mailcore.EventNotify, Title: "Ada Lovelace"})
	if !sealed() {
		t.Fatal("new mail, the window hidden: no new-mail logo")
	}
	s.showMain()
	if sealed() {
		t.Fatal("opened from the tray: the tray still shows new mail")
	}
}
