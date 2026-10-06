// Command comms-mail is the comms-mail desktop UI. It connects to
// comms-maild over a Unix socket and never speaks IMAP itself.
//
//	comms-mail            # starts comms-maild when it is not running
//	comms-maild install   # or have it start at every login
//
//	UITK_MAIL_SOCK=/tmp/mail.sock comms-mail
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/comms-mail/mailui"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
)

func main() {
	headless := flag.Bool("headless", false, "paint offscreen and write mail.png")
	shot := flag.String("screenshot", "", "write mail-*.png into this directory and exit")
	light := flag.Bool("light", false, "start with the light look")
	classic := flag.Bool("classic", false, "classic layout (preview below the thread list)")
	sock := flag.String("socket", mailcore.DefaultSocket(), "comms-maild Unix socket")
	flag.Parse()
	// The icon packs built with comms-mail, found after the user's own.
	mailui.UseShippedArt()

	if *shot != "" {
		if err := mailui.WriteScreenshots(*shot); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Starts comms-maild when nothing answers (comms-maild install makes it
	// start at every login instead).
	cli, err := mailcore.EnsureDaemon(*sock)
	if err != nil {
		log.Fatal(err)
	}
	defer cli.Close()

	// The theme every uitoolkit app shares (look.json), or comms-mail's own
	// from Settings › Appearance. uitoolkit only draws the themes whose
	// engines are in the build: `make` builds with all of them.
	for _, name := range []string{style.LoadAppearance().Name, mailui.OwnTheme()} {
		if note := style.MissingThemeNote(name); note != "" {
			log.Printf("comms-mail: %s; build comms-mail with `make` (-tags theme_engine_all)", note)
		}
	}
	look := mailui.PreferredLook()
	if *light {
		look = style.WithTheme(look, style.ThemeLight)
	}
	layout := mailui.LayoutVertical
	if *classic {
		layout = mailui.LayoutClassic
	}

	// AppID is what the desktop files the windows under (Wayland's app_id,
	// X11's WM_CLASS), whatever the binary is called.
	a := uitoolkit.New(uitoolkit.Options{Look: look, Headless: *headless, WatchLook: true, AppID: "comms-mail"})
	// The window icon the desktop shows in its title bar, task bar and
	// switcher: comms-mail's logo (logo-normal.png).
	a.SetIcon(mailui.AppIcons()...)
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, MinWidth: 860, MinHeight: 560,
	})
	if err != nil {
		log.Fatal(err)
	}
	win.SetContent(mailui.Open(a, win, cli, mailui.AppOptions{
		Light: style.LookAppearance(look).Theme == style.ThemeLight, Layout: layout,
	}))
	if *headless {
		if err := win.WritePNG("mail.png"); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote mail.png")
		return
	}
	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
