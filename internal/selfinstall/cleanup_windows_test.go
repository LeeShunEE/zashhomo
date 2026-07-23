//go:build windows

package selfinstall

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// cleanupPathEntry removes dir from the user's PATH registry value so the test
// leaves the environment as it found it.
func cleanupPathEntry(t *testing.T, dir string) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Logf("cleanup: open key: %v", err)
		return
	}
	defer k.Close()
	cur, typ, err := k.GetStringValue("Path")
	if err != nil {
		return
	}
	target := strings.TrimRight(strings.ToLower(dir), `\`)
	var keep []string
	for _, p := range strings.Split(cur, ";") {
		if strings.TrimRight(strings.ToLower(strings.TrimSpace(p)), `\`) == target {
			continue
		}
		keep = append(keep, p)
	}
	newVal := strings.Join(keep, ";")
	if typ == registry.EXPAND_SZ {
		_ = k.SetExpandStringValue("Path", newVal)
	} else {
		_ = k.SetStringValue("Path", newVal)
	}
}
