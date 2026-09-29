# uitoolkit gaps found building comms-mail

Gaps and rough edges in [uitoolkit](https://github.com/codemodify/uitoolkit)
hit while building this mail client, for the toolkit's maintainers. Each
entry says where it was verified and what would fix it. New findings are
appended under **Open** as they turn up; numbers are never reused, so a
number always means the same gap.

Last checked against **uitoolkit v0.22.1** (2026-09-29). 0.22 closed
sixteen of the first seventeen; what comms-mail now uses for each is under
**Resolved**.

## Open

### 2. No font fallback: emoji, and whole scripts, are boxes
Mail arrives in every script. Titillium Web (the interface face, 456
glyphs) has Latin only — no Greek, no Cyrillic, no CJK — and nothing
falls back to another face for a rune it lacks, so a subject in Russian,
Greek or Japanese is a row of boxes in the message list, and so are the
emoji that marketing mail and people alike put in subjects ("👀", "🥳").
0.22.1's docs/contracts.md now states this as the rule ("One face draws
everything"), which is honest, and comms-mail keeps to its side of it
(marks are icons, never characters). But a mail client shows other
people's text, which it cannot choose.
**Fix:** a fallback for *text content* — a label's, a table cell's, rich
text's — to installed faces that have the rune (Noto Sans for the scripts,
Noto Color Emoji), as browsers and every other toolkit do. The interface's
own strings can stay one face.

### 18. Icons: heroicons' forward is fast-forward; the drawn paperclip is a box; no filled star or dot
Checked with the 0.22 marks in comms-mail's message list:
- **heroicons' `forward.png` is the media fast-forward** (two triangles),
  not a mail forward arrow. lucide, material-symbols, phosphor and
  tabler all draw a forward arrow. heroicons is the pack comms-mail's
  owner uses.
- **The drawn (classic / sharp) paperclip reads as a rounded box** at list
  size (20 px): nested rounded rectangles, no clip shape. The star and the
  envelope drawn beside it are clear.
- #16 asked for a filled star and a dot. There is one star (an outline),
  so "starred" is an outline star in amber; and no dot, so comms-mail
  marks unread with the envelope (`IconMail`), which works.
**Fix:** heroicons' `arrow-uturn-right` as its forward; a drawn paperclip
with the clip's bend; `IconStarFilled` (or a filled variant) and a dot.

### 19. TokenField cuts a quoted name at its comma
`TokenField.splitInput` ends a token at every separator, including one
inside a quoted display name: typing or pasting `"Doe, Jane"
<jane@example.com>,` gives `"Doe` (refused by Accept) and then
`"Doe Jane" <jane@example.com>` with the comma lost. Names with commas are
common in address books ("Last, First"). comms-mail replaces the editor's
OnInput with its own split that reads the address grammar (quotes,
comments, angle brackets).
**Fix:** a `Split func(text string) (done []string, rest string)` on
TokenField, or have the default split skip separators inside `"…"`,
`(…)` and `<…>`.

### 20. A chip cuts its own text short
`Token.Measure` returns a fractional width (`2*pad + Advance(text) +
cross`); the bounds it is arranged in are whole pixels; `Token.Paint` then
finds `Advance(text) > room` by the fraction and ellipsizes. So a chip
sized for its text shows "Bob <bob@example.or…" — about one chip in two,
depending on the text. Seen in comms-mail's To field.
**Fix:** round the measured width up (as Grid's `ceilPx` does), or fit
with a small tolerance.

### 21. TokenField measured without a width asks for every chip on one line
`TokenField.Measure` with no MaxW returns the width of all the chips in a
row. A `Form` sizes its field column from that (Grid measures columns
unbounded), and a Flex track never goes below its content, so three
addresses push the Write window's form past the window's edge, clipped.
comms-mail's recipient field reports a text field's width (400 px) when no
width is offered, and wraps to the width it is given.
**Fix:** when unbounded, report a field's preferred width (or the widest
chip), since the field can wrap; its height-for-width answer is what
matters.

### 22. Grid measures its rows at the columns' natural widths, not the width it is offered
`Grid.Measure` computes `columns(-1)` and, while their sum fits in MaxW,
measures every row at those natural widths; only `Arrange` gives a Flex
column the rest of the width. A child whose height depends on its width
(a chip field, a wrapping label) is measured narrow and tall, then
arranged wide and short, and the form is left taller than what it draws —
blank space under comms-mail's Write form when the recipients wrap at
400 px but not in the window.
**Fix:** when MaxW is given, measure rows at `columns(c.MaxW)` — the
widths Arrange will use.

### 23. ClassifyImageSrc reads `//host/x.gif` as a local file
A protocol-relative address has no scheme, so `ClassifyImageSrc` returns
`ImageLocal`. It is a network fetch (the page's scheme, http or https),
and tracking pixels use it. An application that trusts the classification
would either treat a tracker as a local file or never offer to load a
real remote image. comms-mail counts `//` as remote itself.
**Fix:** `//` → `ImageRemote`.

### 24. Rich-text table cells are drawn as plain text, on one line
The model keeps each cell's spans (`Block.Cells [][]Span`), but
`RichText.layoutTableRow` draws a cell as `CellText(j)` — its plain text —
in one face, fitted to one line with "…". So bold, italics, code and links
in a `<td>` are lost on screen (a newsletter's table of links loses its
links; a Markdown table's `**bold**` cell is plain), and a long cell is cut
rather than wrapped. Separately, `spacing` gives a TableRow no space after
it, so the block that follows a table (a quote's rule) starts right under
its last row.
**Fix:** lay a cell's spans out like a paragraph's, wrapped to the column's
width (the row as tall as its tallest cell); paragraph spacing after the
last row of a run.

### 25. Prompt closes before the caller can refuse the value, and its button is always OK
`Prompt` dismisses on OK and then reports the value, so a name the
server refuses ("a folder with that name exists") cannot be shown in the
prompt: comms-mail shows a warning and then opens the prompt again with
what was typed. And the accept button is "OK" where the action has a name
("Rename", "Create") — the button names the action in every desktop's
guidelines.
**Fix:** a `Validate func(string) error` that keeps the prompt up with the
error under the field (asynchronous would be better still: the check is a
server round trip), and an accept label.

### 26. A chip is always a capsule, even in a square-cornered theme
`Token.Paint` takes half the chip's height as its radius and uses the
look's radius only when it is smaller **and above zero**
(`if m := lk.Metrics().Radius; m > 0 && m < rad`). A square-cornered
theme — Metal, which comms-mail's owner uses, has `Radius` 0, as does any
look with `corners: square` — therefore gets round capsules, the opposite
of what the comment above that line says it intends ("a square-cornered
era gets square chips"). There is also no way for an application to ask
for square chips in a rounded theme. The owner asked for square chips in
the Write window; comms-mail cannot draw them without redrawing the chip.
**Fix:** `m >= 0` (zero radius is square); and a `ChipRadius` (or shape)
on TokenField / Token for an application that wants its own.

### 27. Too few icons to put on menus and buttons, and Button has none
`ToolIcon` has 23 ids. A mail client's menus need more, and most of them
are already PNGs in the five shipped sets (ShippedIconStems) with no id
to name them by: **trash** (Delete), **archive**, **junk**, **tag**,
**folder** (Move to), **reply-all**, **settings**, **external-link**
(Open HTML, open in browser), **eye** (Show Images), **user** (VIP),
**bell** (Notify), **send**. **print** is not shipped at all. There is no
way to draw a stem by name either (`ToolIconByName` knows the 23), so an
application cannot use the PNGs that are there. And `widgets.Button` has
no `Icon` — only ToolButton and MenuItem do — so a dialog's buttons cannot
carry one (KDE puts icons on OK, Cancel, Apply, Save).
comms-mail uses the ids that fit (open, reply, forward, check, mail, star,
mute, save, attach, download, new, pen, undo, info) and two stand-ins
(tag → flag, junk → warning); Delete, Archive, Move to, Print, VIP,
Settings and Quit have no icon, and the action row under a message uses
ToolButtons to get icons at all.
**Fix:** ids for the stems above (and a print icon), `IconByStem(name)`
for the rest, and `Button.Icon`.

### 28. Withdrawn
Filed as "nothing lays controls out in a row that wraps". Wrong:
`widgets.Wrap` does exactly that and has since v0.20.0
(docs/widgets.md, Layout and structure). comms-mail uses it.

## Resolved

Closed in 0.22, and how comms-mail uses each.

| # | Gap | 0.22 | In comms-mail |
| --- | --- | --- | --- |
| 1 | No focus-preserving completion popup | `widget.SetPopupKeysPass` | The address suggestions float over the Write window while typing goes on in the field |
| 3 | Image placeholder is an empty box | Alt text in the placeholder | Blocked remote images show their alt text |
| 4 | ResolveImage can't tell inline from remote; no image list | `ResolveImageKind`, `Doc.Images` | The reading views ask the document for its images; the regex is gone |
| 5 | No chip field | `TokenField` | To / Cc / Bcc are chips (with #19–#21 worked around) |
| 6 | No per-tab hide | `SetTabVisible` | The HTML tab is there only for mail with HTML |
| 7 | `SetText` leaves the caret at 0 | Caret at the end | Nothing to change (tests only) |
| 8 | No height for N rows | `HeightForRows` | Sizes the suggestion popup |
| 9 | No folder-picking dialog | `FileOpenFolder` | Import's Add a folder…, Save All |
| 10 | No prompt dialog | `Prompt` / `MessageBoxOptions.Input` | New Folder, Rename Folder (with #25 worked around) |
| 11 | Wrapping label clipped in a row | Height-for-width in Flex | Nothing to change: the invite card's title had been given its own line |
| 12 | No suggested name for Save | `FileDialogOptions.Name` | Save message as, Save As |
| 13 | A text field takes a file drop | Files go to whoever takes files | Files dropped on the message text are attached (tested) |
| 14 | ScrollView cannot shrink to its content | `ShrinkToContent` | The reading pane's header |
| 15 | A missing theme turns dark silently | `Appearance.Missing`, `MissingThemeNote`, light fallback | The startup line uses it |
| 16 | No icons in cells, headers, tree nodes | `CellIcon`, `TableColumn.Icon`, `TreeNode.Icon`, seven mail icons | Star, paperclip, status (unread / forwarded / replied), muted bell; filter pins; attachment rows (see #18) |
| 17 | Rich text has no tables or quotes | Table, Quote and Rule blocks | The Markdown view draws tables, quotes and rules (see #24) |
