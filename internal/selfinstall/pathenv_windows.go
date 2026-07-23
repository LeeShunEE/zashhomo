//go:build windows

package selfinstall

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// broadcastEnvChange notifies running processes (e.g. Explorer) that the
// environment changed, so freshly launched terminals pick up the new PATH
// without a re-login.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	msg, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	proc.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(msg)),
		uintptr(smtoAbortIfHung),
		5000,
		0,
	)
}

// ensureOnPath adds dir to the current user's PATH (HKCU\Environment) if absent.
// The change affects newly launched terminals; the current shell must be
// reopened to see it.
func ensureOnPath(dir string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	// Path may be REG_SZ or REG_EXPAND_SZ; read as string either way.
	cur, valType, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return "", err
	}

	if pathContains(cur, dir) {
		return "", nil // already present
	}

	updated := dir
	if cur != "" {
		updated = strings.TrimRight(cur, ";") + ";" + dir
	}
	// Preserve expandable type when the existing value used it.
	if valType == registry.EXPAND_SZ {
		if err := k.SetExpandStringValue("Path", updated); err != nil {
			return "", err
		}
	} else {
		if err := k.SetStringValue("Path", updated); err != nil {
			return "", err
		}
	}
	broadcastEnvChange()
	return "added to your PATH — open a new terminal for `zashhomo` to be found", nil
}

// removeFromPath drops dir from the current user's PATH (HKCU\Environment) if
// present. It is the inverse of ensureOnPath and is best-effort: a missing Path
// value or an absent entry is treated as success.
func removeFromPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	cur, valType, err := k.GetStringValue("Path")
	if err == registry.ErrNotExist {
		return nil
	}
	if err != nil {
		return err
	}
	if !pathContains(cur, dir) {
		return nil // nothing to remove
	}

	target := strings.TrimRight(strings.ToLower(dir), `\`)
	var keep []string
	for _, p := range strings.Split(cur, ";") {
		if strings.TrimRight(strings.ToLower(strings.TrimSpace(p)), `\`) == target {
			continue
		}
		keep = append(keep, p)
	}
	updated := strings.Join(keep, ";")
	if valType == registry.EXPAND_SZ {
		if err := k.SetExpandStringValue("Path", updated); err != nil {
			return err
		}
	} else {
		if err := k.SetStringValue("Path", updated); err != nil {
			return err
		}
	}
	broadcastEnvChange()
	return nil
}

// pathContains reports whether pathList (';'-separated) already has dir.
func pathContains(pathList, dir string) bool {
	dir = strings.TrimRight(strings.ToLower(dir), `\`)
	for _, p := range strings.Split(pathList, ";") {
		if strings.TrimRight(strings.ToLower(strings.TrimSpace(p)), `\`) == dir {
			return true
		}
	}
	return false
}
