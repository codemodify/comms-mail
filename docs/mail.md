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
| A choice of where secrets live | The desktop keyring, secretvault, an encrypted file, or `mail.json`; choosing one moves every password and token there, and `master.key`, `*.tok` and the old `secret-tool` entry go |
| `Bcc` is no longer written into the message | Blind recipients still receive it; they are just no longer disclosed |
| Moves re-key on `COPYUID` | Cache entries change id after a move; a server without UIDPLUS drops the entry until the next sync |
| Deletion reconciliation without QRESYNC | Messages deleted elsewhere finally disappear from the cache |
| Folder / message ids are sanitised | Cached folder metadata was re-keyed, so the first sync after upgrading re-reads it |

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

**Starting the daemon.** `comms-mail` starts `comms-maild` itself when
nothing answers on the socket — the systemd user unit if one is installed,
else the `comms-maild` beside `comms-mail` or on `PATH` — and the daemon
keeps running after the window closes, so mail still syncs and notifies.
To have it start at every login:

```bash
comms-maild install     # systemd user unit (enabled; started now unless one runs),
                        # or ~/.config/autostart/comms-maild.desktop without systemd
comms-maild uninstall   # stop starting it at login
```

The unit restarts the daemon if it fails, but not when it exits because
another one already holds the socket (exit status 3). Started by systemd the
daemon may have no display; opening an attachment then happens in the
window instead.

**Log.** The daemon writes `~/.data/comms-mail/logs/comms-maild.log` (mode
0600, rotated to `.log.1` at 4 MB): start and stop, connections lost and
back, folders that fail to sync, sends that fail or wait in the Outbox,
Outbox retries, and every error the daemon answers to the window (the
method, never its parameters). It never holds message content or
credentials. The demo store writes none.

```bash
# By hand: the daemon (empty until you add an account), then the window
go run ./cmd/comms-maild
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

`comms-maild` is meant to be pointed at a real account. **Add Account** takes a typed (masked) password; it is kept where you chose — the desktop keyring, secretvault, an encrypted file, or `mail.json` (see [Where passwords are kept](#where-passwords-are-kept)). OAuth sign-ins are kept there too. Optional `passEnv` / `UITK_MAIL_PASS` still work when no password is saved.

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

Config directory (`mail.json`, `mailui.json`):

- `$UITK_MAIL_CONFIG` overrides the account file path directly
- else `$XDG_CONFIG_HOME/comms-mail`
- else `~/.config/comms-mail`

Cache / offline store:

- `$UITK_MAIL_DATA`
- else `~/.data/comms-mail`

The cache is deliberately not under the XDG `~/.local/share` default:
config and data sit beside each other as `~/.config/comms-mail` and
`~/.data/comms-mail`, so the whole cache can be deleted (a re-sync rebuilds
it) without touching the account file.

What lives in it:

| Path | Holds |
| --- | --- |
| `mail.db` (+ `-wal`, `-shm`) | SQLite, mode `0600`. `messages`: one row per message — headers, flags, tags, thread, parts — plus its decoded text once downloaded. `message_text`: the search index — each downloaded message's text in trigrams (FTS5, no copy of the text). `folder_meta`: each folder's UIDVALIDITY / UIDNEXT / HIGHESTMODSEQ. `kv`: accounts, identities and signatures, folders, tags, filter rules, smart folders, VIPs, muted threads, categories, notification settings, the offline outbox. |
| `raw/<account>/<message>.eml` | The message exactly as the server sent it, once downloaded (a click, or the background prefetch). Source view, attachments and re-parsing read it. |
| `open/` | Attachment copies written for **Open** to hand to the desktop; removed after a day (the next time one is opened, and at start). |
| `secrets/` | `vault.json`: passwords and OAuth tokens, encrypted with your passphrase (the encrypted-file store). |

Settings are not in the cache: `mail.json` (accounts; passwords only for the plain-file store) is in the
config directory above, and the window's own settings (layout, density, card
view) are in `mailui.json` beside it.

Example `mail.json` (written mode `0600`). With the plain-file store (and before a store is chosen) `password` is here in plain text; any other store takes it out of this file:

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

Saving an account without its password (`accounts.put`, Settings) keeps the saved one only for the same server and user — the port may change. Pointing a server at another host, or another user, needs that server's password again; the save is refused until it is given, so no request can send a saved password to a host of its choosing.

Mail written here gets a Message-ID like other clients' — `<32 random hex digits@sender's domain>` — which names neither the software nor the sender, and does not say when it was written.

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

Type the incoming/SMTP password (masked field); **Save account** writes `protocol` plus `password` into `mail.json` (file mode `0600`). `passEnv` is optional fallback only. Account Central and Settings → Accounts show **IMAP** or **POP3** after save.

### Message view: text and HTML

The reading pane — and a message opened in a tab of its own, which is the
same thing larger — shows the header (From, To, Cc, Date, tags,
attachments, whether you replied or forwarded), the invitation card, a
row of actions over the attachments, and three tabs: **Message** (the
`text/plain` body, the default), **Source** (the raw RFC822, fetched when
the tab is shown) and **Markdown** (the message rendered, below).

Nothing of a message's HTML is drawn in the window. A message that has an
HTML part has **Open HTML** in the action row, beside the attachments'
Open / Save / Save All: it opens the part as it was sent in the browser
(see *Message / Source / Markdown tabs* below). The row folds onto a
second line in a narrow pane.

Images: a `data:` image draws as it is, and an inline `cid:` image — a
picture carried in the message itself, a logo or a chart — is read from its
part (by Content-ID, `messages.inlineImages`) and drawn at once; nothing
leaves the machine. A remote (`http(s)`) image is a read receipt for the
sender, so it stays a placeholder and a line above the body says so, with
**Show Images** (this message) and **Always from This Sender** (remembered;
`images.allowSender`). comms-maild does the fetching (`images.fetch`): http
and https only, public addresses only — a message cannot make your machine
call `localhost`, your router or a cloud metadata address, checked on the
address actually dialled — at most 100 images of 8 MB each and 48 MB in
all, 15 s each, no cookies, no proxy, not while working offline. PNG, JPEG,
GIF and WebP are drawn; pictures seen once are kept for the session.
**Settings → Privacy** lists the senders trusted with Always and takes
them back. A message opened in a tab has the same views.

A link is followed on a click, after a confirmation showing the real target
so link text cannot disguise where it goes; a `mailto:` opens a
pre-addressed Write window instead.

**Print…** (Ctrl+P, or the message's context menu) opens the message in
your browser as a page — headers, then the HTML with its inline images, or
the text — to print or save as PDF from there. The page cannot load
anything: a Content-Security-Policy blocks every request and script, and
remote image sources are taken out, so printing is not a read receipt.
The page is a temporary file, removed after an hour (the next time you
print, and when the window starts); attachments dragged out to another
window go after a day the same way.
**Save As…** writes the message exactly as stored, as a `.eml`
(`messages.getRaw`).

**Ctrl+U** opens a read-only JetBrains Mono
window of the stored RFC822 (`messages.getSource`). The Source tab in the
preview pane uses the same daemon bytes — not a reconstructed header dump.

### Calendar invitations

A message that carries a meeting invitation — a `text/calendar` part or an
`.ics` attachment, as Google Calendar, Outlook / Exchange, Thunderbird and
Apple Calendar send — shows an **invite card** above its body, in the
reading pane and in a message tab: the title, when (in your time zone) and
where, how it repeats, the organizer, and each guest with their answer
(yes / maybe / no). When the invite asks you, **Accept**, **Maybe** and
**Decline** answer it.

An answer is an iTIP `REPLY` (RFC 5546) mailed to the organizer the way
calendar software expects (RFC 6047): a short text line and a
`text/calendar; method=REPLY` alternative, carrying the event's UID,
SEQUENCE and occurrence and your `PARTSTAT`, with times in UTC so no
time-zone block has to travel. It goes out like any mail — from the
identity whose address is on the guest list, through the account's SMTP,
queued in the Outbox when offline, a copy filed in Sent. The answer you sent
is remembered per version of the invite, so the card says *You accepted*;
an organizer's update (a new SEQUENCE) asks again. You can change your
answer — that sends a new reply.

Under the buttons: **Tell the organizer** (untick it to keep your answer
here without mailing anyone), a **note** that goes with the answer (as the
reply's `COMMENT` and in its text), and — when the invite reached you
through a list, none of your addresses on it — **Answer as**, to pick the
identity that answers (it is added as a guest). **Open in Calendar** hands
the event to your desktop's calendar application (the invite's `.ics`,
opened like an attachment). **Less** folds the guest list and the options
away on every card, so a short window keeps room for the message; **More**
brings them back.

A **cancellation** says so with no buttons; a guest's **reply** to an
invite you sent says who accepted, declined or said maybe; a **published**
event (shared for information) asks nothing. A guest's **proposal of a new
time** (`COUNTER`) shows the time proposed with **Decline Proposal**, which
mails them an iTIP `DECLINECOUNTER`; taking the new time is done in your
calendar, which then sends everyone the update. Time zones are read from
the IANA name, the Windows names Outlook uses, or the invite's own
`VTIMEZONE` rules.

If the server cannot be reached, an answer (like any message) waits in the
Outbox and the card says so; it is sent once, when the server answers. A
server that refuses the message outright (a rejected address, a failed
login) is reported and nothing is queued.

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

Refresh/access tokens are **never** written to `mail.json`. They are kept beside the passwords, in the store you chose ([Where passwords are kept](#where-passwords-are-kept)). The OAuth **client id** (and secret, when the app is confidential) is stored with the token, so refreshing an hour later works even if the `UITK_MAIL_OAUTH_*` variables are no longer exported.

Expired access tokens are refreshed with the stored refresh token.

## Sync (IDLE / QRESYNC / CONDSTORE)

After listen, LocalStore starts a **push supervisor**:

The folder the window shows is watched too (`folders.focus`), besides Inbox
and Sent, and caught up the moment it is chosen; the other folders are
polled every two minutes.

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


**Tags and other clients.** Tags are IMAP keywords on the server.
Thunderbird's five default tags — the same five comms-mail starts with,
Important / Work / Personal / To Do / Later — are written and read as its
keywords `$label1`…`$label5`, so a tag set in either client shows in the
other; other tags are their name with spaces as underscores. A sync applies
the tag changes made elsewhere since it last looked (each message remembers
the keywords it last saw), so a tag added or removed in another client
arrives here, while a tag only this machine has — a filter rule's — is left
alone. Other clients' bookkeeping keywords (`$Forwarded`, `$MDNSent`,
`NonJunk`, …) are not tags.

**Mark Folder Read** marks the messages that were unread when you chose it
— by UID, not the whole mailbox, which would also mark mail that arrived
since — and is queued like any change when offline; a sync meanwhile does
not set them unread again.
## Offline outbox

While offline (or after a transport error), **send / move / delete / flag** apply to the local cache immediately and enqueue an `OutboxOp`. The folder tree **Outbox** node lists queued sends.

Queued sends keep their **attachments**, so a message composed offline goes
out complete. A send that fails in transport is queued **once** and reports
the error (it used to both queue a copy and error, so a user retry sent the
message twice).

Flush: File → Work Offline (toggle back on), Get Messages, or `outbox.flush`.
Queued **flag** changes are also retried on their own every two minutes while
online, so a mark-read that hit a network blip still reaches the server.

A queued move or delete records the folder the message came from and that
folder's `UIDVALIDITY`. Replay only does the server's half — `UID MOVE` from
that folder, or `\Deleted` + `UID EXPUNGE` there for a delete from Trash —
because the cache took the change when it was made. An op whose folder was
renumbered since is dropped rather than sent to a UID that now names
something else, and so is an op queued by an older comms-maild that did not
record its source. Removing a tag clears its IMAP keyword; the Unread,
Starred and Attachment pins are never written as keywords.

Conflict-safe cache:

- Pending ops skip CONDSTORE/QRESYNC flag overwrite for that UID (local mutation wins until flush).
- Flush of a vanished UID is a no-op (dropped).
- `UIDVALIDITY` change drops stale UIDs; the user re-syncs.


What waits in the Outbox — sends, drafts, moves, flag and tag changes — is
retried every two minutes while online, not only when you go back online;
an op that has failed ten times waits for a flush by hand. Only one replay
runs at a time, and a send that fails again on retry is not queued a second
time, so nothing is sent twice.
## Cache integrity

`mail.db` is SQLite in WAL mode. A save writes only the messages whose
fields changed (each row carries a fingerprint of them) and the collections
whose encoding changed, all in one transaction, so a crash leaves the state
before the save or after it — never half of one. Marking a message read
writes one row; it used to rewrite every cache file, the message list
included (30 MB on a real mailbox), with the store lock held.

The schema has a version (`PRAGMA user_version`, 2 since the search index).
A newer comms-maild upgrades an older cache in place; an older one refuses a
newer cache rather than misread it.

A `mail.db` that will not open is moved aside as `mail.db.corrupt` and a
fresh one is created; `status.get` reports it in `health` so you know the
next sync is refilling the cache from the server. Other files — `mail.json`,
token blobs, `.eml` blobs, saved attachments — are written
**tmp → fsync → rename**, with the parent directory fsynced.

Account ids are reduced to `[a-z0-9_-]` and every derived path is checked
against the data directory, so an `accounts.put` with an id like `../../..`
cannot write (or, on `accounts.delete`, `RemoveAll`) outside the cache.

`mail.json` is re-`chmod`ed to `0600` on every save, including a file you
created by hand with a looser umask.

### Where passwords are kept

comms-mail keeps every secret it needs — account passwords and OAuth
sign-ins — in one of four places, chosen by you (`"secretStore"` in
`mail.json`). PGP and S/MIME keys are not among them: those stay in
secretvault, which does that work itself (see the security primer).

| Store | Where | Unlocking |
| --- | --- | --- |
| **Desktop keyring** | The system's own: the freedesktop Secret Service on Linux (GNOME Keyring, KWallet, KeePassXC), the Keychain on macOS, the Credential Manager on Windows. Items are labelled `comms-mail: <name>` (attributes `application=comms-mail`, `name=…`). | The desktop unlocks it at login; a locked one shows its own prompt. |
| **secretvault** | [codemodify/secretvault](https://github.com/codemodify/secretvault), the owner's own store, reached through its daemon's socket (`$SECRETVAULT_SOCK`, else `$XDG_RUNTIME_DIR/secretvault/secretvaultd.sock`) with its JSON-RPC protocol; comms-mail links none of its code. Items `comms-mail/pass/<account>/<imap\|pop\|smtp>` (kind `password`) and `comms-mail/oauth/<account>` (kind `api-key`) in its default vault, labelled for its own windows. | secretvault's: it asks before letting `comms-maild` read, and remembers. While it is locked, comms-maild holds no secret — what it read is dropped and its sessions closed — and waits; it asks secretvault to unlock only when you do (Fetch, or Settings › Privacy › **Unlock secretvault…**). |
| **Encrypted file** | `~/.data/comms-mail/secrets/vault.json` (mode `0600`): AES-256-GCM under a key from your passphrase (Argon2id; the salt and cost are bound into the encryption, so they cannot be swapped for weaker ones). | Your passphrase, once each time comms-maild starts. |
| **Plain file** | Passwords in `mail.json`, as comms-mail always kept them; OAuth tokens in `oauth-tokens.json` beside it (both `0600`). Readable by any program running as you. | — |

- **Choosing.** The window asks — with nothing picked beforehand — when
  it finds passwords readable in `mail.json` (an install from before the
  choice), and before the first account's password is saved. **Not now**
  leaves everything as it is.
- **Moving.** Settings › Privacy › **Change where…** moves every secret to
  another store: they are written there first and taken out of the old
  place last (the encrypted file deleted, keyring items removed, or the
  passwords taken out of `mail.json`), so a failure part-way leaves them
  where they were. Moving from an install before the choice also deletes
  the old token files, `master.key`, and the copy of that key older builds
  put in the keyring through `secret-tool`.
- **Locked.** While the store in use cannot be read — the encrypted file
  not yet unlocked, the keyring or secretvault locked, secretvault told
  no — the daemon connects to **no server** (it never tries a login
  without its password): mail already downloaded shows, a message you
  send waits in the Outbox, and accounts cannot be changed. The window
  unlocks at start and on Fetch; for secretvault it waits at start, and
  sync resumes by itself when secretvault unlocks (or starts, if it was
  not running yet).
- **The passphrase** (encrypted file): at least 8 characters; Settings ›
  Privacy › **Change passphrase…**; **Forgot it…** on the unlock window
  starts over — every saved secret is deleted, no store is chosen, the
  accounts and their mail stay, and each account needs its password again.
- The RPC log records these requests by name only, never a passphrase.

`secrets.status`, `secrets.use` (`{store, passphrase}`), `secrets.unlock`,
`vault.change` and `vault.reset` are the daemon's side of this. A store
that keeps no secrets (the demo) reports `supported: false`, and the window
asks nothing. Tests never reach your real keyring or secretvault: they
run the keyring code against a fake Secret Service on a private D-Bus, and
the secretvault code against a stand-in daemon (`internal/svtest`).

## Folders

Right-click a folder for **New Folder…** / **New Subfolder…** (each asks
for a name, over the window; the button — Create or Rename — stays grey
until there is one, and a name the server refuses is said under the
field, with the name still there),
**Rename Folder…**, **Move Folder To** (another folder, or the
top level), **Delete Folder…**, **Mark Folder Read** and **Compact Folder**
(removes from the server what another client deleted by marking it). Only
folders you made can be renamed, moved or deleted; Inbox, Sent, Drafts,
Trash and the rest cannot.

A folder can also be **dragged**: onto another folder of its account it
goes inside it, onto the account it goes to the top level — the same move
as Move Folder To (`folders.move`). It does not go into itself, into a
folder inside it, or into another account.

Folders nest as the server names them: `Archives/2023` shows as `2023`
under `Archives`, using the server's own delimiter (`/` or `.`). A mailbox
that only holds others (Gmail's `[Gmail]`, `\Noselect`) is their parent and
is never opened. On a server that keeps every folder under `INBOX.` (a
namespace), they are not all shown inside Inbox. A list that mixes folders
— All folders, a unified or tag view — names each message's folder beside
its sender.

A rename is an IMAP `RENAME` (the server renames the folders under it
too). The folder keeps its id and its messages keep theirs, so nothing is
downloaded again. Sync matches folders to server mailboxes by their server
name, not by an id made from the name, so a renamed folder is not added a
second time; and a folder renamed or deleted in another client is dropped
here (its new name arrives as a folder of its own) once the server no
longer lists it. Folder names outside ASCII (`Entwürfe`, `送信済み`) are
sent in IMAP's modified UTF-7 once — they used to be encoded twice and
could not be opened.

## Fast search + Smart folders

A query is a case-insensitive piece of text — part of a word, or several words as they appear — looked for in the subject, the addresses and the message text. The text is what the Message tab shows, so words in HTML-only mail are found, and markup is not.

The text is looked up in a full-text index in `mail.db` (`message_text`, SQLite FTS5 with trigrams), written in the same transaction as the message: it lasts across restarts and is not rebuilt. A cache from before it is indexed once, at the first start (under a second for ~700 downloaded messages). Text downloaded but not yet saved is read directly, and a query of one or two characters (too short for trigrams) looks in the plain-text part only. Search in one folder (`messages.list` with a filter) and in all of them (`messages.search`) both use it, then apply the pins.

A list (`messages.list`, `messages.search`, `messages.searchServer`) carries each message's headers, flags, parts and snippet, but not its text — on a real Inbox that is 0.6 MB a refresh instead of 3.7 MB. The reading pane fetches the text of the message it shows (`messages.get`), and shows it at once when this window has read it before.

**Search:** the **Search** button after Write (or Ctrl+F) opens the search dialog: the words to look for, **All folders**, **On the server too**, and **Search** / **Cancel** / **Clear**. Search narrows the list to what matches; the Search button stays pressed while it does (its tip says what is being searched for), and Clear, in the same dialog, shows the whole folder again. **All folders** makes it a search across every folder (and account) rather than the current one — over the daemon's index, so it covers every synced header and the bodies already downloaded. Results are a flat, date-sorted list with each message's folder beside its sender; picking a folder returns to the folder view.

**On the server too:** every message's headers are cached, but only some bodies (the recent ones and any opened), so a word in the body of older mail is not in the index. With **On the server too** ticked, a search is also sent to the mail server — `UID SEARCH TEXT` in the current folder, or in every folder with All folders (`messages.searchServer`) — and the messages it finds join the list; the status line says how many came from the server. The local results show at once; the server's are merged when they arrive. Each word must match (a "quoted phrase" is one word); a query outside ASCII is sent as a UTF-8 literal. On Gmail (a server that advertises `X-GM-EXT-1`) the query goes whole as `X-GM-RAW`, so Gmail's own operators work: `has:attachment`, `older_than:1y`, `from:`, `label:`, `larger:5M`. Not while working offline, and not for POP or imported mail.

**Smart / Search folders:** still exist on the daemon (`smart.*` RPC) but are **not shown** in the folder tree or File/Tools menus as of v0.10.4. Use Search for ad-hoc search.

## Threading + mute

Conversations group by `In-Reply-To` / `References` when Message-IDs exist, otherwise a normalized subject key. Roots are re-derived across the whole cache after a sync adds messages, so a reply that arrives before its parent (or a reply to a reply) still lands in the right conversation. View → **Threaded** indents replies under the first message and shows a count on the root.

Replies you send carry **`In-Reply-To`** and **`References`**, so they thread
in the recipient's client too, and Reply honours the sender's `Reply-To`.

**Message → Mute Thread** (and Unmute). Muted threads:

- stay in normal folders, with a muted-bell icon before the topic
- do not drive notifications
- View → Hide muted threads removes them from the list

## Attachments

Message view stays **text-only**. Each attachment row shows its name plus inline **Open** and **Save As** (toolkit `Button`). A single click on the name selects only; a double click — or that row’s **Open** — calls `messages.openPart`: comms-maild writes a cache file under the data dir (`open/` or a temp file for MemoryStore) and launches `xdg-open` (or `open` on macOS) when a display is available. `UITK_MAIL_NO_OPEN=1` skips the spawn (tests / headless). **Save As** uses the toolkit file dialog and writes `messages.part` bytes to the chosen path (mode `0600`). Attachment bytes are decoded from the cached `.eml`, so a row saves **its own** part (a `report.pdf` row no longer writes the text body under that name).

`messages.openPart` refuses to hand the desktop opener anything it would
execute or render as markup (`.desktop`, `.sh`, `.js`, `.html`, `.svg`, …) —
save it and inspect it instead. The cache file is written under the data dir
with the attachment's **base** name, so a `filename="../../…"` cannot escape. The attachment toolbar (where the shared Open used to sit) has **Save All**: a folder dialog picks where (a path typed in it that names a file uses its folder; a missing one with no extension is created), then every attachment on the current message is written there (`0600`; `name-2.ext` on collisions).

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

### Drafts and autosave

The Write window saves a changed message to Drafts on its own every 10
seconds — the same draft, updated in place — so a crash or a lost window
costs at most that much; the status line says when it last did. Closing
asks first when something is unsaved (the menu's Close and the window's
close button alike): **Yes** keeps the draft, **No** throws away a draft
only autosave made (a draft you saved or opened is kept, as last saved).
Sending removes the draft.

A message that went closes its Write window, and is in Sent — no dialog to
dismiss. Only what needs attention asks for it: a send that failed (the
window stays, with the error), or one the server could not be reached for
("Not sent yet": it waits in the Outbox and goes when the server answers).

On an IMAP account a saved draft is appended to the server's Drafts and
the cached copy takes the UID the server gives it (UIDPLUS `APPENDUID`),
so a sync does not add it a second time; saving again appends the new
version and removes the old one, so the server holds one draft, the latest.
A server that does not report UIDs gets its copy recognised by Message-ID
on the next sync. Sent copies are filed the same way. A draft saved while offline is kept here and
appended to the server's Drafts — once, with its latest text — when the
account is reachable again.

## VIP, notifications, categories

- **VIP** senders (Message → Add sender to VIP). The VIP smart folder is **not** shown in the sidebar; Settings still lists VIP contacts, and VIP-only notifications still work.
- **Notification rules** (**Menu → Notify**): new mail, optional VIP-only, optional `notify-send` on Linux. No display / no `notify-send` → stub (RPC event `mail.notify` still fires). `UITK_MAIL_NO_NOTIFY=1` disables the desktop helper.
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
- HTML mail is **rendered** (uitoolkit's HTML subset) with clickable links; scripts, external CSS and remote images never load. A `text/plain` part, when present, is shown as text instead.
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
| `folders.move` | `{folderId, parent?}` — user folders only: IMAP `RENAME` to the new path; no parent = top level |
| `folders.compact` | `{folderId}` — `EXPUNGE`: remove what is marked deleted |
| `folders.rename` | `{folderId, name}` — user folders only: IMAP `RENAME`, the folder keeps its id and messages |
| `folders.delete` | `{folderId}` — user folders only, with their messages, on the server too |
| `folders.markRead` | `{folderId}` |
| `folders.virtual` | — Outbox / (hidden unified / categories / smart / VIP) / tags |
| `messages.list` | `{folderId, filter?}` (virtual ids ok) |
| `messages.get` | `{id}` (disk raw / in-memory body if already fetched; no extra IMAP) |
| `messages.search` | `{accountId?, folderId?, filter}` |
| `messages.searchServer` | `{folderId?, query}` — IMAP `UID SEARCH TEXT` (Gmail: `X-GM-RAW`) in the folder or every folder; the cached messages it names |
| `messages.setFlags` | `{id, patch}` |
| `messages.move` | `{ids, dest}` |
| `messages.delete` | `{ids}` |
| `messages.append` | `{folderId, message}` |
| `messages.update` | `{id, message}` |
| `messages.getPart` | `{id, partId}` |
| `messages.openPart` | `{id, partId}` → `{path}` on disk + `xdg-open` |
| `messages.inlineImages` | `{id}` → `[{cid, mime, data}]`, the message's image parts by Content-ID |
| `images.fetch` | `{urls}` → `[{url, mime?, data?, error?}]` — public http(s) addresses only, size and time bounded |
| `images.senders` / `images.allowSender` | senders whose remote images load without asking / `{address, allow}` |
| `messages.invite` | `{id}` → the calendar invitation in the message (`null` when none), with `you` and your `answer` |
| `invite.reply` | `{id, partstat, comment?, noSend?, identityId?}` — `ACCEPTED`, `TENTATIVE` or `DECLINED`: mails the organizer an iTIP REPLY (unless `noSend`), files it in Sent, returns the invite updated; `DECLINECOUNTER` turns down a guest's proposed time |
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

Search in the UI calls `messages.list` with the pin/query filter so the list is daemon-filtered.

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

Condition fields: `from`, `to`, `subject`, `body`, `header` (with `"header": "List-Id"` — any header of the message), `attachment`, `unread`, `tag`.

A **header** test reads the header it names (`List-Id`, `X-Spam-Flag`, `Reply-To`, …). New mail is fetched with the headers the enabled rules test (`BODY.PEEK[HEADER.FIELDS (…)]`, for those headers alone) and keeps them (`Message.Headers`); **Run now** gets them first for mail already here — from the stored source when a message is downloaded, else from the server (up to 5,000 messages a run) — and a header looked for and absent is remembered as empty, so it is not asked for again. A header a message lacks reads as empty: it contains nothing. A name that could not be a header's (spaces, colons, brackets) is refused when the rule is saved.
Actions: `move` (`folder`), `tag`, `markRead`, `markUnread`, `delete`, `stop`.
AND across conditions. Persist in MemoryStore or the disk cache. The sidebar Tags group and Settings → Tags share one Tags store (locked Unread / Starred / Attachment plus keywords). Settings can add, edit, and remove user tags.

## UI features (v0.10.13)

- **Empty by default** — no demo accounts unless `UITK_MAIL=memory`. First-run Yes/No is only “There are no accounts, want to add one?” Password / `0600` notes are on the Add Account form.
- **Import from other mail clients** — Settings → Accounts → Import scans every client it knows and lists each one it finds as its own section, with two checkboxes:
  - *Config* — the accounts it has set up (servers, ports, encryption, user names, identities, signatures), with a checkbox per account beneath. Accounts already set up here are left out. Passwords are never read; each imported account gets an empty password to fill in, or sign in with OAuth (a Gmail / Outlook account from Geary is marked for it).
  - *Contacts* — the people in its address book (Thunderbird's abook / collected addresses, Evolution's local books, KAddressBook vCards, Claws Mail's address books, mutt aliases, macOS Contacts), added to the address book the Write window completes from.
  - *Filters* — Thunderbird's and KMail's message filters, as rules (Settings → Filters), each scoped to its account's Inbox as the client ran it; a filter that tests or does something rules here cannot (age, priority, forward, reply, moving mail to another account…) is left out whole, and the import says which and why. A filter on a header of the message's own — Thunderbird's custom header, KMail's `List-Id` and the like — comes as a header test.
    - Thunderbird: each server's `msgFilterRules.dat`.
    - KMail: `akonadi_mailfilter_agentrc` (older KMail: `kmail2rc`). A KMail filter runs on the accounts it names, all of them, or — KMail's default — all but IMAP ones; it becomes a rule for each of those that is set up here. Filters that run only by hand or on sending are left out. KMail names the folder a filter moves mail to, and a tag it adds, by an id in Akonadi's database, which is looked up there: the SQLite file (the default since KDE Gear 26.04), opened read-only, or Akonadi's own MySQL server, reached over its socket (`UNIX_SOCKET` in `akonadiserverrc`, else `$XDG_RUNTIME_DIR/akonadi/mysql.socket`) with a read-only session — which answers only while Akonadi runs, so the import says to start KMail and scan again when it does not. PostgreSQL is not read; those filters are left out and the import says so.
  - *Emails* — mail kept only on disk. It goes into an **On This Computer** account that has no server and is never synced, one folder per source folder, read / unread and starred as the client had them (maildir `:2,S`/`F` file names, mbox `Status:` / `X-Status:` and Thunderbird's `X-Mozilla-Status`, Claws Mail's `.claws_mark`, MH `.mh_sequences`, Apple `.emlx` flags; mail from a store that records nothing comes in read), messages the client had marked deleted left out, its original bytes kept (so source and attachments open), de-duplicated by Message-ID (or a digest where there is none) so importing twice adds nothing. Mail on an IMAP server is not listed: it syncs once the account is added.

  | Client | Config from | Emails from |
  | --- | --- | --- |
  | Thunderbird (also Flatpak / Snap) | `prefs.js` | Local Folders (mbox / maildir) |
  | KMail (best-effort) | Akonadi resources, `mailtransports`, `emailidentities` | local maildir |
  | Evolution (also Flatpak) | `sources/*.source` | "On This Computer" Maildir++ |
  | Claws Mail | `accountrc` | MH mailboxes from `folderlist.xml` |
  | Geary (also Flatpak) | `geary.ini` | — (IMAP cache only) |
  | mutt / neomutt | `muttrc` (`folder`, `spoolfile`, `smtp_url`, `from`, `source`) | local `folder`, spool, `mailboxes` |
  | Apple Mail | `MailData/Accounts.plist` (to OS X 10.10), else Internet Accounts (`~/Library/Accounts/Accounts4.sqlite`, best-effort) | "On My Mac" `.mbox` (`.emlx`) |
  | Any folder | — | **Add a folder…** (a folder dialog: maildir, MH, `.eml` / `.emlx`, or a tree of them) or **Add a mailbox file…** (mbox) |

  Other clients' databases (Thunderbird's address books, Akonadi, Contacts, Internet Accounts) are opened read-only, or a copy of them is, so a client left running is neither locked nor changed. macOS Contacts is `AddressBook-v22.abcddb` in `~/Library/Application Support/AddressBook` and each account's copy under `Sources/`. Property lists are read in XML or binary form, NSKeyedArchiver archives included.

  Outlook (`.pst`) is not supported yet.
- **Add account** — IMAP vs POP3 radios, domain auto-guess (including POP hosts), **Test connection** (and optional auto-detect after email+password), masked password field, or Sign in with Google / Microsoft (or device code; IMAP). Saved accounts show the protocol on Account Central and in Settings. `passEnv` remains an optional fallback.
- **Remove account** — File menu, Account Central, and Settings → Accounts. Confirm, then drop the account from `mail.json` and the local cache. The folder tree refreshes; if none remain, the first-run “add one?” prompt returns.
- **Message / Source / Markdown tabs** — Message is the `text/plain` body (default) and Source the raw RFC822.
  - **Open HTML** (in the action row, for a message with an HTML part; nothing of the HTML is drawn in the window) opens the part as it was sent (only its transfer encoding and charset undone — styles, layout and remote images kept; `mailcore.OriginalHTML`), its inline `cid:` images embedded, opened as a temporary page (removed after an hour) under a Content-Security-Policy that blocks scripts, plugins, frames and forms, since a local file could run them. Loading its remote images tells the sender you opened it; the button's tip says so. A message without HTML has no Open HTML.
  - **Markdown** is the message's body rendered from Markdown (`mailcore.BodyMarkdown`) in the window's rich-text view — a clean reading view of HTML mail, and real formatting (headings, emphasis, lists, links, code) for mail written in Markdown. HTML mail is converted — headings, **bold** / *italic*, `code`, links as `[text](url)`, images as `![alt](address)` (never fetched; a 1×1 tracking pixel is left out, an embedded `data:` image is named), lists, `>` quotes, fenced code, and data tables as Markdown tables, while the tables newsletters use for layout become plain blocks. The rendering draws inline images at once; remote ones wait for **Show Images** / **Always**, as before. Tables are drawn as tables (a bold header row, ruled columns, cells keeping their bold, code and links and wrapping to their column), quotes with a rule down their side for each level, and `---` as a line. An image that is not drawn — a remote one not yet asked for — shows its alt text. Markdown written by others is read by comms-mail's own converter (`mailcore.MarkdownToHTML`: CommonMark's common ground and GitHub's tables); raw HTML in it stays text.
  - A message opened in its own tab has the same header, action row and tabs.
- **3-pane splitters** — dragging folder|list or list|preview keeps exclusive pane bounds; preview chrome cannot paint over the thread list.
- **Overflow scrollbars** — thread list, folder tree, and long message bodies show a vertical track/thumb; wheel/trackpad still scroll; offset clamps at the last row. The thread table clips rows under the sticky header (flush at the top; no paint-through while scrolling).
- **Thread columns** — three columns of marks, headed by their icons: a star (filled amber on a starred message), a paperclip (attachments) and the status — a dot while unread, then forward or reply once you have forwarded or answered it (IMAP `$Forwarded` / `\Answered`, from any client). Then Topic (a muted thread's topic starts with a muted bell), Who and When. No Size. Click a column header to sort; the status column puts unread first, then forwarded, then replied. Marks are icons from the look's icon set, never characters from the font. Unread rows are also bold.
- **Card / Table** — **Menu → View** Card view. Remembered in `~/.config/comms-mail/mailui.json`. Star after the message context menu paints immediately.
- **Density** — **Menu → View** Compact / Default / Relaxed.
- **Theme** — comms-mail follows the theme chosen for every uitoolkit app (`~/.config/uitoolkit/look.json`, what uitoolkit's Settings writes), and follows it live. **Settings › Appearance** lists the themes in the build (a filter for light or dark, sortable by name, year, light or dark); clicking one gives comms-mail that theme alone — kept in `mailui.json` (`"theme"`), over look.json's, which is not touched, so other uitoolkit apps keep theirs and a later change to look.json does not undo it. **Use the shared theme** goes back to following look.json. Corners, icons and fonts still come from look.json. uitoolkit only draws a theme whose engine is in the build (its engines are opt-in at build time): `make` builds comms-mail with all of them (`-tags theme_engine_all`); a theme a build lacks is drawn as the default, and comms-mail says so on stderr when it starts.
- **Folder actions** — the folder tree's right-click menu creates a folder, marks a real folder read (cache + server `UID STORE \Seen`), and deletes a user-created folder (system and virtual folders are refused; the server mailbox is `DELETE`d, then the folder, its messages and its sync state are dropped — confirmed first).
- **Folder tree** — account folders and Tags at the top of the sidebar; Outbox is pinned to the **bottom** of the pane (separated from Tags). Unified Folders, Smart Folders, Categories, and VIP are not shown. Click an account root to open that Inbox. The Tags group is the same list as Settings → Tags: locked Unread / Starred / Attachment pins (a check icon when on) plus every keyword (Important, Work, Personal, To Do, Later, and user-created).
- **Chrome** — no path/subtitle strip, no bottom status bar, no unread-folder-count footer, no sidebar Account / Folders section headers, no identity or Tags ComboBox, and no active-filter banner (`Filter on · N shown` / `Clear filter`) above the thread list. The folder pane runs the window's height on the left, with nothing over it: the tree (including Tags) at its top and Outbox at its foot. The title bar holds the app menu's button, **Fetch**, **Write** and **Search** — push buttons with an icon alone, their names in their tips (the menu's is the menu icon, three bars; Search stays pressed while a search is narrowing the list) — and the tabs, starting where the list and the reading pane do; over the folder pane it is empty, and like the rest of its free space it moves the window. That row is the window's title bar (`Window.SetTitleBar`): where uitoolkit draws the frame — by default on KDE Plasma, always on GNOME — it is the caption, with the caption buttons at the desktop's sides, under every theme (comms-mail asks for `CaptionMerged`, so a classic theme such as KDE 1 or Windows 95 does not put its own title strip above it, and under BeOS it is a full-width caption rather than the yellow tab); with **Use system title bar and borders** (or `UITK_DECORATIONS=server`) it is the first row under the desktop's frame. See [decorations.md](https://github.com/codemodify/uitoolkit/blob/dev/docs/decorations.md). Tag, Archive, Junk, Cards, Classic, and Delete are not on that row. There is no strip above the Topic / Who / When header, and no search field in the window: search is a dialog (see *Search* above). Filter pins are only on the Tags tree. Reply and Forward are keyboard / context menu only. There is no menu bar: the app menu drops from its button (or F10) — **View** submenu (layout / list / density / Threaded / Hide muted threads), **Notify** submenu (new mail / VIP-only / desktop), Settings (Ctrl+,), Quit (Ctrl+Q). Settings is Accounts + Signatures + Tags (the same Tags model as the sidebar). Menu and toolbar hover do not refresh the folder tree or the message list.
- **Threaded** view and **Mute Thread**.
- **Message tabs** — **E**, a double click or Return on a row opens the message in a tab of its own (an envelope before its subject; the Mail tab shows the inbox), in the title bar after the menu button and Fetch / Write / Search, the way the uitoolkit Files sample opens folders. A tab's page takes the place of the list and the reading pane; the folder pane stays. The first tab, **Mail**, is the list and the reading pane and cannot be closed. Ctrl+Tab / Ctrl+Shift+Tab switch tabs and Ctrl+W closes one; a tab's right-click menu has Close Tab and Close Other Tabs. The message keys act on the tab's message while it is in front, and archiving, junking, deleting or moving it closes its tab. Open tabs survive a layout or density change.
- **Recipients as chips** — each address in To / Cc / Bcc is a chip with a cross that takes it out. A comma, a semicolon or Return ends an address; a comma inside a quoted name (`"Doe, Jane" <jane@example.com>`) is part of the name. Text that is not an address stays in the field as text, where you can see and fix it (and Send still says what is wrong with it); an address already there is not added twice; Backspace in the empty field takes the last chip back to edit; leaving the field keeps what was typed. A draft or a reply opens with its recipients as chips.
- **Address autocomplete** — To / Cc / Bcc complete from an address book built out of the cached messages (everyone in a From, To or Cc). Type into a recipient and a ranked list drops over the window under it — most-corresponded-with first, matched on name or address, leaving out anyone already on the field — while the keyboard stays in the field, so typing goes on; Down/Up choose, Return or Tab (or a click) makes the highlighted one a chip, Escape dismisses. A name with a comma in it is quoted when it goes in. `contacts.suggest` serves it.
- **Attach by dropping** — files dropped anywhere on the Write window, the message text included, are attached (not typed in as their paths); text dropped on a field goes into it.
- **Reply / Reply All** — Reply goes to the sender (or `Reply-To`) only. Reply All adds everyone else on To and keeps the original Cc, leaving out every address you send from.
- **Move to** — the message menu's Move to lists the account's folders as the sidebar nests them. Dragging selected rows onto a folder in the sidebar moves them too.
- **Undo** — Archive, Junk, Delete and Move take the rows out at once and show a bar with **Undo** (also Ctrl+Z) for six seconds; the daemon hears of the change only when that window closes, a second move cuts it short, or the window closes or Mail quits.
- **Signatures** — Settings → Signatures sets one per From address. Write puts it under the message and above any quote, where it can be edited or deleted, and swaps it when you change From; a message sent from Write is never given a second copy.
- **Attachments** — per-row Open / Save As; toolbar Save All (one folder pick, then write all files); single click selects, double click or row Open opens.
- **Snappy open** — a click shows the headers from the list row at once and loads the body in the background ("Loading message…", Retry on failure); recent bodies are prefetched after each sync, so most clicks need no round trip.
- **Colored tags** — Settings → Tags is the same list as the sidebar Tags group, with Add / Edit / Remove (Remove disabled for Unread, Starred, and Attachment). New mail is tagged Unread; parts with a filename get Attachment.

## Keyboard (Thunderbird-like)

Documented in [keyboard.md](https://github.com/codemodify/uitoolkit/blob/dev/docs/keyboard.md). When the thread list (not a text field) has focus:

| Key | Action |
| --- | --- |
| **n** / **p** | Next / previous message |
| **d** / **#** / **Del** | Delete |
| **t** | Tag menu (toggle any of your tags) |
| **e** / double click / Return | Open in a new tab |
| **Ctrl+Tab** / **Ctrl+Shift+Tab** | Next / previous tab |
| **Ctrl+W** | Close the tab |
| **r** | Reply |
| **Shift+R** / **Ctrl+Shift+R** | Reply All |
| **f** | Forward |
| **a** | Archive |
| **Ctrl+Z** | Undo the last archive / junk / delete / move |
| **c** | Compose |
| **m** | Mark as read |
| **s** | Star / unstar |
| **j** | Junk (move to Junk; Ctrl+Z takes it back) |
| **F5** | Fetch / sync |
| **Ctrl+,** | Settings |
| **F10** | The app menu |
| **Ctrl+Q** | Quit |

## How to try (dogfood)

```bash
UITK_MAIL=memory go run ./cmd/comms-maild
UITK_SCENE=auto go run ./cmd/comms-mail
```

- **VIP** — open a Kai / Thunderbird Team message → Message → Add sender to VIP (Menu → Notify can restrict alerts to VIP senders; no VIP folder in the tree).
- **Threading** — View → Threaded; look for “Thread: lunch plans (3)”. Message → Mute Thread.
- **OAuth** — File → Add Account, enter a Gmail/Outlook address (hosts fill in), paste your client id, Sign in with Google / Microsoft. Approve in the browser (or use Device code). Then Get Messages.

## Screenshots

```bash
go run ./cmd/comms-mail-demo -screenshot docs/screenshots
```

Writes `mail-dark.png`, `mail-light.png`, `mail-classic.png`, `mail-compose.png`, `mail-prefs.png`, `mail-cards.png`, `mail-compact.png`, `mail-filters.png`, `mail-empty.png` (first-run dialog), `mail-account.png` (Add Account), `mail-smart.png` (daemon saved-search view; not a tree section), `mail-invite.png` (a meeting invitation's card).
