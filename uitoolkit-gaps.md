# uitoolkit gaps found building comms-mail

Gaps and rough edges in [uitoolkit](https://github.com/codemodify/uitoolkit)
hit while building this mail client, for the toolkit's maintainers. Each
entry says where it was verified and what would fix it. New findings are
appended under **Open** as they turn up; numbers are never reused, so a
number always means the same gap.

Last checked against **uitoolkit v0.23.11** (2026-10-14), which
comms-mail now builds with: every open item re-read against the code,
every closed one re-checked by comms-mail's tests, and the release's
fixes taken up and rendered. 0.22 closed sixteen of the first seventeen;
0.22.2 eight of the next ten; 0.22.3 the rest of #19 and #24, and #30, #31
and #34; 0.22.4 and 0.22.5 #32, #33, #35, #36, #38, #39 and #40; 0.23.1
#37, #41 and #42; 0.23.2 #43 and #44; 0.23.3 #45 and #46; 0.23.4 none;
0.23.5 #47, #48, #49, #50, #51, #53 and #54; 0.23.6 #55 and #56 (and so
#29, which #56 filed again); 0.23.7 #2 for every script but emoji; 0.23.8
#57 and #58; 0.23.9 #59, #60 and #61; 0.23.10 #62 and #63; 0.23.11 #64.
Open: #52 (declined and settled), and #65 (found 2026-10-14 in 0.23.11).
What comms-mail uses for each closed item is under **Resolved**, with the
report and the toolkit's answer.

## Open

### 52. No way to print
The toolkit has a Print icon (`IconPrint`) but nothing that prints: no
print dialog, no page setup, no way to lay a document out on pages or
render one to PDF, and no use of the desktop's print service (the
`org.freedesktop.portal.Print` portal, which takes a PDF; `PrintDlgEx`
on Windows; `NSPrintOperation` on macOS). Searched in 0.23.4: platform/,
app/, widgets/, richtext/ and docs/ have no print path, and the only PDF
in the repository is test data.
comms-mail's Print writes the message as an HTML page, with a
Content-Security-Policy that blocks every request so a remote image
cannot tell the sender it was printed, and opens it in the browser to
print or save as PDF (mailui/print.go, `printMessage`). The user leaves
the app to print, in a window that is not the app's.
**Fix asked for:** a print path — `richtext` (or a painted component) laid
out on pages and rendered to PDF, and the platform's print dialog for
that PDF (the portal's `PreparePrint` and `Print` on Linux).

Found 2026-10-06 against 0.23.4.

**Declined, and settled** (0.23.5). "No printing" is one of the toolkit
author's standing decisions (RESUME.md: "Decisions standing … no
printing"), and the README's list of gaps says of it "There is none".
0.23.5's notes call it the one item left open across the four
applications that report against the toolkit, pinned. comms-mail keeps
printing through the browser.

### 65. A keycap is a digit beside a box where a symbols face is installed
0.23.11 says keycaps compose, and its own test passes here — with a font
index holding nothing but Noto Color Emoji (`fakeFonts`,
style/emoji_test.go:463 and :476). With the faces a Noto machine also
has, `1️⃣` and `#️⃣` come apart: the digit from the emoji face and the
enclosing keycap, U+20E3, as an outline box from Noto Sans Symbols 2.
The same test with "Noto Sans Symbols 2" added to the fake index (a
scratch copy, not committed):

| | advance at 16 px | pictures |
| --- | --- | --- |
| 👩 | 19.92 | 1 |
| 👨‍👩‍👧 | 19.92 | 1.0 |
| 👍🏽 | 19.92 | 1.0 |
| **1️⃣** | **36.28** | **1.8** |
| **#️⃣** | **36.28** | **1.8** |

`faceRuns` keeps a position in a colour run only when
`emojiPresentation` marked it (style/shape_complex_layout.go:209), and
`emojiPresentation` (:249) has no arm for U+20E3 — on purpose, says its
comment (:256–262): "a font that composes the sequence has the character
in its cmap, so the general rule keeps it with the picture". It does
not: an unmarked U+20E3 goes to `faceAt`, which asks the outline
fallbacks before any colour face, and "Noto Sans Symbols 2" and "Noto
Sans Symbols" — both in the default list (style/fontfallback.go:50),
both on most machines with Noto — have an outline glyph for it
(`fc-list ':charset=20e3'`: Noto Color Emoji, Noto Sans Symbols, Noto
Sans Symbols 2). The run is cut before the keycap, and the emoji face's
GSUB never sees the sequence. In comms-mail's reading pane `1️⃣` is a
plain 1 beside a box.
**Fix asked for:** mark U+20E3 as emoji when it follows a keycap base —
0–9, # or *, with or without U+FE0F (UTS #51) — and give the sequence
test an outline face that covers U+20E3, as a real machine has.

Found 2026-10-14 against 0.23.11.

## Resolved

### Closed in 0.23.11

| # | Gap | 0.23.11 | In comms-mail |
| --- | --- | --- | --- |
| 64 | An emoji joined by a zero-width joiner was drawn in pieces | A position marked as part of an emoji sequence stays in the colour run, whether or not the colour face has a glyph of its own for it | 👨‍👩‍👧, 👨‍💻 and 🏳️‍🌈 are one picture each again — 19.9 px wide at 16 px, where 0.23.10 made them 39.8–59.8 — in comms-mail's list and reading pane. 👍🏽, 🇷🇴, ❤️ and the space after 👀 are as 0.23.10 left them. Keycaps, which the release says compose, do not on a machine with Noto Sans Symbols: #65 |

Also from 0.23.11, nothing for comms-mail to change: `Prefix` returns
the longest prefix that fits, a rack's wake-up outlives the window that
asked for it, and the tray's contract says a `StatusIcon`'s Name is sent
beside its picture — comms-mail gives the picture alone, which is what
the toolkit says to do. The screenshots render as 0.23.10's but for the
version string.

The reports and the toolkit's answers, as they were:

#### 64. An emoji joined by a zero-width joiner is drawn in pieces
0.23.10 draws a family, a profession and every other sequence joined by
U+200D as its separate pictures: 👨‍👩‍👧 is a man, a woman and a girl side
by side, 👨‍💻 a man and a laptop. In 0.23.9 each was one picture. Measured
with `Font.Draw` at 16 px between two H's under `font_emoji`:

| | 0.23.9 | 0.23.10 |
| --- | --- | --- |
| 👨‍👩‍👧 | advance 41.5, one picture | **81.4, three pictures** |
| 👨‍💻 | 41.5, one picture | **61.4, two pictures** |
| 👍🏽 | 41.5, one picture | 41.5, one picture |
| 🇷🇴 | 41.5, one picture | 41.5, one picture |
| ❤️ | 41.5, one picture | 41.5, one picture |

0.23.10 picks the face per position (`uitkFontmap.faceAt`,
style/shape_complex_layout.go:115–151). `emojiPresentation` rightly
marks a joiner between emoji as emoji (:234–265, the joiner at :258), so
`faceAt` asks `m.colour` for it (:123–124) — but `emojiSheetFor` takes a
colour face only where it has a *picture* for the rune
(style/emoji_on.go:127), and the joiner, an invisible format character,
has none. So the joiner falls through to the interface face, `faceRuns`
(:187) cuts the run there, and the colour face never sees the whole
sequence its GSUB would join. `keepsItsRun` (:166–177) would have kept
the joiner in its run, but it tests `Other_Default_Ignorable_Code_Point`,
which U+200D is not in: the joiner is default-ignorable as a format
character (Cf). The rule it replaced, go-text's `ignoreFaceChange`, used
the whole Default_Ignorable_Code_Point set.
**Fix asked for:** a joiner or a variation selector marked as emoji goes
to the face of the emoji before it, picture or not; and `keepsItsRun`
tests Default_Ignorable_Code_Point (`harfbuzz.IsDefaultIgnorable`), so
only whitespace leaves a colour run.

Found 2026-10-14 against 0.23.10.

### Closed in 0.23.10

| # | Gap | 0.23.10 | In comms-mail |
| --- | --- | --- | --- |
| 62 | A space after an emoji was as wide as an emoji | The toolkit splits its face runs itself, by position; whitespace leaves a colour run | "👀 Big sale 🥳 today only" has ordinary spaces in comms-mail's list and headers |
| 63 | The default fallback list named no Arabic, Hebrew or Indic face | The list names each script's Noto face, then the face a platform covers many with: Arabic, Hebrew, the Indic scripts, Thai, Lao, Khmer, Myanmar, Ethiopic, Armenian, Georgian | `useScriptFaces` and its list are gone; Arabic, Hebrew, Devanagari, Thai and Japanese subjects draw in the list and the reading pane with nothing named by comms-mail |

Also from 0.23.10, nothing for comms-mail to change: `Prefix` and `Fit`
measure the piece they return, a parsed face is no longer shared between
goroutines unguarded, every digit is a key (`Key3` keeps its name,
comms-mail's # still deletes), and `IconCloud` and icon badges are new.
The screenshots render as 0.23.9's but for the version string.

The reports and the toolkit's answers, as they were:

#### 62. A space after an emoji is as wide as an emoji
Under `font_emoji` every emoji draws, in colour and on its line — but a
space that follows one is about three spaces wide. "👀 Big sale 🥳
today" reads "👀   Big sale 🥳   today" in comms-mail's list, headers
and text; "🇷🇴 🇺🇸" has a gap of three letters between the flags.
Measured with `Font.Draw` at 16 px (Titillium): a space alone advances
3.5; in "H👀 H" the gap after the emoji is 22 px where "H👀H" has 3, and
in "H 👀H" the space *before* it is the ordinary 6.

The run splitter go-text's `Segmenter` uses never changes face on a
space — `ignoreFaceChange` counts every space separator as ignorable
(github.com/go-text/typesetting v0.3.5, shaping/input.go:428 and
:487–494, after Pango) — so a space that follows an emoji stays in the
emoji face's run (uitoolkit calls it at style/shape_complex_layout.go:248)
and is shaped with that face's own space. Noto Color Emoji's space is
2550 units of 2048, 1.245 em — as wide as its emoji (sfnt
`GlyphAdvance`) — so the space takes an emoji's width.
**Fix asked for:** in a run shaped with a colour emoji face, give a
space (and any `ignoreFaceChange` rune) the advance of the face the text
is in, or split such runes out of the emoji run before shaping so they
are shaped with the face around them.

Found 2026-10-13 against 0.23.9.

#### 63. The default fallback list names no face for Arabic, Hebrew or the Indic scripts
0.23.9 borrows those scripts in a shaping build (#61), and with "Noto Sans
Arabic", "Noto Sans Hebrew" and "Noto Sans Devanagari" named they shape
and run right to left correctly in comms-mail. Unnamed, they are boxes:
the default list (style/fontfallback.go:38–54) has CJK families, Noto
Sans, DejaVu Sans, FreeSans, Arial Unicode MS, Segoe UI Symbol and the
outline emoji and symbol faces. DejaVu Sans covers Arabic and Hebrew
where it is installed; on a machine with Noto and no DejaVu — as here —
nothing in the list does, though Noto Sans Arabic, Noto Sans Hebrew and
Noto Sans Devanagari are all installed. An application showing other
people's text — a mail client, a chat, a file manager — cannot know
which scripts will come.
comms-mail names them itself, after the toolkit's own list
(`useScriptFaces`, mailui/theme.go): the Noto Sans faces for Arabic,
Hebrew, nine Indic and four South-East Asian scripts, Georgian, Armenian
and Ethiopic, and Noto Naskh Arabic.
**Fix asked for:** name those families in the default list, as the CJK
ones are: they are skipped where they are not installed, and looked for
only for a rune nothing before them has.

Found 2026-10-13 against 0.23.9.

### Closed in 0.23.9

| # | Gap | 0.23.9 | In comms-mail |
| --- | --- | --- | --- |
| 59 | `font_emoji` drew each emoji above its line | The colour cell's bearing is measured from the top of the line, as an outline's is | The Makefile builds with `font_emoji`. Measured at 16 px: 👀 inked at y 43–55 in a line box 40–64.3 (baseline 58.1), 3 px from the letters either side. In comms-mail's list, headers and text 👀 🥳 👨‍👩‍👧 🇷🇴 🇺🇸 draw in colour on their line |
| 60 | An outline face won over the colour one | A character to be drawn as emoji (the pictographic planes, a variation selector, a joiner between emoji) goes to a colour face first; a modifier stays with its base | 👍🏽 and ❤️ draw in colour beside 👀, 👍🏽 as one picture, though Noto Sans Symbols 2 is installed. The family emoji draws as one picture (Noto's neutral grey figures) |
| 61 | `font_complex` never borrowed Arabic, Hebrew or Indic letters | `borrowable` asks whether the build shapes | With the faces named, Arabic joins and runs right to left, Hebrew runs right to left and wraps, Devanagari's conjuncts form — in the list and in the reading pane's bold title. The default list still names none of those faces: #63, which comms-mail works round |

Also from 0.23.9, nothing for comms-mail to change: the caret, `Prefix`
and wrapping measure right-to-left text as widths (four Hebrew words no
longer go on one line four times the box), and growing the emoji sheet
no longer turns the emoji already drawn back into masks — several emoji
in one list keep their colours.

The reports and the toolkit's answers, as they were:

#### 59. `font_emoji` draws each emoji above its line, where it is clipped away
Built with `font_emoji`, comms-mail's subjects, headers and message text
keep the room for each emoji and show nothing in it — a few coloured
specks above the line at most. "👀 Big sale 🥳" reads "  Big sale
today". With `font_complex` alone they are boxes, which at least say
something is there.

The picture is drawn above the line box. Drawing "H", "g" and "👀" with
`Font.Draw` at a line box whose top is y 100 (Titillium, `BakeFamily`):

| size | line box | H | g | 👀 |
| --- | --- | --- | --- | --- |
| 14 | 100–121.3 | 106–115 | 109–119 | **87–97** |
| 16 | 100–124.3 | 107–117 | 110–121 | **85–97** |
| 24 | 100–136.5 | 111–127 | 115–132 | **77–96** |

The emoji's bottom is above the line's top at every size, so in a window,
where every component is clipped to its box, it is cut away whole. Every
other glyph's vertical bearing is measured from the top of the line box
— `ascent + ymin` (style/opentype.go:678), applied at style/font.go:752 —
and the emoji cell's is `Bearing: Pt(0, -h)` with `h` the line's ascent
(style/emoji_on.go:262 and :281), as if measured up from the baseline: the
picture's top is put `h` above the line's top. The toolkit's own test
draws 👀 at (4, 4) in a 40 px image and asks only that coloured pixels
appear (style/emoji_test.go:44 and :68–77), which a picture drawn 18 px
too high still leaves.
comms-mail builds with `font_complex`, not `font_emoji`, until this is
fixed (Makefile).
**Fix asked for:** `Bearing: Pt(0, a.ascent-h)` — 0 while `h` is the
ascent, so the picture stands on the baseline and reaches the top of the
text — and a test that the emoji's ink lies inside the line box.

Found 2026-10-13 against 0.23.8.

#### 60. An outline face wins over the colour one, so emoji come out half in colour
Under `font_emoji` a rune goes to the interface face, then to the outline
fallbacks, and to a colour emoji face only when none of those has it
(`uitkFontmap.ResolveFace`, style/shape_complex_layout.go:99–121, the
colour face at :114). 0.23.8 also added outline symbol faces to the
default fallback list — "Noto Emoji", "Symbola", "Noto Sans Symbols 2",
"Noto Sans Symbols" (style/fontfallback.go:53) — and Noto Sans Symbols 2
is on most Linux machines with Noto installed, as it is here. It has 👍
and ❤, so in "👀 🥳 👨‍👩‍👧 🇷🇴 👍🏽 ❤️" the first four go to Noto Color Emoji
and 👍 and ❤️ are drawn in black outline beside them. A sequence is split
across the two: 👍🏽 is an outline thumb from one face and a skin-tone
swatch from the other, where Noto Color Emoji's own GSUB makes it one
picture; and ❤️'s U+FE0F, which asks for the emoji presentation, is not
heeded.
**Fix asked for:** in a `font_emoji` build, send a rune whose default
presentation is emoji (Emoji_Presentation), one followed by U+FE0F, a
modifier sequence and a ZWJ sequence to the colour face before the
outline fallbacks (UTS #51), and keep the run together so the colour
face's GSUB composes it.

Found 2026-10-13 against 0.23.8 (outline face: Noto Sans Symbols 2).

#### 61. `font_complex` never borrows Arabic, Hebrew or Indic letters
Built with `font_complex`, Arabic, Hebrew and Hindi in comms-mail's
subjects and text are boxes, now in right-to-left order. Adding "Noto
Sans Arabic", "Noto Sans Hebrew" and "Noto Sans Devanagari" — all
installed here, plain TTF files — to `SetFallbackFamilies` changes only
the Arabic comma and question mark, which are now drawn; every letter
stays a box.

`borrowable` refuses the scripts that need shaping — Arabic, Hebrew,
Syriac, Devanagari and the other Indic scripts, Thai, Khmer, Myanmar and
more (style/fontfallback.go:121–142) — which is right for a
`font_fallback` build that cannot shape them. But the shaping path asks
the same question before it borrows (`if borrowable(r)`,
style/shape_complex_layout.go:103), so in a `font_complex` build those
letters can only be shaped by the interface face itself, and no
interface face here has them. The comma and the question mark got
through because Unicode gives them no script. The default list
(style/fontfallback.go:38–54) names no Arabic, Hebrew or Indic family
either, so even once borrowing is allowed a machine with Noto Sans
Arabic would not use it unless the application names it.
**Fix asked for:** borrow every script in a build that shapes
(`font_complex`, `font_emoji`), keeping `borrowable` for the
glyph-at-a-time build; and name the common faces for those scripts in
the default list (Noto Sans Arabic, Noto Naskh Arabic, Noto Sans Hebrew,
Noto Sans Devanagari, Noto Sans Thai, DejaVu Sans).

Found 2026-10-13 against 0.23.8.

### Closed in 0.23.8

| # | Gap | 0.23.8 | In comms-mail |
| --- | --- | --- | --- |
| 57 | A text view given its text before it was laid out opened scrolled down | The inner box is clamped at zero, and a line taller than the box keeps its top in view | `reader.setBody`'s scroll to the top is gone; `TestTheMessageTextStartsAtItsTop` passes on the toolkit's fix alone, in the plain build and with `font_complex` |
| 58 | Emoji stayed boxes with `font_fallback` | A lender need not draw Latin; outline emoji and symbol faces join the default list; `-tags font_emoji` draws colour bitmap emoji (CBDT, sbix), untinted | Answered as asked, and not yet usable: under `font_emoji` the emoji are drawn above their line and clipped away (#59), and an outline face wins over the colour one for some (#60). comms-mail builds with `font_complex` meanwhile, where emoji are boxes and 👍 ❤ draw in outline |

Also from 0.23.8:

- **`-tags font_complex`** — shaping, bidirectional layout and Unicode
  line breaking (+2.16 MB, no font data). The Makefile builds with it:
  Japanese text now wraps without starting a line with 。, and borrowing
  still draws Cyrillic, Greek and CJK. Arabic, Hebrew and Indic are
  still boxes: #61.
- **A scroll bar keeps the presses that start on it** — in the list, the
  table, the tree and the card list, a drag on the bar no longer drags
  the row beside it, and a press in the gutter no longer moves the
  selection. comms-mail's message list and folder tree are those views;
  nothing to change.
- **The fallback list takes effect at once** when an application changes
  it.
- comms-mail's screenshots differ from 0.23.7's only by a pixel here and
  there inside words: `font_complex` takes kerning from the face's own
  `GPOS`. Nothing moves.

The reports and the toolkit's answers, as they were:

#### 57. A text view given its text before it is laid out opens scrolled down
The message comms-mail shows at start opens a few pixels down its text,
the top of its first line cut off by the view's edge; every message
opened after it starts at the top. The view is given the text before the
window's first layout, while it is still 0 px tall.

`TextArea.SetText` keeps the caret in view (`ensureCaretVisible`,
widgets/textarea.go:122 and :690–703 in 0.23.7). Its guard against a box
with no room, `inner.Dy() <= 0` (:693), never fires for a box shorter
than its padding: `inner()` (:244–248) is `XYWH(pad, pad, w-2·pad, h-2·pad)`,
and `paintengine2d.XYWH` canonicalises the negative height
(paintengine2d geom.go:66–68), so a 0 px view's inner box is 20 px tall
(2 × the 10 px pad). The caret's line, 26.3 px, does not fit in 20, so
the view scrolls by the difference (:703), and once it grows that 6.3 px
is a legal offset and stays. Given the text at 0, 5, 10 and 15 px tall, a
view opens 6.3, 11.3, 16.3 and 21.3 px down once it is 260 tall; at 25
and 30 px, 21.3 and 16.3. Traced with a print in `ensureCaretVisible`:
bounds 280 × 0, inner height 20, caret on line 0, line 26.3.
comms-mail scrolls the Message tab's text to the top whenever it is given
new text (`reader.setBody`, mailui/reader.go), with a test.
**Fix asked for:** an inner box that does not go below zero —
`Rect{Min: (pad, pad), Max: (pad + max(w−2·pad, 0), pad + max(h−2·pad, 0))}`
rather than `XYWH` — so the guard fires; and where the caret's line is
taller than the box, keep the line's top in view (`scrollY = y`) rather
than its bottom.

Found 2026-10-13 against 0.23.7 (the same in 0.23.6).

#### 58. Emoji stay boxes with `font_fallback`
0.23.7's fallback draws Cyrillic, Greek, Chinese, Japanese and Korean in
comms-mail's message list and reading pane from the machine's own faces
(Noto Sans CJK here), and ✓ and → as well. Emoji — "👀 Big sale 🥳",
in subjects every day — are still boxes, for two reasons:

- **A fallback face must draw Latin.** `bakeFallbackFace`
  (style/fontfallback_on.go:98) bakes through `bakeFaceAscent`
  (style/font.go:863), which refuses any face
  without an 'A' — "cannot draw Latin" (style/font.go:878–881). That rule
  is right for the face that draws the interface and wrong for one that
  lends a single glyph: Noto Color Emoji, Noto Emoji or a symbols face
  can never lend. Added to `SetFallbackFamilies`, Noto Color Emoji
  changes nothing; the default list (style/fontfallback.go:36–44) names
  no emoji family at all.
- **Colour glyphs are not drawn.** Glyphs come from
  `golang.org/x/image/font/sfnt` (v0.18.0), whose `LoadGlyph` answers
  "sfnt: colored glyph" for 👀 and 🥳 in NotoColorEmoji.ttf (CBDT
  bitmaps), with no outline. Noto Color Emoji is the only face on this
  machine with emoji, and the usual one on Linux.

**Fix asked for:** skip the Latin check for a face baked to lend a glyph
(it only has to cover the rune), add the common emoji families to the
default list (Noto Color Emoji, Noto Emoji, Apple Color Emoji, Segoe UI
Emoji, Twemoji), and draw colour bitmap glyphs (CBDT/CBLC, sbix: PNG
strikes, scaled to the line), or at least an outline emoji face where
one is installed.

Found 2026-10-13 against 0.23.7.

### Closed in 0.23.7

| # | Gap | 0.23.7 | In comms-mail |
| --- | --- | --- | --- |
| 2 | No font fallback: emoji, and whole scripts, were boxes | `-tags font_fallback`: a rune the face lacks is borrowed from the first of `style.FallbackFamilies()` that has it, baked to the borrowing face's baseline; the faces are the machine's own | The Makefile builds the window with `font_fallback`. Imported mail with subjects and senders in Russian, Greek, Chinese, Japanese and Korean draws them in the list and the reading pane (from Noto Sans CJK), and ✓ and → too; without the tag every one was a row of boxes. Emoji are still boxes: #58 |

Also from 0.23.7, nothing for comms-mail to change: a window keeps its
look's outline when its own bar is the caption (comms-mail's tabs are:
Hangar keeps its cut corners and Deck its silhouette, rendered), words
fitted to the width they were measured at are not cut short, a button's
mark strip is the same in logical pixels at every scale, a panel's floor
holds its chrome (the invitation card and the search dialog are panels;
their tests pass), and two ways a passphrase was left on the clipboard
are closed. `style.Motion` lets a window say its motion is the thing
itself; comms-mail has none such. The screenshots render as before, but
for the message text starting at its top (#57).

The reports and the toolkit's answers, as they were:

#### 2. No font fallback: emoji, and whole scripts, are boxes
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

**Closed in 0.23.7** for every script but emoji, which #58 carries on.

### Closed in 0.23.6

| # | Gap | 0.23.6 | In comms-mail |
| --- | --- | --- | --- |
| 55 | A wrapping label, or a `FitRows` view, lost a line at a fractional width | Wrapped text is measured at `floor` of the width offered, and every measured size is rounded up to whole pixels on both axes, so layout cannot hand back less | Nothing to change. The sweep that found it — warning lines, bold lines and the path view at widths 120–420 and scales 1 to 2, 13,545 layouts — finds nothing now (it found 25, all at 1.25), and the 1.25 / 213 px case renders whole |
| 56 (29) | A button with an icon was 64 px wider than one without | **Breaking:** `Engine.DrawButton` is handed a `ButtonDraw`, the label and the mark, so a marked button is one strip wider (`style.ButtonLabelBox`, `ButtonIconBox`) | Nothing to change: comms-mail draws no engine of its own. Measured at 1x: "No" 59 px plain and 91 with an icon, "Maybe" 86 and 118 (+32 where it was +64). Rendered in twelve looks — wmaker-default, metal-ocean, dark, light, Clearlooks, Win95, Aqua, Breeze, Adwaita, BeOS, KDE 1, Hangar — the mark and the label never meet. Rows of buttons in Settings and the account window are narrower; `foldRow` stays, for windows narrower still |

Also from 0.23.6:

- **The desktop's reduced motion is recorded, not obeyed** (breaking).
  comms-mail has no motion setting of its own, so it passes the
  desktop's on (`style.SetFollowDesktopMotion(true)` in `Open`), as the
  toolkit asks of such an application; tested.
- **Seven icons a vault is made of.** Unlock buttons wear the open
  padlock (`IconLockOpen`) where they wore the closed one, OpenPGP keys,
  S/MIME certificates and a signature's key the key (`IconKey`), and
  Change passphrase the masked password (`IconPassword`) where it wore
  the pen.
- **`widgets.Toast`**, a line that says something and goes. comms-mail
  has 110 status lines (`session.mark`) — errors among them — that show
  only in the status bar, which is off by default; whether they become
  toasts is comms-mail's to decide (BACKLOG.md).
- Nothing else for comms-mail to change: a key with no focus reaches the
  content root, a scroll view leaves Ctrl+Page Up and Down to the tabs, a
  popup is shown at its size in the look it is drawn in, and the fallback
  file chooser takes every pattern of a filter. The screenshots render as
  before but for the narrower buttons.

The reports and the toolkit's answers, as they were:

#### 55. A wrapping label, or a `FitRows` view, loses a line at a fractional width
0.23.5 rounds a wrapping `Label`'s measured *width* up to a whole pixel
(widgets/label.go:187), so a label sized to its own text keeps its last
line. A label *stretched* to its parent's width is still caught: it is
measured at the width it is offered (:163, `wrapW` from `c.MaxW`), and
layout then rounds each edge of its box to the nearest pixel
(`widget.PixelRect`, widget/base.go:242–249), which can hand it up to a
pixel less. At a fractional scale a column's inner width is fractional —
at 1.25, a window 213 wide with 7 dp of padding is 248.5 px inside — the
text is measured at 248.5 (6 lines), laid out at 248 (7), and the lines,
centred, lose their first and last: #49's symptom, back at that width.
`TextArea.FitRows` is caught the same way (widgets/textarea.go:191–198,
`rowsFor(w)` at `c.MaxW`): a path measured at 282.5 px as two lines is
laid out at 282, wraps to three, and the third is behind a scroll bar.
Found by laying out comms-mail's warning lines and its path view at
widths 120–420 and scales 1, 1.25, 1.5, 1.75 and 2: 24 cut labels and
one hidden line in about 13,500 layouts, all at 1.25. comms-mail does
not work round it.
**Fix asked for:** measure wrapped text at the narrowest whole width the
box can be handed — `floor(c.MaxW)` — in `Label.Measure` and
`TextArea.rowsFor`, or have layout round a child's offered width down
before it measures.

Found 2026-10-06 against 0.23.5.

#### 56. A button with an icon is 64 px wider than one without
*First filed as #29 (2026-09-28). Answered in 0.22.4 with documentation,
left open as a design change, and unchanged since. Filed again because it
is still the gap comms-mail hits most often.*

`Button.Icon` keeps the engine's label centred by reserving a strip at
*each* end: `Button.Measure` adds `(side + gap) * 2`
(widgets/button.go:166–171 in 0.23.5), where a leading icon needs one
strip. Measured on 0.23.5 at 1x, the same in the light and the dark
look: "No" is 59 px wide plain and 123 with an icon, "Maybe" 86 and 150,
"Accept" 85 and 149, "Remove" 95 and 159. That is +64 px on every
button, half of it an empty strip after the label.

In a mail client buttons come in rows, so a few icons push a row past a
narrow window. comms-mail's invitation answers (Accept / Maybe / No),
Settings' account and filter rows, the Add account sign-in row, the
remote-images bar and the Passwords page's Unlock each fold onto a
second line instead (`foldRow`, a `widgets.Wrap`, 21 rows in
mailui/), and the rule editor's Remove is an icon-only tool button to
save the room. The alternatives the docs name (`NewToolButton`,
`ToolIconBtn`, `NewIconButton`, `NewWrap`) do save room, but by giving up
the push-button look or the word, or by folding the row.
**Fix asked for:** a leading-icon layout that moves the label over by one
strip rather than reserving two: an opt-in `Button.IconLeading`, with
the toolkit drawing the label after the mark itself, and the engine
drawing the face (the trade-off 0.22.4 named), or a label box that
`DrawButton` draws the text into, so every engine's own label treatment
stays.

Found again 2026-10-06 against 0.23.5.

#### 29. `Button.Icon` makes a button an icon-and-a-half wider
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

**Unchanged in 0.23.3.**

**Unchanged in 0.23.4 and 0.23.5** (widgets/button.go is the same file).

**Filed again as #56** (2026-10-06), so it is read as a current request
and not as history. This entry keeps the history.

**Closed in 0.23.6** with #56.

### Closed in 0.23.5

| # | Gap | 0.23.5 | In comms-mail |
| --- | --- | --- | --- |
| 47 | An app could not keep the window-menu button off for itself | `Application.SetHideWindowMenu` pins the choice against look.json's `hideWindowMenu` (`HideWindowMenuPinned`) | `noWindowMenu` calls it once; the `OnLookChange` re-apply is gone. Tested by applying a look.json that shows the button |
| 48 | No read-only text that can be selected and fits its text | `TextField.ReadOnly`; `TextArea.FitRows` and `MaxRows` | The Passwords page's paths are `NewTextView` with `FitRows`; `fitText` is gone. The toolkit's tests cover Backspace, Delete and an IME's delete-surrounding on a read-only field. A stretched view can still lose a line at a fractional width (#55) |
| 49 | A wrapping `Label` with an `Icon` cut off its first and last lines | Measured at the width its text gets; a wrapping label's width rounded up to a whole pixel | `iconLine` is a wrapping `Label` with its `Icon` again (sender warnings, signed/encrypted lines, the Security tab, Settings' "Not available"); its row of a mark and a label is gone. Rendered: nothing cut. Stretched to a fractional width it still can be (#55) |
| 50 | A `Label` was plain or a `Title`: no bold at the body's size | `Label.Bold` | Educate's step names are bold labels; the `strong` component that drew them is gone |
| 51 | Measuring a `ScrollView` changed what it scrolled | `Measure` writes nothing down; `Arrange` decides | The Security tab is its scroll view again (`stillScroll` is gone) and the Message tab's header box measures its scroll view (`reserveBox`'s own measuring is gone). The tests that caught it, and the Security tab's speed test, pass |
| 53 | An app could not add an icon of its own | `style.RegisterIcon`, `IconDrawer`, `RegisteredIcon`; `IconByStem` answers registered stems | The mail server in the Security tab's route and Educate's picture is registered under "server" and drawn by `DrawToolIcon` in every set. Compact Folder, Body as plain text and Turn Off can now have icons; drawing them is comms-mail's (BACKLOG.md) |
| 54 | A window told no one when it took or lost the focus | `Window.OnActiveChange` | The tray's new-mail logo goes back when the window takes the focus; the root's check on every paint is gone. Tested by focusing a headless window out and in |

Also from 0.23.5, nothing for comms-mail to change: a tray icon's own
picture is no longer offered beside a theme name hosts would prefer
(comms-mail gives a picture alone), a re-made X11 window keeps the windows
it owns, keyboard focus in a menu scrolls its row into view, and lists,
tables and trees act on the row under the pointer and only on a real
double click. paintengine2d moves to v0.12.0. comms-mail's screenshots
render as before, except that the message text no longer runs under its
scroll bar (0.23.4).

The reports and the toolkit's answers, as they were:

#### 47. An app cannot turn the window-menu button off for itself
comms-mail wants no window-menu button (KWin's **M** at the left end of
its captions) on any of its windows. There are two switches, and neither
is the app's:

- `Application.SetHideWindowMenu` is the *user's* look.json
  `"hideWindowMenu"`: `ApplyAppearance` (app/desktopprefs.go:151) sets it
  back from the file every time it applies one — every change uitoolkit's
  Settings makes to look.json, with `WatchLook` — and `ThemeOverride`,
  the app's own level of the cascade, has no field for it.
- `Window.SetCaptionButtonVisible(platform.CaptionMenu, false)` is per
  window, so every window an app opens has to remember it, and windows
  the toolkit opens for it are out of reach. Where the desktop can keep a
  window above, `keepAboveInMenuSlot` (app/frame.go:562) has already put
  keep-above in the slot by the time the hidden button is asked about.

comms-mail calls `SetHideWindowMenu(true)` and again from `OnLookChange`
after each look (mailui/theme.go, `noWindowMenu`), which works because
`ApplyAppearance` always ends in `SetLook`, whose hooks run last.
**Fix asked for:** an app-level choice the file does not overwrite — a
`HideWindowMenu` on `ThemeOverride` / `Options`, or `SetHideWindowMenu`
from the app pinning it as `SetTitleBarPrefs` pins the layout.

Found checking 0.23.3; the same in 0.23.4.

#### 48. No read-only text that can be selected and fits its text
Settings › Security › Passwords shows the encrypted and plain files'
paths as text to select and copy, not edit. None of the three candidates
does it alone:

- `Label` cannot be selected at all.
- `TextField` has no read-only mode. `Accept` refuses typing, pastes and
  a selection's deletion (`replaceSel`), but Backspace and Delete with
  nothing selected change `Text` directly (widgets/textfield.go:684 and
  :701 in 0.23.3, :680 and :697 in 0.23.4), and so does
  `IMEDeleteSurrounding`.
- `NewTextView` is read-only and selectable (Ctrl+A, Ctrl+C), but is
  `MinRows` tall whatever it holds (`TextArea.Measure`,
  widgets/textarea.go:176 / :174): a path wider than the box wraps onto
  a second line hidden behind a scroll bar.

comms-mail wraps a `NewTextView` in a component that lays the view out
at the width it is measured at, counts `Lines()`, sets `MinRows`, and
puts the view back in its own box, since a parent's probe can measure
after the layout (mailui/vault.go, `fitText`).
**Fix asked for:** a selectable `Label` (`Selectable`), or a
`TextField.ReadOnly`, and a `TextArea` that can be as tall as its text
(`FitRows`, Qt's `sizeHint` from the document, GTK's natural height).

Found 2026-10-04 against 0.23.3; the same in 0.23.4.

#### 49. A wrapping `Label` with an `Icon` cuts off its first and last lines
`Label.Measure` wraps the text at the whole width offered
(widgets/label.go:144, `wrapW = c.MaxW - 2`) and adds the mark's width
afterwards; `Paint` takes the mark's room off first and wraps at what is
left (:211–214). So a long line wraps to more lines than it was measured
for, and the lines, centred in the box, lose the first and the last —
comms-mail's sender warnings, the reader's signed/encrypted lines, and
Settings' "Not available: …" all did, at ordinary widths.
comms-mail sets the mark alone (`NewIconLabel(icon, "")`) beside a
wrapping label of its own in a row (mailui/icons.go, `iconLine`).
**Fix asked for:** measure at the width the text will have —
`wrapW = c.MaxW - 2 - iconSide - iconGap` when there is an icon.

Found 2026-10-04; the same in 0.23.3 and 0.23.4 (measure :140–156, paint
:205–214).

#### 50. A `Label` is plain or a `Title`: no bold at the text's own size
Settings › Security › Educate names each hop ("From your mail server to
theirs") over the words about it: a run-in heading, bold at the body's
size, as GTK's `heading` class, Qt's `QFont::setBold` and HTML's
`<strong>` give. `Label` picks its face from `Title` (`TitleFont`, the
page title's size, a size above the section titles it sits under) or
`Mono`, else the body font (`Label.font`, widgets/label.go:97–109 in
0.23.3 and 0.23.4); the look's `BoldFont` is used only by `Card.Bold`
and the header bar. comms-mail draws the line itself with `BoldFont`
and its own wrapping (mailui/educate.go, `strong`).
**Fix asked for:** `Label.Bold`, the body size in `BoldFont`, wrapping
as a plain label does.

Found 2026-10-04 against 0.23.3; the same in 0.23.4.

#### 51. Measuring a `ScrollView` changes what it scrolls
`ScrollView.Measure` keeps the size its child measured as the size it
scrolls (`s.content = s.child.Measure(…)`, widgets/scroll.go:130 in
0.23.3), and `Arrange` does too (:174). The scroll range, the clamp and
the bar's thumb all read it (`maxOff` :211, `clamp` :215, the bar's
geometry :297). So a measure after the layout — a splitter asking its
panes' minimum widths (`MinWidthOf` probes at a quarter of the natural
width), a parent measuring unbounded — leaves the view scrolling the
content as it would be at *that* width until the next `Arrange`: the
thumb half-way down at the end of the content, the wheel going on into
nothing. In comms-mail's Security tab, scrolled to the end, the range
read 573 px after the layout, 2979 after a min-width probe and 4147
after a narrow measure; the Message tab's header, whose chips and
warnings wrap, 609 then 1144, its scroll jumping. comms-mail puts the
Security tab's scroll view behind a wrapper that answers measures itself
(mailui/securitytab.go, `stillScroll`), and sizes the header by
measuring what its scroll view shows, never the scroll view
(mailui/sharebox.go, `reserveBox`).
**Fix asked for:** `Measure` without side effects — the scroll range
from `Arrange` alone.

Found 2026-10-05 against 0.23.3; the same in 0.23.4 (widgets/scroll.go
is unchanged).

#### 53. An app cannot add an icon of its own
Some of comms-mail's marks are in neither the 56 typed ids nor the 80
shipped stems (docs/widgets.md:282 and :317): a mail server, in the
Security tab's route and the Educate page's three hops; Compact Folder;
Body as plain text; a filter's Turn Off. `IconByStem` answers only typed
names and shipped stems (style/icon.go:447) and gives `IconNone` for any
other, so an app has no `ToolIcon` to pass to `Button.Icon`,
`MenuItem.Icon` or `DrawToolIcon` for a mark it ships. `AddSearchPath`
art (docs/recipes.md:102) can only stand in for stems the toolkit
already knows, and even a known stem-only icon draws the missing-icon
mark in the drawn sets (docs/widgets.md:326), which every pack uses unless
the user picks otherwise.
comms-mail draws the server itself in strokes like the drawn sets'
(mailui/figure.go, `drawServer`), and leaves the other three rows
without a mark.
**Fix asked for:** typed ids for `server`, `compact`, `text` (plain text)
and `power` (turn off), or a way for an app to register its own icon — a
stem with a vector for the drawn sets (`style.RegisterIcon(stem, draw)`),
that the user's file sets can still replace by stem.

Found 2026-10-06 against 0.23.4.

#### 54. A window tells no one when it takes or loses the focus
comms-mail's tray shows the logo in a seal while new mail waits to be
seen, and puts the plain logo back when the window takes the focus.
`Window.Active` says whether it has the focus (app/window.go:520 in
0.23.3, :538 in 0.23.4), but nothing says when that changes: `setActive`
(:524 / :542), reached from the desktop's activated state and from
focus in and out, repaints the window and calls nothing else. The
window's hooks are `OnMove`, `OnResize`, `OnLockKeys`, `OnSecureInput`,
`SetOnCaptionDrag` and `SetOnCloseRequest`, and the application's
`OnLookChange`; none is for the focus.
comms-mail's root component checks `widget.WindowActive` each time it
paints, since `setActive` repaints the whole window (mailui/tray.go,
`mailSeen`; mailui/keys.go, `shortcutRoot.painted`): a check on every
frame, which works only while the toolkit repaints on the change.
**Fix asked for:** `Window.OnActiveChange(func(active bool))`, called
from `setActive` when the state changes (Qt's `QEvent::WindowActivate`,
GTK's `notify::is-active`, Win32's `WM_ACTIVATE`).

Found 2026-10-06 against 0.23.3 and 0.23.4.

### Closed in 0.23.3

| # | Gap | 0.23.3 | In comms-mail |
| --- | --- | --- | --- |
| 45 | `CaptionMerged` under BeOS's fitted caption cut the bar away | `DecorationState.Merged`: a merged caption drops the fitted width and the silhouette, as a maximized or tiled window does | `captionStyle` and its look-change hook are gone; comms-mail asks for `CaptionMerged` under every pack again. Checked under BeOS: a full-width yellow caption with the menu, Fetch / Write / Search, the tabs and the window's buttons. Tested through the frame's own `DecorationOf` |
| 46 | An unselected tab's title and mark were centred on its slot | `style.TabFaceOf` (an engine hook, `TabFaceEngine`): the title and mark sit on the face the look drew | Checked on all seven Window Maker, NeXT and OpenStep packs with the Mail tab behind a message tab: "Mail" is inside its face; irix, BeOS and sourcegit unchanged |

Also from 0.23.3, nothing for comms-mail to change: a menu row's
accessible action now runs (`PopupMenu.AccessibleAction`), an accessible
list row is where the row is drawn, Caps Lock is reported before the
first key on Wayland and macOS, and a secret copied to the clipboard no
longer outlives it. comms-mail's own session (metal-ocean, Relaxed, small
icons, the toolkit's frame) reports no diagnostics.

The reports and the toolkit's answers, as they were:

#### 45. `CaptionMerged` under a caption that fits its title (BeOS) cuts the bar away
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

#### 46. An unselected browser tab's title and mark are centred on its slot, not its face
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
