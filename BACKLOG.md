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

## Calendar invitations
- **Add to calendar** — there is no calendar here; today the `.ics`
  attachment can be saved and opened in one. A hand-off (xdg-open the
  `.ics`, or CalDAV) would close the loop.
- **Answer without sending / with a comment** — Thunderbird offers "Do not
  send a response" and a note to the organizer; every answer here sends.
- **Proposed new times (`COUNTER`)** — shown, not actionable.
- **Pick the identity** when none of your addresses is on the guest list
  (an invite sent to a mailing list): today the account's default answers
  and is added as a guest.
- **Conflicts** — no "you are busy then" hint (needs a calendar).
- **A failed send is also queued** — if SMTP rejects the reply the error
  shows, but the send path also queues it in the Outbox; pressing the
  button again queues a second reply. Same for any send; worth reconciling.
- **Card height** — on a short window the invite card takes room from the
  body; a collapsible card, or putting it inside the scrolling body, would
  fix it.

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

- **Import contacts / filters** (see below) — account settings and local
  mail import are done; these remain.
- **Import: tests for the client config readers** — Evolution, Claws Mail,
  Geary, mutt and Apple Mail settings parsing has no fixture tests yet (their
  mail discovery goes through the tested generic scanner).
- **Import: keep read/unread** — imported mail is all marked read; maildir
  `:2,S` flags, mbox `Status:` and emlx flags could carry the real state.
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
