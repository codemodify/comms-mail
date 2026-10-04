package mailui

import (
	"strings"

	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// One verb, one icon: a button's icon comes from what it says, so Save is
// the same disk and Delete the same bin wherever they appear. Only the
// toolkit's typed ids are used — a stem reached by name draws the
// missing-icon mark in the drawn sets that most packs use by default.
var actionIcons = map[string]style.ToolIcon{
	"ok":                     style.IconCheck,
	"apply":                  style.IconCheck,
	"yes":                    style.IconCheck,
	"always":                 style.IconCheck,
	"cancel":                 style.IconClose,
	"not now":                style.IconClose,
	"no":                     style.IconClose,
	"close":                  style.IconClose,
	"decline proposal":       style.IconClose,
	"maybe":                  style.IconQuestion,
	"forgot it":              style.IconQuestion,
	"save":                   style.IconSave,
	"save account":           style.IconSave,
	"remove":                 style.IconTrash,
	"delete":                 style.IconTrash,
	"remove account":         style.IconUser,
	"add account":            style.IconUser,
	"sign in with google":    style.IconUser,
	"sign in with microsoft": style.IconUser,
	"device code":            style.IconUser,
	"add":                    style.IconPlus,
	"add a test":             style.IconPlus,
	"add an action":          style.IconPlus,
	"add a folder":           style.IconFolder,
	"add a mailbox file":     style.IconOpen,
	"edit":                   style.IconPen,
	"change":                 style.IconPen,
	"change passphrase":      style.IconPen,
	"write":                  style.IconPen,
	"fetch":                  style.IconDownload,
	"import":                 style.IconDownload,
	"undo":                   style.IconUndo,
	"show images":            style.IconEye,
	"open in calendar":       style.IconExternalLink,
	"open inbox":             style.IconInbox,
	"account settings":       style.IconSettings,
	"test connection":        style.IconCheck,
	"run now":                style.IconSync,
	"unlock":                 style.IconLock,
	"unlock secretvault":     style.IconLock,
	"more":                   style.IconArrowDown,
	"less":                   style.IconArrowUp,
}

// actionIcon is the icon for a control labelled text ("Save", "Edit…",
// "&Quit"), or IconNone for a label that has none.
func actionIcon(text string) style.ToolIcon {
	key := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.TrimRight(text, "…. "), "&", "")))
	return actionIcons[key]
}

// newButton is widgets.NewButton with the icon for what it says.
func newButton(text string, on func()) *widgets.Button {
	b := widgets.NewButton(text, on)
	b.Icon = actionIcon(text)
	return b
}

// foldRow is a row of buttons that folds onto another line when the window
// is too narrow for it, rather than running off its edge — buttons with
// icons are wide.
func foldRow(kids ...widget.Component) *widgets.Wrap {
	w := widgets.NewWrap(kids...)
	w.Gap = 8
	return w
}

// foldRowTrail is foldRow with its last button against the right edge.
func foldRowTrail(kids ...widget.Component) *widgets.Wrap {
	w := foldRow(kids...)
	w.TrailRight = true
	return w
}

// iconOnly is a small button that is its icon alone, for a row of form
// fields that ends in one: what it does is its tip and its accessible
// name.
func iconOnly(icon style.ToolIcon, what string) *widgets.ToolButton {
	b := widgets.NewToolButton("", icon, nil)
	b.Tip = what
	b.SetAccessibleName(what)
	return b
}
