package panel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	// Non-existent file
	if fileExists("/nonexistent/path/file.txt") {
		t.Error("expected false for nonexistent file")
	}

	// Existing file
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(file) {
		t.Error("expected true for existing file")
	}

	// Directory should return false
	if fileExists(dir) {
		t.Error("expected false for directory")
	}
}