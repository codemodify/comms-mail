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

- **Import: Apple Mail accounts on 10.11+ are best-effort** — read from
  `Accounts4.sqlite` by the published layout, not yet tried on a real Mac
  (which property holds the port, how Google / iCloud accounts nest).
- **Replied / forwarded in the message list** — both are tracked and the
  reading pane says so; the list has no mark for them (the UI font has no
  ↩ / ↪ glyph and the toolkit draws no stand-in — uitoolkit-gaps.md #2).
- **Import: KMail filters with Akonadi on MySQL** — their folders and tags
  are ids in Akonadi's database; only SQLite is read, so with MySQL (the
  default before KDE Gear 26.04) filters that move or tag are left out.
- **Rules on any header** — a rule tests From, To/Cc, Subject and the body;
  imported filters on `List-Id` and other headers (common in KMail) are left
  out.
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
- **OAuth (Gmail / Microsoft 365)** — decided 2026-09-28: the owner's own
  client ID while comms-mail has one user (works today). A built-in
  registration only when others use it (Google verification + CASA).
- **Keyring** — the owner is building their own; it plugs in where
  `TODO(keyring)` is in `mailcore/vault.go` to unlock without a typed
  passphrase.

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


