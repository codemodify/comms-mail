// Command comms-mail is the comms-mail desktop UI. It connects to
// comms-maild over a Unix socket and never speaks IMAP itself.
//
//	comms-maild    # other terminal (empty until add-account)
//	comms-mail
//
//	UITK_MAIL_SOCK=/tmp/mail.sock comms-mail
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/comms-mail/mailui"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/icons"
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

	if *shot != "" {
		if err := mailui.WriteScreenshots(*shot); err != nil {
			log.Fatal(err)
		}
		return
	}

	cli, err := mailcore.DialWait(*sock, 3*time.Second)
	if err != nil {
		log.Fatalf("%v\nStart the daemon first: comms-maild", err)
	}
	defer cli.Close()

	look := style.PreferredLook()
	if *light {
		look = style.WithTheme(look, style.ThemeLight)
	}
	layout := mailui.LayoutVertical
	if *classic {
		layout = mailui.LayoutClassic
	}

	a := uitoolkit.New(uitoolkit.Options{Look: look, Headless: *headless, WatchLook: true})
	// The window icon the desktop shows in its title bar, task bar and switcher.
	a.SetIcon(icons.AppIconRGB("mail", 0x2f, 0x6f, 0xd0)...)
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
