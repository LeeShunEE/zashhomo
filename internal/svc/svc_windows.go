//go:build windows

package svc

import (
	"errors"

	"golang.org/x/sys/windows"
)

// platformState reports the service state by opening the service with only
// SERVICE_QUERY_STATUS access. kardianos's Status() instead opens the handle
// with SERVICE_START|SERVICE_STOP as well, which the SCM denies to a
// non-elevated process — so its query errors and a running service is
// misreported as stopped. Querying with query-only access succeeds without
// elevation, so the interactive menu sees the true run state.
func platformState() State {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		// Can't reach the SCM at all; fall back to kardianos's view.
		return genericState()
	}
	defer windows.CloseServiceHandle(m)

	name, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return genericState()
	}
	h, err := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return State{}
		}
		// Installed but unreadable: keep control actions available.
		return State{Installed: true}
	}
	defer windows.CloseServiceHandle(h)

	var status windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &status); err != nil {
		return State{Installed: true}
	}
	running := status.CurrentState == windows.SERVICE_RUNNING ||
		status.CurrentState == windows.SERVICE_START_PENDING
	return State{Installed: true, Running: running}
}
