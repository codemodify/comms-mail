# Tray and desktop notifications

The status item and the toast machinery belong to uitoolkit; this page is
only what comms-mail does with them. For the Linux tray itself — KDE SNI,
GNOME AppIndicator, Waybar, HostMenu vs ToolkitMenu, Windows
`Shell_NotifyIconW`, macOS `NSStatusItem` — see uitoolkit's
[tray.md](https://github.com/codemodify/uitoolkit/blob/dev/docs/tray.md).

comms-mail calls only exported toolkit APIs (`app.NewStatusItem`,
`platform.StatusItem`, `platform.Notifier`). It adds nothing of its own to
the platform layer.

## What the UI does

`comms-mail` opens a `StatusItem` for as long as it runs:

- **Click** the tray icon, or the toast, to show, raise and focus the mail
  window, creating it again if it was closed.
- The window manager's **close** button hides to the tray while the item
  is alive. **Quit** on the tray menu really exits.
- The menu is drawn by the desktop (HostMenu, `Menu=/MenuBar`). Toolkit
  chrome instead is `StatusItemOptions.MenuChrome = ToolkitMenu`.
- **New mail** arrives as the daemon's `mail.notify` event
  (`{title, body, count}`) over the socket. The UI raises the desktop
  notification itself, with sender and subject when there is exactly one
  message. The main window does not have to be visible.

## What the daemon does *not* do

`comms-maild` raises no toast. It links no GUI code at all — `go list
-deps ./cmd/comms-maild | grep uitoolkit` is empty — so it only broadcasts
`mail.notify` and lets whichever client is connected show it.

The one seam is in `mailcore`:

```go
// DesktopNotifier, when set, shows one desktop toast.
var DesktopNotifier func(title, body string)
```

`comms-maild` leaves it nil. `comms-mail-demo`, which is a single process
and already links the UI, sets it to `mailui.DesktopNotify`, so a
one-process run still toasts for mail that arrives with no window open.
Anything embedding `mailcore` can fill it the same way.

`UITK_MAIL_NO_NOTIFY` suppresses the core's call into that seam, and the
core skips it when neither `DISPLAY` nor `WAYLAND_DISPLAY` is set. It is a
daemon-side guard: the UI's own toast, raised from the `mail.notify`
event, is governed by Prefs -> Notify instead.

## Testing

```bash
UITK_TRAY=fake tools/testenv.sh go test ./mailui
UITK_TRAY=stub go run ./cmd/comms-mail
UITK_TRAY=fake go run ./cmd/comms-mail
```

Headless and `CGO_ENABLED=0` builds get a stub item, so the tests need no
bus and no tray host.
