# comms-mail polish backlog

Things worth polishing, gathered while building toward a daily driver. Not
blockers — the roadmap features work — but the rough edges and smaller wants
that make the difference in daily use. Grouped, roughly by area. Append as
new ones turn up.

## Reading / HTML
- **CSS background images and SVG** are not drawn (the renderer handles
  `<img>` in PNG / JPEG / GIF / WebP only).
- **Emoji show as ▯** (tofu) — blocked on a uitoolkit font-fallback gap
  (uitoolkit-gaps.md #2), but very visible in subjects and bodies.
- **HTML tab only when there is HTML** (or auto-select it for HTML-only
  mail) — needs TabView per-tab hide (uitoolkit-gaps.md #6).

## Calendar invitations
- **Conflicts** — no "you are busy then" hint: comms-mail has no calendar
  to check. It could read the desktop's (Evolution Data Server over D-Bus,
  or a CalDAV account) — a feature of its own.
- **Accept a proposed time** — declining a guest's COUNTER works; taking
  it needs the event (all guests, to send them the update), which lives in
  the calendar application.

## Compose
- **Chip/pill recipient fields** — removable recipient pills (uitoolkit
  gap #5); today they are comma-separated text.
- **Spell check.**
- **Drag files into the body to attach** — files dropped anywhere else on
  the Write window are attached; on the body the text area takes the
  paths as text first (uitoolkit-gaps.md #13).
- **HTML compose** — the editor is plain text only; replying to HTML loses
  its formatting.

## Folders / search
- **Drag a folder onto another** to move it — Move Folder To does it from
  the menu; the tree has no drag for its own nodes (uitoolkit).
- **Messages marked deleted by other clients** are listed until Compact
  Folder removes them; they could be hidden, or shown struck through.
- **Gmail search syntax** — On server uses IMAP TEXT; Gmail's `X-GM-RAW`
  would allow its own operators (`has:attachment`, `older_than:`).

- **Import contacts / filters** (see below) — account settings and local
  mail import are done; these remain.
- **Import: Apple Mail on macOS 10.11+** — accounts live in
  `~/Library/Accounts/Accounts4.sqlite` (NSKeyedArchiver blobs), not read;
  the import says to add them by hand. Evolution's `_2E`-escaped Maildir++
  folder names are shown escaped.
- **Import: answered / forwarded state** — read and starred carry over;
  replied / forwarded (maildir R/P, mbox X-Status A, emlx bits 2/8) have no
  field here yet.
- **Import: Outlook (.pst)** — PINNED by the user; not until unpinned.
- **Import contacts** — Thunderbird address book (abook.sqlite / .mab) and
  KMail/KAddressBook vCards, to seed the address book beyond what the cached
  messages give.
- **Import filters** — Thunderbird message filters / KMail filters into the
  Sorting Office rules.

## Offline / sync
- **IDLE watches only Inbox + Sent** — other folders poll every 2 minutes;
  push for all folders (or the selected one) would feel instant.

- **Filter-rule tags stay local** — a tag a filter rule adds is not pushed
  to the server (other clients do not see it); it is kept here through
  syncs.

## Message list / actions
- **Undo for tag changes** — Undo covers move/delete/archive/junk, not
  tagging.
- **Bulk-action progress** — feedback for a move/delete over a large
  selection.

## Storage / perf
- **List payloads carry the text body** for downloaded messages; a
  headers-only list query would shrink them on large folders.
- **Search index is in-memory**, rebuilt on load; SQLite FTS would scale
  further and persist.

## Layout
- **Invite card at the minimum window size** — the reading pane's header
  scrolls when it would leave the body under ~200 px, so at 860×560 the
  card's buttons are a scroll away (every control stays reachable; a
  test checks each window at its minimum size).

## Accounts / setup / trust
- **OAuth (Gmail / Microsoft 365)** — needs the client-id decision (own
  registration vs a project app) before it is usable.

## From the roadmap (features, not polish)
Thunderbird / KMail import · calendar invites · PGP · S/MIME.
