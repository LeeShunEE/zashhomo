//go:build windows

package core

// killOrphanKernels is a no-op on Windows. The Windows implementation uses
// Job Objects with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, which automatically
// terminates child processes when the parent exits.
func killOrphanKernels(binPath string) error {
	return nil
}
