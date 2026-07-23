//go:build !windows && !linux && !darwin

package sysproxy

import "fmt"

// errUnsupported is returned on platforms without a system-proxy integration.
var errUnsupported = fmt.Errorf("sysproxy: system proxy management is not supported on this platform")

func enable(string, int) error { return errUnsupported }

func disable() error { return errUnsupported }

func get() (State, error) { return State{}, errUnsupported }
