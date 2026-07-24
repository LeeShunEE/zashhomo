//go:build windows

package svc

import (
	"errors"
	"fmt"
	"time"

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

// waitUninstalled polls the SCM until the service record is really gone.
// Windows' DeleteService only *marks* a service for deletion: the record — and
// with it a successful OpenService, which is how the service library tests for
// existence — survives until the last handle to it is closed and the service
// process has exited. Reinstalling in that window fails with "service already
// exists", so removal waits here for the SCM to catch up.
func waitUninstalled(timeout time.Duration) error {
	name, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return nil // cannot check; let the caller proceed
	}
	deadline := time.Now().Add(timeout)
	for {
		if serviceGone(name) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service %s is still marked for deletion after %s; "+
				"close services.msc or Task Manager's Services tab (an open handle "+
				"blocks removal) and try again", serviceName, timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// serviceGone reports whether the SCM no longer has a record for the service.
// An unreachable SCM counts as gone so a failed query cannot stall a reinstall.
func serviceGone(name *uint16) bool {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return true
	}
	defer windows.CloseServiceHandle(m)

	h, err := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST)
	}
	windows.CloseServiceHandle(h)
	return false
}
