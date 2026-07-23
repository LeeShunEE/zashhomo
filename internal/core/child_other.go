//go:build !windows && !linux && !darwin

package core

import "os/exec"

// prepareChild is a no-op on platforms without a specific implementation.
func prepareChild(*exec.Cmd) {}

// trackChild is a no-op on platforms without a specific implementation.
func trackChild(*exec.Cmd) error { return nil }
