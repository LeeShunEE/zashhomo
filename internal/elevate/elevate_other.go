//go:build !windows

package elevate

// IsAdmin returns true on non-Windows platforms.
// Service installation on Unix-like systems typically uses sudo or
// runs as root already, so we don't implement auto-elevation.
func IsAdmin() bool {
	return true
}

// RunElevated is a no-op on non-Windows platforms.
// Users should run the install command with appropriate privileges
// (e.g., via sudo).
func RunElevated(args []string) error {
	return nil
}
