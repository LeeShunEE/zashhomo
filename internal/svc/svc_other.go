//go:build !windows

package svc

// platformState uses kardianos's Status() on non-Windows systems, where the
// status query does not require elevated access.
func platformState() State { return genericState() }
