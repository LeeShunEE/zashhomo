//go:build !windows

package svc

import "time"

// platformState uses kardianos's Status() on non-Windows systems, where the
// status query does not require elevated access.
func platformState() State { return genericState() }

// waitUninstalled is a no-op outside Windows: systemd and launchd drop the unit
// synchronously, so there is no deletion-pending window to wait out.
func waitUninstalled(time.Duration) error { return nil }
