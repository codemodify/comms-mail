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
- **secretvault** — passwords and OAuth sign-ins can be kept there (done
  2026-10-03): comms-maild speaks its socket protocol, links none of its
  code, and needs nothing published. Next: reading signed / encrypted mail
  (`mail.inspect`), then Sign / Encrypt when writing (`mail.compose`),
  then keys in Settings. comms-mail uses secretvault's *default* vault; a
  setting for another is not there yet.
- **Edit account: Test connection** — with the password left empty it
  checks only that the server answers; testing with the saved password
  needs the daemon to probe by account id.
- **Keyring on macOS and Windows** — written (Keychain through the
  security tool, Credential Manager through advapi32) and compiled, not
  yet run on those systems.

## Security (decided 2026-09-28, revised 2026-10-03 and 2026-10-04; docs/security/security-primer.md)
- **Sender warnings** — done 2026-10-03 (docs/mail.md, Who sent it). Left:
  the message list still shows only the display name and has no mark for
  a failed check or a look-alike. The topmost `Authentication-Results`
  counts only when your provider's server wrote it — done 2026-10-05; a
  provider not told by its servers' names, the alias table, the delivering
  server or the account's usual checker has no "trust this server" button
  yet.
- **Security in the reading pane** (asked 2026-10-05: everything we can
  say of a message's security) — chips under From and a Security tab, done
  2026-10-05 for what was already worked out (docs/mail.md, Security at a
  glance), and the way it came from the `Received` headers (each hop, TLS
  and cipher where noted, times; done 2026-10-05). Next, in this order:
  content checks (links whose
  text names another domain, IP / look-alike / punycode links, tracking
  pixels and the hosts the HTML would reach, risky attachments, read
  receipts asked, forms); the sender domain's DMARC policy, MTA-STS, DANE,
  BIMI and DNSSEC looked up (opt-in), your own connection's TLS; whom it
  was encrypted to (asked of secretvault, below; comms-mail's own engine
  can say it itself).
- **Reading and sending signed / encrypted mail** — done 2026-10-03
  (docs/mail.md, Signed and encrypted mail). Left: attachments *inside* an
  encrypted message are not listed or opened yet, nor carried by a
  forward of one; Autocrypt keys reach secretvault only when a message is
  opened, not as it arrives; the list has no signed / encrypted mark; an
  encrypted message cannot have Bcc recipients; the Write window cannot
  say before Send which recipients lack a key (asked of secretvault).
- **PGP and S/MIME** — each format's keys kept where chosen in Settings ›
  Security › Keys (decided 2026-10-04, done the same day; the 2026-10-03
  "secretvault only" now the default): in Secret Vault, secretvault keeps
  them and does the work, in the vault chosen for them; in the keyring,
  the encrypted file or a plain file, comms-mail does it — OpenPGP with
  ProtonMail's go-crypto, S/MIME with our own CMS (`internal/cms`,
  standard library only). secretvault no longer needs to keep the
  passwords to do the crypto: its daemon running is enough. Left for the built-in engine: no key lookup beyond mail and
  files (WKD, keyservers, LDAP); certificate revocation (CRL, OCSP) is not
  checked; no key expiry or renewal reminders; one OpenPGP key per
  address is used (the newest); attachments inside an encrypted message
  are, as with secretvault, not listed yet.
- **Asked of secretvault** — S/MIME roots beyond an organisation's own CA
  (a public CA's certificate does not verify as trusted today); a check
  before Send of which recipients can be encrypted to; an Autocrypt header
  on outgoing mail; encrypting to yourself only (for drafts). Publishing
  it is not needed: comms-mail talks to its daemon, not its code.
  For the Security tab (2026-10-05): `mail.inspect` already reports each
  encrypted layer's recipients, the key that opened it, its cipher and
  integrity, each signature's time, hash and algorithm, the signing
  subkey, and an S/MIME certificate's issuer, serial, dates and chain —
  comms-mail is to read them (it reads a few today). Asked of it: the
  signing key's size and its creation and expiry dates, and a
  certificate's key algorithm; S/MIME revocation (CRL, OCSP) and the
  system's roots (both on its roadmap); refreshing contacts' keys from
  WKD or keyservers, so a revocation reaches them. Already there, to use:
  `contact.lookup` (LDAP, WKD, keyserver) for a signer whose key you do
  not have.
- **Keys from one secretvault vault to another** — done 2026-10-05:
  secretvault answered with `item.move` (its b8a5f52), and Settings ›
  Security › Keys moves a format's keys with it when another of its vaults
  is chosen (docs/mail.md). A secretvault from before it says to update.
- **Drafts of encrypted mail** — kept on this machine only, never uploaded
  to the server's Drafts (decided 2026-10-03).
- **Signing** — on by default whenever there is a key for the From
  address (decided 2026-10-03).
- **Search inside encrypted mail** — a setting, off by default: decrypted
  text stays out of mail.db and the search index unless it is on.
- **Passphrase fields that never hold a string** — uitoolkit 0.22 has a
  `SecretField` (a wiped byte buffer, no copy, input method off), a
  secret clipboard, and Caps Lock state. The passphrase and account
  password fields could use it; the passphrase still crosses the socket
  to comms-maild as JSON text, so the protocol would carry bytes too.


