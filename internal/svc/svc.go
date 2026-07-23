// Package svc wraps kardianos/service to install, control, and run zashhomo as
// a native OS service (systemd / launchd / Windows service).
package svc

import (
	"context"
	"fmt"

	"github.com/kardianos/service"
)

const (
	serviceName        = "zashhomo"
	serviceDisplayName = "zashhomo"
	serviceDescription = "Lightweight mihomo supervisor and manager with a built-in zashboard panel."
)

// RunFunc is the daemon body; it must block until ctx is cancelled.
type RunFunc func(ctx context.Context) error

// program adapts a RunFunc to service.Interface.
type program struct {
	run    RunFunc
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (p *program) Start(s service.Service) error {
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		if err := p.run(p.ctx); err != nil {
			if l, lerr := s.Logger(nil); lerr == nil {
				l.Error(err)
			}
		}
	}()
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	return nil
}

// config builds the service definition. The service invokes "zashhomo run".
func config() *service.Config {
	return &service.Config{
		Name:        serviceName,
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		Arguments:   []string{"run"},
	}
}

// newService constructs the service around run (run may be nil for control-only).
func newService(run RunFunc) (service.Service, *program, error) {
	prog := &program{run: run}
	s, err := service.New(prog, config())
	if err != nil {
		return nil, nil, err
	}
	return s, prog, nil
}

// Run executes the daemon under service management. When launched interactively
// (e.g. `zashhomo run` in a terminal) it runs in the foreground; under the
// service manager it integrates with start/stop control.
func Run(run RunFunc) error {
	s, _, err := newService(run)
	if err != nil {
		return err
	}
	return s.Run()
}

// Control performs a service lifecycle action: install, uninstall, start, stop,
// restart. "status" is handled by Status.
func Control(action string) error {
	s, _, err := newService(nil)
	if err != nil {
		return err
	}
	return service.Control(s, action)
}

// Status returns a human-readable service status string.
func Status() (string, error) {
	s, _, err := newService(nil)
	if err != nil {
		return "", err
	}
	st, err := s.Status()
	if err != nil {
		return "", err
	}
	switch st {
	case service.StatusRunning:
		return "running", nil
	case service.StatusStopped:
		return "stopped", nil
	default:
		return "unknown", nil
	}
}

// Platform reports the detected service system (e.g. systemd, launchd).
func Platform() string {
	return service.Platform()
}

// Install registers and starts the service.
func Install() error {
	if err := Control("install"); err != nil {
		return fmt.Errorf("install service: %w", err)
	}
	if err := Control("start"); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// Uninstall stops (best effort) and removes the service.
func Uninstall() error {
	_ = Control("stop")
	if err := Control("uninstall"); err != nil {
		return fmt.Errorf("uninstall service: %w", err)
	}
	return nil
}
