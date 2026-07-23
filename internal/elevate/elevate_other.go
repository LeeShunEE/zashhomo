//go:build !windows

package elevate

import (
	"errors"
	"os"
)

// ErrNeedsElevation signals that the operation requires elevated privileges.
var ErrNeedsElevation = errors.New("operation requires elevated privileges")

// IsAdmin returns true if the current process runs as root (uid 0).
// On Unix systems, this checks the effective UID to determine if the process
// has the necessary privileges for system-level service installation.
func IsAdmin() bool {
	return os.Geteuid() == 0
}

// RunElevated returns an error indicating manual elevation is needed.
// Unlike Windows, Unix systems don't have a programmatic UAC equivalent;
// users must re-run with sudo.
func RunElevated(args []string) error {
	return ErrNeedsElevation
}
