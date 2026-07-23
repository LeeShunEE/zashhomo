package selfinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyExe(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	want := []byte("zashhomo-binary-bytes")
	if err := os.WriteFile(src, want, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "zashhomo")

	if err := copyExe(src, dst); err != nil {
		t.Fatalf("copyExe: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}
}

// TestCopyExeReplacesExisting covers the path where the destination already
// exists (the rename-aside fallback must not corrupt the result).
func TestCopyExeReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "zashhomo")
	if err := os.WriteFile(src, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyExe(src, dst); err != nil {
		t.Fatalf("copyExe: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "NEW" {
		t.Fatalf("expected NEW, got %q", got)
	}
}

func TestEnsureInstalledCopiesAndReportsPath(t *testing.T) {
	installDir := t.TempDir()
	t.Setenv("ZASHHOMO_BIN", installDir)

	res, err := EnsureInstalled()
	if err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}
	if filepath.Dir(res.Path) != filepath.Clean(installDir) {
		t.Fatalf("installed to %s, want under %s", res.Path, installDir)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("installed binary missing: %v", err)
	}
	if !res.Copied {
		t.Fatalf("expected Copied=true on fresh install")
	}

	// Cleanup: on Windows, EnsureInstalled adds installDir to the user PATH in
	// the registry; remove it so the test leaves no trace.
	cleanupPathEntry(t, installDir)
}
