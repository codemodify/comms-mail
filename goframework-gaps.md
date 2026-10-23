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

Last checked against the **goframework.io workspace at 2c0be7b**
(2026-10-23 18:58), unpublished. comms-mail builds against a checkout at
~/go/src/goframework.io, offline (`GOPROXY=off`), through go.mod replace
lines that `make replace` writes (244 modules, unchanged at 2c0be7b). It
was ported against 3248771. The commits since then change only documents
and the site's tools, no module's Go code or go.mod. On 85a3be7
comms-mail builds, vets and passes all its tests (run uncached); 2c0be7b
changes only the porting guide. Its thirteen screenshots match the ones
uitoolkit v0.23.21 drew pixel for pixel, except where the demo's own text
changed and where the version reads "(devel)". On the KDE Plasma desktop
it runs, and its tray item and menu register.

## Open

None.

## Fixed

| # | Gap | Fixed in | Change |
| --- | --- | --- | --- |
| 1 | The porting guide named comms-mail's old `UITK_MAIL_*` variables (found against 3248771) | 2c0be7b | desktop-ui-porting.md:481–483 and 506–507 name comms-mail's own `COMMS_MAIL_*`; comms-chat-lan's `UITK_CHAT_*` is the example of an application's own prefix |

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
- **No printing** (uitoolkit-gaps.md #52): still none. desktop-ui.md
  lists printing among the things it does not do. comms-mail keeps
  printing through the browser (mailui/print.go).
