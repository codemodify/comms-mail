//go:build !windows

package mailcore

import (
	"os/exec"
	"syscall"
)

// detach puts cmd in a session of its own, so it outlives this process.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
