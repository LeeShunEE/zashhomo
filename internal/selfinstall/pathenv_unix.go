//go:build !windows

package selfinstall

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ensureOnPath checks whether dir is already on PATH. On Unix we do not edit
// shell profiles automatically (too many shells/rc files); instead we return a
// hint when the directory is not reachable. Standard system dirs like
// /usr/local/bin are normally already on PATH, so this is usually silent.
func ensureOnPath(dir string) (string, error) {
	for _, p := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if p == "" {
			continue
		}
		if filepath.Clean(p) == filepath.Clean(dir) {
			return "", nil
		}
	}
	return fmt.Sprintf("add %s to your PATH, e.g.  export PATH=\"%s:$PATH\"", dir, dir), nil
}
