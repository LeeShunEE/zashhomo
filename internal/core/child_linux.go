//go:build linux

package core

import (
	"os/exec"
	"syscall"
)

// prepareChild puts the kernel in its own process group and asks the kernel to
// deliver SIGKILL to the child if this (parent) process dies, so a crashed or
// killed supervisor never leaves an orphaned mihomo behind.
func prepareChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}

// trackChild is a no-op on Linux; Pdeathsig handles orphan prevention.
func trackChild(*exec.Cmd) error { return nil }
