# goframework.io gaps found building comms-mail

Gaps and rough edges in goframework.io's desktop-ui (and the modules
beside it) hit while building this mail client, for the framework's
maintainers. Each entry says where it was verified and what would fix it.
New findings are appended under **Open** as they turn up; numbers are
never reused, so a number always means the same gap.

This file takes over from uitoolkit-gaps.md, which ended at uitoolkit
v0.23.21 with nothing open but #52 (no printing, declined and settled).
That file is in git history (last at f8f7cdc); comments and BACKLOG.md
entries that cite "uitoolkit-gaps.md #N" mean that file.

Last checked against the **goframework.io workspace at 065d26a**
(2026-10-23), unpublished; its modules are repositories of their own, and
the fixes below name their commits. comms-mail builds against a checkout at
~/go/src/goframework.io, offline (`GOPROXY=off`), through go.mod replace
lines that `make replace` writes (244 modules). It was ported against
3248771. On 065d26a (todo.md alone after 20c5a06), with #2's fixes in desktop-ui and its modules,
comms-mail builds, vets and passes all its tests (run uncached), and
go.mod's replace block is unchanged.

What was checked:
- Its thirteen offscreen screenshots match uitoolkit v0.23.21's pixel for
  pixel, but for the demo's renamed text and the version, "(devel)".
- On the KDE Plasma desktop it runs, and its tray item and menu register.
- End to end, 2026-10-23, in a nested KWin (`kwin_wayland --virtual` with
  Xwayland, on a session bus of its own, driven through
  tools/desktop-e2e-rig.py's fake input and shot with KWin's ScreenShot2),
  comms-mail-demo on desktop-ui's Wayland backend and again on its X11
  one. With the owner's look.json (Hangar, Titillium Web) and comms-mail's
  own theme (KDE 1), the window draws as uitoolkit v0.23.21's did in the
  same session, which also shows desktop-ui reading uitoolkit's look.json
  while its own is missing. Every check passed on both backends:
  - the tray item registers with a StatusNotifierWatcher; Activate brings
    the window back; its menu (Show Mail, Quit) quits
  - closing the window hides it while the tray shows the icon
  - Fetch's new mail raises a desktop notification (sender, subject, icon,
    Open action), and invoking it opens the window
  - typing; the recipient chips; the clipboard both ways
  - the attach dialog; sending with an attachment
  - dragging a message onto a folder
  - message tabs (E, Ctrl+Tab); a restart brings the tabs back
  - resizing, with the minimum size passed to KWin
  - Settings; the save-draft question on closing a Write window
  - a link in a message: comms-mail's confirmation, then the portal
    handing it to the desktop's browser; Open HTML through xdg-open

## Open

None.

## Fixed

| # | Gap | Fixed in | Change |
| --- | --- | --- | --- |
| 1 | The porting guide named comms-mail's old `UITK_MAIL_*` variables (found against 3248771) | 2c0be7b | desktop-ui-porting.md:481–483 and 506–507 name comms-mail's own `COMMS_MAIL_*`; comms-chat-lan's `UITK_CHAT_*` is the example of an application's own prefix |
| 2 | A selected browser tab's title was drawn in the text colour on the selection fill: black on navy in the `light` pack, desktop-ui's default (found against 67963fa; in uitoolkit v0.23.21 too) | desktop-ui 5cb5da4, desktop-ui-widgets f9f02b9, themes -web f73d124, -win31 854547c, -aqua fa15fe4, -macos 1693554, -hangar 6e13cf5; desktop-ui-tests cb5e001 | `DrawBrowserTabOf` returns the ink of the face it painted and `TabLabelInkOf` answers it; the same fault fixed in seven other widgets; every pack's tabs checked at 4.5:1. Rendered again in a nested KWin on Wayland and X11: comms-mail's Mail tab is white on navy |
| 3 | The e2e rig's KWin screenshot sent its options as `a{sb}`, which KWin refuses (found against 67963fa) | d87921d | the options go out `a{sv}`. The rig's own `shot()`, unpatched, returns the 1600×1200 workspace on Wayland and X11 |

## Not gaps

Looked into while porting; working as documented.

- **The guide's go.work and an application with modules from outside
  goframework.** An early, partial go.work failed with
  `goframework.io/app-spec@v0.0.3: module lookup disabled by GOPROXY=off`.
  The guide's whole recipe, including the `go work edit` step, which
  copies the workspace's replacements (desktop-ui-porting.md:47, "setting
  up"), builds a module that imports desktop-ui and golang.org/x/text.
  comms-mail uses go.mod replace lines instead, by choice. That route needs
  every module of the workspace replaced, 244 of them, not only the six
  comms-mail imports: the modules require one another at v1.0.0 (58
  go.mod files) and v0.0.3 (151).
- **tools/rename writes `v1.0.0`, not v0.0.4.** On purpose: v1.0.0 is the
  placeholder every unreleased module is required at
  (tools/rename/maps/uitoolkit.map:11, the guide's "setting up").
- **`ui.Version()` is "(devel)"** while desktop-ui comes from a replace
  line or a workspace (desktop-ui/version.go:8). comms-mail's status bars,
  Settings' title and About show it. With a release required, they show
  the release.
- **The default theme is `light`, not Plastik**, where there is no
  look.json: desktop-ui-porting.md:213, as documented.
- **A link opened in a bare session starts the desktop's own services.**
  On a session bus of its own, desktop-ui's portal call starts
  xdg-desktop-portal, which starts its KDE Wallet backend (a password
  prompt) and opens the link in the desktop's browser. comms-mail never
  calls the Secret Service there (watched with dbus-monitor). That is
  the portal working; on a desktop session both are already running.
- **No printing** (uitoolkit-gaps.md #52): still none. desktop-ui.md
  lists printing among the things it does not do. comms-mail keeps
  printing through the browser (mailui/print.go).
