# uitoolkit gaps found building comms-mail

Gaps and rough edges in [uitoolkit](https://github.com/codemodify/uitoolkit)
hit while building this mail client, for the toolkit's maintainers. Each
entry says where it was verified and what would fix it. New findings are
appended under **Open** as they turn up; numbers are never reused, so a
number always means the same gap.

Last checked against **uitoolkit v0.22.2** (2026-09-29). 0.22 closed
sixteen of the first seventeen; 0.22.2 closed eight of the next ten, and
half of each of the other two (#19, #24). Checking 0.22.2 turned up seven
new items (#29–#35). What comms-mail uses for each closed item is under
**Resolved**.

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

### 19. TokenField cuts a quoted name at its comma — still, while typing
0.22.2 honours quotes: pasting `"Doe, Jane" <jane@example.com>, bob@x.org,`
gives the two chips it should. But an unclosed quote is read as no quote
("the user is still typing"), and while someone *types* that address the
quote is unclosed at the moment the comma is typed — so the comma splits,
`"Doe` is refused, and the comma is gone: the chip comes out as
`"Doe Jane" <jane@example.com>`. Verified on 0.22.2 with `Window.Type` on
a bare TokenField (Accept = a mail-address check). Comments (`(Smith, J)`)
and angle brackets are not skipped either.
comms-mail keeps its own split, which treats an open quote as open (the
comma stays in it until the quote is closed) and skips `(…)` and `<…>`.
**Fix:** while a quote is open at the end of the text, do not split inside
it — the separator can only end the value once the quote is closed. (A
stray `"` then holds splitting until it is closed or deleted, which the
user can see in the editor.)

### 24. Rich-text tables: a wrapped cell overlaps the next row
0.22.2 closed what was asked: a cell keeps its bold and its links, and the
block after a table gets its space (verified). Laying cells out as
paragraphs brought two new problems, #31 and #32 below.

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

### 30. `uitoolkit.Version` still says 0.22.1 in the 0.22.2 release
`version.go` at tag v0.22.2 (aee5242) is `const Version = "0.22.1"`, so
every application that shows the toolkit's version (comms-mail's Write
window and status bar) shows the wrong one.
**Fix:** bump it with the release; a test that compares it with the
latest `release:` note would keep it from drifting.

### 31. A table's header row is laid out as a level-1 heading
`layoutTableRow` lays a header row's cells out with
`kind = richtext.Heading` and the row's `Level`, which the HTML parser
sets to 1 for `<th>` — so a header cell is an H1: title-sized, where it
should be bold at the body's size. "Item / Price / Notes" come out as
headlines, and "Price" breaks mid-word in a narrow column.
comms-mail writes a Markdown table's header row as `<td><b>…</b></td>`
until this is fixed.
**Fix:** a header cell in the table face, bold.

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

### 34. heroicons' reply-all is a share icon
`icons/heroicons/reply-all.png` (0.22.2) is the three-linked-dots share
mark, not a reply-all arrow; the other four packs draw a double reply
arrow. comms-mail uses `IconReplyAll` in the message menu.
**Fix:** heroicons has no reply-all — two `arrow-uturn-left`s offset, as
the other packs do it.

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

## Resolved

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
