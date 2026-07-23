// Command zashhomo is a lightweight cross-platform supervisor/manager for the
// mihomo proxy kernel with a built-in zashboard web panel.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/zashhomo/zashhomo/internal/config"
	"github.com/zashhomo/zashhomo/internal/core"
	"github.com/zashhomo/zashhomo/internal/daemon"
	"github.com/zashhomo/zashhomo/internal/ghrelease"
	"github.com/zashhomo/zashhomo/internal/panel"
	"github.com/zashhomo/zashhomo/internal/paths"
	"github.com/zashhomo/zashhomo/internal/subscription"
	"github.com/zashhomo/zashhomo/internal/svc"
)

// version is overridden at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

// selfRepo is this project's GitHub repo, used by `update --self`.
// NOTE: replace the owner to match your fork before publishing.
const selfRepo = "zashhomo/zashhomo"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "install":
		err = cmdInstall(args)
	case "run":
		err = cmdRun(args)
	case "start", "stop", "restart":
		err = svc.Control(cmd)
	case "status":
		err = cmdStatus()
	case "update":
		err = cmdUpdate(args)
	case "sub":
		err = cmdSub(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "version", "-v", "--version":
		fmt.Printf("zashhomo %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `zashhomo — lightweight mihomo supervisor + zashboard panel

Usage:
  zashhomo install            Download kernel+panel, write config, register & start service
  zashhomo run                Run the daemon in the foreground (used by the service)
  zashhomo start|stop|restart Control the installed service
  zashhomo status             Show service status
  zashhomo update [flags]     Update components (--core --ui --self --all)
  zashhomo sub add <url>      Add a subscription
  zashhomo sub update         Regenerate config and hot-reload the kernel
  zashhomo uninstall [--purge] Stop & remove the service (and files with --purge)
  zashhomo version            Print version
`)
}

// loadOrInit loads the config, creating and persisting defaults on first use.
func loadOrInit(p *paths.Paths) (*config.Config, error) {
	if err := p.EnsureDirs(); err != nil {
		return nil, err
	}
	cfg, err := config.Load(p.Config)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(p.Config); statErr != nil {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func cmdInstall(_ []string) error {
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}

	fmt.Println("• Installing mihomo kernel…")
	tag, updated, err := core.Install(p, cfg.CoreVersion)
	if err != nil {
		return err
	}
	cfg.CoreVersion = tag
	fmt.Printf("  kernel %s (%s)\n", tag, statusWord(updated))

	fmt.Println("• Installing zashboard panel…")
	utag, uupdated, err := panel.Install(p, cfg.UIVersion)
	if err != nil {
		return err
	}
	cfg.UIVersion = utag
	fmt.Printf("  panel %s (%s)\n", utag, statusWord(uupdated))

	fmt.Println("• Writing mihomo config…")
	if err := subscription.GenerateConfig(p, cfg); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Printf("• Registering service (%s)…\n", svc.Platform())
	if err := svc.Install(); err != nil {
		return err
	}

	fmt.Printf("\n✓ Installed. Open the panel at http://%s\n", cfg.WebAddr)
	if len(cfg.Subscriptions) == 0 {
		fmt.Println("  Add a subscription:  zashhomo sub add <url>")
	}
	return nil
}

func cmdRun(_ []string) error {
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}
	// Run under the service manager; interactive runs stay in the foreground and
	// honour Ctrl-C via the signal handler below.
	return svc.Run(func(ctx context.Context) error {
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		return daemon.Run(ctx, p, cfg)
	})
}

func cmdStatus() error {
	st, err := svc.Status()
	if err != nil {
		return err
	}
	p := paths.New()
	cfg, _ := config.Load(p.Config)
	fmt.Printf("service: %s (%s)\n", st, svc.Platform())
	if cfg != nil {
		fmt.Printf("panel:   http://%s\n", cfg.WebAddr)
		fmt.Printf("kernel:  %s\n", orDash(cfg.CoreVersion))
		fmt.Printf("panelv:  %s\n", orDash(cfg.UIVersion))
		fmt.Printf("subs:    %d\n", len(cfg.Subscriptions))
	}
	return nil
}

func cmdUpdate(args []string) error {
	var doCore, doUI, doSelf, doAll bool
	for _, a := range args {
		switch a {
		case "--core":
			doCore = true
		case "--ui":
			doUI = true
		case "--self":
			doSelf = true
		case "--all":
			doAll = true
		default:
			return fmt.Errorf("update: unknown flag %q", a)
		}
	}
	if !doCore && !doUI && !doSelf && !doAll {
		doAll = true
	}

	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}

	if doCore || doAll {
		tag, updated, err := core.Install(p, cfg.CoreVersion)
		if err != nil {
			return err
		}
		cfg.CoreVersion = tag
		fmt.Printf("kernel %s (%s)\n", tag, statusWord(updated))
	}
	if doUI || doAll {
		tag, updated, err := panel.Install(p, cfg.UIVersion)
		if err != nil {
			return err
		}
		cfg.UIVersion = tag
		fmt.Printf("panel %s (%s)\n", tag, statusWord(updated))
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if doSelf || doAll {
		if err := selfUpdate(); err != nil {
			// When self-update was explicitly requested, treat failure as fatal.
			// Under --all it is best-effort: don't undo a successful core/ui update
			// just because no self release exists yet or GitHub is unreachable.
			if doSelf {
				return err
			}
			fmt.Fprintf(os.Stderr, "warning: self-update skipped: %v\n", err)
		}
	}
	if (doCore || doUI || doAll) && !doSelf {
		fmt.Println("restart the service to apply:  zashhomo restart")
	}
	return nil
}

// selfUpdate replaces the running zashhomo binary with the latest release.
func selfUpdate() error {
	rel, err := ghrelease.Latest(selfRepo)
	if err != nil {
		return fmt.Errorf("self-update: %w", err)
	}
	name := selfAssetName(rel.TagName)
	asset := rel.FindByExactName(name)
	if asset == nil {
		// Fall back: match os/arch without the tag.
		asset = rel.FindBest(func(n string) int {
			ln := strings.ToLower(n)
			if strings.Contains(ln, runtime.GOOS) && strings.Contains(ln, runtime.GOARCH) {
				return 1
			}
			return 0
		})
	}
	if asset == nil {
		return fmt.Errorf("self-update: no asset for %s/%s in %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	newPath := exe + ".new"
	if err := ghrelease.Download(asset.URL, newPath); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(newPath, 0o755)
	}
	// Windows cannot overwrite a running exe; rename the old out of the way first.
	oldPath := exe + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("self-update: swap out old binary: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		// Roll back.
		os.Rename(oldPath, exe)
		return fmt.Errorf("self-update: install new binary: %w", err)
	}
	_ = os.Remove(oldPath)
	fmt.Printf("zashhomo updated to %s (restart the service to apply)\n", rel.TagName)
	return nil
}

// selfAssetName is the release artifact name for this platform.
func selfAssetName(_ string) string {
	name := fmt.Sprintf("zashhomo-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func cmdSub(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sub: expected 'add <url>' or 'update'")
	}
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}

	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("sub add: missing <url>")
		}
		url := args[1]
		name := ""
		if len(args) >= 3 {
			name = args[2]
		}
		cfg.AddSubscription(name, url)
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("added subscription (%d total)\n", len(cfg.Subscriptions))
		// Try a live reload; ignore errors when the service isn't running.
		if err := subscription.Reload(context.Background(), cfg, p.MihomoConfig()); err == nil {
			fmt.Println("kernel reloaded")
		} else {
			fmt.Println("run `zashhomo restart` (or start the service) to apply")
		}
		return nil

	case "update":
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			return err
		}
		if err := subscription.Reload(context.Background(), cfg, p.MihomoConfig()); err != nil {
			return fmt.Errorf("reload (is the service running?): %w", err)
		}
		fmt.Println("subscriptions reloaded")
		return nil

	default:
		return fmt.Errorf("sub: unknown subcommand %q", args[0])
	}
}

func cmdUninstall(args []string) error {
	purge := false
	for _, a := range args {
		if a == "--purge" {
			purge = true
		}
	}
	if err := svc.Uninstall(); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	fmt.Println("service removed")
	if purge {
		p := paths.New()
		for _, dir := range []string{p.Data, filepath.Dir(p.Config)} {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Fprintf(os.Stderr, "warning: remove %s: %v\n", dir, err)
			}
		}
		fmt.Println("data and config removed")
	} else {
		fmt.Println("data and config kept (use --purge to remove)")
	}
	return nil
}

func statusWord(updated bool) string {
	if updated {
		return "installed"
	}
	return "up to date"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
