// Package daemon wires the kernel supervisor, web panel, and subscription
// refresher into a single long-running loop.
package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/core"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/subscription"
	"github.com/LeeShunEE/zashhomo/internal/sysproxy"
	"github.com/LeeShunEE/zashhomo/internal/web"
)

// Run starts all components and blocks until ctx is cancelled. It returns after
// components have been shut down.
func Run(ctx context.Context, p *paths.Paths, cfg *config.Config) error {
	logger := newLogger(p)
	logger.Printf("zashhomo daemon starting; data=%s web=%s controller=%s",
		p.Data, cfg.WebAddr, cfg.ControllerAddr)

	// Ensure a mihomo config exists before the kernel starts. When one already
	// exists we don't regenerate it (that would refetch subscriptions), but we do
	// re-apply the persisted TUN setting so a panel toggle survives a restart
	// rather than waiting for the next subscription refresh.
	if _, err := os.Stat(p.MihomoConfig()); err != nil {
		logger.Printf("generating mihomo config.yaml")
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			logger.Printf("generate config failed: %v", err)
		}
	} else if err := subscription.ApplyTun(p, cfg); err != nil {
		logger.Printf("apply persisted tun to config.yaml failed: %v", err)
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

	// When the user opted into system-proxy management, point the OS proxy at the
	// mixed-port while the daemon runs and clear it on shutdown. This is best
	// effort: it acts on the session running the daemon, so on Windows a service
	// running as LocalSystem cannot reach the interactive user's settings — those
	// users should toggle it with `zashhomo system-proxy` instead.
	if cfg.SystemProxy {
		if err := sysproxy.Enable("127.0.0.1", cfg.MixedPort); err != nil {
			logger.Printf("system proxy enable failed: %v", err)
		} else {
			logger.Printf("system proxy enabled (127.0.0.1:%d)", cfg.MixedPort)
		}
		defer func() {
			if err := sysproxy.Disable(); err != nil {
				logger.Printf("system proxy disable failed: %v", err)
			} else {
				logger.Printf("system proxy disabled")
			}
		}()
	}

	// cfgMu guards the mutable parts of cfg (currently cfg.Tun) shared between the
	// tun-sync loop that writes it and the refresh loop that reads it to
	// regenerate config.yaml.
	var cfgMu sync.Mutex

	// Periodic subscription refresh (reload the kernel config).
	wg.Add(1)
	go func() {
		defer wg.Done()
		refreshLoop(ctx, p, cfg, &cfgMu, logger)
	}()

	// Persist TUN toggles made in the panel. The panel patches only the running
	// kernel, so we watch its live config and mirror the tun block into
	// zashhomo.yaml, letting the choice survive a kernel/service restart.
	wg.Add(1)
	go func() {
		defer wg.Done()
		tunSyncLoop(ctx, cfg, &cfgMu, logger)
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
func refreshLoop(ctx context.Context, p *paths.Paths, cfg *config.Config, cfgMu *sync.Mutex, logger *log.Logger) {
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
			cfgMu.Lock()
			err := subscription.GenerateConfig(p, cfg)
			cfgMu.Unlock()
			if err != nil {
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

// tunSyncInterval is how often the tun-sync loop polls the kernel's live config.
const tunSyncInterval = 5 * time.Second

// tunSyncLoop watches the running kernel's live `tun` block and persists any
// change into zashhomo.yaml. The panel toggles TUN by patching the kernel in
// memory only; mirroring it here is what makes the toggle survive a restart
// (config.yaml is regenerated with cfg.Tun on the next start). It only reacts to
// changes from the first value it observes, so an already-persisted setting that
// the kernel came up with is left untouched.
func tunSyncLoop(ctx context.Context, cfg *config.Config, cfgMu *sync.Mutex, logger *log.Logger) {
	ticker := time.NewTicker(tunSyncInterval)
	defer ticker.Stop()
	var baseline string // canonical JSON of the last observed tun; "" until first seen
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tun, err := subscription.FetchTun(ctx, cfg)
			if err != nil {
				continue // kernel may still be starting; try again next tick
			}
			cur := canonicalJSON(tun)
			if baseline == "" {
				baseline = cur // establish a baseline without overriding persisted intent
				continue
			}
			if cur == baseline {
				continue
			}
			baseline = cur
			cfgMu.Lock()
			cfg.Tun = tun
			err = cfg.Save()
			cfgMu.Unlock()
			if err != nil {
				logger.Printf("tun sync: save config: %v", err)
				continue
			}
			logger.Printf("tun setting synced from panel (enable=%v)", tun["enable"])
		}
	}
}

// canonicalJSON renders v as sorted-key JSON so two tun blocks can be compared
// for equality regardless of map iteration order.
func canonicalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// newLogger writes to both stderr and a rotating-ish log file in the data dir.
func newLogger(p *paths.Paths) *log.Logger {
	var w io.Writer = os.Stderr
	if f, err := os.OpenFile(p.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		w = io.MultiWriter(os.Stderr, f)
	}
	return log.New(w, "", log.LstdFlags)
}
