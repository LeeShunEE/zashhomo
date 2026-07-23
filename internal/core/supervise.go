package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"
)

// Supervisor runs the mihomo kernel and keeps it alive.
type Supervisor struct {
	BinPath    string
	DataDir    string
	ConfigPath string
	// ControllerAddr is the mihomo external-controller (e.g. 127.0.0.1:9090).
	ControllerAddr string
	Secret         string
	// Logf logs supervisor events; if nil, logging is discarded.
	Logf func(format string, args ...any)
	// Stdout/Stderr receive the kernel's output; if nil, output is discarded.
	Stdout io.Writer
	Stderr io.Writer
}

func (s *Supervisor) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Run supervises mihomo until ctx is cancelled. It restarts the kernel on
// crash or repeated health-check failure with exponential backoff (1s..30s).
func (s *Supervisor) Run(ctx context.Context) error {
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		start := time.Now()
		s.logf("starting mihomo: %s -d %s -f %s", s.BinPath, s.DataDir, s.ConfigPath)
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		// A process that stayed up a while resets the backoff.
		if time.Since(start) > maxBackoff {
			backoff = time.Second
		}
		s.logf("mihomo exited (%v); restarting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// runOnce starts one mihomo process and blocks until it exits. A health monitor
// cancels the process if it becomes unresponsive.
func (s *Supervisor) runOnce(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.BinPath, "-d", s.DataDir, "-f", s.ConfigPath)
	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		s.monitor(ctx, cancel)
	}()

	err := cmd.Wait()
	cancel()
	<-monitorDone
	return err
}

// monitor polls the kernel health endpoint and cancels via kill on repeated
// failure so Run can restart it.
func (s *Supervisor) monitor(ctx context.Context, kill context.CancelFunc) {
	// Grace period for the kernel to bind its controller.
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.healthy(ctx) {
				fails = 0
				continue
			}
			fails++
			s.logf("health check failed (%d/3)", fails)
			if fails >= 3 {
				s.logf("kernel unresponsive; killing for restart")
				kill()
				return
			}
		}
	}
}

// healthy reports whether GET /version on the controller succeeds.
func (s *Supervisor) healthy(ctx context.Context) bool {
	url := "http://" + s.ControllerAddr + "/version"
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	if s.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+s.Secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode == http.StatusOK
}
