package mailui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Appearance: the themes in this build, one click to use one in
// comms-mail (theme.go: it overrides the theme every uitoolkit app shares,
// for comms-mail alone), and a way back to the shared one.

// themeLabel is how the list names a pack.
func themeLabel(p style.ThemePack) string {
	if l := strings.TrimSpace(p.Label); l != "" {
		return l
	}
	r := []rune(p.Name)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// themeLabelFor names the pack called name, or says it is not in this
// build.
func themeLabelFor(name string) string {
	if p, ok := style.LoadTheme(name); ok {
		return themeLabel(p)
	}
	return name + " (not in this build)"
}

func prefsAppearance(a *app.Application) widget.Component {
	all := style.ListThemes()
	var rows []style.ThemePack
	shade := 0 // all, light, dark
	var table *widgets.TableView
	var follow *widgets.Button

	filter := func() {
		rows = rows[:0]
		for _, p := range all {
			switch {
			case shade == 1 && p.Palette != style.ThemeLight, shade == 2 && p.Palette != style.ThemeDark:
				continue
			}
			rows = append(rows, p)
		}
		table.RowCount = len(rows)
		table.Selected = -1
		cur := effectiveAppearance().Name
		for i, p := range rows {
			if p.Name == cur {
				table.Selected = i
			}
		}
		table.Invalidate()
	}
	// Use the shared theme is on while comms-mail has a theme of its own;
	// its tip says which it uses and which every other uitoolkit app keeps.
	show := func() {
		shared := style.LoadAppearance().Name
		if own := ownTheme(); own != "" {
			follow.Tip = fmt.Sprintf("comms-mail uses %s; every other uitoolkit app keeps %s, the theme they share (%s).",
				themeLabelFor(own), themeLabelFor(shared), style.AppearancePath())
			follow.SetEnabled(true)
		} else {
			follow.Tip = fmt.Sprintf("comms-mail follows %s, the theme every uitoolkit app shares (%s). Pick one in the list to use another in comms-mail alone.",
				themeLabelFor(shared), style.AppearancePath())
			follow.SetEnabled(false)
		}
		follow.Text = "Use the shared theme (" + themeLabelFor(shared) + ")"
		follow.Invalidate()
	}

	table = widgets.NewTableView([]widgets.TableColumn{
		{Title: "Theme", MinWidth: 180, Sortable: true},
		{Title: "Year", Width: 60, MinWidth: 50, Sortable: true},
		{Title: "Light or dark", Width: 110, MinWidth: 90, Sortable: true},
	}, 0, func(row, col int) string {
		if row < 0 || row >= len(rows) {
			return ""
		}
		p := rows[row]
		switch col {
		case 0:
			return themeLabel(p)
		case 1:
			if p.Year > 0 {
				return strconv.Itoa(p.Year)
			}
			return ""
		default:
			if p.Palette == style.ThemeLight {
				return "Light"
			}
			return "Dark"
		}
	}, func(row int) {
		if row < 0 || row >= len(rows) || rows[row].Name == effectiveAppearance().Name && ownTheme() != "" {
			return
		}
		setOwnTheme(a, rows[row].Name)
		show()
	})
	// A header sorts the list: by name, by year, or light before dark.
	table.OnSort = func(col int, asc bool) {
		key := func(p style.ThemePack) string {
			switch col {
			case 1:
				return fmt.Sprintf("%04d %s", p.Year, strings.ToLower(themeLabel(p)))
			case 2:
				return string(p.Palette) + " " + strings.ToLower(themeLabel(p))
			}
			return strings.ToLower(themeLabel(p))
		}
		sort.SliceStable(all, func(i, j int) bool {
			if asc {
				return key(all[i]) < key(all[j])
			}
			return key(all[i]) > key(all[j])
		})
		filter()
	}
	shades := widgets.NewComboBox([]string{"All themes", "Light themes", "Dark themes"}, 0, func(i int) {
		shade = i
		filter()
	})
	follow = newButton("", func() {
		setOwnTheme(a, "")
		show()
		filter()
	})
	filter()
	show()

	col := widgets.NewColumn(foldRow(shades, follow), table).WithGap(8)
	col.AddFlex(table, 1)
	return col
}
