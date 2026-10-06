# Email client layouts, 1971–2026

How mail clients have laid out their screens over fifty years, gathered as
input for the layout of comms-mail. There are 79 screenshots of about 70
clients, plus comms-mail's own screens at the end.

**How this was put together.** Each screenshot was found on Wikipedia or
Wikimedia Commons, in a vendor's documentation or press kit, a book or
manual, a computer-history gallery, or an archived web page. Each one was
opened and checked to show the client's real mail screen. The caption says
what is in the picture, and the version shown when that is not the era's
own. [Sources and licences](#sources-and-licences) lists where every image
came from.

**Before sharing.** Only some images are freely licensed (Commons: GPL,
LGPL, MPL, MIT, Apache, CC, public domain). Many are fair-use Wikipedia
images, vendor screenshots, book figures or frames from videos. Keep this
report private unless each image's rights are checked.

**Gaps.** No screenshot was found for VMS MAIL, cc:Mail, BlitzMail,
Outlook 98/2000, 1990s Hotmail or Yahoo! Mail, or a Pine folder index from
the 1990s. The Wayback Machine was offline and Wikimedia was rate-limiting
during the search. Where a period screenshot was missing, a later version
with the same layout stands in, and its caption says so.

---

## The short version: seven layouts

Nearly every client in fifty years uses one of seven layouts, or a mix of
them.

| # | Layout | First seen | Still used by |
| --- | --- | --- | --- |
| 1 | **The line and the index.** A list of one-line headers fills the screen; opening a message replaces it with the text; commands are keys, often listed on screen | `mail` (1970s), MH, Elm, Pine | Alpine, Mutt, NeoMutt, aerc |
| 2 | **Windows in windows.** A window per mailbox, a window per message, a floating folder list | Eudora, Microsoft Mail, NeXTMail, Pegasus (late 80s–mid 90s) | the Write window everywhere |
| 3 | **Three panes, message below.** Folder tree on the left; message list on top, reading pane under it | Microsoft Exchange client and Netscape Messenger (1995–97), Outlook Express | Thunderbird's classic view, Evolution, KMail, Claws, The Bat! |
| 4 | **Three columns, message on the right.** Two-line rows grouped by date; the reading pane takes half the window | Outlook 2003 | Apple Mail, Windows Mail, Geary, the new Outlook, Thunderbird's cards, Airmail, Mimestream |
| 5 | **The conversation.** A thread read as a stack of messages, the newest open, older ones folded to a line | Gmail (2004) | nearly all of them now |
| 6 | **One column, triage.** The list is the whole screen; each row a card with a snippet; acted on by swipe, hover or key; bundles and screeners sort what arrives | iPhone Mail (2007), Mailbox (2013), Inbox by Gmail (2014) | HEY, Superhuman, Spark, Gmail's default, Notion Mail |
| 7 | **The suite and the assistant.** An app rail (mail, calendar, contacts, tasks), side panels about the sender, AI summaries and replies | Outlook's navigation pane and Evolution's switchers (late 90s–2000s); Gmail 2022, new Outlook | Thunderbird's Spaces, Fastmail, Shortwave, Spark, Apple Mail's Summarize |

What changed inside those layouts matters just as much:

- **The row** went from one line of columns (flags, number, date, sender,
  size, subject) to two lines (sender and time, then subject) in 2003, to
  three or four lines with a snippet and avatar (Sparrow, iPhone, Apple
  Mail Lion), to cards (Inbox by Gmail, Thunderbird 115+). Marks moved from
  letters (`N`, `O`, `D`, `+`) to icon columns to icons at the row's end.
- **The header of the message** shrank. It went from a full header block
  to one line of sender with an avatar, the recipients folded, the date at
  the right, the subject large, tags as chips, and attachments as tiles.
  Actions moved into that header (Thunderbird 3, Outlook 2010, Geary,
  Thunderbird 140).
- **Commands** moved from the key legend at the bottom of the screen
  (Pine, Elm) to labelled toolbars (1990s), ribbons (2007–2013), and
  buttons in the reading pane. Then came hover actions and swipes on the
  row (2013+), and a command palette (Superhuman).
- **Search** moved from a menu to a box on the toolbar, then to a bar
  across the top of the window (Gmail, Thunderbird 140, the new Outlook).
- **Folders** gave way to views. Favourites (Outlook 2003) became smart
  folders (Airmail), then split inboxes and user-defined views (Superhuman,
  Notion Mail).

---

## 1971–1989: the terminal and the first windows

Line-mode and full-screen terminal clients, and the first graphical ones
on the Mac, NeXT and Windows. Where no screenshot from those years was
found, a later version with the same layout stands in.

### Unix `mail` / BSD `Mail` (1970s; shown: BSD Mail 8.1, 1993)
![BSD Mail](screens/1993-bsd-mail.png)
Line mode, with no screen layout at all: a banner, the `&` prompt, and
one-letter commands that take a message list (`t`, `d`, `r`, `s`, `h`,
`q`). Everything after it was built to get away from typing message
numbers.

### MH, through xmh (1979; shown: 1992)
![xmh](screens/1992-mh-xmh.gif)
One window stacked top to bottom: menu buttons, a row of folder buttons,
the table of contents, then the message. Each list row is MH's `scan`
format: number, `+` for the current message, date, sender, subject, and
the start of the body after `<<`. This is the first snippet in a list.

### IBM PROFS (1981; shown: 1987)
![PROFS](screens/1987-ibm-profs.png)
A 3270 form: "OPEN THE MAIL". Two lines per item (From / To / Type / Due
date, then Subject), each opened by its own PF key. The key legend at the
bottom was the menu.

### Emacs RMAIL (1985; shown: 2020)
![RMAIL](screens/2020-emacs-rmail.png)
Two Emacs windows: the summary (number, date, sender, line count, labels,
subject) above the message. The list above the message, which three-pane
clients later made standard, was already here.

### Elm (1986; shown: 2.5, 2003)
![Elm](screens/2003-elm.png)
The full-screen index: status, number, date, sender, line count, subject,
with the current row in reverse video. The commands are spelled out at the
bottom above a `Command:` prompt.

### Microsoft Mail (1988; shown: PC Networks 2.1, 1991)
![Microsoft Mail 2.1](screens/1991-microsoft-mail-pc-networks.png)
An Inbox window with From / Subject / Date / Time / Pri columns. A message
opens in its own window, with a header block and Reply / Forward / Delete /
Next / Previous buttons along the bottom.

### Eudora for Mac (1988; shown: 1.5, 1995)
![Eudora Mac](screens/1995-eudora-mac.gif)
One window per mailbox, and no column headers: a status letter (R, F, D),
sender, date, size, subject. Messages open in windows of their own.

### Lotus Notes (1989; shown: 2.1, 1992)
![Lotus Notes 2](screens/1992-lotus-notes.png)
Not a mail screen but the workspace: tabbed pages of database tiles. Mail
is one database among the others, which is how Notes treated it for the
next twenty years.

### Pine (1989; shown: 3.96, 1997)
![Pine](screens/1997-pine.png)
A menu of one-key commands, and a two-row key legend on every screen. Its
index view is the one Alpine shows (below, 2008).

### NeXTMail (1988; shown: NeXTSTEP 2.0, 1990)
![NeXTMail](screens/1990-nextmail.png)
One window: a toolbar of large icons, the list, a split, and the message,
here with rich text and an inline photograph. This is the modern desktop
mail window, fifteen years early.

### QuickMail (CE Software; shown: 1990)
![QuickMail](screens/1990-quickmail.png)
A message form rather than a list: priority, From / To / CC / BCC, and a
"Regarding" field. Office mail treated messages as forms.

---

## 1990–1999: graphical clients, windows in windows, the first three panes

### Pegasus Mail (1990; shown: 2.x, 1995)
![Pegasus Mail](screens/1995-pegasus-mail-2.png)
A folder window inside the main frame. A row of labelled icon buttons
(Open, Reply, Forward, Move, Copy, Delete, Info) sits above a From /
Subject / Date list. No tree, no preview.

### Microsoft Mail 3.x (1992; shown: in Windows NT 3.x)
![Microsoft Mail 3](screens/1993-microsoft-mail-3.png)
The tree arrives: a "Private Folders" tree beside the list, in a window
inside the frame, with the opened message overlapping it.

### Microsoft Exchange client (1995)
![Exchange client](screens/1995-microsoft-exchange-client.png)
One window, a tree on the left, and a list with importance, envelope and
paperclip columns. Unread rows are bold, and the status bar reads
"1 Item, 1 Unread". Much of Outlook is already here.

### Claris Emailer (1996)
![Claris Emailer](screens/1996-claris-emailer.png)
A browser window with a folder list and columns including Account. It is
one of the first clients built around several accounts in one window.

### Lotus Notes 4 (1996)
![Lotus Notes 4](screens/1996-lotus-notes-4.png)
An action bar (New Memo, Reply, Reply With History, Move To Folder),
navigator pane, and a view with an unread-star margin; no preview in this
shot.

### Eudora Pro 4 (1997)
![Eudora Pro 4](screens/1997-eudora-pro-4.png)
Windows in windows at their most developed: a floating Mailboxes tree, a
mailbox window with its own preview pane, and tabs for the open windows
along the bottom. The columns are status, priority, paperclip, label,
who, date, size and subject.

### Novell GroupWise 5 (1997)
![GroupWise 5](screens/1997-groupwise-5.png)
A launcher window (In Box, Out Box, Trash, Calendar, Send Mail, Schedule,
Task, Note, Phone Message) and a separate In Box window. Mail is one kind
of item among several.

### Netscape Messenger 4 (1997)
![Netscape Messenger](screens/1997-netscape-messenger-4.png)
Big labelled toolbar icons, and a folder pop-up instead of a tree. The
list sits over a collapsible message pane.

### Microsoft Outlook 97
![Outlook 97](screens/1997-outlook-97.png)
Partly covered by its About box. What shows is the Outlook Bar down the
left (Inbox, Calendar, Contacts, Tasks: the first app rail), a view
picker ("Messages with AutoPreview"), and a folder banner.

### KMail (KDE Beta 3, 1998)
![KMail 1998](screens/1998-kmail-kde-beta3.png)
Folders top-left, list top-right, and the reader across the whole width
below. A separate composer window overlaps it.

### Outlook Express 4.5 for Mac, and Outlook Express 5 (1999)
![Outlook Express Mac](screens/1999-outlook-express-4-5-mac.png)
The three panes that most clients used for the next decade: folders on
the left, the list top right (status, !, paperclip, subject, from, date),
and the preview below, under a labelled toolbar.

![Outlook Express 5](screens/1999-outlook-express-5.png)
The Windows edition's start page, with a Folders tree and a Contacts pane
under it.

### Hotmail (1996; shown: 2001) and Yahoo! Mail (1997; shown: 2001)
![Hotmail 2001](screens/2001-hotmail.jpg)
![Yahoo Mail 2001](screens/2001-yahoo-mail.png)
Webmail: a page, not an application. A full-width table of messages with
no preview pane, links down the left, ads, and paging ("1–25 of 119").

### Mutt (1995; shown: 2004)
![Mutt](screens/2004-mutt.png)
The terminal's answer to the preview pane: a short index at the top and
the message below it, with a help line at the top and status bars between
the parts.

### BeOS Mail (1998; shown: R5, 2000)
![BeMail](screens/2000-beos-bemail.png)
Only a compose window. BeOS had no mail list: the Tracker, its file
manager, showed mail as files with columns. This is mail as a file
system.

### The Bat! (1998; shown: 2018)
![The Bat!](screens/2018-the-bat.png)
The classic three panes, with Unread / Total count columns in the folder
tree and a quick-reply box under the preview.

---

## 2000–2012: the three panes mature, conversations, the phone

### Apple Mail (2001; shown: Jaguar 2002, Leopard 2007, Lion 2011)
![Apple Mail Jaguar](screens/2002-apple-mail-jaguar.png)
2002: list over preview, with the mailboxes in a drawer sliding out of the
window's side.

![Apple Mail Leopard](screens/2007-apple-mail-leopard.jpg)
2007: the mailboxes come into the window as a source list, with Notes, To
Do and RSS beside them.

![Apple Mail Lion](screens/2011-apple-mail-lion.jpg)
2011: the widescreen turn. A narrow list of four-line rows (sender, date,
subject, two-line snippet, thread count) on the left, and the
conversation as stacked message cards on the right.

### Outlook 2003
![Outlook 2003](screens/2003-outlook-2003.jpg)
The most copied layout change of the decade. The reading pane moves to the
right and takes half the window. Rows become two lines (bold sender and
time, then the subject) and are grouped under Today and Yesterday.
Favourite Folders sit above the tree, and big Mail / Calendar / Contacts /
Tasks buttons below it.

### Outlook 2007, Outlook 2010
![Outlook 2007](screens/2007-outlook-2007.png)
2007: the To-Do Bar (month calendar, appointments, tasks) is added at the
right edge.

![Outlook 2010](screens/2010-outlook-2010.png)
2010: the ribbon reaches the mail window, with conversations in the list
and the People Pane under the message.

### Gmail (2004; shown: 2004 and 2012)
![Gmail 2004](screens/2004-gmail.png)
The conversation: the messages of a thread stacked as cards, an inline
reply box, and labels instead of folders. The footer hints at keyboard
shortcuts.

![Gmail 2012](screens/2012-gmail.png)
A full-width list of single-line rows (checkbox, star, importance, sender,
subject, grey snippet, date), with no reading pane. A message opens in
place of the list.

### Evolution (2000; shown: 2.x, 2005)
![Evolution 2005](screens/2005-evolution-2.png)
Three panes over big Mail / Contacts / Tasks / Calendar switcher buttons,
with a "Subject or Sender contains" search bar above the list.

### Sylpheed (2000; shown: 2.2, 2006), Claws Mail (2007)
![Sylpheed](screens/2006-sylpheed-2.2.jpg)
![Claws Mail](screens/2007-claws-mail-2.7.png)
Classic three panes. Claws adds a quick-search bar between list and
message, and a strip of buttons for the message's parts.

### Balsa (shown: 2004)
![Balsa](screens/2004-balsa.png)
Open mailboxes as notebook tabs over the list, and GPG signature status in
the message.

### Mozilla Thunderbird 2 (2007) and 3.1 (2010)
![Thunderbird 2](screens/2007-thunderbird-2.png)
2007: three panes, with Tag and Junk on the toolbar and a search box at
the right.

![Thunderbird 3.1](screens/2010-thunderbird-3.1.png)
2010: a tab strip, a global search box, and a Quick Filter bar over the
list. Reply, forward, archive, junk and delete move into the message
header. comms-mail's chrome comes from this version.

### Windows Mail (Vista, 2007), Windows Live Mail (2012)
![Windows Mail](screens/2007-windows-mail.png)
![Windows Live Mail](screens/2012-windows-live-mail.png)
Outlook Express's three panes, then a ribbon, a module switcher and a
calendar pane.

### Alpine (2008)
![Alpine](screens/2008-alpine.png)
Pine's index: flags, number, date, sender, size, subject with an ASCII
thread tree, and the two-row command legend at the bottom.

### iPhone Mail (2008), Android Gmail (2009)
![iPhone Mail](screens/2008-iphone-mail.jpg)
One column: blue unread dot, bold sender, time, subject, a two-line
snippet. The row that desktop clients later copied.

![Android Gmail](screens/2009-android-gmail-g1.png)
Two-line rows, and a conversation view with a label chip.

### Opera Mail (M2, shown: about 2009)
![Opera M2](screens/2009-opera-m2.png)
Mail inside the browser, organised by views (Unread, Active threads,
Attachments, Labels) rather than folders.

### Zimbra (shown: 2009)
![Zimbra](screens/2009-zimbra.jpg)
A desktop-like web app: tabs for Mail, Address Book, Calendar, Tasks;
one-line rows with a grey snippet; reading pane below.

### Postbox 2 (2010)
![Postbox](screens/2010-postbox-2.png)
A second "Focus" pane that filters the list by attributes, topics,
contacts and date: faceted browsing of mail.

### Sparrow (2010)
![Sparrow](screens/2010-sparrow.png)
A narrow icon rail (Tweetie style), multi-line rows with a three-line
snippet and count badge, and four toolbar icons in all. Many later
clients started from this.

### Lotus Notes 8.5.3 (2011), Yahoo! Mail (2011), Hotmail (2010), Outlook.com (2012)
![Lotus Notes 8.5](screens/2011-lotus-notes-8.5.3.jpg)
![Yahoo Mail 2011](screens/2011-yahoo-mail.png)
![Hotmail Wave 4](screens/2010-windows-live-hotmail.png)
![Outlook.com](screens/2012-outlook-com.png)
Webmail catches up with the desktop: a folder list, two-line rows with
checkbox and flag, a reading pane. Outlook.com (2012) adds "Quick views"
(Flagged, Photos, Documents, Shipping) and a column about the sender.

---

## 2013–2026: triage, bundles, screeners, rails and assistants

### Mailbox (2013)
![Mailbox](screens/2013-mailbox.jpg)
Swiping is the interface. A row swiped one way is archived, the other way
snoozed ("Later"). The top bar has three places: Later, Inbox, Done.

### Inbox by Gmail (2014)
![Inbox by Gmail](screens/2014-inbox-by-gmail.png)
Cards grouped by day, bundles (Purchases, Promos) as single rows, pinned
items and reminders in the list, and "sweep" to mark a whole day done.

### Outlook mobile (2015), Windows 10 Mail (2015)
![Outlook mobile](screens/2015-outlook-mobile.png)
![Windows 10 Mail](screens/2015-windows-10-mail.png)
Focused / Other tabs and swipe actions on the phone. On the desktop,
three panes with three-line rows and a blue bar marking unread.

### Mailspring (2017), Newton (2017)
![Mailspring](screens/2017-mailspring.png)
Four columns: folders, threads, the conversation with an inline composer,
and a card about the sender.

![Newton](screens/2017-newton.png)
Follow-up prompts under the cards ("No response. Follow up?").

### Superhuman (about 2019)
![Superhuman](screens/2019-superhuman.png)
No folder pane, a wide list of one-line rows, actions on hover, and a
column about the sender. Everything runs from the keyboard, through a
command palette that shows each action's key.

### HEY (2020)
![HEY](screens/2020-hey.jpg)
The Imbox: one centred column, NEW FOR YOU above PREVIOUSLY SEEN, a
Screener for first-time senders, and stacks for "reply later" and "set
aside".

### NeoMutt (2020), aerc (2024)
![NeoMutt](screens/2020-neomutt.png)
![aerc](screens/2024-aerc.png)
The terminal keeps the index. It adds a sidebar of mailboxes with counts,
tabs for accounts, thread trees drawn with box characters, and a vim-style
command line.

### Geary (2021)
![Geary](screens/2021-geary.png)
Three panes, each with its own header bar. Rows have three lines with
relative time ("22m ago"). The conversation view has the actions above it.

### Airmail (2022), Mimestream (2026)
![Airmail](screens/2022-airmail.png)
![Mimestream](screens/2026-mimestream.png)
Mac three-column clients: smart folders or Gmail categories in the
sidebar, multi-line rows with label and attachment chips, and removable
tag chips on the message.

### Gmail (2022)
![Gmail 2022](screens/2022-gmail.png)
An app rail at the far left (Mail, Chat, Spaces, Meet), a Compose pill,
one-line rows with attachment chips, and no reading pane by default.
Side apps (Calendar, Keep, Tasks) sit at the right edge.

### Evolution (2023), KMail (2024)
![Evolution 2023](screens/2023-evolution.png)
![KMail 2024](screens/2024-kmail.png)
The classic layout, kept current. Evolution adds a To Do bar; KMail uses
two-line rows and shows the DKIM result in its status bar.

### The new Outlook for Windows (2024)
![New Outlook](screens/2024-new-outlook-windows.png)
App rail, folders with Favourites, Focused / Other tabs, rows grouped by
date, meeting cards with RSVP inside the list, a one-line ribbon, and a
reading pane with a meeting card.

### Proton Mail (2024), Fastmail (2025)
![Proton Mail](screens/2024-proton-mail.png)
![Fastmail](screens/2025-fastmail.png)
Privacy webmail: one-line rows with no reading pane (Proton's "row"
layout), or rows with label chips, pins, snooze time and an attachment
clip (Fastmail).

### Shortwave (2024), Spark (2025), Notion Mail (2025)
![Shortwave](screens/2024-shortwave.png)
![Spark](screens/2025-spark.png)
![Notion Mail](screens/2025-notion-mail.png)
Split inboxes and bundles (Important / Other / Todos; Notifications,
Newsletters) and user-defined views. Shortwave adds an AI summary line,
suggested replies and an AI panel. Spark shows actions on hover.

### Thunderbird 140 (2025)
![Thunderbird 140](screens/2025-thunderbird-140.png)
The client comms-mail replaces, today:
- a Spaces rail (mail, contacts, calendar, tasks, chat, settings), a
  unified search bar across the top, and folders with Unified Folders and
  Tags;
- the list in **Cards** view: two lines per message, tag, attachment and
  star icons on the card, and threads shown as a "2 replies" pill with the
  replies indented under it;
- a **compact message header**: avatar and sender, To on one line, the
  subject large, tags as chips, and Reply / Forward / Archive / Junk /
  Delete / More as buttons in the header row.

### Apple Mail (macOS Tahoe, 2026)
![Apple Mail 2026](screens/2026-apple-mail.png)
The sidebar can be hidden, leaving two panes: a list with category tabs
and four-line rows, and the message with capsule buttons for every action
and a Summarize button.

---

## comms-mail today

For comparison, comms-mail's screens as of this report (the demo mailbox;
light and dark looks).

**Three columns** (the default), with the Thunderbird 3 chrome in the title
bar: M menu, Fetch / Write, message tabs, quick filter, All folders, On
server.
![comms-mail vertical](screens/2026-comms-mail-vertical.png)

**Classic**: the list above the reading pane (the M menu open).
![comms-mail classic](screens/2026-comms-mail-classic.png)

**Card view**:
![comms-mail cards](screens/2026-comms-mail-cards.png)

**A message in a tab of its own**, the reading pane larger:
![comms-mail message tab](screens/2026-comms-mail-message-tab.png)

**Write**, and **Settings**:
![comms-mail write](screens/2026-comms-mail-write.png)
![comms-mail settings](screens/2026-comms-mail-settings.png)

### Where it stands against the fifty years

comms-mail is a **layout 3 / layout 4** client: Thunderbird 3's chrome,
with a table list or cards, a reading pane on the right (Vertical) or
below (Classic), and conversations as indented threads. Next to what came
after, these stand out:

1. **The message header is tall.** The subject, then From, To, Date and a
   tags / attachments line each on a line of their own, the action row,
   and the attachment list. In Classic at 1280×800 that leaves about
   130 px for the text; in Vertical the text starts halfway down.
   Thunderbird 140, Apple Mail, Geary and the new Outlook all fit the same
   information in two or three lines: sender (with an avatar or initials)
   and date on one line, recipients folded to one line, the subject large,
   tags as chips, attachments as tiles or chips.
2. **Reply, Forward, Archive, Junk and Delete have no buttons.** They are
   keys and the right-click menu only. Every graphical client since about
   2010 puts them at the top of the message (Thunderbird 3.1 and 140,
   Geary, Outlook, Apple Mail). The action row comms-mail now has
   (attachments, Open HTML) is where they would go.
3. **The list rows are one line of columns** (star, paperclip, status,
   topic, who, when), which is Thunderbird's table. Since 2003 the
   three-column clients use two-line rows (sender and time, then subject)
   and group them under Today / Yesterday / This week. The card view has
   the lines but not the marks (paperclip, replied, forwarded), which
   Thunderbird 140's cards carry.
4. **Search is behind a toggle.** The quick filter opens from a button.
   Since Thunderbird 3 and Gmail, search has been an always-visible box,
   and now a bar across the top.
5. **Source is a peer of the message.** Message / Source / Markdown sit
   side by side under the header. No other client puts the raw message on
   the same level as the message; it is a menu item or a window (View
   Source). Markdown as a reading mode is comms-mail's own.
6. **The Write window shows To, Cc and Bcc always,** and From as a
   drop-down even with one identity. Current clients show To alone and
   open Cc / Bcc on request.
7. **No rail, no views.** Thunderbird's Spaces, Outlook's rail and Gmail's
   rail each put mail beside calendar, contacts and tasks. comms-mail is
   mail only (calendar invitations are handed to the calendar
   application), so a rail would have one item. Views that sort the inbox
   are the other direction: Unread / Starred / Attachment pins are already
   in the Tags tree, and split inboxes, bundles and screeners are what the
   triage clients add.

### Decisions for the layout discussion

- **Message header:** keep the full block, or go compact (sender line,
  folded recipients, subject, chips), with the details on demand?
- **Message actions:** buttons at the top of the message (Reply, Reply
  All, Forward, Archive, Junk, Delete, More), in the action row, as
  Thunderbird 140 does?
- **List rows:** keep the one-line table as the default, or make two-line
  rows (or cards with marks) the default; group by date?
- **Search:** an always-visible search field instead of the toggle?
- **Source:** keep it as a tab, or move it to a menu (and Ctrl+U) as
  everyone else does?
- **Write:** hide Cc / Bcc until asked for; hide From with one identity?
- **Beyond Thunderbird:** anything from layouts 6 and 7 to take up: a
  focused / other split, bundles, a screener for first-time senders,
  snooze, hover actions on rows, a command palette?

---

## Sources and licences

"No licence stated" means the page gives no licence. The image is the
copyright of its publisher or the software's maker, and is here for
private reference.

| File | Client | Source | Licence / author |
| --- | --- | --- | --- |
| 1987-ibm-profs.png | IBM PROFS | https://www.youtube.com/watch?v=FIqbesDvNL8 (frame ~43:16) | No licence stated (HS Tech Channel) |
| 1990-nextmail.png | NeXTMail | http://toastytech.com/guis/ns202.html | No licence stated (Toasty Technology GUI Gallery) |
| 1990-quickmail.png | QuickMail | https://www.youtube.com/watch?v=j8FyVsYKFzw (frame ~14:32) | No licence stated (Apple promotional video, 1990) |
| 1991-microsoft-mail-pc-networks.png | Microsoft Mail 2.1 | https://winworldpc.com/product/pc-mail/21 | No licence stated (WinWorld) |
| 1992-lotus-notes.png | Lotus Notes 2.1 | https://www.youtube.com/watch?v=i5K7rLduyRw (frame ~0:07) | No licence stated (MathiasPohl) |
| 1992-mh-xmh.gif | MH / xmh | https://www.oreilly.com/openbook/mh/getmai.htm | GNU GPL (online book), © O'Reilly, Jerry Peek |
| 1993-bsd-mail.png | BSD Mail | https://commons.wikimedia.org/wiki/File:Mail-interface.png | Public domain, George Shuklin |
| 1993-microsoft-mail-3.png | Microsoft Mail 3.x | https://en.wikipedia.org/wiki/File:Microsoft_Mail.png | Non-free (fair use), Nandhp |
| 1995-eudora-mac.gif | Eudora 1.5 (Mac) | https://tidbits.com/resources/iskm3html/pt4/ch21/ch21a.html | No licence stated (Internet Starter Kit, TidBITS) |
| 1995-microsoft-exchange-client.png | Exchange client | https://guidebookgallery.org/screenshots/mail | GUIdebook, © Marcin Wichary; software © Microsoft |
| 1995-pegasus-mail-2.png | Pegasus Mail 2 | http://www.email.bham.ac.uk/pegasus/PegWinS3.htm | No licence stated (University of Birmingham guide) |
| 1996-claris-emailer.png | Claris Emailer | https://en.wikipedia.org/wiki/File:Clarisemailerscreen.png | Non-free (fair use), Ww2censor |
| 1996-lotus-notes-4.png | Lotus Notes 4 | https://www.rigacci.org/docs/biblio/online/lotusn/ch09.htm | Book figure (Que), no licence stated |
| 1997-eudora-pro-4.png | Eudora Pro 4 | archive.org, qualcomm-eudora-pro-4.0 ISO, Eudora Pro User Manual p. 105 | Qualcomm manual, no licence stated |
| 1997-groupwise-5.png | GroupWise 5 | https://winworldpc.com/screenshot/1d55088a-5250-11ec-b881-0200008a0da4/1c86cbbd-5251-11ec-b881-0200008a0da4 | No licence stated (WinWorld) |
| 1997-netscape-messenger-4.png | Netscape Messenger 4 | https://www.webdesignmuseum.org/software/netscape-communicator-4-01-in-1997 | No licence stated (Web Design Museum) |
| 1997-outlook-97.png | Outlook 97 | https://winworldpc.com/screenshot/c2a86058-7728-c392-11c3-a4e284a2c3a5/541dfdde-539a-11e9-8581-fa163e9022f0 | No licence stated (WinWorld) |
| 1997-pine.png | Pine 3.96 | https://commons.wikimedia.org/wiki/File:Pine-3.96_Screenshot.png | CC0, Majenko |
| 1998-kmail-kde-beta3.png | KMail (KDE Beta 3) | https://commons.wikimedia.org/wiki/File:KDE_Beta3_-_KMail.png | GPL, KDE |
| 1999-outlook-express-4-5-mac.png | Outlook Express 4.5 (Mac) | https://guidebookgallery.org/screenshots/mail | GUIdebook, © Marcin Wichary; software © Microsoft |
| 1999-outlook-express-5.png | Outlook Express 5 | https://guidebookgallery.org/screenshots/mail | GUIdebook, © Marcin Wichary; software © Microsoft |
| 2000-beos-bemail.png | BeOS Mail | https://guidebookgallery.org/screenshots/mail | GUIdebook, © Marcin Wichary; software © Be Inc. |
| 2001-hotmail.jpg | MSN Hotmail | https://en.wikipedia.org/wiki/File:Hotmail_old_screenshot.jpg | Non-free (fair use) |
| 2001-yahoo-mail.png | Yahoo! Mail | https://en.wikipedia.org/wiki/File:Ymail_2001.png | Non-free (fair use), Tim42 |
| 2002-apple-mail-jaguar.png | Apple Mail (10.2) | https://guidebookgallery.org/screenshots/macosx102 | GUIdebook, © Marcin Wichary; software © Apple |
| 2003-elm.png | Elm 2.5 | https://commons.wikimedia.org/wiki/File:Elm.png | BSD, Dave Taylor |
| 2003-outlook-2003.jpg | Outlook 2003 | https://www.slipstick.com/outlook/managing-the-outlook-2003-interface/ | No licence stated (Slipstick Systems) |
| 2004-balsa.png | Balsa | https://commons.wikimedia.org/wiki/File:Balsa-gpg.png | GPL, Deeahbz |
| 2004-gmail.png | Gmail beta | https://en.wikipedia.org/wiki/File:Gmail_2004.png | Non-free (fair use) |
| 2004-mutt.png | Mutt | https://commons.wikimedia.org/wiki/File:Mutt.png | GPL, Arno |
| 2005-evolution-2.png | Evolution 2 | https://commons.wikimedia.org/wiki/File:Evolution_mail.png | GPL, Ojw |
| 2006-sylpheed-2.2.jpg | Sylpheed 2.2 | https://commons.wikimedia.org/wiki/File:Sylpheed_zen.jpg | LGPL |
| 2007-apple-mail-leopard.jpg | Apple Mail 3 | https://web.archive.org/web/20080303033333/http://www.apple.com/macosx/features/mail.html | © Apple |
| 2007-claws-mail-2.7.png | Claws Mail 2.7 | https://commons.wikimedia.org/wiki/File:Claws-mail_271-en.png | CC BY 2.5, Александар Урошевић |
| 2007-outlook-2007.png | Outlook 2007 | https://en.wikipedia.org/wiki/File:Microsoft_Office_2007_Enterprise_screenshot.png | Non-free (fair use) |
| 2007-thunderbird-2.png | Thunderbird 2 | https://commons.wikimedia.org/wiki/File:Mozilla_Thunderbird_2009_Xfce4.png | MPL 1.1, AVRS |
| 2007-windows-mail.png | Windows Mail (Vista) | https://archive.org/details/windows-vista-product-guide | © Microsoft |
| 2008-alpine.png | Alpine 1.10 | https://commons.wikimedia.org/wiki/File:Alpine_email_client.png | Apache 2.0, University of Washington |
| 2008-iphone-mail.jpg | iPhone Mail | https://web.archive.org/web/20081230050637/http://www.apple.com/iphone/features/mail.html | © Apple |
| 2009-android-gmail-g1.png | Android Gmail (G1) | https://www.gsmarena.com/t_mobile_g1-review-337p6.php | © GSMArena / Google |
| 2009-opera-m2.png | Opera Mail (M2) | https://www.freeemailtutorials.com/operaM2/operaMailInterface.php | © Free Email Tutorials |
| 2009-zimbra.jpg | Zimbra | https://commons.wikimedia.org/wiki/File:Zimbra_Hosted_Demo_Interface.JPG | GPL, Zimbra |
| 2010-outlook-2010.png | Outlook 2010 | Microsoft Outlook 2010 Product Guide (download.microsoft.com), p. 11 | © Microsoft |
| 2010-postbox-2.png | Postbox 2 | https://web.archive.org/web/20101230150202/http://www.postbox-inc.com/ | © Postbox Inc. |
| 2010-sparrow.png | Sparrow beta | https://www.macstories.net/mac/sparrow-new-email-client-for-mac-thats-just-like-tweetie/ | © Sparrow / MacStories |
| 2010-thunderbird-3.1.png | Thunderbird 3.1 | https://commons.wikimedia.org/wiki/File:Mozilla_Thunderbird_3.1.png | CC BY-SA 3.0, Mozilla Foundation |
| 2010-windows-live-hotmail.png | Hotmail Wave 4 | https://www.howtogeek.com/23556/screenshot-tour-the-new-hotmail-wave-4/ | © How-To Geek |
| 2011-apple-mail-lion.jpg | Apple Mail 5 | https://web.archive.org/web/20120101003224/http://www.apple.com/macosx/whats-new/mail.html | © Apple |
| 2011-lotus-notes-8.5.3.jpg | Lotus Notes 8.5.3 | https://www.ibm.com/docs/en/notes/8.5.3?topic=SSKTWP_8.5.3/com.ibm.notes85.help.doc/mail_quickref_r.htm | © IBM |
| 2011-yahoo-mail.png | Yahoo! Mail 2011 | https://en.wikipedia.org/wiki/File:Yahoo_Mail_Screenshot.png | Non-free |
| 2012-gmail.png | Gmail 2012 | https://en.wikipedia.org/wiki/File:Gmail_inbox_in_Japanese.png | PD-ineligible (en.wikipedia) |
| 2012-outlook-com.png | Outlook.com | https://news.microsoft.com/download/presskits/outlook-com/docs/OutlookcomRG.pdf | © Microsoft |
| 2012-windows-live-mail.png | Windows Live Mail 2012 | https://en.wikipedia.org/wiki/File:Windows_Live_Mail.png | Non-free, Paowee |
| 2013-mailbox.jpg | Mailbox | https://en.wikipedia.org/wiki/File:Mailbox_screenshot.jpeg | Non-free (fair use), Dropbox |
| 2014-inbox-by-gmail.png | Inbox by Gmail | https://gmail.googleblog.com/2014/10/an-inbox-that-works-for-you.html | © Google |
| 2015-outlook-mobile.png | Outlook for iOS | https://www.microsoft.com/en-us/microsoft-365/blog/2015/01/29/deeper-look-outlook-ios-android/ | © Microsoft |
| 2015-windows-10-mail.png | Windows 10 Mail | https://blogs.windows.com/windowsexperience/2015/07/26/a-look-at-the-great-built-in-apps-in-windows-10/ | © Microsoft |
| 2017-mailspring.png | Mailspring | https://flathub.org/apps/com.getmailspring.Mailspring | No licence stated (app GPL-3.0+) |
| 2017-newton.png | Newton | https://en.wikipedia.org/wiki/File:Newton_iOS_screenshot.png | Non-free (fair use), CloudMagic |
| 2018-the-bat.png | The Bat! | https://en.wikipedia.org/wiki/File:The_Bat_screenshot.png | Non-free (fair use), Chifonr |
| 2019-superhuman.png | Superhuman | https://blog.superhuman.com/how-to-split-your-inbox-in-superhuman/ | © Superhuman |
| 2020-emacs-rmail.png | Emacs RMAIL | https://www.youtube.com/watch?v=YgIORrMaFTc (frame ~0:36) | No licence stated (Muto) |
| 2020-hey.jpg | HEY | https://www.hey.com/how-it-works/ | © 37signals |
| 2020-neomutt.png | NeoMutt | https://github.com/neomutt/gfx/blob/HEAD/screenshots/screenshot/dlg-index2-100.png | NeoMutt project, no licence file |
| 2021-geary.png | Geary | https://flathub.org/apps/org.gnome.Geary | No licence stated (app LGPL-2.1+) |
| 2022-airmail.png | Airmail 5 | https://apps.apple.com/us/app/airmail-5/id918858936?mt=12 | © Bloop S.R.L. |
| 2022-gmail.png | Gmail 2022 | https://workspaceupdates.googleblog.com/2022/01/new-integrated-view-for-gmail.html | © Google |
| 2023-evolution.png | Evolution | https://flathub.org/apps/org.gnome.Evolution | No licence stated (app GPL-2.0+) |
| 2024-aerc.png | aerc | https://commons.wikimedia.org/wiki/File:Aerc_screenshot.png | MIT, Robin Jarry |
| 2024-kmail.png | KMail | https://apps.kde.org/kmail2/ | KDE, no licence stated |
| 2024-new-outlook-windows.png | New Outlook for Windows | https://techcommunity.microsoft.com/blog/outlook/built-for-today-designed-for-the-future---the-new-outlook-for-windows-is-ready-w/4205635 | © Microsoft |
| 2024-proton-mail.png | Proton Mail | https://proton.me/support/side-panel | © Proton AG |
| 2024-shortwave.png | Shortwave | https://www.shortwave.com/ | © Shortwave |
| 2025-fastmail.png | Fastmail | https://www.fastmail.help/hc/en-us/articles/360058753174-Fastmail-guide-for-new-users | © Fastmail |
| 2025-notion-mail.png | Notion Mail | https://www.notion.com/blog/introducing-notion-mail | © Notion Labs |
| 2025-spark.png | Spark | https://apps.apple.com/us/app/spark-mail-ai-email-inbox/id6445813049?mt=12 | © Readdle |
| 2025-thunderbird-140.png | Thunderbird 140 | https://flathub.org/apps/org.mozilla.Thunderbird | No licence stated (app MPL-2.0) |
| 2026-apple-mail.png | Apple Mail (Tahoe) | https://support.apple.com/guide/mail/welcome/mac | © Apple |
| 2026-mimestream.png | Mimestream | https://mimestream.com/ | © Mimestream |
| 2026-comms-mail-*.png | comms-mail | this repository (demo mailbox) | comms-mail |
