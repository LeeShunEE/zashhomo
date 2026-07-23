//go:build windows

package sysproxy

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// internetSettingsPath is the per-user WinINET configuration key. Writing here
// needs no elevation because it lives under HKEY_CURRENT_USER.
const internetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// INTERNET_OPTION_* selectors for InternetSetOptionW. Broadcasting both makes
// already-running apps (browsers, WinINET clients) pick up the change without a
// restart.
const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

var (
	modwininet            = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOption = modwininet.NewProc("InternetSetOptionW")
)

// enable writes the WinINET proxy keys and notifies the system.
func enable(host string, port int) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("sysproxy: open registry: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue("ProxyServer", serverString(host, port)); err != nil {
		return fmt.Errorf("sysproxy: set ProxyServer: %w", err)
	}
	// WinINET uses ';'-separated bypass entries; <local> excludes intranet hosts.
	bypass := strings.Join(append(append([]string{}, defaultBypass...), "<local>"), ";")
	if err := key.SetStringValue("ProxyOverride", bypass); err != nil {
		return fmt.Errorf("sysproxy: set ProxyOverride: %w", err)
	}
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("sysproxy: set ProxyEnable: %w", err)
	}
	return notifyChange()
}

// disable clears the ProxyEnable flag, leaving the server value in place so the
// user's prior endpoint is remembered by the OS proxy UI.
func disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("sysproxy: open registry: %w", err)
	}
	defer key.Close()

	if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
		return fmt.Errorf("sysproxy: clear ProxyEnable: %w", err)
	}
	return notifyChange()
}

// get reads the current WinINET proxy state.
func get() (State, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return State{}, fmt.Errorf("sysproxy: open registry: %w", err)
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil && err != registry.ErrNotExist {
		return State{}, fmt.Errorf("sysproxy: read ProxyEnable: %w", err)
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil && err != registry.ErrNotExist {
		return State{}, fmt.Errorf("sysproxy: read ProxyServer: %w", err)
	}
	st := State{Enabled: enabled == 1}
	if st.Enabled {
		st.Server = server
	}
	return st, nil
}

// notifyChange broadcasts that the WinINET settings changed so live processes
// reload them. Failures here are non-fatal: the registry is already updated and
// the settings apply on the next WinINET refresh.
func notifyChange() error {
	procInternetSetOption.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	procInternetSetOption.Call(0, uintptr(internetOptionRefresh), 0, 0)
	return nil
}
