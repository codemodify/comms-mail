package mailui

// What uitoolkit chose with build tags, desktop-ui chooses by import. Here,
// in the package both window programs and mailui's own tests link (the
// daemon links none of it):
//
//   - every window system: Wayland, X11, the freedesktop services (the
//     tray, notifications, portals) and the AT-SPI bridge on Linux, Win32,
//     AppKit;
//   - every theme engine, so the window draws whichever theme the person
//     chose for all desktop-ui apps, or for comms-mail alone;
//   - complex text with colour emoji: mail comes in every script, so a rune
//     the interface face lacks is borrowed from an installed face, Arabic,
//     Hebrew and the Indic scripts are shaped and laid out right to left,
//     lines break where Unicode allows, and emoji are pictures.
import (
	_ "goframework.io/desktop-ui-platform-all"
	_ "goframework.io/desktop-ui-text-complex"
	_ "goframework.io/desktop-ui-themes-all"
)
