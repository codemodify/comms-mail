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

Last checked against the **goframework.io workspace at 3248771**
(2026-10-23), unpublished: comms-mail builds against a checkout at
~/go/src/goframework.io, offline (`GOPROXY=off`), through go.mod replace
lines that `make replace` writes. On that checkout comms-mail builds,
vets and passes all its tests. Its thirteen screenshots match the ones
uitoolkit v0.23.21 drew pixel for pixel, except where the demo's own text
changed and where the version reads "(devel)". On the KDE Plasma desktop
it runs, and its tray item and menu register.

## Open

### 1. The porting guide names comms-mail's old variables
desktop-ui-porting.md:480 says applications that borrowed the prefix
(`UITK_MAIL_*`, `UITK_CHAT_*`) keep it as their own. Lines 504–505 say
comms-mail reads `UITK_MAIL_NO_NOTIFY` and `UITK_MAIL_NO_OPEN` itself.
Since 2026-10-23 comms-mail's variables are `COMMS_MAIL_*`
(`COMMS_MAIL_NO_NOTIFY`, `COMMS_MAIL_NO_OPEN`, `COMMS_MAIL_SOCK`, …), and no
name with UITK is left in it. Nothing else in the workspace sets the old
names (searched the .md, .py, .sh and .go files outside old-*).
**Fix asked for:** the two rows and the sentence name `COMMS_MAIL_*` for
comms-mail, or drop it from the example.

Found 2026-10-23 against 3248771.

## Not gaps

Looked into while porting; working as documented.

- **The guide's go.work and an application with modules from outside
  goframework.** An early, partial go.work failed with
  `goframework.io/app-spec@v0.0.3: module lookup disabled by GOPROXY=off`.
  The guide's whole recipe, including its third line, which copies the
  workspace's replacements (desktop-ui-porting.md, "setting up"), builds
  a module that imports desktop-ui and golang.org/x/text. comms-mail uses
  go.mod replace lines instead, at the owner's choice. That route needs
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
- **No printing** (uitoolkit-gaps.md #52): still none. desktop-ui.md
  lists printing among the things it does not do. comms-mail keeps
  printing through the browser (mailui/print.go).
