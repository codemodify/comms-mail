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
*Also:* plain dingbats are missing too, not just emoji — ✓ (U+2713) and
✗ (U+2717) render as tofu, so comms-mail's invite card writes "(yes)" /
"(no)" beside each guest where a check mark would read better.

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
