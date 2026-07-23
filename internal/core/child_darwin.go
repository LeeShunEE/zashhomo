//go:build darwin

package core

import (
	"os/exec"
	"syscall"
)

// prepareChild puts the kernel in its own process group. macOS has no
// Pdeathsig equivalent, so crash-time orphan prevention relies on launchd
// managing the zashhomo service; graceful stops kill the child via the context.
func prepareChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// trackChild is a no-op on macOS.
func trackChild(*exec.Cmd) error { return nil }
