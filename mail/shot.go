package mail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// ScreenshotNames are the Mail PNGs written by WriteScreenshots.
var ScreenshotNames = []string{
	"mail-dark.png", "mail-light.png", "mail-classic.png",
	"mail-compose.png", "mail-prefs.png",
	"mail-cards.png", "mail-compact.png", "mail-filters.png",
	"mail-empty.png", "mail-account.png", "mail-smart.png",
}

// WriteScreenshots paints the Mail dogfood frames into dir.
func WriteScreenshots(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		return err
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		return err
	}
	defer cli.Close()

	if err := writeMailShot(cli, filepath.Join(dir, "mail-dark.png"), false, LayoutVertical, false, style.DensityDefault, -1); err != nil {
		return err
	}
	if err := writeMailShot(cli, filepath.Join(dir, "mail-light.png"), true, LayoutVertical, false, style.DensityDefault, -1); err != nil {
		return err
	}
	if err := writeMailShot(cli, filepath.Join(dir, "mail-classic.png"), false, LayoutClassic, false, style.DensityDefault, 0); err != nil {
		return err
	}
	if err := writeMailShot(cli, filepath.Join(dir, "mail-cards.png"), false, LayoutVertical, true, style.DensityDefault, -1); err != nil {
		return err
	}
	if err := writeMailShot(cli, filepath.Join(dir, "mail-compact.png"), false, LayoutVertical, false, style.DensityCompact, -1); err != nil {
		return err
	}
	if err := writeComposeShot(cli, filepath.Join(dir, "mail-compose.png")); err != nil {
		return err
	}
	if err := writePrefsShot(cli, filepath.Join(dir, "mail-prefs.png"), 0); err != nil {
		return err
	}
	if err := writePrefsShot(cli, filepath.Join(dir, "mail-filters.png"), 1); err != nil {
		return err
	}
	if err := writeMailFolderShot(cli, filepath.Join(dir, "mail-smart.png"), FolderID("smart/sf-invoices")); err != nil {
		return err
	}
	if err := writeEmptyShot(filepath.Join(dir, "mail-empty.png")); err != nil {
		return err
	}
	return writeAccountShot(filepath.Join(dir, "mail-account.png"))
}

func writeMailFolderShot(cli *Client, path string, folder FolderID) error {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		return err
	}
	root := Open(a, w, cli, AppOptions{ShowFilter: true})
	w.SetContent(root)
	a.PumpOnce()
	widget.Walk(root, func(c widget.Component) {
		if tv, ok := c.(*widgets.TreeView); ok {
			var hit *widgets.TreeNode
			var walk func([]*widgets.TreeNode)
			walk = func(nodes []*widgets.TreeNode) {
				for _, n := range nodes {
					if id, ok := n.Data.(FolderID); ok && id == folder {
						hit = n
					}
					walk(n.Children)
				}
			}
			walk(tv.Roots)
			if hit != nil {
				tv.Selected = hit
				if tv.OnSelect != nil {
					tv.OnSelect(hit)
				}
			}
		}
	})
	a.PumpOnce()
	if err := w.WritePNG(path); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	w.Close()
	return nil
}

func writeAccountShot(path string) error {
	sock, stop, err := StartEmpty(context.Background())
	if err != nil {
		return err
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		return err
	}
	defer cli.Close()
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := OpenAddAccount(a, cli, nil)
	if err != nil {
		return err
	}
	a.PumpOnce()
	if err := w.WritePNG(path); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	w.Close()
	return nil
}

func writeEmptyShot(path string) error {
	sock, stop, err := StartEmpty(context.Background())
	if err != nil {
		return err
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		return err
	}
	defer cli.Close()
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		return err
	}
	w.SetContent(Open(a, w, cli, AppOptions{ShowFilter: true}))
	a.PumpOnce()
	if err := w.WritePNG(path); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	w.Close()
	return nil
}

func writeMailShot(cli *Client, path string, light bool, layout LayoutMode, cards bool, dens style.Density, menu int) error {
	var look style.LookAndFeel = style.DarkLook()
	if light {
		look = style.LightLook()
	}
	look = style.WithDensity(look, dens)
	a := uitoolkit.New(uitoolkit.Options{Look: look, Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		return err
	}
	w.SetContent(Open(a, w, cli, AppOptions{
		Light: light, Layout: layout, ShowFilter: true, CardView: cards, Density: dens,
	}))
	a.PumpOnce()
	PrepareShot(w, menu)
	if cards {
		PrepareShotCards(w)
	}
	a.PumpOnce()
	if err := w.WritePNG(path); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	w.Close()
	return nil
}

func writeComposeShot(cli *Client, path string) error {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := OpenCompose(a, cli, ComposeOptions{})
	if err != nil {
		return err
	}
	a.PumpOnce()
	if err := w.WritePNG(path); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	w.Close()
	return nil
}

func writePrefsShot(cli *Client, path string, tab int) error {
	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := OpenPrefs(a, cli, nil)
	if err != nil {
		return err
	}
	a.PumpOnce()
	if tab > 0 {
		widget.Walk(w.Content(), func(c widget.Component) {
			if tv, ok := c.(*widgets.TabView); ok {
				tv.Select(tab)
			}
		})
		a.PumpOnce()
	}
	if err := w.WritePNG(path); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	w.Close()
	return nil
}
