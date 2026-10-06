// Package commsmail holds what the comms-mail programs share that is not
// code: its logo.
package commsmail

import _ "embed"

// Logo is comms-mail's logo (logo-normal.png): the icon its windows wear in
// the task bar, and the tray's while no new mail waits to be seen.
//
//go:embed logo-normal.png
var Logo []byte

// LogoNewMail is the logo in a seal (logo-new-mail.png): the tray's icon
// from new mail coming until the window is looked at.
//
//go:embed logo-new-mail.png
var LogoNewMail []byte
