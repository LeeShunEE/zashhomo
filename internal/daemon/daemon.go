// Package daemon wires the kernel supervisor, web panel, and subscription
// refresher into a single long-running loop.
package daemon

import (
	"context"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/core"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/subscription"
	"github.com/LeeShunEE/zashhomo/internal/web"
)

// Run starts all components and blocks until ctx is cancelled. It returns after
// components have been shut down.
func Run(ctx context.Context, p *paths.Paths, cfg *config.Config) error {
	logger := newLogger(p)
	logger.Printf("zashhomo daemon starting; data=%s web=%s controller=%s",
		p.Data, cfg.WebAddr, cfg.ControllerAddr)

	// Ensure a mihomo config exists before the kernel starts.
	if _, err := os.Stat(p.MihomoConfig()); err != nil {
		logger.Printf("generating mihomo config.yaml")
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			logger.Printf("generate config failed: %v", err)
		}
	}

	var wg sync.WaitGroup

	// Kernel supervisor.
	sup := &core.Supervisor{
		BinPath:        p.MihomoBin(),
		DataDir:        p.Data,
		ConfigPath:     p.MihomoConfig(),
		ControllerAddr: cfg.ControllerAddr,
		Secret:         cfg.Secret,
		Logf:           logger.Printf,
		Stdout:         logger.Writer(),
		Stderr:         logger.Writer(),
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := sup.Run(ctx); err != nil {
			logger.Printf("supervisor stopped: %v", err)
		}
	}()

	// Web panel + API proxy.
	srv := &web.Server{
		Addr:           cfg.WebAddr,
		UIDir:          p.UI,
		ControllerAddr: cfg.ControllerAddr,
		Secret:         cfg.Secret,
	}
	webErr := srv.Start()
	logger.Printf("panel available at http://%s", cfg.WebAddr)

	// Periodic subscription refresh (reload the kernel config).
	wg.Add(1)
	go func() {
		defer wg.Done()
		refreshLoop(ctx, p, cfg, logger)
	}()

	// Wait for shutdown or a fatal web error.
	select {
	case <-ctx.Done():
	case err := <-webErr:
		if err != nil {
			logger.Printf("web server error: %v", err)
		}
	}

	logger.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Printf("web shutdown: %v", err)
	}
	wg.Wait()
	logger.Printf("daemon stopped")
	return nil
}

// refreshLoop reloads the kernel config on cfg.RefreshInterval so proxy
// providers pick up subscription changes.
func refreshLoop(ctx context.Context, p *paths.Paths, cfg *config.Config, logger *log.Logger) {
	if len(cfg.Subscriptions) == 0 {
		return
	}
	interval := cfg.RefreshInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := subscription.GenerateConfig(p, cfg); err != nil {
				logger.Printf("refresh: regenerate config: %v", err)
				continue
			}
			if err := subscription.Reload(ctx, cfg, p.MihomoConfig()); err != nil {
				logger.Printf("refresh: reload: %v", err)
				continue
			}
			logger.Printf("subscriptions refreshed")
		}
	}
}

// newLogger writes to both stderr and a rotating-ish log file in the data dir.
func newLogger(p *paths.Paths) *log.Logger {
	var w io.Writer = os.Stderr
	if f, err := os.OpenFile(p.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		w = io.MultiWriter(os.Stderr, f)
	}
	return log.New(w, "", log.LstdFlags)
}
