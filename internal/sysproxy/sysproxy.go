// Package sysproxy sets, clears, and queries the operating system's HTTP/HTTPS
// (and, where supported, SOCKS) proxy so zashhomo can offer one-click system
// proxy management pointed at the mihomo mixed-port.
//
// The concrete behaviour is platform specific:
//   - Windows: the per-user WinINET registry keys under
//     HKCU\…\Internet Settings, followed by a settings-changed broadcast.
//   - macOS: the `networksetup` tool applied to every enabled network service.
//   - Linux: the GNOME `gsettings` proxy keys (best effort; desktop dependent).
//
// All operations act on the current user's session, so the enable/disable
// commands are meant to run as the interactive user, not the elevated service.
package sysproxy

import (
	"fmt"
	"net"
	"strconv"
)

// State describes the system proxy configuration as zashhomo can observe it.
type State struct {
	// Enabled reports whether a manual system proxy is currently active.
	Enabled bool
	// Server is the proxy endpoint (host:port) when Enabled; empty otherwise.
	Server string
}

// defaultBypass lists hosts that should bypass the proxy. Platform layers format
// this to their own syntax (Windows ';'-separated, GNOME string list, …).
var defaultBypass = []string{"localhost", "127.0.0.1", "::1"}

// Enable turns on the system proxy, pointing it at host:port. A blank host
// defaults to loopback, matching how the mihomo mixed-port is reached.
func Enable(host string, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("sysproxy: invalid port %d", port)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return enable(host, port)
}

// Disable turns off the system proxy.
func Disable() error { return disable() }

// Get reports the current system proxy state.
func Get() (State, error) { return get() }

// serverString renders host:port for a proxy endpoint.
func serverString(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
