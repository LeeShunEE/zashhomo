// Package selfinstall copies the running zashhomo executable into a stable
// location on PATH so that `zashhomo` works as a global command after install,
// and so the OS service can target a path that will not move.
package selfinstall

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zashhomo/zashhomo/internal/paths"
)

// Result describes what EnsureInstalled did.
type Result struct {
	// Path is the canonical installed executable path.
	Path string
	// Copied is true if the binary was copied into place (false if already there).
	Copied bool
	// PathNote is a non-empty human hint when the install dir is not yet on PATH
	// and the user must take an action (or open a new terminal).
	PathNote string
}

// EnsureInstalled makes the running binary available as `zashhomo` on PATH.
// It is idempotent: running the already-installed binary is a no-op copy.
func EnsureInstalled() (Result, error) {
	dst := paths.SelfExe()

	cur, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	if resolved, err := filepath.EvalSymlinks(cur); err == nil {
		cur = resolved
	}

	res := Result{Path: dst}

	if !sameFile(cur, dst) {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return Result{}, err
		}
		if err := copyExe(cur, dst); err != nil {
			return Result{}, fmt.Errorf("install binary to %s: %w", dst, err)
		}
		res.Copied = true
	}

	note, err := ensureOnPath(filepath.Dir(dst))
	if err != nil {
		// PATH registration is best-effort; surface as a note rather than fail.
		note = fmt.Sprintf("could not update PATH automatically: %v", err)
	}
	res.PathNote = note
	return res, nil
}

// Uninstall removes the installed executable (best-effort) unless it is the
// currently running binary, which the OS may keep locked.
func Uninstall() error {
	dst := paths.SelfExe()
	cur, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(cur); err == nil {
		cur = resolved
	}
	if sameFile(cur, dst) {
		return fmt.Errorf("skipped removing running binary %s", dst)
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// sameFile reports whether a and b refer to the same on-disk file.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// copyExe copies src to dst atomically. If dst is locked (e.g. a running service
// on Windows), the existing file is renamed aside first.
func copyExe(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmp, 0o755)
	}

	if err := os.Rename(tmp, dst); err != nil {
		// dst may be locked; move it aside and retry.
		old := dst + ".old"
		_ = os.Remove(old)
		if rerr := os.Rename(dst, old); rerr != nil {
			os.Remove(tmp)
			return err
		}
		if rerr := os.Rename(tmp, dst); rerr != nil {
			os.Rename(old, dst) // roll back
			os.Remove(tmp)
			return rerr
		}
		_ = os.Remove(old)
	}
	return nil
}
