//go:build !windows

package selfinstall

import "os"

// scheduleSelfDelete removes path immediately. Unix allows unlinking a running
// executable (the inode lives until the process exits), so no deferral is
// needed. A missing file is treated as success.
func scheduleSelfDelete(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
