//go:build !windows

package elevate

import (
	"os"
	"testing"
)

func TestIsAdmin(t *testing.T) {
	// This test verifies the logic, but the result depends on how the test is run
	expected := os.Geteuid() == 0
	if got := IsAdmin(); got != expected {
		t.Errorf("IsAdmin() = %v, want %v", got, expected)
	}
}

func TestRunElevated(t *testing.T) {
	err := RunElevated([]string{"test"})
	if err != ErrNeedsElevation {
		t.Errorf("RunElevated() error = %v, want %v", err, ErrNeedsElevation)
	}
}

func TestErrNeedsElevation(t *testing.T) {
	// Verify the error is defined and can be used
	if ErrNeedsElevation == nil {
		t.Error("ErrNeedsElevation should not be nil")
	}

	// Verify it's a non-nil error
	if ErrNeedsElevation.Error() == "" {
		t.Error("ErrNeedsElevation should have a non-empty error message")
	}
}
