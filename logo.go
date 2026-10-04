// Package commsmail holds what the comms-mail programs share that is not
// code: its logo.
package commsmail

import _ "embed"

// Logo is comms-mail's logo (logo.png): the icon its windows wear in the
// task bar and the one in the tray.
//
//go:embed logo.png
var Logo []byte
