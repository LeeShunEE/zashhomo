//go:build !windows

package selfinstall

import "testing"

// cleanupPathEntry is a no-op on Unix, where ensureOnPath does not mutate state.
func cleanupPathEntry(t *testing.T, dir string) { t.Helper() }
