# comms-mail polish backlog

Things worth polishing, gathered while building toward a daily driver. Not
blockers — the roadmap features work — but the rough edges and smaller wants
that make the difference in daily use. Grouped, roughly by area. Append as
new ones turn up.

## Reading / HTML
- **CSS background images and SVG** are not drawn (the renderer handles
  `<img>` in PNG / JPEG / GIF / WebP only).
- **Emoji, Greek, Cyrillic and CJK show as ▯** — the toolkit has no font
  fallback and has declined it for good (uitoolkit-gaps.md #2): comms-mail
  has to plan around it. Very visible in subjects and bodies.
- **Tables whose cells wrap can overlap** in the Markdown view — at some
  pane widths the next row is drawn over a wrapped row's second line
  (uitoolkit-gaps.md #32).

## Calendar invitations
- **Conflicts** — no "you are busy then" hint: comms-mail has no calendar
  to check. It could read the desktop's (Evolution Data Server over D-Bus,
  or a CalDAV account) — a feature of its own.
- **Accept a proposed time** — declining a guest's COUNTER works; taking
  it needs the event (all guests, to send them the update), which lives in
  the calendar application.

## Compose
- **Spell check.**
- **HTML compose** — the editor is plain text only; replying to HTML loses
  its formatting.

## Folders / search

- **Import: Apple Mail accounts on 10.11+ are best-effort** — read from
  `Accounts4.sqlite` by the published layout, not yet tried on a real Mac
  (which property holds the port, how Google / iCloud accounts nest).
- **Import: KMail filters with Akonadi on MySQL — untried on a real
  Akonadi** — read over its socket with go-sql-driver/mysql; the socket
  lookup and the shared queries are tested (the queries through SQLite),
  the connection itself only against no server. PostgreSQL is not read.
- **Import: Outlook (.pst)** — PINNED by the user; not until unpinned.

## Offline / sync
- **IDLE for every folder** — Inbox, Sent and the folder showing are
  watched; the rest poll every 2 minutes (one connection a folder would
  pass servers' per-user connection limits; NOTIFY, RFC 5465, would not).


## Storage / perf
- **Every message is held in memory** by the daemon, text included (the
  database is where it persists): fine for thousands, heavy for hundreds of
  thousands. Loading the text from `mail.db` on demand would fix it.

## Icons
- **A few rows still without an icon** — Compact Folder, Select All,
  Body as plain text and a filter's Turn Off: no toolkit icon means what
  they do (Run Now, Unlock and an invitation's More / Less got theirs
  from uitoolkit 0.23). Buttons with icons are wide (uitoolkit-gaps.md
  #29), so their rows fold in narrow windows.

## Layout
- **Invite card at the minimum window size** — the reading pane's header
  scrolls when it would leave the body under ~200 px, so at 860×560 the
  card's buttons are a scroll away (every control stays reachable; a
  test checks each window at its minimum size).

## Accounts / setup / trust
- **OAuth (Gmail / Microsoft 365)** — decided 2026-09-28: the owner's own
  client ID while comms-mail has one user (works today). A built-in
  registration only when others use it (Google verification + CASA).
- **secretvault** — codemodify/secretvault is listed as a place for
  passwords but has no API yet; `TODO(secretvault)` in
  `mailcore/secrets.go` is where it plugs in.
- **Keyring on macOS and Windows** — written (Keychain through the
  security tool, Credential Manager through advapi32) and compiled, not
  yet run on those systems.

## Security (decided 2026-09-28, docs/security/security-primer.md)
- **Sender warnings** — `Authentication-Results` (SPF/DKIM/DMARC) failures,
  look-alike display names, Reply-To on another domain; "Always show
  images" only for senders who passed.
- **Recognise and check signed / encrypted mail** — PGP and S/MIME both;
  encrypted mail says what it is (an S/MIME `smime.p7m` shows as binary
  text today).
- **PGP** — built in (Proton `go-crypto`): keys, decrypt, sign/encrypt on
  send, private keys in the vault.
- **S/MIME** — built in, own CMS code: `.p12` import, decrypt, sign/encrypt
  on send.
- **Search inside encrypted mail** — a setting, off by default.
- **Passphrase fields that never hold a string** — uitoolkit 0.22 has a
  `SecretField` (a wiped byte buffer, no copy, input method off), a
  secret clipboard, and Caps Lock state. The passphrase and account
  password fields could use it; the passphrase still crosses the socket
  to comms-maild as JSON text, so the protocol would carry bytes too.


