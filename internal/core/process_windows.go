//go:build windows

package core

// killOrphanKernels on Windows is a no-op. Windows job objects ensure
// child processes are terminated when the parent exits.
func killOrphanKernels(binPath string) error {
	return nil
}
