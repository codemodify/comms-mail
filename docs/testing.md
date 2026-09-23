# Testing comms-mail

## Mail safety (non-negotiable)

**No test may delete, junk, archive, move, expunge, empty trash, or send
real mail.** The test suite must never touch a live IMAP or POP3 account.

1. Never point a test at a daemon started from the user's
   `~/.config/uitoolkit/mail.json`, production credentials, or any real
   account.
2. UI tests use an in-memory `mailcore.StartDemo` / `MemoryStore`
   fixture, or an isolated temp config that cannot be a real mailbox.
   The default is the fake backend.
3. Do not call the destructive RPCs (`Delete`, `Junk`, `Archive`,
   `Expunge`, Empty Trash, `Move`, `Send`) against a real daemon.
4. `mailcore.IsolateTestEnv` / `IsolateTestEnvTB` point XDG and
   `UITK_MAIL_CONFIG` at a temp dir, force `UITK_MAIL=memory`, and unset
   `UITK_MAIL_HOST` / `_USER` / `_PASS` / `_SOCK`.
   `mailcore.IsDisposableMailSocket` refuses any socket that is not a
   `StartDemo` temp path (a `comms-maild-*` directory).
5. `cmd/comms-mail` run against a daemon you started by hand is **not** a
   test entry point.

## How to run

Run the tests through `tools/testenv.sh`. On a desktop machine a bare
`go test ./...` is not safe: the UI tests reach the real Wayland or X11
display and the real session bus, and open windows on the desktop you are
sitting at.

```bash
tools/testenv.sh go test ./...
```

`testenv.sh` gives the command no `WAYLAND_DISPLAY`, `DISPLAY` or
`WAYLAND_SOCKET`, a private D-Bus session bus with no service directory
(so nothing can be auto-started), and its own `XDG_RUNTIME_DIR`,
`XDG_CONFIG_HOME`, `XDG_CACHE_HOME` and `XDG_DATA_HOME`. Unsetting
`DBUS_SESSION_BUS_ADDRESS` alone is not enough — both godbus and
uitoolkit's `platform` fall back to `$XDG_RUNTIME_DIR/bus`.

The equivalent without the script, if you have no `dbus-daemon`:

```bash
env -u WAYLAND_DISPLAY -u DISPLAY -u WAYLAND_SOCKET \
    -u DBUS_SESSION_BUS_ADDRESS XDG_RUNTIME_DIR=$(mktemp -d) go test ./...
```

## The two packages

`mailcore` has no display or toolkit dependency at all, so its tests are
plain Go tests: protocol, store, MIME, IMAP/POP3/SMTP parsing, path
safety, socket hardening, tags and threading.

`mailui` tests build real widget trees headlessly (measure, arrange,
inject mouse and key events, assert geometry and widget state) against a
`StartDemo` fixture. They do not take screenshots and do not need a
display.

```bash
tools/testenv.sh go test ./mailcore
tools/testenv.sh go test ./mailui
UITK_TRAY=fake tools/testenv.sh go test ./mailui   # exercise the tray path
```

## Checks worth keeping green

```bash
go build ./...

# The daemon must link no GUI code.
go list -deps ./cmd/comms-maild | grep -c uitoolkit   # expect 0

# The app must use only uitoolkit's exported API.
go list -deps ./... | grep uitoolkit | grep internal  # expect no output
```
