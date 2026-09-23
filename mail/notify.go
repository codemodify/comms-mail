package mail

import (
	"fmt"
	"os"
	"strings"

	"github.com/codemodify/uitoolkit/platform"
)

// NotifyEvent is broadcast as mail.notify and optionally shown on the desktop.
type NotifyEvent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	VIP   bool   `json:"vip,omitempty"`
	Count int    `json:"count,omitempty"`
}

func shouldNotify(p NotifyPrefs, m Message, vips map[string]bool) bool {
	if !p.Enabled {
		return false
	}
	if m.Read {
		return false
	}
	if m.ThreadID != "" {
		// mute is checked by caller via snap.muted
	}
	vip := isVIPAddr(m.From, vips)
	if p.VIPOnly && !vip {
		return false
	}
	return true
}

func formatNewMailNotice(store Store, accountID string, n int, vipOnly bool) (title, body string) {
	title = "New mail"
	if vipOnly {
		title = "VIP mail"
	}
	body = fmt.Sprintf("%d new message(s)", n)
	if n != 1 || store == nil {
		return title, body
	}
	var inbox Folder
	found := false
	for _, f := range store.ListFolders(accountID) {
		if f.Kind == FolderInbox {
			inbox = f
			found = true
			break
		}
	}
	if !found {
		return title, body
	}
	msgs := store.ListMessages(inbox.ID)
	var latest Message
	ok := false
	for _, m := range msgs {
		if m.Read {
			continue
		}
		if !ok || m.Date.After(latest.Date) {
			latest = m
			ok = true
		}
	}
	if !ok {
		return title, body
	}
	who := strings.TrimSpace(latest.From)
	if who == "" {
		who = "New mail"
	}
	subj := strings.TrimSpace(latest.Subject)
	if subj == "" {
		subj = "(no subject)"
	}
	return who, subj
}

// daemonNotes is the daemon's notifier, for new mail while no window is
// open to tell (the window sends its own; see session.onDaemonEvent).
var daemonNotes = platform.NewNotifier(platform.NotifierOptions{AppName: "Mail", DesktopEntry: "mailclientui"})

func notifyDesktop(title, body string) {
	if os.Getenv("UITK_MAIL_NO_NOTIFY") != "" {
		return
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return
	}
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		title = "Mail"
	}
	if len(body) > 180 {
		body = body[:180] + "…"
	}
	// Off the caller's goroutine: the daemon answers its clients while
	// the desktop is asked.
	go daemonNotes.Send(newMailNotification(title, body, nil))
}

func classifySender(m Message, override map[string]string) string {
	if cat := override[canonAddr(m.From)]; cat != "" {
		return cat
	}
	blob := strings.ToLower(m.Subject + " " + m.From + " " + m.To + " " + m.Body)
	switch {
	case containsAny(blob, "unsubscribe", "newsletter", "sale", "% off", "promo", "deal of", "win a"):
		return CatPromotions
	case containsAny(blob, "invoice", "receipt", "order #", "shipping", "tracking", "payment", "your package"):
		return CatTransactions
	case containsAny(blob, "notification", "digest", "security alert", "github", "gitlab", "build failed", "weekly status"):
		return CatUpdates
	case looksList(m.From) || strings.Contains(strings.ToLower(m.Subject), "[bulk]") || strings.Contains(strings.ToLower(m.Subject), "[promo]"):
		return CatPromotions
	default:
		return CatPrimary
	}
}

func looksList(from string) bool {
	a := canonAddr(from)
	return strings.Contains(a, "list") || strings.Contains(a, "noreply") || strings.Contains(a, "no-reply")
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func messageCategory(m Message, snap featureSnap) string {
	return classifySender(m, snap.cats)
}
