package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStashBinaryNothingInstalled(t *testing.T) {
	stash, err := stashBinary(filepath.Join(t.TempDir(), "mihomo"))
	if err != nil {
		t.Fatalf("stashBinary on a missing binary: %v", err)
	}
	if stash != "" {
		t.Errorf("stashBinary on a missing binary = %q, want \"\"", stash)
	}
}

// Moving the old kernel aside is what frees its name on Windows, where the
// running executable cannot be overwritten. The displaced copy must survive the
// move intact so a failed extraction can put it back.
func TestStashBinaryFreesTheName(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("old kernel"), 0o755); err != nil {
		t.Fatal(err)
	}

	stash, err := stashBinary(bin)
	if err != nil {
		t.Fatalf("stashBinary: %v", err)
	}
	if stash == "" {
		t.Fatal("stashBinary returned no stash path for an existing binary")
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Errorf("the original path still exists after stashing (err = %v)", err)
	}
	got, err := os.ReadFile(stash)
	if err != nil {
		t.Fatalf("read the stashed binary: %v", err)
	}
	if string(got) != "old kernel" {
		t.Errorf("stashed content = %q, want %q", got, "old kernel")
	}
}

// A fixed ".old" name would collide on the second update, because the first
// stash cannot be deleted while the kernel it belongs to is still running.
func TestStashBinaryTwiceDoesNotCollide(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")

	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := stashBinary(bin)
	if err != nil {
		t.Fatalf("first stashBinary: %v", err)
	}

	if err := os.WriteFile(bin, []byte("v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := stashBinary(bin)
	if err != nil {
		t.Fatalf("second stashBinary: %v", err)
	}
	if second == first {
		t.Errorf("both stashes used the same name %q", second)
	}
	// The sweep at the head of the second call clears the first stash, which is
	// deletable here because no process holds it.
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("the earlier stash was not swept (err = %v)", err)
	}
	got, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("read the second stash: %v", err)
	}
	if string(got) != "v2" {
		t.Errorf("second stash content = %q, want %q", got, "v2")
	}
}

// The sweep must leave the installed kernel and unrelated files alone.
func TestSweepStashesOnlyRemovesStashes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	keep := []string{bin, filepath.Join(dir, "mihomo.yaml"), filepath.Join(dir, "other.old-1")}
	drop := []string{bin + ".old-1", bin + ".old-2"}

	for _, f := range append(append([]string{}, keep...), drop...) {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sweepStashes(bin)

	for _, f := range keep {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("sweepStashes removed %s, which it should have kept", filepath.Base(f))
		}
	}
	for _, f := range drop {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("sweepStashes kept %s, which it should have removed", filepath.Base(f))
		}
	}
}
