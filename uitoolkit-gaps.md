# uitoolkit gaps found building comms-mail

Gaps and rough edges in [uitoolkit](https://github.com/codemodify/uitoolkit)
hit while building this mail client, for the toolkit's maintainers. Each
entry says where it was verified and what would fix it. New findings are
appended under **Future** as they turn up.

Roughly ordered by impact.

## Confirmed

### 1. No focus-preserving completion / type-ahead popup
When a popup is shown via `widget.ShowPopup`, the window routes keys to the
popup and stops bubbling them to the field (`app/window.go` key dispatch:
"The popup owns the keyboard while it is up"). So there is no way to show a
suggestion dropdown on the popup layer while the user keeps typing. Every
autocomplete / type-ahead / search-suggest UI needs this. Worked around in
comms-mail by laying the suggestion list out **inline** and relying on
unhandled keys bubbling from the field to a wrapper — it works but reflows
the form and is fiddly.
**Fix:** a non-capturing popup mode (forwards non-nav keys to the anchor),
or a built-in completion field.

### 2. No emoji / color-font fallback
Emoji and many symbols render as ▯ (tofu) in subjects and bodies — seen
across real messages ("👀", "🥳" in subjects, marketing copy). Mail is full
of emoji, so this is high-impact and very visible.
**Fix:** fall back to an installed emoji font (Noto Color Emoji / system)
for glyphs the primary face lacks.
*Precisely (checked against uitoolkit v0.20.1 dev):* there is no font
fallback at all — `style/symbols.go` draws hand-made vector stand-ins for
exactly four runes (★ 📎 ● 🔇); every other glyph Titillium lacks is tofu.
That includes plain arrows and dingbats, not just emoji: ↳ (U+21B3), →,
✓ (U+2713), ✗ (U+2717), ⊘, └. JetBrains Mono has several of them (✓ ✗ →
└) but is not consulted for UI text. comms-mail works around it with
glyphs Titillium has: › for thread replies, • for an active filter pin,
"(yes)" / "(no)" beside invite guests.

### 3. richtext image placeholder is an empty box with no alt text
For an image-heavy HTML email with images unresolved (a real Apple
newsletter rendered in the HTML pane), the body becomes a wall of empty
boxes — worse than useless. The placeholder shows no alt text and no
broken-image affordance.
**Fix:** render the `<img alt>` text and/or a broken-image glyph inside the
placeholder; honor width/height so layout does not collapse.

### 4. richtext `ResolveImage` can't distinguish inline vs remote, and doesn't enumerate images
The callback gets every non-`data:` src with no signal whether it is `cid:`
(inline, safe to load) or `http(s):` (remote, a tracking pixel). And there
is no way to list which images a document references for a "load images"
control — the app must re-parse the HTML (comms-mail uses a regex).
**Fix:** pass a classification (data / cid / remote), or expose the
document's image list.

## Minor / nice-to-have

### 5. No multi-value chip/token field
No removable "pill" recipient field (Gmail / Thunderbird). comms-mail uses
comma-separated text.

### 6. TabView has no per-tab disable/hide
Wanted the HTML tab only when a message has HTML; had to always show it with
a placeholder.

### 7. `TextField.SetText` leaves the caret at 0
Rather than moving it to the end. Surprising for a programmatic set (bit a
test); most toolkits move to end.

### 8. No "preferred height for N rows" on ListView
`ListView` height requires a manual `Measure` call; a helper for popovers /
inline lists would simplify.

## Future
<!-- append new findings here as they turn up -->

### 9. FileDialog has no folder-picking mode
`FileDialogOptions.Mode` is `FileOpen` or `FileSave`. Choosing a directory
works only implicitly: open the folder and press Open with nothing selected
(the dialog returns what the path field says, then the selection, then the
directory). comms-mail's "Add a folder or mailbox file…" relies on that and
has to explain it in the title. Maildir / MH / .eml imports are directories.
**Fix:** a `FileOpenFolder` mode (the portal's `directory` option does this
natively).

### 10. No text-input dialog (prompt)
`MessageBoxOptions` has no input field, and there is no `Prompt` /
`InputDialog`. Every "name this" (new folder, rename folder, save search)
needs a hand-built window with a field and OK/Cancel — comms-mail has
`askName` for it. Qt's `QInputDialog::getText` / GTK's entry dialog.
**Fix:** a `Prompt(from, title, label, initial, on func(text string, ok bool))`
alongside `Confirm` / `Warn`, as an in-window overlay like them.

### 11. A wrapping Label that flexes in a Row is clipped
`Label.Wrap` measures "to the width its parent offers", but a `FlexBox`
row measures its flex child before it knows the width it will give it: the
label reports one line's height, is then arranged narrower, wraps to three
lines, and draws them centred in a one-line box — the first and last lines
cut off. Seen in comms-mail's invite card (title beside two buttons).
**Fix:** measure flex children again at their final main-axis size (a
height-for-width pass), or have Row re-measure wrapping children after it
distributes the space.

### 12. FileDialog Save has no suggested file name
`FileDialogOptions` has `Path` but no `Name`: to suggest "Invoice.eml" in
a Save dialog, comms-mail passes the whole file path as `Path`. The native
(portal) dialog splits it into folder and name correctly, but the themed
dialog then tries to list the file as a folder and shows an empty list.
**Fix:** a `Name` (suggested file name) option, and for `FileSave` list
`filepath.Dir(Path)` when `Path` is not a directory.

### 13. A text field takes a file drop meant for its container
`Window.dropTarget` gives a drop to the component under the pointer if it
takes *any* offered type, before asking its ancestors. File managers offer
`text/uri-list` and `text/plain` (the paths as text) together, so dropping
files on a `TextArea` inside a `DropZone` that wants files types the paths
into the text instead of handing the files to the zone. comms-mail's Write
window attaches files dropped anywhere else, but not on the body — where
people drop them.
**Fix:** when the offer carries `text/uri-list` (files), prefer the nearest
ancestor that takes it over a descendant that only takes `text/plain`; or
let `TextArea` decline file drops (an option, or by default).

### 14. ScrollView cannot shrink to its content
`ScrollView.Measure` always asks for the whole height it is offered, so a
scrolling area that should be "as tall as what is in it, up to N" — a
message header that grows with an invitation or attachments but must not
push the body out — has to be wrapped in an app-side box that reads
`ContentHeight()` after measuring. comms-mail's first attempt trusted the
scroll view's answer, and the reading pane's header took every pixel up to
its cap for every message.
**Fix:** a `MaxHeight` (or `ShrinkToContent`) option: measure to the
content's height, capped, and scroll past it.


### 15. A theme whose engine is not in the build turns dark, silently
Engines are opt-in at build time (docs/engines.md), which is fine; but
when look.json names a pack whose engine the app was built without,
`LoadAppearance` keeps the name and reports the theme as **dark** (it
parses the unknown name), and the app paints the default dark palette. A
light pack like `metal-steel` then shows as dark, with nothing to say why
— comms-mail's owner saw it as "the theme is wrong". Verified on v0.21.0:
`LoadTheme("metal-steel")` is false in a default build and true with
`-tags theme_engine_metal`. comms-mail now builds with
`theme_engine_all` and logs a line when the theme is missing.
**Fix:** expose "this pack is not in the build" (a flag on Appearance, or
`LoadTheme` reporting why), and fall back to the pack's own family (the
light starter for a light pack) rather than dark.

### 16. No icons in table cells, column headers or tree nodes — and no mail icons
Marks belong in icons, not in the font (fonts are for text only). But
`TableView` cells and column titles are text (`CellText`, `TableColumn.
Title`), and `TreeNode` has a label, bold and a colour swatch only. So the
message list's marks — ★ starred, 📎 attachment, 🔇 muted, ● unread, the
thread reply mark, and the replied / forwarded marks the list still lacks —
can only be characters today (the four the toolkit draws as vector
stand-ins, and nothing for the rest), and a folder tree cannot show an
active filter pin except in bold. The icon set (`style.ToolIcon`) has no
paperclip, star, reply, forward, check, mute or dot either.
**Fix:** `TableColumn.Icon`, a `TableView.CellIcon func(row, col int)
style.ToolIcon` (with a colour), a `TreeNode.Icon`, and ToolIcons for
paperclip, star (filled / outline), reply, forward, check, mute and a dot.
comms-mail waits for these (the owner's call): the replied / forwarded
marks come with them, and ★ 📎 🔇 ● and the thread mark move to them.
