//go:build linux

package mail

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
)

const (
	sniWatcher     = "org.kde.StatusNotifierWatcher"
	sniWatcherPath = "/StatusNotifierWatcher"
	dbusMenuPath   = "/MenuBar"
	dbusMenuIface  = "com.canonical.dbusmenu"
)

// TestMailUIStartsWithStatusNotifierWatcher opens Mail on a session bus
// that has org.kde.StatusNotifierWatcher (real Plasma, or a fake that
// immediately calls GetLayout — the v0.16.0 panic). The UI process must
// keep its window.
func TestMailUIStartsWithStatusNotifierWatcher(t *testing.T) {
	os.Unsetenv("UITK_TRAY")
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skipf("no session bus: %v", err)
	}
	defer conn.Close()

	if !nameHasOwner(conn, sniWatcher) {
		stop, err := startFakeSNIWatcher(conn)
		if err != nil && !nameHasOwner(conn, sniWatcher) {
			t.Skipf("cannot provide %s: %v", sniWatcher, err)
		}
		if err == nil {
			defer stop()
		}
	}

	IsolateTestEnvTB(t)
	// Keep the session bus after isolate rewrites XDG_RUNTIME_DIR.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "autolaunch:")
	t.Setenv("UITK_TRAY", "")

	sock, stopDemo, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stopDemo()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Mail panicked with StatusNotifierWatcher: %v", r)
		}
	}()

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: false})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Mail", Width: 640, Height: 400})
	if err != nil {
		t.Skipf("no display for Mail window: %v", err)
	}
	w.SetContent(Open(a, w, cli, AppOptions{}))
	a.PumpOnce()
	if w.Closed() {
		t.Fatal("Mail window closed at startup")
	}

	var tray platform.StatusItem
	for _, it := range a.StatusItems() {
		if it != nil {
			tray = it
			break
		}
	}
	if tray == nil {
		t.Fatal("Mail Open should attach a StatusItem")
	}
	time.Sleep(300 * time.Millisecond)
	if !w.Visible() && w.Closed() {
		t.Fatal("window lost after watcher GetLayout")
	}
	if tray.Backend() == "stub" {
		t.Fatal("expected live SNI tray on a session with StatusNotifierWatcher")
	}
	if !tray.Alive() {
		t.Fatal("tray died after watcher GetLayout")
	}
	_ = tray.Close()
}

func nameHasOwner(conn *dbus.Conn, name string) bool {
	var owner bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owner); err != nil {
		return false
	}
	return owner
}

type fakeSNIWatcher struct {
	conn *dbus.Conn
}

func (w *fakeSNIWatcher) RegisterStatusNotifierItem(service string) *dbus.Error {
	go func() {
		dest, path := service, dbus.ObjectPath(dbusMenuPath)
		if len(service) > 0 && service[0] == '/' {
			path = dbus.ObjectPath(service)
			if names := w.conn.Names(); len(names) > 0 {
				dest = names[0]
			}
		}
		var rev uint32
		var layout struct {
			ID         int32
			Properties map[string]dbus.Variant
			Children   []dbus.Variant
		}
		_ = w.conn.Object(dest, path).Call(
			dbusMenuIface+".GetLayout", 0, int32(0), int32(-1), []string{},
		).Store(&rev, &layout)
	}()
	return nil
}

func startFakeSNIWatcher(conn *dbus.Conn) (func(), error) {
	reply, err := conn.RequestName(sniWatcher, dbus.NameFlagDoNotQueue)
	if err != nil {
		return func() {}, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return func() {}, errString("StatusNotifierWatcher already owned")
	}
	w := &fakeSNIWatcher{conn: conn}
	if err := conn.ExportMethodTable(map[string]interface{}{
		"RegisterStatusNotifierItem": w.RegisterStatusNotifierItem,
	}, sniWatcherPath, sniWatcher); err != nil {
		_, _ = conn.ReleaseName(sniWatcher)
		return func() {}, err
	}
	return func() {
		_ = conn.Export(nil, sniWatcherPath, sniWatcher)
		_, _ = conn.ReleaseName(sniWatcher)
	}, nil
}

type errString string

func (e errString) Error() string { return string(e) }
