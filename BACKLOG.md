# comms-mail polish backlog

Things worth polishing, gathered while building toward a daily driver. Not
blockers — the roadmap features work — but the rough edges and smaller wants
that make the difference in daily use. Grouped, roughly by area. Append as
new ones turn up.

## Reading / HTML
- **Load remote images on request**, with a per-sender "always show". Real
  newsletters are image-heavy and unreadable with images blocked (renders as
  a wall of empty boxes). High value. Needs a daemon fetch (so the request
  comes from the daemon, on the user's say-so) + a remember-per-sender store.
- **Inline `cid:` images** — resolve from the message's own parts (safe, no
  network). Needs the MIME parser to record Content-ID and a fetch-by-cid.
- **Emoji show as ▯** (tofu) — blocked on a uitoolkit font-fallback gap
  (uitoolkit-gaps.md #2), but very visible in subjects and bodies.
- **HTML tab only when there is HTML** (or auto-select it for HTML-only
  mail) — needs TabView per-tab hide (uitoolkit-gaps.md #6).
- **Opened message tabs are text-only** — no HTML view when a message is
  opened in its own tab.

## Compose
- **Draft autosave** — a crash mid-compose loses the message.
- **Chip/pill recipient fields** — removable recipient pills (uitoolkit
  gap #5); today they are comma-separated text.
- **Spell check.**
- **Drag files into the body to attach.**
- **HTML compose** — the editor is plain text only; replying to HTML loses
  its formatting.

## Folders / search
- **Rename folder** — needs sync to match folders by their server path
  (Remote) rather than a name-derived id, or a renamed folder is duplicated
  on the next sync.
- **Server-side search** — find mail not yet downloaded (IMAP `UID SEARCH`),
  fetching envelopes for hits. Today's search covers synced headers +
  downloaded bodies only.
- **Show the folder of each search result** — a folder column in search /
  unified views.
- **Create a subfolder** — create is always a top-level "New Folder N".
- **Compact folders** — `EXPUNGE` deleted messages (the menu stub was
  removed).

## Offline / sync
- **Offline tag changes are dropped** — only read/starred are queued and
  replayed; keyword (tag) changes made offline are lost.
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

## Accounts / setup / trust
- **OAuth (Gmail / Microsoft 365)** — needs the client-id decision (own
  registration vs a project app) before it is usable.
- **Start the daemon at login** — a systemd user unit.
- **A log file** — for diagnosing issues in daily use.
- **Print / Save as PDF / Save as .eml.**

## From the roadmap (features, not polish)
Thunderbird / KMail import · calendar invites · PGP · S/MIME.
