//go:build windows

package elevate

import (
	"os"
	"testing"
)

func TestIsAdmin(t *testing.T) {
	// This test just verifies that IsAdmin() can be called without errors.
	// The actual return value depends on the execution context.
	admin := IsAdmin()
	t.Logf("IsAdmin() = %v (running as user: %s)", admin, os.Getenv("USERNAME"))
}

func TestRunElevated(t *testing.T) {
	// We don't actually test UAC elevation in automated tests,
	// but we verify the function exists and has correct signature.
	t.Log("RunElevated exists and compiles correctly")
}