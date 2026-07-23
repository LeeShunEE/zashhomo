// Package archive extracts gzip and zip release assets to disk.
package archive

import (
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// GunzipTo decompresses a single-member gzip file (src) to destPath.
func GunzipTo(src, destPath string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return writeFile(destPath, gz, 0o755)
}

// UnzipMemberTo extracts a single member into destPath. It selects the first
// regular file whose base name contains all of tokens (case-insensitive). This
// tolerates upstream zips that ship the binary under its full asset name
// (e.g. mihomo-windows-amd64-compatible-v1.19.29.exe) rather than a fixed name:
// callers pass tokens like {"mihomo", "windows", ".exe"} to pin it down.
func UnzipMemberTo(src string, tokens []string, destPath string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()

	var match *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if baseContainsAll(filepath.Base(f.Name), tokens) {
			match = f
			break
		}
	}
	if match == nil {
		return fmt.Errorf("archive: no member matching %v in %s", tokens, src)
	}
	rc, err := match.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return writeFile(destPath, rc, 0o755)
}

// baseContainsAll reports whether base contains every token (case-insensitive).
func baseContainsAll(base string, tokens []string) bool {
	lower := strings.ToLower(base)
	for _, t := range tokens {
		if !strings.Contains(lower, strings.ToLower(t)) {
			return false
		}
	}
	return true
}

// UnzipAllTo extracts every file in the zip src into destDir, guarding against
// path traversal. If all entries share a single top-level directory, that
// directory is stripped so contents land directly in destDir.
func UnzipAllTo(src, destDir string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()

	strip := commonTopDir(zr.File)

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	cleanDest := filepath.Clean(destDir)
	for _, f := range zr.File {
		name := f.Name
		if strip != "" {
			name = strings.TrimPrefix(name, strip)
		}
		name = strings.TrimLeft(name, "/")
		if name == "" {
			continue
		}
		target := filepath.Join(cleanDest, filepath.FromSlash(name))
		// Path-traversal guard.
		if target != cleanDest && !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("archive: illegal path %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, 0o644)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// commonTopDir returns the single shared top-level directory prefix (with
// trailing slash) if every entry lives under it, else "".
func commonTopDir(files []*zip.File) string {
	var top string
	for _, f := range files {
		name := strings.TrimLeft(f.Name, "/")
		if name == "" {
			continue
		}
		idx := strings.Index(name, "/")
		if idx < 0 {
			return "" // a top-level file exists; no common dir
		}
		dir := name[:idx]
		if top == "" {
			top = dir
		} else if top != dir {
			return ""
		}
	}
	if top == "" {
		return ""
	}
	return top + "/"
}

// writeFile writes r to path atomically with the given mode.
func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ex-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpName)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return closeErr
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
