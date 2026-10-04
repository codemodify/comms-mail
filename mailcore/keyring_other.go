//go:build !linux && !darwin && !windows

package mailcore

import "errors"

var errNoKeyring = errors.New("comms-mail has no keyring support on this system")

func keyringName() string                          { return "the system keyring" }
func keyringBackend() string                       { return "" }
func keyringReady() error                          { return errNoKeyring }
func keyringGet(name string) (string, bool, error) { return "", false, errNoKeyring }
func keyringSet(name, value string) error          { return errNoKeyring }
func keyringDelete(name string) error              { return errNoKeyring }
func keyringNames() ([]string, error)              { return nil, errNoKeyring }
func keyringUnlock() error                         { return errNoKeyring }
