# comms-mail — comms-maild + comms-mail

Thunderbird-chrome mail client built on
[uitoolkit](https://github.com/codemodify/uitoolkit). Two processes:

| Process | Role |
| --- | --- |
| **comms-maild** | Owns accounts, IMAP/POP3/SMTP, OAuth tokens, local cache, folders, messages, tags, filters, identities, smart folders, VIP, categories, outbox, search, mutations. |
| **comms-mail** | Renders chrome, owns the **status item / tray**, and sends JSON-RPC commands. **No IMAP, POP3, SMTP, or OAuth HTTP** in this process. |

Shared types and the RPC client and server live in
[`mailcore`](../mailcore), which links no GUI code; the window, dialogs
and tray live in [`mailui`](../mailui).

## What changed in the hardening pass (read this if you are upgrading)

| Change | What you may notice |
| --- | --- |
| Socket is `0600` in a `0700` dir, same-uid only, lock-file guarded | A second `comms-maild` on the same socket now **fails to start** instead of taking over |
| One fail-closed `tlsMode` (`ssl` / `starttls` / `plain`) | A `starttls` server that stops offering STARTTLS is now an **error**, not a silent cleartext login. `"tls": true` on port 143/110/587 now upgrades instead of going cleartext |
| No plaintext credentials to a remote host | An account deliberately on a custom cleartext port must say `"tlsMode": "plain"`, and even then only loopback will authenticate |
| `compose.send` takes attachment **bytes** | `attachPaths` is gone from the wire; the daemon no longer opens client-supplied paths. `Client.SendIdent` still takes paths and reads them UI-side |
| OAuth master key derivation fixed | Existing `secrets/*.tok` written by a build **with libsecret present** cannot be read (they never could). Delete `secrets/` and sign in again |
| `Bcc` is no longer written into the message | Blind recipients still receive it; they are just no longer disclosed |
| Moves re-key on `COPYUID` | Cache entries change id after a move; a server without UIDPLUS drops the entry until the next sync |
| Deletion reconciliation without QRESYNC | Messages deleted elsewhere finally disappear from the cache |
| Folder / message ids are sanitised | Cached `meta/` filenames changed, so the first sync after upgrading re-reads folder metadata |

## How to start

Default socket:

- `$UITK_MAIL_SOCK` if set
- else `$XDG_RUNTIME_DIR/comms-maild.sock`
- else `/tmp/comms-maild-<uid>.sock`

**The socket is the daemon's only authorisation boundary**, so it is locked
down: the containing directory is created `0700`, the socket itself is
`chmod 0600` immediately after bind, and every accepted connection must come
from the same uid (`SO_PEERCRED`; root is also allowed). A `<socket>.lock`
file means a second `comms-maild` refuses to start rather than silently
stealing the path from the running one — if you see
`another daemon is already serving …`, one is already up.

```bash
# Terminal 1 — daemon (empty until you add an account)
go run ./cmd/comms-maild

# Terminal 2 — Thunderbird UI (first-run Yes/No if no accounts)
UITK_SCENE=auto go run ./cmd/comms-mail
go run ./cmd/comms-mail -classic -light
go run ./cmd/comms-mail -headless    # writes mail.png

# Seeded MemoryStore dogfood / screenshots only:
UITK_MAIL=memory go run ./cmd/comms-maild
```

Convenience (same architecture, one process: daemon goroutine + UI client on a temp socket):

```bash
UITK_SCENE=auto go run ./cmd/comms-mail-demo
go run ./cmd/comms-mail-demo -screenshot docs/screenshots
```

## Tray and new-mail toasts (0.16.0, 0.16.1)

`comms-mail` opens a toolkit `StatusItem` while it runs
(v0.17.0 / v0.18.1: **HostMenu** — Plasma/AppIndicator draw the native
menu from `Menu=/MenuBar`. Left-click / notify-click still raise the
window. Middle-click does not.
Toolkit chrome is `StatusItemOptions.MenuChrome = ToolkitMenu`.
v0.16.4: `Menu=/` + toolkit popup was unreliable on Plasma.
v0.16.2: envelope tray icon; Wayland Show Mail remaps after
close-to-tray. v0.16.1: Plasma `GetLayout` no longer panics):

- **Click** the tray (or the toast) → show / raise / focus the Mail window
  (create it if it was closed). The window-manager close button **hides**
  to the tray while the item is alive; **Quit** on the tray menu exits.
- **New mail** → daemon broadcasts `mail.notify` `{title, body, count}`.
  The UI shows a desktop notification (sender + subject when there is one
  message). The main window does not need to be visible.
- The **daemon raises no toast of its own**. `comms-maild` links no GUI
  code at all (see [the daemon/UI split](#the-daemonui-split)), so the
  `mail.notify` event is the whole story: whoever is connected shows it.
  `comms-mail-demo`, which is one process and already links the UI, fills
  the `mailcore.DesktopNotifier` seam with `mailui.DesktopNotify`, so a
  one-process run toasts as before. Prefs → Notify still gates `Enabled`
  and VIP-only.

Linux tray hosts: see uitoolkit's
[tray.md](https://github.com/codemodify/uitoolkit/blob/dev/docs/tray.md)
(KDE SNI, GNOME AppIndicator, Waybar, …). Headless / `CGO_ENABLED=0`
tests use a stub or `UITK_TRAY=fake`.

## The daemon/UI split

| Package | Contains | Links uitoolkit |
| --- | --- | --- |
| [`mailcore`](../mailcore) | `Store` and its backends, accounts, folders, MIME, IMAP, POP3, SMTP, OAuth, the JSON-RPC protocol and the socket server | **no** |
| [`mailui`](../mailui) | the three-pane window, compose / account / preferences / filter dialogs, the tray item, the screenshot harness | yes |

`go list -deps ./cmd/comms-maild | grep uitoolkit` is empty: the daemon
links no widget set, no font stack and no paint engine. `mailcore` has
exactly one seam pointing at a desktop:

```go
// nil in comms-maild; comms-mail-demo sets it to mailui.DesktopNotify.
var DesktopNotifier func(title, body string)
```

## Real IMAP or POP3 + SMTP (primary path)

`comms-maild` is meant to be pointed at a real account. **Add Account** takes a typed (masked) password and writes it into `mail.json`. That file is **mode `0600`**. The password field is **temporary plaintext** until a secret store exists. OAuth (encrypted refresh token) and optional `passEnv` / `UITK_MAIL_PASS` still work when `password` is empty.

### Connection security (`tlsMode`)

Each server block resolves to exactly one mode, and the client obeys it
**fail-closed** — it never downgrades:

| `tlsMode` | Meaning | Usual ports |
| --- | --- | --- |
| `ssl` | TLS from the first byte | 993 / 995 / 465 |
| `starttls` | cleartext connect, then a **mandatory** upgrade | 143 / 110 / 587 |
| `plain` | no TLS — opt-in only, never a fallback | custom |

- If a `starttls` server does not advertise STARTTLS/STLS the connection is
  **aborted**; it is never continued in the clear. (An SMTP server that drops
  `250-STARTTLS` used to get a cleartext `AUTH`.)
- Credentials are never sent over an unencrypted connection to a non-loopback
  host — neither by the accounts, nor by **Test connection**.
- Certificate verification is always on. There is no switch to disable it.

The older `"tls"` / `"starttls"` booleans are still read. They now map
fail-closed: `"tls": true` on a STARTTLS port (143/110/587) **upgrades**
instead of producing the silent cleartext session it used to. Omit both and
the port decides; an unrecognised port defaults to `ssl` for IMAP/POP3 and
`starttls` for SMTP.

Config file (first existing wins):

- `$UITK_MAIL_CONFIG`
- `$XDG_CONFIG_HOME/uitoolkit/mail.json`
- `~/.config/uitoolkit/mail.json`

Cache / offline store:

- `$UITK_MAIL_DATA`
- `$XDG_DATA_HOME/uitoolkit/mail`
- `~/.local/share/uitoolkit/mail`

Example `mail.json` (written mode `0600`; `password` is temporary plaintext):

```json
{
  "accounts": [
    {
      "id": "home",
      "name": "Ada Lovelace",
      "address": "ada@example.com",
      "protocol": "imap",
      "imap": {
        "host": "imap.example.com:993",
        "user": "ada@example.com",
        "password": "your-app-password",
        "tlsMode": "ssl"
      },
      "smtp": {
        "host": "smtp.example.com:587",
        "user": "ada@example.com",
        "password": "your-app-password",
        "tlsMode": "starttls"
      }
    }
  ]
}
```

```bash
# File → Add Account, type the password, Save account
go run ./cmd/comms-maild
go run ./cmd/comms-mail
```

Optional env fallback when `password` is omitted: `"passEnv": "UITK_MAIL_PASS"` and `export UITK_MAIL_PASS='…'`.

Single-account env (no file) still works:

```bash
export UITK_MAIL=imap
export UITK_MAIL_HOST=imap.example.com:993
export UITK_MAIL_USER=you@example.com
export UITK_MAIL_PASS=secret
export UITK_MAIL_SMTP=smtp.example.com:587   # optional; guessed from IMAP host
export UITK_MAIL_NAME='Ada Lovelace'
go run ./cmd/comms-maild
```

POP3 account (inbox retrieve; SMTP still used to send):

```json
{
  "id": "home-pop",
  "name": "Ada Lovelace",
  "address": "ada@example.com",
  "protocol": "pop3",
  "pop": {
    "host": "pop.example.com:995",
    "user": "ada@example.com",
    "password": "your-app-password",
    "tlsMode": "ssl"
  },
  "smtp": {
    "host": "smtp.example.com:587",
    "user": "ada@example.com",
    "password": "your-app-password",
    "tlsMode": "starttls"
  }
}
```

Default when **no** config and `UITK_MAIL` is unset: **empty** LocalStore (no demo accounts). The UI asks *There are no accounts, want to add one?* Yes opens File → Add Account. No leaves empty chrome. Password / `mail.json` mode `0600` notes live on the Add Account form, not that Yes/No dialog.

`UITK_MAIL=memory` is the **only** way to load the seeded MemoryStore demo (and `comms-mail-demo` and the screenshots still use that on purpose).

### Add Account polish

The wizard has an explicit **IMAP** / **POP3** choice (default IMAP). It guesses hosts from the email domain (`gmail.com` → `imap.gmail.com:993` / `pop.gmail.com:995` / `smtp.gmail.com:465`, Outlook/Hotmail, Yahoo, iCloud, Fastmail, Proton, else `imap.<domain>:993` / `pop.<domain>:995` / `smtp.<domain>:587`). You can still edit the hosts.

**Test connection** dials the typed user/password and reports success or failure (which protocol worked, `host:port`, TLS mode: `ssl` / `starttls` / `plain`). After email + password are entered, a background auto-detect may also probe common `imap.` / `pop.` / `mail.` names on 993/143/995/110 (timeout + status line; it does not freeze the form).

Type the incoming/SMTP password (masked field); **Save account** writes `protocol` plus `password` into `mail.json` (file mode `0600`). `passEnv` is optional fallback only. Account Central and Preferences → Accounts show **IMAP** or **POP3** after save.

### Text-only message view

The Message tab is **plain text**. comms-maild prefers the `text/plain` part; if the message is HTML-only, tags are stripped (`HTMLToText`). There is no HTML engine and no HTML tab. **Ctrl+U** (also the message context menu) opens a read-only JetBrains Mono window of the stored RFC822 (`messages.getSource`). The Source tab in the preview pane uses the same daemon bytes — not a reconstructed header dump.

## OAuth (Google + Microsoft)

Real **authorization-code + PKCE loopback** (`http://127.0.0.1:<port>/oauth/callback`) or **device code** flow. IMAP/SMTP then use **AUTH XOAUTH2**. The `UITK_MAIL_XOAUTH2` bearer passthrough, inline `password`, and `passEnv` / `UITK_MAIL_PASS` still work.

The loopback callback carries an unguessable **`state`** nonce and only the
matching callback is accepted (and only once). Any local process — or a web
page the browser is pointed at — can reach `127.0.0.1:<port>`, so without it
an attacker could complete the flow with their own code and attach their
mailbox to the account. Provider error text echoed into the browser tab is
HTML-escaped and served as `text/plain`.

**Honest gap:** uitoolkit does **not** ship Google or Microsoft client IDs. You must register an app and supply credentials:

| Provider | Register | Env (or wizard fields) |
| --- | --- | --- |
| Google | [Google Cloud Console](https://console.cloud.google.com/apis/credentials) — OAuth client (Desktop or Web). Redirect: `http://127.0.0.1:<port>/oauth/callback`. Scope: `https://mail.google.com/` | `UITK_MAIL_OAUTH_GOOGLE_CLIENT_ID`, `UITK_MAIL_OAUTH_GOOGLE_CLIENT_SECRET` |
| Microsoft | [Azure AD app registration](https://portal.azure.com/) — public client is fine. Redirect same loopback URL, or device code (no redirect). Scopes: `offline_access`, `https://outlook.office.com/IMAP.AccessAsUser.All`, `https://outlook.office.com/SMTP.Send` | `UITK_MAIL_OAUTH_MS_CLIENT_ID`, `UITK_MAIL_OAUTH_MS_CLIENT_SECRET` (secret optional for public clients) |

```bash
export UITK_MAIL_OAUTH_GOOGLE_CLIENT_ID='….apps.googleusercontent.com'
export UITK_MAIL_OAUTH_GOOGLE_CLIENT_SECRET='…'   # if the client is confidential
go run ./cmd/comms-maild
# UI: File → Add Account → Sign in with Google
```

Device flow: **Device code…** on the same dialog (useful when loopback cannot bind or the app only allows device).

### Token storage

Refresh/access tokens are **never** written to `mail.json`. They live under `$XDG_DATA_HOME/uitoolkit/mail/secrets/` (or `$UITK_MAIL_DATA/secrets/`):

- `*.tok` — AES-256-GCM (random nonce prefix), mode `0600`
- `master.key` — the 32-byte key as **hex**, mode `0600`
- If `secret-tool` is on `PATH` the same hex string is also stored as
  `service=uitoolkit-mail`

Both copies hold the identical value and are decoded the same way. (Before
v0.10.14 the libsecret copy was hashed on read but not on write, so on any
desktop with libsecret every stored token became undecryptable and OAuth
accounts failed to authenticate after the first sign-in. If you hit that,
delete `secrets/` and sign in again.)

The OAuth **client id** (and secret, when the app is confidential) is stored
alongside the token, so refreshing an hour later works even if the
`UITK_MAIL_OAUTH_*` variables are no longer exported.

This is an encrypted file (plus optional OS keyring), **not** a TPM-backed vault. Backup the `secrets/` directory with the same care as an SSH key.

Expired access tokens are refreshed with the stored refresh token.

## Sync (IDLE / QRESYNC / CONDSTORE)

After listen, LocalStore starts a **push supervisor**:

- **IDLE** on **Inbox and Sent** (one IMAP connection per watched mailbox; IMAP allows only one selected mailbox per connection).
- IDLE wake (EXISTS / FETCH / EXPUNGE / RECENT) → incremental sync of that folder.
- **QRESYNC** when the server advertises it (`ENABLE QRESYNC`): `SELECT … (QRESYNC (uidvalidity highestmodseq))`, apply `VANISHED`, then flag refresh.
- Else **CONDSTORE** `UID FETCH … (CHANGEDSINCE highestmodseq)` for flags.
- Else a full `UID FETCH 1:* (UID FLAGS)`.
- **Deletion reconciliation**: on a server without QRESYNC (so no `VANISHED`)
  each pass also diffs the mailbox's UID set against the cache and drops what
  is gone. Messages deleted from another client used to stay in the cache for
  ever, and later UID commands then addressed a message that no longer
  existed.
- Other folders: CONDSTORE/FLAGS poll about every **2 minutes**, plus File → Get Messages.
- `UIDVALIDITY` change still wipes that folder’s cache.

Manual Get Messages / `sync.run` always does a full account pass (LIST + incremental UID FETCH).

Sync never holds the store lock across network I/O (snapshot → I/O → re-lock
and apply) and only one sync runs at a time, so a slow or unresponsive server
no longer blocks unrelated RPCs. Progress is announced with `mail.changed` /
`mail.fetched` as each folder completes, and the UI refreshes on those events
instead of waiting for the whole pass. Every IMAP command has a deadline, and
an `AUTHENTICATE` that draws a `+` continuation is answered so the server can
report its error rather than both sides waiting for ever.

**Moves keep server state straight.** A `UID MOVE` / `UID COPY` response
carrying `COPYUID` (UIDPLUS) re-keys the cached message to its destination
UID; without one the cache entry is dropped and the next sync re-adds it. A
message is expunged with `UID EXPUNGE` where UIDPLUS is available, so a bare
`EXPUNGE` can no longer purge other `\Deleted` messages that another client
flagged.

Work Offline (`status.set`) stops treating the transport as reachable; mutations go to the outbox (below). Going online flushes the queue.

## Offline outbox

While offline (or after a transport error), **send / move / delete / flag** apply to the local cache immediately and enqueue an `OutboxOp`. The folder tree **Outbox** node lists queued sends.

Queued sends keep their **attachments**, so a message composed offline goes
out complete. A send that fails in transport is queued **once** and reports
the error (it used to both queue a copy and error, so a user retry sent the
message twice).

Flush: File → Work Offline (toggle back on), Get Messages, or `outbox.flush`.

Conflict-safe cache:

- Pending ops skip CONDSTORE/QRESYNC flag overwrite for that UID (local mutation wins until flush).
- Flush of a vanished UID is a no-op (dropped).
- `UIDVALIDITY` change drops stale UIDs; the user re-syncs.

## Cache integrity

Every cache file (`messages.json`, `folders.json`, the per-folder `meta/`
files, `mail.json`, token blobs, saved attachments) is written
**tmp → fsync → rename**, with the parent directory fsynced, so a crash
mid-write leaves the previous good file rather than a truncated one. On
start, a file that fails to parse is moved aside as `*.corrupt`, the rest of
the cache still loads, and `status.get` reports the recovery in `health` so
you know to re-sync — the old code silently loaded an empty store and then
overwrote the good file with it.

Account ids are reduced to `[a-z0-9_-]` and every derived path is checked
against the data directory, so an `accounts.put` with an id like `../../..`
cannot write (or, on `accounts.delete`, `RemoveAll`) outside the cache.

`mail.json` is re-`chmod`ed to `0600` on every save, including a file you
created by hand with a looser umask — it holds a plaintext password.

## Fast search + Smart folders

Daemon-side inverted index over subject / from / to / body (AND of tokens). Quick Filter and `messages.search` use it when a query is present, then apply pins.

**Smart / Search folders:** still exist on the daemon (`smart.*` RPC) but are **not shown** in the folder tree or File/Tools menus as of v0.10.4. Use Quick Filter for ad-hoc search.

## Threading + mute

Conversations group by `In-Reply-To` / `References` when Message-IDs exist, otherwise a normalized subject key. Roots are re-derived across the whole cache after a sync adds messages, so a reply that arrives before its parent (or a reply to a reply) still lands in the right conversation. View → **Threaded** nests replies (`↳`) and shows a count on the root.

Replies you send carry **`In-Reply-To`** and **`References`**, so they thread
in the recipient's client too, and Reply honours the sender's `Reply-To`.

**Message → Mute Thread** (and Unmute). Muted threads:

- stay in normal folders with a 🔇 marker
- do not drive notifications
- View → Hide muted threads removes them from the list

## Attachments

Message view stays **text-only**. Each attachment row shows its name plus inline **Open** and **Save As** (toolkit `Button`). A single click on the name selects only; a double click — or that row’s **Open** — calls `messages.openPart`: comms-maild writes a cache file under the data dir (`open/` or a temp file for MemoryStore) and launches `xdg-open` (or `open` on macOS) when a display is available. `UITK_MAIL_NO_OPEN=1` skips the spawn (tests / headless). **Save As** uses the toolkit file dialog and writes `messages.part` bytes to the chosen path (mode `0600`). Attachment bytes are decoded from the cached `.eml`, so a row saves **its own** part (a `report.pdf` row no longer writes the text body under that name).

`messages.openPart` refuses to hand the desktop opener anything it would
execute or render as markup (`.desktop`, `.sh`, `.js`, `.html`, `.svg`, …) —
save it and inspect it instead. The cache file is written under the data dir
with the attachment's **base** name, so a `filename="../../…"` cannot escape. The attachment toolbar (where the shared Open used to sit) has **Save All**: one folder pick via the same file dialog (the confirmed path is treated as a directory — an existing file uses its parent; a missing path with no extension is created), then every attachment on the current message is written there (`0600`; `name-2.ext` on collisions).

## Outgoing mail

`BuildRFC822Strict` is the only way a message reaches the wire:

- **`Bcc` is never written as a header.** Blind recipients are passed to SMTP
  as `RCPT TO` only; the header used to disclose the whole blind list to
  every recipient and to the copy filed in Sent.
- Every header value is rejected or folded if it contains CR/LF, and display
  names / subjects / filenames are RFC 2047-encoded. A `To:` field containing
  `\r\nX-Evil: 1` can no longer inject a header or a body.
- Attachment filenames are reduced to a base name before they go into the
  `filename=` parameter.

## VIP, notifications, categories

- **VIP** senders (Message → Add sender to VIP). The VIP smart folder is **not** shown in the sidebar; Preferences still lists VIP contacts, and VIP-only notifications still work.
- **Notification rules** (**M → Notify**): new mail, optional VIP-only, optional `notify-send` on Linux. No display / no `notify-send` → stub (RPC event `mail.notify` still fires). `UITK_MAIL_NO_NOTIFY=1` disables the desktop helper.
- **Categories** (Gmail-lite, local) still classify on the daemon; they are **not** a folder-tree section as of v0.10.4.

Calendar / iTip is **not** in this release (Tier C later).

## IMAP / POP3 / SMTP status (honest)

Implemented in comms-maild:

- IMAP: CONNECT, implicit TLS (993) and **mandatory** STARTTLS (143), mailbox names in modified UTF-7, LOGIN, AUTH PLAIN, AUTH XOAUTH2 (env bearer **or** stored OAuth token), CAPABILITY, ENABLE QRESYNC/CONDSTORE, LIST/LSUB, SELECT/EXAMINE (+ QRESYNC), UID FETCH (ENVELOPE, FLAGS, BODYSTRUCTURE, BODY.PEEK[] / sections), UID STORE, UID SEARCH, UID COPY, UID MOVE (or COPY+\\Deleted+EXPUNGE), APPEND, EXPUNGE, IDLE (Inbox+Sent supervisor), CONDSTORE CHANGEDSINCE, VANISHED when QRESYNC.
- POP3: CONNECT, implicit TLS (995) and STLS (110), USER/PASS, STAT, UIDL, RETR into the local Inbox (leave-on-server — no DELE). Incremental skip by UIDL (or RFC Message-ID if UIDL is missing). Local Drafts/Sent/Trash exist for compose.
- Incremental cache: UIDVALIDITY wipe, UIDNEXT, highestmodseq. Raw `.eml` on disk after body fetch.
- MIME: multipart (nested), text/plain + text/html, attachments (decoded to their own bytes), RFC 2047, charset via `golang.org/x/text`, numeric + named HTML entities, and a 64 MiB cap per decoded part.
- SMTP: implicit TLS (465), STARTTLS (587), AUTH PLAIN / LOGIN / XOAUTH2. Send then IMAP APPEND to Sent (or outbox if offline).
- Multiple accounts in one config; folder tree mirrors LIST + local specials + virtuals.
- Offline read of anything already synced; queued mutations.

Known gaps:

- BODYSTRUCTURE walker covers common multipart/alternative + mixed; exotic message/rfc822 nests may miss a part id. Part ids follow RFC 3501 section numbering (`1`, `1.1`, `2`) in both the parser and the walker — they used to disagree, and nested parts could collide.
- HTML is **stripped to text** in the Message tab (no HTML engine, no HTML tab). Scripts/iframes never run.
- OAuth needs **your** Google/Microsoft app registration (no bundled client IDs). OAuth accounts are **IMAP + SMTP** only (not POP3).
- IDLE watches Inbox + Sent, not every mailbox (others poll). POP3 has no IDLE (Get Messages / periodic Sync).
- **POP3** is inbox retrieve only: no server folders, no server-side flags (read/starred are local), no MOVE/APPEND on the server, deletes stay local (server copy remains), no TOP preview. Sent goes out SMTP and is filed locally.
- Sieve is not implemented (local Sorting Office rules only).
- Desktop notifications need `notify-send` + a display; otherwise they are a no-op.
- iTip / Calendar is out of scope (Tier C).

## Protocol

Unix domain socket, **JSON-RPC 2.0**, one JSON object per line (NDJSON).

Notifications (no `id`): `mail.changed`, `mail.fetched`, `mail.synced`, `mail.notify`.

| Method | Params |
| --- | --- |
| `ping` | — |
| `status.get` | — |
| `status.set` | `{online}` — Work Offline; going online flushes the outbox |
| `accounts.list` | — |
| `accounts.put` | AccountConfig (`protocol` `imap` or `pop3`; `password` stored in mail.json mode 0600; `passEnv` optional) |
| `accounts.delete` | `{id}` — remove account from mail.json and the local cache (server mail is kept) |
| `oauth.start` | `{provider, address, name?, clientId?, clientSecret?, flow?}` |
| `oauth.poll` | `{sessionId}` |
| `oauth.cancel` | `{sessionId}` |
| `hosts.guess` | `{address}` → IMAP/POP3/SMTP guess |
| `hosts.probe` / `accounts.test` | `{address, user?, password?, protocol?, host?, imap?, pop?, auto?, timeoutMs?}` → Test connection (`ok`, protocol, host, tlsMode) |
| `folders.list` | `{accountId}` |
| `folders.get` | `{id}` |
| `folders.create` | `{accountId, name, parent?}` |
| `folders.virtual` | — Outbox / (hidden unified / categories / smart / VIP) / tags |
| `messages.list` | `{folderId, filter?}` (virtual ids ok) |
| `messages.get` | `{id}` (disk raw / in-memory body if already fetched; no extra IMAP) |
| `messages.search` | `{accountId?, folderId?, filter}` |
| `messages.setFlags` | `{id, patch}` |
| `messages.move` | `{ids, dest}` |
| `messages.delete` | `{ids}` |
| `messages.append` | `{folderId, message}` |
| `messages.update` | `{id, message}` |
| `messages.getPart` | `{id, partId}` |
| `messages.openPart` | `{id, partId}` → `{path}` on disk + `xdg-open` |
| `messages.fetch` | `{accountId}` |
| `sync.run` | `{accountId?}` |
| `unread.get` | `{folderId?}` |
| `unread.all` | — every folder + virtual folder in one round trip |
| `compose.send` | `{accountId, identityId?, message, attachments?, id?}` — attachments are **bytes**; the daemon never opens a client-supplied path |
| `compose.saveDraft` | `{accountId, message, id?}` |
| `outbox.list` / `outbox.flush` | queued send/move/delete/flag |
| `smart.list` / `smart.put` / `smart.delete` | saved search folders |
| `threads.mute` / `threads.muted` | conversation mute |
| `vip.list` / `vip.put` / `vip.delete` | VIP senders |
| `notify.get` / `notify.put` | `{enabled, vipOnly, desktop}` |
| `senders.setCategory` / `senders.categories` | Primary/Other/… override |
| `identities.*` / `tags.*` / `filters.*` | unchanged from v0.9 |

Quick Filter in the UI calls `messages.list` with the pin/query filter so the list is daemon-filtered.

### Filter rules (Sorting Office)

```json
{
  "id": "rule-01",
  "name": "Tag invoices",
  "enabled": true,
  "stop": false,
  "conditions": [{"field": "subject", "op": "contains", "value": "Invoice"}],
  "actions": [{"type": "tag", "tag": "Work"}]
}
```

Condition fields: `from`, `to`, `subject`, `body`, `attachment`, `unread`, `tag`.
Actions: `move` (`folder`), `tag`, `markRead`, `markUnread`, `delete`, `stop`.
AND across conditions. Persist in MemoryStore or the disk cache. The sidebar Tags group and Preferences → Tags share one Tags store (locked Unread / Starred / Attachment plus keywords). Preferences can add, edit, and remove user tags.

## UI features (v0.10.13)

- **Empty by default** — no demo accounts unless `UITK_MAIL=memory`. First-run Yes/No is only “There are no accounts, want to add one?” Password / `0600` notes are on the Add Account form.
- **Add account** — IMAP vs POP3 radios, domain auto-guess (including POP hosts), **Test connection** (and optional auto-detect after email+password), masked password field, or Sign in with Google / Microsoft (or device code; IMAP). Saved accounts show the protocol on Account Central and in Preferences. `passEnv` remains an optional fallback.
- **Remove account** — File menu, Account Central, and Preferences → Accounts. Confirm, then drop the account from `mail.json` and the local cache. The folder tree refreshes; if none remain, the first-run “add one?” prompt returns.
- **Text-only Message tab** — prefer `text/plain`; HTML-only mail is tag-stripped. No HTML engine / no HTML tab. The Message/Source body is a read-only `TextView` (scroll + scrollbar; not editable). Compose/Write stays an editable `TextArea`.
- **3-pane splitters** — dragging folder|list or list|preview keeps exclusive pane bounds; preview chrome cannot paint over the thread list.
- **Overflow scrollbars** — thread list, folder tree, and long message bodies show a vertical track/thumb; wheel/trackpad still scroll; offset clamps at the last row. The thread table clips rows under the sticky header (flush at the top; no paint-through while scrolling).
- **Thread columns** — ★, 📎, Topic, Who, When. No Size. Click a column header to sort.
- **Card / Table** — **M → View** Card view. Remembered in `~/.config/uitoolkit/mailui.json`. Star after the message context menu paints immediately.
- **Density** — **M → View** Compact / Default / Relaxed.
- **Folder tree** — account folders and Tags at the top of the sidebar; Outbox is pinned to the **bottom** of the pane (separated from Tags). Unified Folders, Smart Folders, Categories, and VIP are not shown. Click an account root to open that Inbox. The Tags group is the same list as Preferences → Tags: locked Unread / Starred / Attachment pins (✓ + bold when on) plus every keyword (Important, Work, Personal, To Do, Later, and user-created).
- **Chrome** — no path/subtitle strip, no bottom status bar, no unread-folder-count footer, no sidebar Account / Folders section headers, no identity or Tags ComboBox, and no active-filter banner (`Filter on · N shown` / `Clear filter`) above the thread list. The folder tree (including Tags) starts at the top of the sidebar. The menubar row is **M**, a left **Fetch / Write** toolbar, then a right-aligned **Filter** toolbar. That row is the window's title bar (`Window.SetTitleBar`): where uitoolkit draws the frame — by default on KDE Plasma, always on GNOME — it is the caption, with the caption buttons at the desktop's sides and its free space moving the window; with **Use system title bar and borders** (or `UITK_DECORATIONS=server`) it is the first row under the desktop's frame. See [decorations.md](https://github.com/codemodify/uitoolkit/blob/dev/docs/decorations.md). Tag, Archive, Junk, Cards, Classic, and Delete are not on those bars. There is no strip above the Topic / Who / When header. The Quick Filter field stays hidden until that button or Ctrl+F (`ShowFilter` defaults off; a saved `mailui.json` `showFilter: true` is honored) and opens on the same row; Escape hides it again and keeps the query. Filter pins are only on the Tags tree. Reply and Forward are keyboard / context menu only. The menu bar is a single **M** menu: **View** submenu (layout / list / density / Threaded / Hide muted threads), **Notify** submenu (new mail / VIP-only / desktop), Preferences, Quit. Preferences is Accounts + Tags (the same Tags model as the sidebar). Menu and toolbar hover do not refresh the folder tree or the message list.
- **Threaded** view and **Mute Thread**.
- **Attachments** — per-row Open / Save As; toolbar Save All (one folder pick, then write all files); single click selects, double click or row Open opens.
- **Snappy open** — unread click patches the row; preview uses a cached `messages.get` body.
- **Colored tags** — Preferences → Tags is the same list as the sidebar Tags group, with Add / Edit / Remove (Remove disabled for Unread, Starred, and Attachment). New mail is tagged Unread; parts with a filename get Attachment.

## Keyboard (Thunderbird-like)

Documented in [keyboard.md](https://github.com/codemodify/uitoolkit/blob/dev/docs/keyboard.md). When the thread list (not a text field) has focus:

| Key | Action |
| --- | --- |
| **n** / **p** | Next / previous message |
| **#** | Delete (also Del) |
| **r** | Reply |
| **f** | Forward |
| **c** | Compose |
| **m** | Mark as read |
| **F5** | Fetch / sync |
| **Ctrl+,** | Preferences |

## How to try (dogfood)

```bash
UITK_MAIL=memory go run ./cmd/comms-maild
UITK_SCENE=auto go run ./cmd/comms-mail
```

- **VIP** — open a Kai / Thunderbird Team message → Message → Add sender to VIP (M → Notify can restrict alerts to VIP senders; no VIP folder in the tree).
- **Threading** — View → Threaded; look for “Thread: lunch plans (3)”. Message → Mute Thread.
- **OAuth** — File → Add Account, enter a Gmail/Outlook address (hosts fill in), paste your client id, Sign in with Google / Microsoft. Approve in the browser (or use Device code). Then Get Messages.

## Screenshots

```bash
go run ./cmd/comms-mail-demo -screenshot docs/screenshots
```

Writes `mail-dark.png`, `mail-light.png`, `mail-classic.png`, `mail-compose.png`, `mail-prefs.png`, `mail-cards.png`, `mail-compact.png`, `mail-filters.png`, `mail-empty.png` (first-run dialog), `mail-account.png` (Add Account), `mail-smart.png` (daemon saved-search view; not a tree section).
