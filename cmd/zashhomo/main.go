// Command zashhomo is a lightweight cross-platform supervisor/manager for the
// mihomo proxy kernel with a built-in zashboard web panel.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/core"
	"github.com/LeeShunEE/zashhomo/internal/daemon"
	"github.com/LeeShunEE/zashhomo/internal/elevate"
	"github.com/LeeShunEE/zashhomo/internal/ghrelease"
	"github.com/LeeShunEE/zashhomo/internal/panel"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/selfinstall"
	"github.com/LeeShunEE/zashhomo/internal/subscription"
	"github.com/LeeShunEE/zashhomo/internal/svc"
	"github.com/LeeShunEE/zashhomo/internal/ui"
)

// version is overridden at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

// selfRepo is this project's GitHub repo, used by `update --self`.
const selfRepo = "LeeShunEE/zashhomo"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := dispatch(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// dispatch executes a single command (cmd with args). It does not call os.Exit,
// so the interactive console can reuse it line by line.
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "install":
		return cmdInstall(args)
	case "run":
		return cmdRun(args)
	case "start", "stop", "restart":
		return svc.Control(cmd)
	case "status":
		return cmdStatus()
	case "update":
		return cmdUpdate(args)
	case "sub":
		return cmdSub(args)
	case "uninstall":
		return cmdUninstall(args)
	case "version", "-v", "--version":
		fmt.Printf("zashhomo %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	case "interactive", "-i", "--interactive":
		return cmdInteractive(args)
	default:
		return fmt.Errorf("unknown command: %s (try 'zashhomo help')", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `zashhomo — lightweight mihomo supervisor + zashboard panel

Usage:
  zashhomo install [--mixed-port N] [--web-port N] [--web-addr ADDR]
                              Download kernel+panel, write config, register & start service
  zashhomo run [--mixed-port N] [--web-port N] [--web-addr ADDR]
                              Run the daemon in the foreground (used by the service)
  zashhomo -i | interactive   Interactive management console
  zashhomo start|stop|restart Control the installed service
  zashhomo status             Show service status
  zashhomo update [flags]     Update components (--core --ui --self --all)
  zashhomo sub add <url>      Add a subscription
  zashhomo sub update         Regenerate config and hot-reload the kernel
  zashhomo uninstall [--purge] Stop & remove the service (and files with --purge)
  zashhomo version            Print version

Ports: --mixed-port sets the mihomo proxy port (default 9190);
       --web-port sets the panel port, keeping its host (default 127.0.0.1:9191);
       --web-addr sets the full panel address, e.g. 0.0.0.0:9191 to expose externally.
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

// parseListenFlags parses the optional --mixed-port / --web-port / --web-addr
// flags shared by install and run. A returned port of 0 / addr of "" means
// "keep the configured default".
func parseListenFlags(name string, args []string) (mixedPort, webPort int, webAddr string, err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mp := fs.Int("mixed-port", 0, "mihomo mixed (http+socks) proxy `port` (default 9190)")
	wp := fs.Int("web-port", 0, "panel `port`; keeps the configured host (default 127.0.0.1:9191)")
	wa := fs.String("web-addr", "", "panel listen `address` (host:port); e.g. 0.0.0.0:9191 to expose externally")
	if perr := fs.Parse(args); perr != nil {
		if perr == flag.ErrHelp {
			// -h already printed the flag usage; treat as a clean exit.
			os.Exit(0)
		}
		return 0, 0, "", perr
	}
	return *mp, *wp, *wa, nil
}

// applyWebAddr overrides the panel listen address from the CLI flags: --web-addr
// wins (a full host:port, e.g. 0.0.0.0:9191); otherwise --web-port changes only
// the port and keeps the configured host (loopback by default).
func applyWebAddr(cfg *config.Config, webAddr string, webPort int) {
	if webAddr != "" {
		cfg.WebAddr = webAddr
		return
	}
	if webPort > 0 {
		host, _, err := net.SplitHostPort(cfg.WebAddr)
		if err != nil || host == "" {
			host = "127.0.0.1"
		}
		cfg.WebAddr = fmt.Sprintf("%s:%d", host, webPort)
	}
}

// panelURL returns the panel address with a one-shot login token, for display in
// install/status output. A wildcard listen address is shown as localhost.
func panelURL(cfg *config.Config) string {
	host, port, err := net.SplitHostPort(cfg.WebAddr)
	if err != nil {
		return "http://" + cfg.WebAddr
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/?token=%s", host, port, cfg.Secret)
}

func cmdInstall(args []string) error {
	// Check for administrator privileges on Windows
	if !elevate.IsAdmin() {
		fmt.Fprintln(os.Stderr, "Installing the Windows service requires administrator privileges.")
		fmt.Fprintln(os.Stderr, "Requesting elevation...")

		// Auto-elevate using UAC
		if err := elevate.RunElevated(os.Args[1:]); err != nil {
			return fmt.Errorf("failed to elevate privileges: %w", err)
		}
		// The elevated process will handle the installation and exit.
		// This instance should exit cleanly.
		return nil
	}

	mixedPort, webPort, webAddr, err := parseListenFlags("install", args)
	if err != nil {
		return err
	}
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}
	if mixedPort > 0 {
		cfg.MixedPort = mixedPort
	}
	applyWebAddr(cfg, webAddr, webPort)

	tag, updated, err := core.Install(p, cfg.CoreVersion)
	if err != nil {
		return err
	}
	cfg.CoreVersion = tag
	if !updated {
		fmt.Printf("  kernel %s (up to date)\n", tag)
	}

	utag, uupdated, err := panel.Install(p, cfg.UIVersion)
	if err != nil {
		return err
	}
	cfg.UIVersion = utag
	if !uupdated {
		fmt.Printf("  panel %s (up to date)\n", utag)
	}

	fmt.Println("• Writing mihomo config…")
	if err := subscription.GenerateConfig(p, cfg); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	// Register `zashhomo` as a global CLI and pin the service to a stable path.
	fmt.Println("• Registering global `zashhomo` command…")
	exePath := ""
	res, err := selfinstall.EnsureInstalled()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not install to PATH (%v); using current binary\n", err)
	} else {
		exePath = res.Path
		if res.Copied {
			fmt.Printf("  installed to %s\n", res.Path)
		} else {
			fmt.Printf("  already installed at %s\n", res.Path)
		}
	}

	fmt.Printf("• Registering service (%s)…\n", svc.Platform())
	if err := svc.Install(exePath); err != nil {
		return err
	}

	fmt.Printf("\n✓ Installed. Open the panel at:\n  %s\n", panelURL(cfg))
	if res.PathNote != "" {
		fmt.Printf("  note: %s\n", res.PathNote)
	}
	if len(cfg.Subscriptions) == 0 {
		fmt.Println("  Add a subscription:  zashhomo sub add <url>")
	}
	return nil
}

func cmdRun(args []string) error {
	mixedPort, webPort, webAddr, err := parseListenFlags("run", args)
	if err != nil {
		return err
	}
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}
	if mixedPort > 0 || webPort > 0 || webAddr != "" {
		if mixedPort > 0 {
			cfg.MixedPort = mixedPort
		}
		applyWebAddr(cfg, webAddr, webPort)
		// Persist and regenerate the kernel config so the new ports take effect.
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
	}
	// Run under the service manager; interactive runs stay in the foreground and
	// honour Ctrl-C via the signal handler below.
	return svc.Run(func(ctx context.Context) error {
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		return daemon.Run(ctx, p, cfg)
	})
}

// cmdInteractive runs a read-eval loop over the regular subcommands — a
// lightweight management console. The daemon itself is run by the installed
// service (or `zashhomo run`); here the user just issues management commands.
func cmdInteractive(_ []string) error {
	fmt.Printf("zashhomo %s — interactive console. Type 'help' for commands, 'exit' to quit.\n\n", version)
	_ = dispatch("status", nil)
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Print("zashhomo> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
			}
			fmt.Println()
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "exit", "quit":
			return nil
		case "help", "?":
			usage()
			continue
		case "interactive", "-i", "--interactive":
			fmt.Println("already in interactive mode")
			continue
		case "run":
			fmt.Println("`run` starts the foreground daemon; run it directly with `zashhomo run` instead.")
			continue
		}
		if err := dispatch(fields[0], fields[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
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
		fmt.Printf("panel:   %s\n", panelURL(cfg))
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
		if !updated {
			fmt.Printf("  kernel %s (up to date)\n", tag)
		}
	}
	if doUI || doAll {
		tag, updated, err := panel.Install(p, cfg.UIVersion)
		if err != nil {
			return err
		}
		cfg.UIVersion = tag
		if !updated {
			fmt.Printf("  panel %s (up to date)\n", tag)
		}
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
func selfUpdate() (err error) {
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

	st := ui.NewStage("Updating zashhomo")
	st.Start()
	defer func() {
		if err != nil {
			st.Done("failed")
		} else {
			st.Done(fmt.Sprintf("%s ✓", rel.TagName))
		}
	}()

	newPath := exe + ".new"
	if err := st.Download(asset.URL, newPath); err != nil {
		return fmt.Errorf("self-update: download: %w", err)
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
	fmt.Println("  restart the service to apply:  zashhomo restart")
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
	// Check for administrator privileges on Windows
	if !elevate.IsAdmin() {
		fmt.Fprintln(os.Stderr, "Uninstalling the Windows service requires administrator privileges.")
		fmt.Fprintln(os.Stderr, "Requesting elevation...")

		// Auto-elevate using UAC
		if err := elevate.RunElevated(os.Args[1:]); err != nil {
			return fmt.Errorf("failed to elevate privileges: %w", err)
		}
		// The elevated process will handle the uninstallation and exit.
		// This instance should exit cleanly.
		return nil
	}

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
	if err := selfinstall.Uninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "note: %v (remove it manually if desired)\n", err)
	} else {
		fmt.Println("global command removed")
	}
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

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
