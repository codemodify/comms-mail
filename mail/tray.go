package mail

import (
	"time"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widgets"
)

func (s *session) attachTray() {
	if s == nil || s.app == nil {
		return
	}
	if s.notifier() == nil {
		// New mail is a desktop notification of its own — the portal's
		// or the notification server's — whose click opens the window,
		// whether or not the tray has an icon.
		s.trayMu.Lock()
		s.notes = newMailNotifier(s.app)
		s.trayMu.Unlock()
	}
	if s.statusItem() == nil {
		look := s.app.Look()
		item, err := s.app.NewStatusItem(platform.StatusItemOptions{
			ID:         "mailclientui",
			Title:      "Mail",
			Tooltip:    "Mail",
			MenuChrome: platform.HostMenu,
			Icon:       app.StatusIconFromTool(style.IconMail, look, 22),
			Menu: app.StatusMenuFromItems([]*widgets.MenuItem{
				widgets.ItemIcon(style.IconMail, "Show Mail", s.showMain),
				widgets.Sep(),
				widgets.Item("Quit", s.quitFromTray),
			}),
			OnClick:       s.showMain,
			OnNotifyClick: s.showMain,
		})
		if err == nil && item != nil {
			s.setStatusItem(item)
			if s.win != nil && item.Alive() {
				s.win.SetCloseHides(true)
			}
		}
	}
	// Subscribe after the tray is published so the event goroutine sees it.
	// The daemon event stream drives both the toast and the list refresh,
	// so it is wired even when the tray itself is unavailable.
	s.listenDaemon()
}

// statusItem / setStatusItem guard s.tray: it is written by the UI goroutine
// (attach, quit) and read by the daemon event goroutine.
func (s *session) statusItem() platform.StatusItem {
	s.trayMu.Lock()
	defer s.trayMu.Unlock()
	return s.tray
}

// newMailNotifier makes the window's notifier (a seam for tests).
var newMailNotifier = func(a *app.Application) mailNotifier {
	return a.NewNotifier(platform.NotifierOptions{AppName: "Mail", DesktopEntry: "mailclientui"})
}

// mailNotifier is what Mail needs of a platform.Notifier.
type mailNotifier interface {
	Send(platform.DesktopNotification) (string, error)
	Close()
}

func (s *session) notifier() mailNotifier {
	s.trayMu.Lock()
	defer s.trayMu.Unlock()
	return s.notes
}

func (s *session) setStatusItem(it platform.StatusItem) {
	s.trayMu.Lock()
	s.tray = it
	s.trayMu.Unlock()
}
func (s *session) listenDaemon() {
	if s.cli == nil || s.refresher != nil {
		return
	}
	// Bursts of IDLE-driven mail.changed events collapse into one refresh:
	// each refresh re-queries every folder's unread count.
	s.refresher = newRefreshCoalescer(250*time.Millisecond, func() {
		queued := s.postLive(func() {
			s.pendingRefresh.Store(false)
			s.refreshList()
			s.rebuildTree()
			s.refreshStatus()
		})
		if !queued {
			s.pendingRefresh.Store(true)
		}
	})
	s.cli.OnEvent(s.onDaemonEvent)
}

// DrainDaemonEvents applies a refresh that a daemon event asked for while no
// UI loop was pumping. It reports whether anything was pending. Headless
// renderers and tests call it; the running UI never needs to.
func (s *session) DrainDaemonEvents() bool {
	if s == nil || !s.pendingRefresh.Swap(false) {
		return false
	}
	s.refreshList()
	s.rebuildTree()
	s.refreshStatus()
	return true
}
func (s *session) showMain() {
	if s == nil || s.app == nil {
		return
	}
	if s.win == nil || s.win.Closed() {
		win, err := s.app.NewWindow(platform.WindowOptions{
			Title: "Mail", Width: 1280, Height: 800, MinWidth: 860, MinHeight: 560,
		})
		if err != nil {
			return
		}
		s.win = win
		win.SetContent(s.build())
		if it := s.statusItem(); it != nil && it.Alive() {
			win.SetCloseHides(true)
		}
	}
	s.win.Show()
	s.win.Raise()
}
func (s *session) quitFromTray() {
	if s.refresher != nil {
		s.refresher.stop()
	}
	if s.win != nil {
		s.win.SetCloseHides(false)
		s.win.Close()
	}
	if it := s.statusItem(); it != nil {
		_ = it.Close()
		s.setStatusItem(nil)
	}
	if n := s.notifier(); n != nil {
		n.Close()
	}
	if s.app != nil {
		s.app.Quit()
	}
}
func (s *session) onDaemonEvent(ev Event) {
	switch ev.Method {
	case EventChanged, EventFetched, EventSynced:
		// New mail, a flag change, or a deletion from another client: the
		// window used to show nothing until the user pressed Fetch.
		if s.refresher != nil {
			s.refresher.request()
		}
	case EventNotify:
		title := ev.Title
		if title == "" {
			title = "New mail"
		}
		if n := s.notifier(); n != nil {
			// One notification for new mail, replaced by the next rather
			// than stacked (Thunderbird's and KMail's way); this runs on
			// the daemon's event goroutine, so the D-Bus call may wait.
			_, _ = n.Send(newMailNotification(title, ev.Body, s.showMain))
		}
		if s.refresher != nil {
			s.refresher.request()
		}
	}
}

// newMailNotification is the desktop notification for new mail: a click on
// it, or on its button, opens the window.
func newMailNotification(title, body string, open func()) platform.DesktopNotification {
	return platform.DesktopNotification{
		ID:       "new-mail",
		Title:    title,
		Body:     body,
		IconName: "mail-unread",
		Actions:  []platform.NotificationAction{{ID: "open", Label: "Open Mail"}},
		OnActivate: func(string) {
			if open != nil {
				open()
			}
		},
	}
}
