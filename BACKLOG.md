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

- **Import: Apple Mail on macOS 10.11+** — accounts live in
  `~/Library/Accounts/Accounts4.sqlite` (NSKeyedArchiver blobs), not read;
  the import says to add them by hand.
- **Replied / forwarded in the message list** — both are tracked and the
  reading pane says so; the list has no mark for them (the UI font has no
  ↩ / ↪ glyph and the toolkit draws no stand-in — uitoolkit-gaps.md #2).
- **Import: KMail filters and Apple Mail contacts** — Thunderbird's filters
  and the address books of Thunderbird, Evolution, KAddressBook, Claws Mail
  and mutt import; these two do not yet.
- **Import: Outlook (.pst)** — PINNED by the user; not until unpinned.

## Offline / sync
- **IDLE for every folder** — Inbox, Sent and the folder showing are
  watched; the rest poll every 2 minutes (one connection a folder would
  pass servers' per-user connection limits; NOTIFY, RFC 5465, would not).


## Storage / perf
- **Every message is held in memory** by the daemon, text included (the
  database is where it persists): fine for thousands, heavy for hundreds of
  thousands. Loading the text from `mail.db` on demand would fix it.

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
