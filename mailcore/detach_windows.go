//go:build windows

package mailcore

import "os/exec"

func detach(*exec.Cmd) {}
