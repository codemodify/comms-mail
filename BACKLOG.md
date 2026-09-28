# comms-mail polish backlog

Things worth polishing, gathered while building toward a daily driver. Not
blockers — the roadmap features work — but the rough edges and smaller wants
that make the difference in daily use. Grouped, roughly by area. Append as
new ones turn up.

## Reading / HTML
- **Manage trusted image senders** — "Always from This Sender" can be
  given but not taken back in the UI (the daemon's `images.allowSender`
  with allow=false does it); a list in Settings would.
- **CSS background images and SVG** are not drawn (the renderer handles
  `<img>` in PNG / JPEG / GIF / WebP only).
- **Emoji show as ▯** (tofu) — blocked on a uitoolkit font-fallback gap
  (uitoolkit-gaps.md #2), but very visible in subjects and bodies.
- **HTML tab only when there is HTML** (or auto-select it for HTML-only
  mail) — needs TabView per-tab hide (uitoolkit-gaps.md #6).
- **Opened message tabs are text-only** — no HTML view when a message is
  opened in its own tab.

## Calendar invitations
- **Conflicts** — no "you are busy then" hint: comms-mail has no calendar
  to check. It could read the desktop's (Evolution Data Server over D-Bus,
  or a CalDAV account) — a feature of its own.
- **Accept a proposed time** — declining a guest's COUNTER works; taking
  it needs the event (all guests, to send them the update), which lives in
  the calendar application.
- **Remember Less / More** across restarts (it lasts the session).

## Compose
- **Drafts saved offline stay local** — a draft saved (or autosaved) while
  offline is kept in the cache but not queued for the server's Drafts; it
  reaches the server only if saved again once online after a sync.
- **Chip/pill recipient fields** — removable recipient pills (uitoolkit
  gap #5); today they are comma-separated text.
- **Spell check.**
- **Drag files into the body to attach.**
- **HTML compose** — the editor is plain text only; replying to HTML loses
  its formatting.

## Folders / search
- **Folder hierarchy from the server** — folders synced from IMAP are
  listed flat (Parent is only set for folders made here); nesting them by
  the server's delimiter would show `Archives/2023` under `Archives`.
- **Move a folder** (drag it onto another) — rename covers the name only.
- **Gmail search syntax** — On server uses IMAP TEXT; Gmail's `X-GM-RAW`
  would allow its own operators (`has:attachment`, `older_than:`).
- **Show the folder of each search result** — a folder column in search /
  unified views.
- **Compact folders** — `EXPUNGE` deleted messages (the menu stub was
  removed).

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
- **Tags set in other clients don't sync down** — a message's keywords are
  read when it is first fetched; later keyword changes made elsewhere are
  not merged (read/starred are). Merging needs care: a server without
  custom keywords (no `\*` in PERMANENTFLAGS) would wipe local tags.
- **Offline mark-folder-read reverts** — done offline it is not queued, so
  the next flag sync sets the messages unread again.
- **IDLE watches only Inbox + Sent** — other folders poll every 2 minutes;
  push for all folders (or the selected one) would feel instant.

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
- **Audit windows for min-size clipping** — the Settings Accounts tab clipped its buttons at small sizes (fixed); other dialogs (compose, add-account, import) should be checked, or long fixed headers made scrollable, so controls are never below the fold.

## Accounts / setup / trust
- **OAuth (Gmail / Microsoft 365)** — needs the client-id decision (own
  registration vs a project app) before it is usable.
- **Start the daemon at login** — a systemd user unit.
- **A log file** — for diagnosing issues in daily use.
- **Print / Save as PDF / Save as .eml.**

## From the roadmap (features, not polish)
Thunderbird / KMail import · calendar invites · PGP · S/MIME.
