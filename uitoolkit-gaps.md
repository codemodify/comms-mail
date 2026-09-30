# uitoolkit gaps found building comms-mail

Gaps and rough edges in [uitoolkit](https://github.com/codemodify/uitoolkit)
hit while building this mail client, for the toolkit's maintainers. Each
entry says where it was verified and what would fix it. New findings are
appended under **Open** as they turn up; numbers are never reused, so a
number always means the same gap.

Last checked against **uitoolkit v0.22.4** (2026-09-29). 0.22 closed
sixteen of the first seventeen; 0.22.2 eight of the next ten; 0.22.3 the
rest of #19 and #24, and #30, #31 and #34 of the seven new ones. #32 is
fixed at some widths and not at others (below); #29, #33 and #35 are
design changes the toolkit has not taken up yet. What comms-mail uses for
each closed item is under **Resolved**.

## Open

### 2. No font fallback: emoji, and whole scripts, are boxes
Mail arrives in every script. Titillium Web (the interface face, 456
glyphs) has Latin only — no Greek, no Cyrillic, no CJK — and nothing
falls back to another face for a rune it lacks, so a subject in Russian,
Greek or Japanese is a row of boxes in the message list, and so are the
emoji that marketing mail and people alike put in subjects ("👀", "🥳").
comms-mail keeps to its side of the rule (marks are icons, never
characters), but a mail client shows other people's text, which it cannot
choose.
**Fix asked for:** a fallback for *text content* to installed faces that
have the rune.

**Declined, and settled** (2026-09-29, second time of asking). One face
draws everything; a rune it lacks is a box. docs/contracts.md states it
at length and names the answer for a product that cannot live with it —
Qt or GTK, which have shaping engines. This will not be reopened, so
comms-mail should plan around it rather than around its arriving.

### 29. `Button.Icon` makes a button an icon-and-a-half wider
The documented trade-off (the label stays centred, so a strip is reserved
at *both* ends) costs about 64 px at 1x per button: "No" is 124 px wide,
"Maybe" 150. Rows that fitted the narrowest windows stopped fitting as
soon as their buttons got icons — in comms-mail the invitation's answers,
Settings' account and filter rows, the Add account sign-in row, the
rule editor's Remove, and the remote-images bar (caught by comms-mail's
minimum-size test). comms-mail folds those rows with `widgets.Wrap` and
made the rule editor's Remove an icon-only tool button.
**Fix:** a leading-icon layout that moves the label over by one strip
rather than reserving two — through a label rect the engine draws into
(`DrawButton` taking the text's box), or an opt-in `IconLeading` for
looks whose label treatment tolerates it.

**Still open in 0.22.3**, listed there as a design change (all 33 engines,
or an opt-in).

**Answered in 0.22.4 with documentation rather than a change, and the
reasoning is now written down where someone who has never used this
toolkit will find it.**

The 64 px is inherent to not touching the engine: a push button's label
is drawn *by the engine*, every engine centres it and decorates it its
own way (Clearlooks embosses it, others shadow or grey it), so the widget
cannot move the label without taking over drawing it and losing all of
that. Reserving a strip at each end is what keeps the label centred and
every era's treatment untouched.

What was missing was not the trade-off but the guidance. `Button.Icon`'s
doc comment now states the cost in the first line and names the three
alternatives, and [docs/recipes.md](https://github.com/codemodify/uitoolkit/blob/dev/docs/recipes.md)
opens with a table of which control to reach for — `ToolIconBtn` for an
icon-only button, `NewToolButton` for a tight icon-and-label one,
`NewWrap` to let a tight row fold — which is what comms-mail worked out
for itself.

Left open as a design change: an `IconLeading` opt-in that draws the
label itself at normal width, trading the engine's label treatment on
those buttons.

### 32. A table row whose cells wrap is overlapped by the next row
With cells now wrapping, a row two lines tall is drawn two lines tall but
the next row starts one line below its top, over its second line; the
column rules are drawn beside the first line only. Seen in comms-mail's
Markdown view with this, at 1280×800 in the reading pane:

```markdown
| Item | Price | Notes |
| --- | --- | --- |
| **Tea** | £3 | see [the list](https://example.com/list) and `code` |
| Cake with a long name that should wrap inside its cell | £4.50 | |
```

"and code" (Tea's second line) is drawn on the same line as "Cake…", and
"wrap inside its cell" sits under the Cake row outside its rules. After a
resize the header row overlaps the first row too. `layoutTableRow`
computes a multi-line row's height (`lay.h` covers every line); where the
next row's top comes from was not traced. comms-mail has no workaround.
**Fix:** position each row by the laid-out height of the rows above it;
draw the column rules for the row's full height.

**Closed in 0.22.3, and it was two bugs. The column rules and the rule under a row were drawn at the *first line's* height, so a wrapped row's later lines sat outside their own cell. And columns were being squeezed past their longest word — which broke "Price" into "Pric" and "£4.50" into "£4.5" and "0", growing each row a line — because the shrink still assumed cells elide. Each column has its own floor now, and is never rounded below it.**

**Still there at some widths (checked on 0.22.3).** With the window at
1100×800 the table above is right: every row as tall as its wrapped
cells, the rules down the whole row. At 1280×800 — the reading pane about
440 px wide — it is not: the Tea row draws two lines ("see the list and"
/ "code") but the Cake row starts at Tea's second line, so "Cake with a
long name that should" shares a line with "code", and there is no rule
under Tea. `RequestLayout` and `Invalidate` on the RichText, and
`SetHTML` of the same HTML, leave it as it is; resizing to 1100 puts it
right. The header row is right at both widths. Not traced; a guess is
that the height used to place the next row and the lines drawn come from
two different widths at this size (with and without the scroll bar's
gutter, say), so the Tea row is one line tall for the one and two for the
other.

**Closed in 0.22.4, and the guess in the report was close: two widths,
but not the scroll bar's — an estimated height and a laid-out one.**

Not a table bug at all. Block tops are a prefix sum, `tops[k+1] =
tops[k] + heights[k]`, so changing `heights[i]` invalidates `tops[i+1]`
onward — and `setHeight` kept `tops[i+1]`, one too many. The block
*directly after* one whose height changed was placed at the offset the
old height gave.

A height is estimated before its block is laid out and corrected when it
is, so it fired wherever an estimate was wrong. That is why it was
width-dependent (at 1100 nothing wrapped, so no estimate was wrong), why
the header row was right (its height was guessed correctly), why no rule
was drawn under Tea (the row below was placed inside it), and why
nothing comms-mail could call fixed it while a resize did — a resize
recomputes every top from zero.

Six pixels, one character: `min(t.topsOK, i+1)` is `min(t.topsOK, i)`.
The splice path beside it had always used the right form.

### 33. Icon packs installed before an update go stale, and new icons draw as "no icon"
The premiere packs are not embedded: they are read from
`~/.config/uitoolkit/icons/<set>/`, which the README says to refresh by
copying `icons/*` by hand after every pull. Until someone does, every
application on the machine draws the old art (heroicons' forward stayed
the fast-forward the 0.22.2 notes say was fixed) and the **no-icon**
placeholder for every icon added since (`heroicons missing print.png,
using no-icon`). An application cannot ship an update that uses a new
icon without its users running a copy they have never heard of.
comms-mail's owner had packs installed on 2026-09-13; they were refreshed
by hand for this check.
**Fix:** embed the premiere packs and use the installed copy only as an
override, or fall back to the embedded PNG for a stem the installed copy
lacks; at the least, have the Settings app refresh an installed premiere
pack whose files are older than the library's.

**Still open in 0.22.3**, listed there as a design change. The packs were
copied by hand again for this check (heroicons' reply-all, #34, is only
fixed on a machine that does).

**Closed in 0.22.4, by the other half of the design rather than by embedding.**

`~/.config/uitoolkit/{icons,themes,skins}` is the *user's* — the
toolkit's `~/.icons` — and an application writing there was the bug: two
that do overwrite each other, last one wins, and neither can tell. What
was missing was a way for an application to ship its own art at all.

`style.AddSearchPath("/opt/comms-mail/share")` registers a directory
shaped like the user's (`icons/`, `themes/`, `skins/`), private to that
process. The user's copy comes first file by file, so a person's own set
still wins where it has an icon, and a stem their copy predates — `print`,
the corrected `reply-all` — is answered by the application. comms-mail
can ship the packs it needs and stop asking anyone to copy anything.

### 35. An asynchronous check in a prompt needs a keep-open trick
`MessageBoxInput.Validate` runs on the UI goroutine; for a check that is a
round trip (the mail server refusing a folder name) its docs say to keep
the dialog up and call `SetInputError` when the answer comes. There is no
way to say "keep it up" other than returning a non-nil error — comms-mail
returns `errors.New("")`, which shows no message — no busy state for the
button while the answer is on its way (a second press has to be ignored
by the caller), and no call to close the box with its result; comms-mail
closes it with `widget.DismissOverlay(mb.Overlay())`, so `OnResult` never
runs. It works; it is not a pattern anyone would find.
**Fix:** `ValidateAsync func(string, done func(error))`, with the accept
button busy until `done`, and the box closing with its result on nil.

**Still open in 0.22.3**, listed there as a design change.

**Closed in 0.22.4, and the report was right that the documented route
did not work.** "Keep the dialog and call `SetInputError`" needed two
things the API did not have: a way to say "keep it up" other than
returning a non-nil error (`errors.New("")` worked only because an empty
message hides the label — an accident, not an API), and a way to close
with a result at all, since `finish` was unexported.

`MessageBoxInput.ValidateAsync(value, done)` keeps the dialog up with the
accepting button busy and ignoring further presses until `done`:
`done(nil)` closes it with its accepting result so `OnResult` runs,
`done(err)` shows the reason and re-enables. `MessageBox.Close(result)`
is the general case, and `MessageBox.Checking` reports whether a check is
out. comms-mail can drop the `errors.New("")` trick and the
`DismissOverlay` close.

### 36. `Splitter` has no minimum size for a pane
A splitter divides its space by `Ratio` alone (`panes` in
widgets/splitter.go: `aw := avail * s.Ratio`, clamped to the whole), so a
pane can be dragged, or start, narrower than what it holds. comms-mail's
folder pane has the menu and the Fetch / Write / Search buttons over it
(#37) and must not be narrower than them: at 1280 px its 17 % was 3 px
too narrow. comms-mail raises `Ratio` itself after a layout that left the
pane too narrow, a frame late.
**Fix:** `MinA` / `MinB` (1x design lengths) that `panes` and a drag both
respect — Qt's `QSplitter` takes the children's minimum sizes, GTK's
`GtkPaned` has `shrink-start-child`.

**No longer needed by comms-mail** (2026-09-30): the buttons moved off the
folder pane (#37), so nothing has to fit over it. Left open because the
gap is real — a pane can still be dragged narrower than what it holds.

### 37. A sidebar cannot run up under the title bar
comms-mail's owner asked for the folder pane to take the window's whole
height, with the menu and the Fetch / Write / Search buttons on top of it
and the tabs beginning after it — the layout Thunderbird 115+, Apple Mail,
GNOME's split views and the new Outlook share. The title bar is one
full-width row (`Window.SetTitleBar`, a `HeaderBar` of start / centre /
end), so the pane cannot reach into it and nothing ties a header bar's
start section to a pane's width. comms-mail puts the buttons in a start
section it sizes after each layout to end where the pages begin,
measuring where the tabs landed (`sideHead` and `edgeWatch` in
mailui/search.go) — a frame behind while the divider is dragged, and the
caption band still runs across the top of the pane.
**Fix:** a split header bar: one whose start section follows a pane's
width (libadwaita's `NavigationSplitView` / `OverlaySplitView` with a
header bar per side, AppKit's full-height sidebar with its toolbar), or at
least `HeaderBar.StartWidth` bound to a component's width.

**Superseded in comms-mail** (2026-09-30): what the owner meant was a
folder pane with nothing over it. comms-mail now leaves the window's own
title bar full width (its title and caption buttons) and puts the menu,
Fetch / Write / Search and the tabs in a row of the window's content,
beside the folder pane and over the pages; `sideHead` and `edgeWatch` are
gone. What is left of this gap is the one-row version — the folder pane
up under the caption, the app's row in the caption beside it — which
would save that row.

## Resolved

### Closed in 0.22.3

| # | Gap | 0.22.3 | In comms-mail |
| --- | --- | --- | --- |
| 19 | TokenField cut a quoted name at its comma while it was typed | An unclosed quote holds the split until it is closed or deleted; comments and angle brackets nest | comms-mail's own split is gone; typing `"Doe, Jane" <jane@example.com>,` makes one chip (tested). comms-mail only drops a repeated address whose capitals differ, which `Unique` does not |
| 24 | Table cells were plain text on one line | (0.22.2) cells keep their spans and wrap; the block after a table gets its space | The Markdown view draws formatted cells; see #32 for what is left |
| 30 | `uitoolkit.Version` said 0.22.1 in 0.22.2 | Bumped, and a test ties it to the release notes | comms-mail shows 0.22.3 |
| 31 | A header cell was laid out as an H1 | Bold at the table's own size | comms-mail writes `<th>` again |
| 34 | heroicons' reply-all was the share mark | `arrow-turn-up-left` | Checked after refreshing the installed pack (#33) |

### Closed in 0.22.2

| # | Gap | 0.22.2 | In comms-mail |
| --- | --- | --- | --- |
| 18 | heroicons' forward was fast-forward; the drawn paperclip a box; no filled star or dot | heroicons' forward is `arrow-turn-up-right`; the drawn paperclip is a hairpin that reads at 20 px; `IconStarFilled` and `IconDot`, drawn by the toolkit in every set | Starred rows show the filled star in amber, unread rows the dot; checked in the drawn set and in all five packs (heroicons after #33's copy) |
| 20 | A chip cut its own text short | `Token.Measure` asks for whole pixels | Checked: every chip shows its whole address |
| 21 | TokenField measured without a width asked for every chip on one line | A field's width, `TokenField.PreferredWidth`, the widest chip as the floor | comms-mail's own width override is gone |
| 22 | Grid measured rows at natural widths | Tracks that fold are told from tracks that cannot by height; rows measured at the widths `Arrange` uses | Checked: no space left under the Write form with several recipients |
| 23 | `//host/x.gif` read as a local file | `ImageRemote` | comms-mail's own classification is gone; the resolver uses the toolkit's kind |
| 25 | A prompt could not refuse a value or name its button | `Validate`, `AcceptLabel`, `SetInputError`, `PromptFor` | New Folder says Create, Rename Folder Rename; a name the server refuses is said under the field with the name still there (see #35) |
| 26 | Chips were capsules in square packs | Zero is a radius; 41 of 135 packs draw square chips | Checked in metal-steel, the owner's pack |
| 27 | Too few icons; none on Button | 15 typed ids plus `IconStarFilled` and `IconDot`, `IconByStem`, `Button.Icon` | Every menu row, tool bar button and dialog button with a meaning an icon has, through one verb-to-icon table; only typed ids, since a stem draws no-icon in the drawn sets (see #29, #33, #34) |

Also taken up from 0.22.2: `StatusItem.Shown` — comms-mail closes to the
tray only while a tray shows its icon, and puts the window back if the
tray goes away.

### Withdrawn

**28.** Filed as "nothing lays controls out in a row that wraps". Wrong:
`widgets.Wrap` does exactly that and has since v0.20.0 (docs/widgets.md,
Layout and structure). comms-mail uses it.

### Closed in 0.22

| # | Gap | 0.22 | In comms-mail |
| --- | --- | --- | --- |
| 1 | No focus-preserving completion popup | `widget.SetPopupKeysPass` | The address suggestions float over the Write window while typing goes on in the field |
| 3 | Image placeholder is an empty box | Alt text in the placeholder | Blocked remote images show their alt text |
| 4 | ResolveImage can't tell inline from remote; no image list | `ResolveImageKind`, `Doc.Images` | The reading views ask the document for its images; the regex is gone |
| 5 | No chip field | `TokenField` | To / Cc / Bcc are chips |
| 6 | No per-tab hide | `SetTabVisible` | Used for the HTML tab, since replaced by an Open HTML button |
| 7 | `SetText` leaves the caret at 0 | Caret at the end | Nothing to change (tests only) |
| 8 | No height for N rows | `HeightForRows` | Sizes the suggestion popup |
| 9 | No folder-picking dialog | `FileOpenFolder` | Import's Add a folder…, Save All |
| 10 | No prompt dialog | `Prompt` / `MessageBoxOptions.Input` | New Folder, Rename Folder |
| 11 | Wrapping label clipped in a row | Height-for-width in Flex | Nothing to change: the invite card's title had been given its own line |
| 12 | No suggested name for Save | `FileDialogOptions.Name` | Save message as, Save As |
| 13 | A text field takes a file drop | Files go to whoever takes files | Files dropped on the message text are attached (tested) |
| 14 | ScrollView cannot shrink to its content | `ShrinkToContent` | The reading pane's header |
| 15 | A missing theme turns dark silently | `Appearance.Missing`, `MissingThemeNote`, light fallback | The startup line uses it |
| 16 | No icons in cells, headers, tree nodes | `CellIcon`, `TableColumn.Icon`, `TreeNode.Icon`, seven mail icons | Star, paperclip, status, muted bell; filter pins; attachment rows |
| 17 | Rich text has no tables or quotes | Table, Quote and Rule blocks | The Markdown view draws tables, quotes and rules |
