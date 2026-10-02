# uitoolkit gaps found building comms-mail

Gaps and rough edges in [uitoolkit](https://github.com/codemodify/uitoolkit)
hit while building this mail client, for the toolkit's maintainers. Each
entry says where it was verified and what would fix it. New findings are
appended under **Open** as they turn up; numbers are never reused, so a
number always means the same gap.

Last checked against **uitoolkit v0.23.2** (2026-10-02), from scratch:
every open item re-read against the code, every closed one re-checked by
comms-mail's tests, and the release's new pieces tried on the packs
comms-mail's owner uses and a sample of the rest. 0.22 closed sixteen of
the first seventeen; 0.22.2 eight of the next ten; 0.22.3 the rest of #19
and #24, and #30, #31 and #34; 0.22.4 and 0.22.5 #32, #33, #35, #36, #38,
#39 and #40; 0.23.1 #37, #41 and #42; 0.23.2 #43 and #44. Open: #2
(declined and settled), #29 (a design change), and #45 and #46, found
checking 0.23.2. What comms-mail uses for each closed item is under
**Resolved**, with the report and the toolkit's answer.

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

**Still open in 0.22.5**, by design. The release notes point at
`IconButton` where a row is tight, which is what comms-mail's title bar
uses now (#39).

**Unchanged in 0.23.0.** One stale pointer: `Button.Icon`'s doc comment
(widgets/button.go:77–83) still sends an icon-only button to
`ToolIconBtn`, the advice 0.22.5 corrected in docs/recipes.md (#39) —
the recipes table says `NewIconButton`, the field's own doc says the
opposite.

**0.23.1: the doc pointer is fixed** — `Button.Icon`'s comment now sends
an icon-only button to `NewIconButton` and a tool bar's mark to
`ToolIconBtn`, as docs/recipes.md does. The leading-icon layout itself is
still the open design change.

**Unchanged in 0.23.2.**

### 45. `CaptionMerged` under a caption that fits its title (BeOS) cuts the bar away
BeOS's caption is a tab only as wide as its title and buttons
(`DecorationSpec.CaptionFits`, style/decoration.go:194–200), and the
window's silhouette "leaves the rest of the top edge to the desktop". With
`Window.SetCaptionStyle(style.CaptionMerged)` the application's bar is
laid out across the window's full width from its top edge (the header
bar is 0–32 at 1280 × 800), but the silhouette is not dropped: the yellow
tab is still drawn, empty — no title, no window buttons — and everything
right of it above the body is outside the window. comms-mail's menu,
Fetch / Write / Search and tabs showed only their bottom 8 px. The deck
skin is also a split frame but does not fit its caption, and draws
merged correctly, as do the 23 other stacked packs rendered merged
(Window Maker, NeXT, OpenStep, Aqua, Luna, OS/2 Warp, AmigaOS 3.1,
System 7, OPEN LOOK, Motif, IRIX, HP VUE, KDE 1, Windows 95, Metal,
Plastik, Oxygen, and the Cassette and Nocturne skins). No
diagnostic is reported for this; 0.23.0's caption note recommends
`CaptionMerged` "under every pack".
comms-mail asks for `CaptionMerged` except where the look's caption fits
its title, and decides again when the look changes (`captionStyle`,
mailui/titlebar.go). Under BeOS the tab stays and comms-mail's bar is the
row under it — whole, and lined up with the pages since 0.23.1 (#42).
Tested by switching beos → kde1 → beos.
**Fix:** under `CaptionMerged`, drop `CaptionFits` and the silhouette as
`DecorationOf` already does for a maximized or tiled window
(style/decoration.go:349–354), so the bar is the caption
across the whole top edge; or refuse `CaptionMerged` for such a frame and
say so through `diag`, as the stacked case is reported.

### 46. An unselected browser tab's title and mark are centred on its slot, not its face
Window Maker, NeXT and OpenStep draw an unselected tab lower than the
selected one, with its top edge several pixels down the strip. `BrowserTabs`
places a tab's title in `labelBox(i, s, g)` and its mark in `iconRect`,
both from the tab's slot `s`, the strip's full height
(widgets/browsertabs.go:666–668 and 722–739), so the title of an
unselected tab is centred on the strip rather than the face drawn for it.
Its tops cross the tab's top edge. Seen on all seven packs of that
family (wmaker-default, wmaker-openstep, wmaker-night,
wmaker-steelbluesilk, next, next-night, openstep) with comms-mail's Mail
tab behind a message tab, at 1280 × 800 with the toolkit's frame; irix,
hp-vue, win95, kde1, breeze, plastik, metal-ocean, adwaita and sourcegit
are right. It was the same in 0.23.1, so it is not 0.23.2's new label
placement; that only made it the toolkit's own code that places it.
comms-mail's owner runs wmaker-default at times. comms-mail has no
workaround: nothing says where an engine drew a tab's face.
**Fix:** place the title and the mark against the face the engine drew
for the tab in its state (an engine hook for the face rect, or the
inset `TabContentInsetOf` already answers for the sides, for the top).

## Resolved

### Closed in 0.23.2

| # | Gap | 0.23.2 | In comms-mail |
| --- | --- | --- | --- |
| 43 | An `IconButton`'s mark shrank with the control height | The mark is the look's icon size (`style.IconSizeOf`, look.json `iconSize`), clamped to the face, never under 10 | `padTitleMarks` is gone: a stated `Pad` would now override the owner's icon size. Tested at Compact on metal-ocean by the mark's own ink: 12 to 19 px in a 26 px face, where 0.23.1 drew 6 |
| 44 | `MinWidthOf` read through a wrapper to a splitter's placeholder | A wrapper that holds one thing answers for it | comms-mail's `MinWidth` on `shortcutRoot`, `edgeWatch` and `reserveBox` is gone; the main window still reads 848 (Vertical) and 546 (Classic) |

Also from 0.23.2:
- A minimum stated with `SetMinSize` lasts through a resize (checked:
  the Passphrase window keeps 528). comms-mail's minimum-size test now
  also fails a window that would open narrower than its own minimum,
  since `SetMinSize` does not resize it.
- A browser tab's title is set against its mark (`BrowserTabs.Align`,
  `AlignStart`), and the selected tab merges with what is under it.
  comms-mail takes both as they come.

The reports and the toolkit's answers, as they were:

#### 43. An `IconButton`'s mark shrinks with the control height, to 6 px at Compact
A non-flat `IconButton` draws its mark as the button's side less 8 design
px each side (widgets/iconbutton.go:126–131, `pad := style.Dip(lk, 8)`),
and the button is a square of `Metrics().ControlH` (line 112). So the mark
is whatever the control height leaves, and look.json's `iconSize`
(small 16, medium 24, large 32) is not consulted:

| Pack | Density | ControlH | Mark |
| --- | --- | --- | --- |
| metal-ocean | Compact | 20 | **6** |
| wmaker-default | Compact | 22 | **8** |
| sourcegit | Compact | 22 | 11 |
| wmaker-default | Default | 28 | 12 |
| light | Default | 34 | 18 |

comms-mail's owner runs wmaker-default at Compact: the title bar's menu,
Fetch, Write and Search showed two bars, a few pixels and a speck,
beside tab marks (`BrowserTab.Icon`) drawn at 16 in the same strip.
comms-mail sets `IconButton.Pad` itself from the look's control height so
the mark is 16 where the button has room and never less than 3 px from
its edge, again on every look change (`padTitleMarks`,
mailui/titlebar.go), with a test at Compact.
**Fix:** size the mark from the look's icon size (the `iconSize`
preference, as tool buttons and tabs are sized), clamped to the button's
face, rather than from a fixed inset off the control height.

#### 44. `MinWidthOf` reads through a wrapper to a splitter's placeholder size
A component with children that does not implement `MinWidther` is probed
(`MinWidthByProbe`, widget/minwidth.go:67), and the probe looks only at
the component's own measurements. A `Splitter` measures as a fixed
320 × 200 when unbounded (widgets/splitter.go:88), so any pass-through
wrapper around one — a key handler, a drop target, a watcher, the
commonest thing an application writes — reports 320, whatever the
splitter's own `MinWidth` says. comms-mail's main window has two such
wrappers. `widget.MinWidthOf` of its content was **320**, against 848
once they implement `MinWidth`, so the line `Window.SetMinSize`'s doc
gives (`win.SetMinSize(widget.MinWidthOf(content)/win.Scale(), 0)`) would
have let the window shrink to a third of what its panes need.
comms-mail's wrappers implement `MinWidth` now (`shortcutRoot`,
`edgeWatch`, `reserveBox`).
**Fix:** have the probe never answer less than its widest visible
child's minimum (`MinWidthOfChildren`), which is a floor for every kind
of container, so a wrapper that forgot `MinWidther` is merely imprecise
rather than wrong by a factor of three. Or say in `SetMinSize`'s doc
that every component of the application's own with children must
implement `MinWidther` before that line can be trusted.

### Closed in 0.23.1

| # | Gap | 0.23.1 | In comms-mail |
| --- | --- | --- | --- |
| 37 | A sidebar cannot run up under the title bar | Answered by design: `StartWidth` (0.22.5) holds under either frame, and its doc now says to call `Arrange(Bounds())` after setting it inside a layout pass; a header bar split per pane is not planned | `alignTitle` is the documented pattern; tested after the first layout, a resize and a divider move |
| 41 | A `Stack`'s minimum width was its widest page's natural width | `Stack.MinWidth`: the widest of its pages' minimums, hidden ones included (Qt's rule) | `stackMin` is gone; the reading pane is 441 px at 1280 with the toolkit's own answer |
| 42 | `StartWidth` was ignored under a stacked frame | Kept in a stacked frame's row | Checked under kde1 with `CaptionStacked` forced: the menu button and the pages both start at x 226. comms-mail keeps `CaptionMerged` by choice: its tabs are its title bar, as Thunderbird's are |

Also from 0.23.1:
- `Window.SetMinSize` / `MinSize`: every comms-mail window raises its
  minimum width to `widget.MinWidthOf` of its content where that is more
  than the hand-chosen one (`fitMinWidth`, mailui/winsize.go). It found
  the Passphrase window's buttons overlapping at its old minimum (440;
  its content needs 528). comms-mail's minimum-size test now also fails
  on controls that overlap. That check found Settings' Import… hidden
  under Close at 520 × 440, now fixed.
- Browser tabs no longer repaint their chrome to place a mark: the
  slanted Window Maker tabs comms-mail's owner uses draw cleanly with
  the Mail tab's icon and the message tabs' close buttons.

The reports and the toolkit's answers, as they were:

#### 37. A sidebar cannot run up under the title bar
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

**Updated** (2026-09-30): what the owner wants is a folder pane with
nothing over it, and the menu, Fetch / Write / Search and the tabs in the
title bar, starting where the pages do. comms-mail now leaves the title
bar's part over the folder pane empty (caption space, `titleGap` in
mailui/titlebar.go) and sizes it after each layout to where the pages
landed (`edgeWatch`) — the same one-frame lag on a drag, and the caption
band still runs across the top of the pane. A title bar whose start
section follows a pane's width is still what would do this properly.

**0.22.5: not taken up as asked, and the reasoning holds.** The window
lays the caption out *before* the content, so a header bar bound to a
pane's width would only move the measurement, not the ordering. What
landed is `Splitter.OnRatioChanged` (on a drag and on `SetRatio`) and
`HeaderBar.StartWidth` as a plain number that the next layout takes up.
A sidebar running *beside* the title bar needs client-side decorations
and is not planned; a full-width title bar with the sidebar under it —
what comms-mail has — needs none of it.

**In comms-mail** (2026-09-30): `OnRatioChanged` covers a drag but not a
window resize or the first layout. Both move the pages without changing
the ratio (it is a share of a width that changed, and 0.22.5's pane
minimums can clamp it), so there is nothing to hear, and the menu and
tabs sat a frame behind: a `-headless` render, which paints the first
layout, showed them at the window's left edge over the folder pane.
comms-mail watches where the pages land (`edgeWatch`) and, when that is
not where the title bar's items start, sets `StartWidth` and re-arranges
the header bar with its own bounds in the same pass (`alignTitle`,
mailui/titlebar.go). That covers a drag too, so `OnRatioChanged` is not
used. Tested: the menu button is at the pages' edge after the first
layout, and after `SetRatio(0.3)` with one layout between.
**Left:** nothing blocks comms-mail. It would help the next application
to find this: either `StartWidth`'s doc saying that a change made while
the content is laid out needs `Arrange(Bounds())` on the bar to show in
that frame, or the window laying out again a caption whose layout was
requested while the content was being laid out.

**Unchanged in 0.23.0.** `StartWidth`'s doc (widgets/headerbar.go:51–65)
still describes only the `OnRatioChanged` route, which leaves a resize a
frame behind. And it is not kept at all under a stacked frame — #42.

#### 41. A `Stack`'s minimum width is its widest page's natural width, hidden pages included
0.22.5's `Splitter` keeps each pane at `widget.MinWidthOf` of what it
holds (#36). comms-mail's list pane is a `widgets.Stack` of two pages
shown one at a time: the message list (a `FlexBox` around the
`TableView`) and Account Central. `Stack` has no `MinWidth`, so
`MinWidthOf` probes it (`MinWidthByProbe`, widget/minwidth.go:67):

- Unbounded, the stack measures 973 × 6196: the list's height (every
  row) and Account Central's width. Account Central is hidden, but
  `Stack.Measure` (widgets/flex.go:231) measures every child, visible or
  not.
- At a quarter of that width it is still 6196 tall, because the list is
  as tall at any width. The probe reads "did not get taller, so it does
  not fold" and returns 973.
- The pages' own minimums are 296 (the list) and 260 (Account Central).

At 1280 × 800 the splitter then gave the list 973 px and left the
reading pane 265 px wide instead of 441. comms-mail wraps the stack in a
component whose `MinWidth` is `widget.MinWidthOfChildren` of its pages
(`stackMin`, mailui/titlebar.go), which skips hidden ones.
**Fix:** `Stack.MinWidth()`, the largest `MinWidthOf` among its pages
(Qt's `QStackedLayout` takes the largest of its pages' minimums), and
`Stack.Measure` leaving hidden children out, as `MinWidthOfChildren`
already does. More generally, the probe cannot tell "nothing folds" from
"the tallest child is one that does not fold", so any container that
shows one child at a time needs its own answer.

**Unchanged in 0.23.0**: `Stack` still has no `MinWidth`, and its
`Measure` (widgets/flex.go:231) still counts hidden pages. The
workaround stays.

#### 42. `HeaderBar.StartWidth` is ignored under a stacked frame
With the toolkit drawing the frame and a pack whose era stacks it (KDE 1,
Windows 95, Metal: their own title strip, the application's title bar
in a row under it), the row ignores `StartWidth`. `HeaderBar.Arrange`
applies it only in the merged case (widgets/headerbar.go:345); the
stacked case places the row at the frame's inset whatever `StartWidth`
says (line 334–336). So under kde1 comms-mail's menu and Fetch / Write /
Search sat at the window's left edge, over the folder pane the owner
asked to have nothing over, with the tabs after them rather than over
the pages — checked by rendering comms-mail under kde1 with
`DecorationsClient` at 1280 × 800. `StartWidth`'s doc states no
exception.
comms-mail now sets `Window.SetCaptionStyle(style.CaptionMerged)`: its
tabs are its title bar under every pack, as Thunderbird's are, which is
also what the new caption diagnostic recommends. That is the right call
for comms-mail on its own merits; the gap is for an application that
wants the era's strip *and* a row lined up with a pane.
**Fix:** keep `StartWidth` in the stacked row too (as room after the
row's inset), or say in its doc that it applies only to a merged caption.

0.23.0's diagnostics caught the case comms-mail had missed: under kde1
it reports "asked a title bar of the application's own (SetTitleBar),
got it in a row under the look's title strip". comms-mail's tests now
open a window under kde1 with the toolkit's frame and fail on that
finding.

### Taken up from 0.23.0

Nothing comms-mail filed, but these replace or avoid work of its own:

- `Window.SetCaptionStyle(style.CaptionMerged)`: the tabs are the title
  bar under every pack (#42).
- `diag` / `Application.Diagnostics()`: a test fails if the toolkit
  overrules the title bar; a session reports no findings at all in
  either build.
- `BrowserTab.Icon`: the Mail tab shows the inbox, a message tab the
  envelope.
- New typed ids: `IconPlus` (Add, Add a test, Add an action),
  `IconInbox` (Open Inbox), `IconSync` (a filter's Run Now), `IconLock`
  (Unlock), `IconArrowDown` / `IconArrowUp` (an invitation's More / Less).
- `Options.AppID`: "comms-mail" (and "comms-mail-demo"), so the desktop
  files the windows under comms-mail whatever the binary is called. Two
  uitoolkit programs no longer share one task-bar entry.
- A stale installed set now draws the toolkit's mark for a typed id it
  lacks. comms-mail still ships the packs (#33), which come first, so the
  set's own art is what shows.

### Closed in 0.22.4 and 0.22.5

| # | Gap | Fixed by | In comms-mail |
| --- | --- | --- | --- |
| 32 | A wrapped table row was overlapped by the next row at some widths | Block tops recomputed from the block whose height changed (0.22.4) | Checked at 1280 × 800, where it failed before: in the reading pane's Markdown view every row is as tall as its wrapped cells and the rules run the row's full height |
| 33 | Installed icon packs go stale; new icons draw as "no icon" | `style.AddSearchPath` (0.22.4): an application's own art, searched after the user's, file by file | `make build` / `make install` copy the five packs from the uitoolkit they build with to `bin/../share/comms-mail/icons/`; `mailui.UseShippedArt` registers that, `$XDG_DATA_HOME/comms-mail` and `$XDG_DATA_DIRS/comms-mail`. Checked with the owner's heroicons, which predates `menu`: the menu button draws the no-icon mark without the shipped copy and the hamburger with it. Nothing is copied into `~/.config/uitoolkit` any more |
| 35 | An asynchronous check in a prompt needed a keep-open trick | `MessageBoxInput.ValidateAsync`, `MessageBox.Close` (0.22.4) | New Folder and Rename Folder check the name with the server through `ValidateAsync`; the `errors.New("")` trick and the `DismissOverlay` close are gone |
| 36 | `Splitter` had no minimum size for a pane | Panes keep `widget.MinWidthOf` of what they hold; `MinA` / `MinB`, `AllowCollapse` (0.22.5) | The folder pane, the list and the reading pane keep their own minimums; the list's `Stack` needed a wrapper to report one (#41) |
| 38 | No typed icon for an app menu or an overflow menu | `IconMenu`, `IconMore`, with vectors in the drawn sets (0.22.5) | The app menu's button shows `IconMenu` in every set; the cog fallback is gone |
| 39 | No icon-only push button | `NewIconButton`; `Button.Checked` / `Toggle` (0.22.5) | Fetch, Write and Search are `IconButton`s; Search stays pressed while a search is on. comms-mail's own icon drawing and the dot it painted are gone |
| 40 | No button that drops a menu | `NewMenuButton`, which owns its items' accelerators (0.22.5) | The app menu is a `MenuButton` whose `Build` makes the rows at each opening; F10 opens it; comms-mail's own Ctrl+Q handler is gone, since the button's accelerator quits (tested) |

The reports and the toolkit's answers, as they were:

#### 32. A table row whose cells wrap is overlapped by the next row
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

#### 33. Icon packs installed before an update go stale, and new icons draw as "no icon"
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

#### 35. An asynchronous check in a prompt needs a keep-open trick
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

#### 36. `Splitter` has no minimum size for a pane
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

**Addressed after 0.22.4. A `Splitter` takes its panes' own minimums — what `widget.MinWidthOf` says each needs — so a pane cannot be dragged or opened narrower than the things inside it, which is what Qt's QSplitter does. `MinA` / `MinB` override that and `AllowCollapse` lets a pane close entirely (and now really close: the old hard 8 % floor on `Ratio` applied even to a pane meant to collapse). The pane rect is snapped to whole pixels too, so `PaneA` is the rect the child actually got.**

#### 38. No typed icon for an app menu or an overflow menu
comms-mail's owner asked for the app menu (View, Notify, Settings, Quit)
to be a button with an icon. Every desktop draws that button as "more"
(⋯ / ⋮) or a hamburger (☰, GNOME's `open-menu`). The packs ship `more`
(and `list`, which is a bulleted list, not a hamburger), but only by
stem: `IconByStem("more")` has no vector in the drawn sets, so in a look
that draws its own icons — the default for most packs — it is the
no-icon mark. comms-mail uses "more" where the look draws from a pack and
falls back to `IconSettings` (a cog) where it draws its own.
**Fix:** typed `IconMore` and `IconMenu` (a hamburger), with vectors in
the drawn sets.

**Addressed after 0.22.4. Typed `IconMore` and `IconMenu`, both with vectors in the drawn sets, and `menu` rendered into all five packs from the same pinned upstreams. This was the hazard `IconByStem` documents — a stem with no typed id draws the missing-icon mark in a drawn set, and every pack uses a drawn set unless the user picks otherwise — left standing on the two commonest button marks there are.**

#### 39. No icon-only push button
Also asked for by comms-mail's owner: Fetch, Write and Search as *real
buttons* — the look's push-button face — with an icon alone. The recipes
page (0.22.4) says a button that is only a mark is `ToolIconBtn`, which
has the tool face: flat, with no frame until hovered, in most eras, so it
does not read as a button ("why don't they look like real buttons?"). And
`Button.Icon` with no text leaves the engine's empty label centred and the
mark to one side, in a button 64 px wider (#29). comms-mail draws the
icon itself on an empty `Button` through `Button.Content`, centred and in
the label's colour.
**Fix:** a push button whose content is its icon (`NewIconButton(icon,
name, on)`: the look's button face, the icon centred, the name as its
tip and accessible name), sized as a square of the control height; and
a way to show it latched on (a toggle push button — comms-mail marks an
active search with a dot it paints).

**Addressed after 0.22.4, and the report is right that the recipe was wrong. `ToolIconBtn` is a *tool* item: flat with no frame until hovered in most eras, which is why it did not read as a button. `widgets.NewIconButton(icon, name, on)` is the third shape — the look's push-button face, the icon centred, a square of the control height, and `name` as both the tooltip and the accessible name so the two cannot drift. `Button.Checked` / `Toggle` latch it; 53 of the 135 packs draw a checked button exactly as an ordinary one, so on those it is drawn pressed instead, which is how Windows 3.1, Motif, CDE and OPEN LOOK drew a toggle anyway. docs/recipes.md's table is corrected — it had sent you to the control that does not do this.**

#### 40. No button that drops a menu
The app menu used to be a one-menu `MenuBar` ("M"); as a button, it is an
ordinary `Button` whose `OnClick` calls `ShowContextMenu` under it. That
loses what a menu button does — the menu opening on press rather than
release, a drag from the button into the menu, the button staying down
while its menu is open, and F10 / the menu key reaching it — and the
menu bar's accelerators: Ctrl+Q stopped quitting until comms-mail
handled it itself.
**Fix:** a `MenuButton` (Qt's `QToolButton` with a menu, GTK's
`GtkMenuButton`) that owns its menu's accelerators like a menu bar does.

**Addressed after 0.22.4. `widgets.NewMenuButton(icon, name, items...)`: opens on press so a drag can run into the menu, stays down while it is open, closes on a second press, and **owns its items' accelerators** — the matcher is now shared with `MenuBar` rather than written twice, which is how Ctrl+Q came to work in one and not the other. `Build` supplies the items at each opening for a menu that depends on the moment.**

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
