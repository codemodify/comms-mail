package mail

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
)

func TestMailTrayFakeClickRaises(t *testing.T) {
	IsolateTestEnvTB(t)
	t.Setenv("UITK_TRAY", "fake")
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
	mailIcon := app.StatusIconFromTool(style.IconMail, style.DarkLook(), 22)
	infoIcon := app.StatusIconFromTool(style.IconInfo, style.DarkLook(), 22)
	if tray.Icon().Name != mailIcon.Name || tray.Icon().Name == infoIcon.Name {
		t.Fatalf("tray icon %q want mail %q not info %q", tray.Icon().Name, mailIcon.Name, infoIcon.Name)
	}
	if tray.Icon().Name != "mail-unread" {
		t.Fatalf("freedesktop name %q", tray.Icon().Name)
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
	IsolateTestEnvTB(t)
	t.Setenv("UITK_TRAY", "fake")
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
	IsolateTestEnvTB(t)
	os.Unsetenv("UITK_TRAY")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Mail StatusItem panicked: %v", r)
		}
	}()
	// Same options attachTray uses (without a headless Application, which
	// would force a stub and hide the Plasma GetLayout path).
	item, err := platform.NewStatusItem(platform.StatusItemOptions{
		ID:      "mailclientui",
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
	s := NewDemoStore()
	accts := s.Accounts()
	if len(accts) == 0 {
		t.Fatal("demo accounts")
	}
	title, body := formatNewMailNotice(s, accts[0].ID, 1, false)
	if title == "" || body == "" {
		t.Fatalf("empty notice %q %q", title, body)
	}
	if title == "New mail" && strings.Contains(body, "new message") {
		t.Fatalf("demo inbox should yield sender/subject, got %q %q", title, body)
	}
}
