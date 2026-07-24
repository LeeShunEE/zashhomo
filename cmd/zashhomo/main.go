// Command zashhomo is a lightweight cross-platform supervisor/manager for the
// mihomo proxy kernel with a built-in zashboard web panel.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

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
	"github.com/LeeShunEE/zashhomo/internal/sysproxy"
	"github.com/LeeShunEE/zashhomo/internal/ui"
)

// version is overridden at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

// selfRepo is this project's GitHub repo, used by `update --self`.
const selfRepo = "LeeShunEE/zashhomo"

func main() {
	args := applyElevatedLog(os.Args[1:])
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	if err := dispatch(args[0], args[1:]); err != nil {
		printCmdError(err)
		os.Exit(1)
	}
}

// printCmdError reports err on stderr unless the elevated child already relayed
// its own message (ErrChildReported), in which case a second line is just noise.
func printCmdError(err error) {
	if err == nil || errors.Is(err, elevate.ErrChildReported) {
		return
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
}

// clearScreen wipes the terminal, including scrollback, so each interactive
// action starts on a clean screen instead of scrolling past prior output.
func clearScreen() { fmt.Print("\033[H\033[2J\033[3J") }

// applyElevatedLog handles the private --elevated-log <path> flag set by the
// Windows UAC relauncher: it redirects this process's stdout/stderr (and the
// standard logger) to that file so the non-elevated parent can print them in
// the original console, then returns args with the flag removed. When the flag
// is absent — the normal case, and always on non-Windows — args are unchanged.
func applyElevatedLog(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		path := ""
		switch {
		case a == elevate.ElevatedLogFlag:
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, elevate.ElevatedLogFlag+"="):
			path = strings.TrimPrefix(a, elevate.ElevatedLogFlag+"=")
		default:
			out = append(out, a)
			continue
		}
		if path != "" {
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
				os.Stdout = f
				os.Stderr = f
				log.SetOutput(f)
			}
		}
	}
	return out
}

// dispatch executes a single command (cmd with args). It does not call os.Exit,
// so the interactive console can reuse it line by line.
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "install":
		return cmdInstall(args)
	case "run":
		return cmdRun(args)
	case "service":
		return cmdService(args)
	case "status":
		return cmdStatus()
	case "dashboard":
		return cmdDashboard()
	case "onboard":
		return cmdOnboard()
	case "update":
		return cmdUpdate(args)
	case "sub":
		return cmdSub(args)
	case "system-proxy", "sysproxy":
		return cmdSystemProxy(args)
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

// ensureElevated guarantees the current process has administrator privileges for
// operations that manage the OS service (install/uninstall/start/stop/restart).
// When already elevated it returns (false, nil) and the caller proceeds. When
// not, it relaunches this executable via UAC to run cmd exactly and returns
// (true, nil), signalling the caller to exit cleanly — the elevated instance
// performs the work. Passing the specific command (rather than reusing os.Args)
// means elevation triggered from inside the interactive menu runs the chosen
// action directly instead of reopening the console. On Unix platforms, if the
// user lacks root privileges, it returns a clear error with sudo instructions.
func ensureElevated(what string, cmd []string) (elevated bool, err error) {
	if elevate.IsAdmin() {
		return false, nil
	}

	// On Unix, provide clear guidance instead of auto-elevation
	if runtime.GOOS != "windows" {
		return false, fmt.Errorf("%s requires root privileges.\nPlease run with: sudo zashhomo %s", what, strings.Join(cmd, " "))
	}

	// Windows: UAC auto-elevation
	fmt.Fprintf(os.Stderr, "%s requires administrator privileges.\nRequesting elevation…\n", what)
	if err := elevate.RunElevated(cmd); err != nil {
		// When the elevated child already relayed its own error, pass the
		// sentinel through unwrapped so the caller can stay quiet.
		if errors.Is(err, elevate.ErrChildReported) {
			return false, err
		}
		return false, fmt.Errorf("failed to elevate privileges: %w", err)
	}
	return true, nil
}

// ensureCanWrite gates every command that changes the files zashhomo owns — the
// config, the profile cache, the generated mihomo config, the installed kernel
// and panel. Those live beside the service (ProgramData on Windows, /etc and
// /var/lib for a root install), so a plain user session cannot touch them.
//
// It returns (true, nil) when the work has been handed to an elevated copy of
// this process, exactly like ensureElevated, and (false, nil) when the current
// session may proceed as it is. Elevation is decided by an actual write probe
// rather than by "am I admin", so a data directory the user does own (via
// ZASHHOMO_DATA, or a non-root Unix install) never raises a needless prompt.
func ensureCanWrite(what string, cmd []string) (elevated bool, err error) {
	// Unix only: a root-owned install this session isn't looking at. Elevating is
	// not the fix — New() would still resolve to the user's own copy — so stop
	// with the command that does work rather than write a config nothing reads.
	if sys := paths.ForeignSystemInstall(); sys != "" {
		return false, fmt.Errorf("zashhomo is installed system-wide (%s), but this session would edit your personal copy instead,\n"+
			"which the service never reads.\nRun it as root:  sudo zashhomo %s\n"+
			"(or set ZASHHOMO_CONFIG_DIR to manage a separate per-user setup on purpose)",
			sys, strings.Join(cmd, " "))
	}
	if paths.New().ManagedWritable() {
		return false, nil
	}
	return ensureElevated(what, cmd)
}

// cmdService controls the installed OS service. Lifecycle actions need admin
// rights, so it checks (and elevates) before touching the service manager.
func cmdService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("service: expected 'start', 'stop', 'restart', or 'status'")
	}
	switch action := args[0]; action {
	case "start", "stop", "restart":
		if elevated, err := ensureElevated("Controlling the service", append([]string{"service"}, args...)); err != nil || elevated {
			return err
		}
		// Control only asks the service manager to act; it returns while the
		// service is still transitioning. Wait for the target state so the
		// spinner covers the real work and the result is a genuine confirmation
		// rather than "the request was accepted".
		label := map[string]string{
			"start":   "Starting service",
			"stop":    "Stopping service",
			"restart": "Restarting service",
		}[action]
		return ui.Run(label, "✓", func() error {
			if err := svc.Control(action); err != nil {
				return err
			}
			if action == "stop" {
				return svc.WaitStopped(svc.StateWait)
			}
			return svc.WaitRunning(svc.StateWait)
		})
	case "status":
		return cmdStatus()
	default:
		return fmt.Errorf("service: unknown subcommand %q (want start|stop|restart|status)", action)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `zashhomo — lightweight mihomo supervisor + zashboard panel

Usage:
  zashhomo install [--mixed-port N] [--web-port N] [--web-addr ADDR] [--force]
                              Download kernel+panel, write config, register & start service
                              (--force replaces an already-installed service without asking)
  zashhomo run [--mixed-port N] [--web-port N] [--web-addr ADDR]
                              Run the daemon in the foreground (used by the service)
  zashhomo -i | interactive   Interactive management console
  zashhomo service start|stop|restart|status
                              Control the installed service (start/stop/restart need admin)
  zashhomo status             Show service status
  zashhomo dashboard          Open the zashboard panel in your default browser
  zashhomo onboard            Guided setup: install, subscribe, start, proxy, panel
  zashhomo system-proxy enable|disable
                              Set or clear the OS system proxy (points at the mixed-port)
  zashhomo update [flags]     Update components (--core --ui --self --all)
  zashhomo sub add <url> [name]
                              Add a subscription and download it into the cache
  zashhomo sub list           List subscriptions with their state (▸ marks the active one)
  zashhomo sub show <index>   Show one subscription in full
  zashhomo sub switch <index> Make that subscription the active profile (reads the cache)
  zashhomo sub update [index] Refresh one subscription, or every enabled one
  zashhomo sub enable|disable <index>
                              Resume or pause a subscription (paused ones never refresh)
  zashhomo sub auto <index> on|off
                              Turn that subscription's scheduled update on or off
  zashhomo sub interval [dur] Show or set the global refresh interval (e.g. 6h)
  zashhomo sub interval <index> <dur>
                              Give one subscription its own interval ('default' to clear)
  zashhomo sub remove <index> Remove the subscription at <index> (see 'sub list')
  zashhomo sub edit           Open the config file in your editor
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

// mixedProxyURL returns the local endpoint of mihomo's mixed proxy port, which
// serves HTTP and SOCKS on the same port. It binds to loopback (allow-lan is
// forced off), so clients point at 127.0.0.1.
func mixedProxyURL(cfg *config.Config) string {
	return fmt.Sprintf("http://127.0.0.1:%d", cfg.MixedPort)
}

func cmdInstall(args []string) error {
	// --force replaces an already-installed service. Strip it before the shared
	// listen-flag parser (which doesn't know it) and before elevation.
	force, rest := popFlag(args, "--force")

	// Decide whether to replace an existing service here, in the original console —
	// the elevated child runs hidden and can't prompt. When the user confirms we
	// carry the decision across the UAC boundary via --force.
	if !force && svc.GetState().Installed {
		fmt.Println("The zashhomo service is already installed.")
		switch strings.ToLower(promptLine("Reinstall / replace it? [y/N]: ")) {
		case "y", "yes":
			force = true
		default:
			fmt.Println("cancelled")
			return nil
		}
	}

	elevArgs := append([]string{"install"}, rest...)
	if force {
		elevArgs = append(elevArgs, "--force")
	}
	if elevated, err := ensureElevated("Installing the service", elevArgs); err != nil || elevated {
		return err
	}

	mixedPort, webPort, webAddr, err := parseListenFlags("install", rest)
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

	// Both installers animate themselves and finalize their own line, including
	// the "(up to date)" case, so nothing is printed here.
	tag, _, err := core.Install(p, cfg.CoreVersion)
	if err != nil {
		return err
	}
	cfg.CoreVersion = tag

	utag, _, err := panel.Install(p, cfg.UIVersion)
	if err != nil {
		return err
	}
	cfg.UIVersion = utag

	// GenerateConfig fetches every subscription, so this step is network-bound.
	if err := ui.Run("Writing mihomo config", "✓", func() error {
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			return err
		}
		return cfg.Save()
	}); err != nil {
		return err
	}

	// Register `zashhomo` as a global CLI and pin the service to a stable path.
	exePath := ""
	res, err := ui.RunValue("Registering global `zashhomo` command",
		selfinstall.EnsureInstalled,
		func(r selfinstall.Result) string {
			if r.Copied {
				return "installed to " + r.Path
			}
			return "already at " + r.Path
		})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not install to PATH (%v); using current binary\n", err)
	} else {
		exePath = res.Path
	}

	// With --force, remove any existing registration first so the (re)install
	// doesn't fail with "service already exists".
	if force && svc.GetState().Installed {
		if err := ui.Run("Removing existing service", "✓", svc.Uninstall); err != nil {
			return err
		}
	}

	// svc.Install registers *and* starts the service, so keep spinning until the
	// service manager reports it settled in the running state.
	if err := ui.Run(fmt.Sprintf("Registering service (%s)", svc.Platform()), "✓", func() error {
		if err := svc.Install(exePath); err != nil {
			return err
		}
		return svc.WaitRunning(svc.StateWait)
	}); err != nil {
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
func cmdInteractive(args []string) error {
	// The arrow-key menu needs a real terminal for both reading keys and
	// rendering. When stdin/stdout are redirected (pipes, CI, tests) fall back
	// to the line-based console below.
	if ui.IsTerminal(os.Stdin) && ui.IsTerminal(os.Stdout) {
		return cmdMenu(args)
	}
	return cmdConsoleLine(args)
}

// cmdMenu runs the arrow-key management menu, looping until the user exits.
// Each selection tears down the menu (alt screen), runs the command so its
// normal stdout/spinners render on the main screen, then returns to the menu.
func cmdMenu(_ []string) error {
	// First launch for this user: offer the guided setup once, before the menu
	// they have never seen appears. Declining is remembered too, so this asks at
	// most once per user.
	if start, err := offerOnboarding(paths.New()); err != nil {
		return err
	} else if start {
		clearScreen()
		if err := cmdOnboard(); err != nil {
			printCmdError(err)
		}
		fmt.Println()
		promptLine("Press Enter to open the menu… ")
	}

	for {
		// Rebuild the menu (and its banner+status header) from the current state
		// each loop so ordering, greyed items, and the status block reflect
		// installs/starts made moments ago.
		st := svc.GetState()
		// The header probes the OS proxy and the running kernel, so building it
		// can take a second or two. Spin on the main screen first, then hand the
		// finished header to the menu — the alt screen would otherwise open on a
		// blank frame with no sign that anything is happening.
		var header string
		_ = ui.Run("Reading status", "", func() error {
			header = menuHeader(st)
			return nil
		})
		it, err := runMenu(st, header)
		if err != nil {
			return err
		}
		if it.action == "" || it.action == "exit" {
			return nil
		}
		// Placeholder rows ("No subscriptions configured") carry no command; go
		// straight back to the menu instead of asking to force them through.
		if it.action == "noop" {
			continue
		}
		clearScreen()
		// A greyed action doesn't apply in the current state; make the user
		// confirm before forcing it through.
		if it.disabled != "" && !confirmForce(it.label, it.disabled) {
			continue
		}
		fields := strings.Fields(it.action)
		switch fields[0] {
		case "sub-add":
			url := promptLine("Subscription URL (blank to cancel): ")
			if url == "" {
				fmt.Println("cancelled")
			} else if err := dispatch("sub", []string{"add", url}); err != nil {
				printCmdError(err)
			}
		case "sub-remove":
			if err := removeSubscriptionAt(fields[1:]); err != nil {
				printCmdError(err)
			}
		case "sub-interval":
			val := promptLine("Global refresh interval (e.g. 6h, 30m; blank to cancel): ")
			if val == "" {
				fmt.Println("cancelled")
			} else if err := dispatch("sub", []string{"interval", val}); err != nil {
				printCmdError(err)
			}
		case "sub-interval-at":
			val := promptLine("Update interval for this subscription (e.g. 6h, 30m; 'default' to follow the global one; blank to cancel): ")
			if val == "" {
				fmt.Println("cancelled")
			} else if err := dispatch("sub", []string{"interval", fields[1], val}); err != nil {
				printCmdError(err)
			}
		default:
			if err := dispatch(fields[0], fields[1:]); err != nil {
				printCmdError(err)
			}
		}
		fmt.Println()
		promptLine("Press Enter to return to the menu… ")
	}
}

// confirmForce warns that an action doesn't apply in the current state (reason)
// and asks whether to run it anyway. It returns true to proceed.
func confirmForce(label, reason string) bool {
	fmt.Printf("%s — %s.\n", strings.TrimSuffix(label, " ▸"), reason)
	switch strings.ToLower(promptLine("Run it anyway? [y/N]: ")) {
	case "y", "yes":
		fmt.Println()
		return true
	default:
		fmt.Println("cancelled")
		return false
	}
}

// promptLine prints a prompt and reads one trimmed line from stdin.
func promptLine(prompt string) string {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

// cmdConsoleLine is the legacy line-based console, kept as a non-TTY fallback.
func cmdConsoleLine(_ []string) error {
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
			printCmdError(err)
		}
	}
}

func cmdStatus() error {
	state := svc.GetState()
	st := "running"
	stStyle := theme.StatusOk
	if !state.Installed {
		st = "not installed"
		stStyle = theme.StatusWarn
	} else if !state.Running {
		st = "stopped"
		stStyle = theme.StatusWarn
	}

	p := paths.New()
	cfg, _ := config.Load(p.Config)

	// Output with themed styles
	line := func(label, value string) string {
		return theme.OutputLabel.Render(label) + theme.OutputValue.Render(value) + "\n"
	}

	fmt.Print(line("service: ", stStyle.Render(st)+" ("+svc.Platform()+")"))
	fmt.Print(line("version: ", version))
	if cfg != nil {
		// proxy and tun are probed live (an OS query and a call to the kernel's
		// REST API), so gather them under a spinner before printing the block —
		// otherwise status just hangs for a few seconds with nothing on screen.
		var proxy, tun string
		_ = ui.Run("Reading live state", "", func() error {
			proxy, tun = systemProxyStatus(cfg), tunStatus(cfg)
			return nil
		})
		fmt.Print(line("proxy:   ", proxy))
		fmt.Print(line("mixed:   ", mixedProxyURL(cfg)))
		fmt.Print(line("tun:     ", tun))
		fmt.Print(line("panel:   ", panelURL(cfg)))
		fmt.Print(line("kernel:  ", orDash(cfg.CoreVersion)))
		fmt.Print(line("panelv:  ", orDash(cfg.UIVersion)))
		fmt.Print(line("subs:    ", fmt.Sprintf("%d", len(cfg.Subscriptions))))
	}
	return nil
}

// cmdDashboard opens the zashboard panel in the user's default browser. The URL
// carries the login token, so the panel comes up already authenticated; it is
// printed as well so the user can open it by hand if the launch fails.
func cmdDashboard() error {
	cfg, err := config.Load(paths.New().Config)
	if err != nil {
		return err
	}
	if !svc.GetState().Running {
		fmt.Println(theme.StatusWarn.Render("the service is not running — the panel won't answer until it is started"))
	}
	url := panelURL(cfg)
	if err := openBrowser(url); err != nil {
		return fmt.Errorf("%w\nopen it manually: %s", err, url)
	}
	fmt.Printf("opening the dashboard in your browser:\n  %s\n", url)
	return nil
}

// openBrowser hands rawURL to the OS default browser. The browser is started but
// not waited on, so the interactive menu stays responsive; a goroutine reaps the
// launcher so it doesn't linger as a zombie for the life of the menu.
func openBrowser(rawURL string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "windows":
		// rundll32 passes the URL to the shell verbatim; `cmd /c start` would
		// treat the "&" of a query string as a command separator.
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}
	case "darwin":
		name, args = "open", []string{rawURL}
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return fmt.Errorf("no browser launcher found (xdg-open is missing)")
		}
		name, args = "xdg-open", []string{rawURL}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser (%s): %w", name, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// systemProxyStatus renders the live system-proxy state for the status block. It
// queries the OS directly so a proxy toggled outside zashhomo is still reflected,
// falling back to the persisted intent when the query is unavailable.
func systemProxyStatus(cfg *config.Config) string {
	st, err := sysproxy.Get()
	if err != nil {
		if cfg.SystemProxy {
			return theme.StatusWarn.Render("managed (query failed)")
		}
		return "disabled"
	}
	if st.Enabled {
		server := st.Server
		if server == "" {
			server = fmt.Sprintf("127.0.0.1:%d", cfg.MixedPort)
		}
		return theme.StatusOk.Render("enabled") + " (" + server + ")"
	}
	return "disabled"
}

// tunStatus renders the TUN mode state for the status block. It prefers the
// running kernel's live config so a panel toggle shows immediately, and falls
// back to the persisted setting (flagged as such) when the kernel can't be
// queried, e.g. while the service is stopped.
func tunStatus(cfg *config.Config) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if tun, err := subscription.FetchTun(ctx, cfg); err == nil {
		return tunStateString(tun, false)
	}
	if len(cfg.Tun) > 0 {
		return tunStateString(cfg.Tun, true)
	}
	return "disabled"
}

// tunStateString formats a tun block's enable/stack. When persisted is true the
// value comes from config rather than a live kernel, so an enabled state is
// shown in the warn style to signal it isn't confirmed running.
func tunStateString(tun map[string]any, persisted bool) string {
	on, _ := tun["enable"].(bool)
	if !on {
		return "disabled"
	}
	label := "enabled"
	if stack, ok := tun["stack"].(string); ok && stack != "" {
		label += " (" + stack + ")"
	}
	if persisted {
		return theme.StatusWarn.Render(label + " [config]")
	}
	return theme.StatusOk.Render(label)
}

// cmdSystemProxy enables or disables the OS system proxy, pointing it at the
// configured mixed-port, and records the choice in the config so the daemon can
// restore it on the next service start.
func cmdSystemProxy(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("system-proxy: expected 'enable' or 'disable'")
	}
	if elevated, err := elevateSystemProxy(args); err != nil || elevated {
		return err
	}
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}
	switch args[0] {
	case "enable", "on":
		// Applying the OS proxy broadcasts a settings change to every listener,
		// which can stall for a moment; keep the line alive while it does.
		if err := ui.Run("Enabling system proxy",
			fmt.Sprintf("✓ 127.0.0.1:%d", cfg.MixedPort), func() error {
				if err := sysproxy.Enable("127.0.0.1", cfg.MixedPort); err != nil {
					return err
				}
				cfg.SystemProxy = true
				return cfg.Save()
			}); err != nil {
			return err
		}
		return nil
	case "disable", "off":
		if err := ui.Run("Disabling system proxy", "✓", func() error {
			if err := sysproxy.Disable(); err != nil {
				return err
			}
			cfg.SystemProxy = false
			return cfg.Save()
		}); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("system-proxy: unknown subcommand %q (want enable|disable)", args[0])
	}
}

// elevateSystemProxy re-runs the system-proxy command elevated on Windows, where
// the config lives under ProgramData and an unelevated session cannot record the
// choice. Everywhere else the config is the user's own file, so there is nothing
// to elevate for — and elevating would be actively wrong, since the proxy itself
// is per-user state (GNOME's gsettings under sudo writes root's dconf).
//
// The Windows caveat: with the usual split-token administrator the elevated
// process is still the same user, so the per-user proxy keys land where they
// should. Only over-the-shoulder elevation — a standard user typing a *different*
// admin's credentials — would set that other account's proxy, so say so first.
func elevateSystemProxy(args []string) (elevated bool, err error) {
	if runtime.GOOS != "windows" || elevate.IsAdmin() {
		return false, nil
	}
	fmt.Println("Recording the system-proxy setting needs administrator rights.")
	fmt.Println(theme.Hint.Render("If the UAC prompt asks for a different account, the proxy would be set for that account instead of yours."))
	return ensureElevated("Setting the system proxy", append([]string{"system-proxy"}, args...))
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

	// --core and --ui unpack into the data directory and record the new tags in
	// the config, so they need the rights the installer had. A lone --self only
	// replaces the binary in the user's own install directory, which the session
	// already owns — prompting there would be friction for nothing.
	touchesData := doCore || doUI || doAll
	if touchesData {
		if elevated, err := ensureCanWrite("Updating the installed components", append([]string{"update"}, args...)); err != nil || elevated {
			return err
		}
	}

	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}

	// Both installers animate themselves and finalize their own line, including
	// the "(up to date)" case, so nothing is printed here.
	if doCore || doAll {
		tag, _, err := core.Install(p, cfg.CoreVersion)
		if err != nil {
			return err
		}
		cfg.CoreVersion = tag
	}
	if doUI || doAll {
		tag, _, err := panel.Install(p, cfg.UIVersion)
		if err != nil {
			return err
		}
		cfg.UIVersion = tag
	}
	// Only the two branches above change a recorded version. Saving unconditionally
	// would write the config for a lone --self too — the one case deliberately let
	// through without elevation, and so the one case where the write would fail.
	if touchesData {
		if err := cfg.Save(); err != nil {
			return err
		}
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
	// Every branch above changes something the running service has already loaded,
	// self-update included, so the hint applies whichever one ran.
	fmt.Println("restart the service to apply:  zashhomo service restart")
	return nil
}

// selfUpdate replaces the running zashhomo binary with the latest release.
func selfUpdate() (err error) {
	// The animation covers the release query too: it is a network call that can
	// stall well before there is anything to download.
	tag := ""
	st := ui.NewStage("Updating zashhomo")
	st.Start()
	defer func() {
		if err != nil {
			st.Done("failed")
		} else {
			st.Done(fmt.Sprintf("%s ✓", tag))
		}
	}()

	rel, err := ghrelease.Latest(selfRepo)
	if err != nil {
		return fmt.Errorf("self-update: %w", err)
	}
	tag = rel.TagName
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
	// The restart hint belongs to cmdUpdate, not here: st.Done runs on return, so
	// anything printed at this point would land in the middle of the spinner line
	// that is still being redrawn in place.
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

// subMutates reports whether a `sub` invocation changes anything on disk. The
// listings — and the argument-less forms of `interval`, which only report the
// current value — must not raise an elevation prompt just to print something.
func subMutates(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "add", "update", "switch", "use", "activate", "enable", "disable",
		"auto", "remove", "rm", "del", "delete", "edit":
		return true
	case "interval":
		rest := args[1:]
		if len(rest) == 0 {
			return false // `sub interval` just prints the global value
		}
		// A duration always carries a unit, so a leading integer is unambiguously
		// an index — and that per-subscription form only writes when a value
		// follows it (`sub interval 0` reports, `sub interval 0 6h` sets).
		if _, err := strconv.Atoi(rest[0]); err != nil {
			return true // global form: `sub interval 6h`
		}
		return len(rest) >= 2
	}
	return false
}

func cmdSub(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sub: expected 'add', 'list', 'show', 'switch', 'enable', 'disable', 'auto', 'interval', 'update', 'remove', or 'edit'")
	}
	// Every mutating form rewrites zashhomo.yaml, and most also touch the profile
	// cache and the generated mihomo config — all of which live beside the service.
	// Elevate before loadOrInit, which already tries to create those directories.
	// The argument list is fully concrete by this point (the menu asks for URLs and
	// durations before dispatching), so it survives the trip across the elevation
	// boundary, where the hidden child process cannot prompt for anything.
	if subMutates(args) {
		if elevated, err := ensureCanWrite("Changing subscriptions", append([]string{"sub"}, args...)); err != nil || elevated {
			return err
		}
	}
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}

	switch args[0] {
	case "add":
		return subAdd(p, cfg, args[1:])
	case "update":
		return subUpdate(p, cfg, args[1:])
	case "switch", "use", "activate":
		return subSwitch(p, cfg, args[1:])
	case "enable":
		return subSetEnabled(p, cfg, args[1:], true)
	case "disable":
		return subSetEnabled(p, cfg, args[1:], false)
	case "auto":
		return subAuto(cfg, args[1:])
	case "interval":
		return subInterval(p, cfg, args[1:])
	case "list", "ls":
		printSubscriptions(p, cfg)
		return nil
	case "show":
		return subShow(p, cfg, args[1:])
	case "remove", "rm", "del", "delete":
		return subRemove(p, cfg, args[1:])
	case "edit":
		return openInEditor(p.Config)
	default:
		return fmt.Errorf("sub: unknown subcommand %q", args[0])
	}
}

// subIndex parses a subscription index argument and checks it against cfg.
func subIndex(cfg *config.Config, cmd, arg string) (int, error) {
	index, err := strconv.Atoi(arg)
	if err != nil {
		return 0, fmt.Errorf("sub %s: invalid index %q", cmd, arg)
	}
	if index < 0 || index >= len(cfg.Subscriptions) {
		return 0, fmt.Errorf("sub %s: index %d out of range (0-%d)", cmd, index, len(cfg.Subscriptions)-1)
	}
	return index, nil
}

// applyActive regenerates config.yaml from the active subscription and asks a
// running kernel to reload it. Every command that changes which profile is live,
// or refreshes the live one, ends here. A failed reload is reported as a hint
// rather than an error: with the service stopped there is simply nothing to
// reload, and the config on disk is already correct.
func applyActive(p *paths.Paths, cfg *config.Config) error {
	if err := ui.Run("Writing mihomo config", "✓", func() error {
		return subscription.GenerateConfig(p, cfg)
	}); err != nil {
		return err
	}
	if err := ui.Run("Reloading kernel", "✓", func() error {
		return subscription.Reload(context.Background(), cfg, p.MihomoConfig())
	}); err != nil {
		fmt.Println("run `zashhomo service restart` (or start the service) to apply")
	}
	return nil
}

// subAdd registers a new subscription and downloads it into the profile cache.
// The first subscription added becomes the active one, so it also takes effect
// immediately; later ones are cached and wait to be switched to.
func subAdd(p *paths.Paths, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("sub add: missing <url>")
	}
	name := ""
	if len(args) >= 2 {
		name = args[1]
	}
	index := cfg.AddSubscription(name, args[0])

	if err := ui.Run("Fetching subscription", "✓", func() error {
		if err := subscription.Refresh(p, cfg, index); err != nil {
			return err
		}
		return cfg.Save()
	}); err != nil {
		return err
	}

	sub := cfg.Subscriptions[index]
	fmt.Printf("added subscription %q (%d total)\n", sub.DisplayName(index), len(cfg.Subscriptions))
	if cfg.ActiveIndex() != index {
		fmt.Printf("it is cached but not active; switch to it with:  zashhomo sub switch %d\n", index)
		return nil
	}
	return applyActive(p, cfg)
}

// subUpdate refreshes the cached document of one subscription, or of every
// enabled one when no index is given. The kernel is only reloaded when the
// active profile actually changed.
func subUpdate(p *paths.Paths, cfg *config.Config, args []string) error {
	active := cfg.ActiveIndex()
	touchedActive := false

	if len(args) == 0 {
		if len(cfg.Subscriptions) == 0 {
			fmt.Println("no subscriptions configured — add one with:  zashhomo sub add <url>")
			return nil
		}
		if err := ui.Run("Fetching subscriptions", "✓", func() error {
			err := subscription.RefreshAll(p, cfg)
			if saveErr := cfg.Save(); err == nil {
				err = saveErr
			}
			return err
		}); err != nil {
			return err
		}
		touchedActive = active >= 0
	} else {
		index, err := subIndex(cfg, "update", args[0])
		if err != nil {
			return err
		}
		if err := ui.Run("Fetching "+cfg.Subscriptions[index].DisplayName(index), "✓", func() error {
			if err := subscription.Refresh(p, cfg, index); err != nil {
				return err
			}
			return cfg.Save()
		}); err != nil {
			return err
		}
		touchedActive = index == active
	}

	if !touchedActive {
		fmt.Println("updated; it is not the active subscription, so the kernel is unchanged")
		return nil
	}
	return applyActive(p, cfg)
}

// subSwitch makes another subscription the active profile. It reads the cache,
// so switching works offline once a subscription has been fetched.
func subSwitch(p *paths.Paths, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("sub switch: missing <index> (see 'zashhomo sub list')")
	}
	index, err := subIndex(cfg, "switch", args[0])
	if err != nil {
		return err
	}
	if index == cfg.ActiveIndex() {
		fmt.Printf("%q is already the active subscription\n", cfg.Subscriptions[index].DisplayName(index))
		return nil
	}
	if err := cfg.SetActive(index); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("switched to %q\n", cfg.Subscriptions[index].DisplayName(index))
	return applyActive(p, cfg)
}

// subSetEnabled pauses or resumes a subscription. Disabling the active one hands
// the active slot to the next enabled profile, which is a change to what the
// kernel routes through, so config.yaml is rewritten in that case.
func subSetEnabled(p *paths.Paths, cfg *config.Config, args []string, enable bool) error {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	if len(args) < 1 {
		return fmt.Errorf("sub %s: missing <index> (see 'zashhomo sub list')", verb)
	}
	index, err := subIndex(cfg, verb, args[0])
	if err != nil {
		return err
	}
	before := cfg.ActiveIndex()
	if err := cfg.SetSubEnabled(index, enable); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	name := cfg.Subscriptions[index].DisplayName(index)
	after := cfg.ActiveIndex()
	if enable {
		fmt.Printf("enabled %q\n", name)
	} else {
		fmt.Printf("disabled %q — it no longer refreshes and cannot be switched to\n", name)
	}
	if after == before {
		return nil
	}
	if after < 0 {
		fmt.Println("no enabled subscription left; falling back to a direct-only config")
	} else {
		fmt.Printf("active subscription is now %q\n", cfg.Subscriptions[after].DisplayName(after))
	}
	return applyActive(p, cfg)
}

// subAuto turns a subscription's scheduled refresh on or off. Nothing the kernel
// reads changes, so there is no config to rewrite.
func subAuto(cfg *config.Config, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("sub auto: expected <index> on|off")
	}
	index, err := subIndex(cfg, "auto", args[0])
	if err != nil {
		return err
	}
	var on bool
	switch strings.ToLower(args[1]) {
	case "on", "true", "yes", "enable":
		on = true
	case "off", "false", "no", "disable":
		on = false
	default:
		return fmt.Errorf("sub auto: expected on|off, got %q", args[1])
	}
	if err := cfg.SetSubAutoUpdate(index, on); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	s := cfg.Subscriptions[index]
	if on {
		fmt.Printf("scheduled update enabled for %q (every %s)\n", s.DisplayName(index), s.RefreshIntervalOr(cfg.RefreshInterval()))
	} else {
		fmt.Printf("scheduled update disabled for %q — refresh it by hand with:  zashhomo sub update %d\n", s.DisplayName(index), index)
	}
	return nil
}

// subInterval shows or sets a refresh interval. With no numeric index it acts on
// the global default; with one it sets that subscription's own interval, where
// "default" clears the override and puts it back on the global one. A duration
// always carries a unit, so a leading integer is unambiguously an index.
func subInterval(p *paths.Paths, cfg *config.Config, args []string) error {
	if len(args) == 0 {
		fmt.Printf("global refresh interval: %s\n", cfg.RefreshInterval())
		fmt.Println("set with:  zashhomo sub interval <duration>          (e.g. 6h, 30m, 90m)")
		fmt.Println("per sub:   zashhomo sub interval <index> <duration>  ('default' follows the global one)")
		return nil
	}

	// Global form: `sub interval 6h`.
	if _, err := strconv.Atoi(args[0]); err != nil {
		if err := cfg.SetRefreshInterval(args[0]); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("global refresh interval set to %s\n", cfg.RefreshInterval())
		fmt.Println("subscriptions with their own interval are unaffected")
		return nil
	}

	// Per-subscription form: `sub interval 0 [6h|default]`.
	index, err := subIndex(cfg, "interval", args[0])
	if err != nil {
		return err
	}
	if len(args) < 2 {
		s := cfg.Subscriptions[index]
		fmt.Printf("%s: refreshes every %s%s\n", s.DisplayName(index),
			s.RefreshIntervalOr(cfg.RefreshInterval()), intervalOrigin(s))
		return nil
	}
	value := args[1]
	if strings.EqualFold(value, "default") || strings.EqualFold(value, "global") {
		value = ""
	}
	if err := cfg.SetSubInterval(index, value); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	s := cfg.Subscriptions[index]
	fmt.Printf("%s: refreshes every %s%s\n", s.DisplayName(index),
		s.RefreshIntervalOr(cfg.RefreshInterval()), intervalOrigin(s))
	if !s.AutoUpdate() {
		fmt.Println(theme.Hint.Render("scheduled update is currently off for this subscription, so the interval is not in effect"))
	}
	return nil
}

// intervalOrigin annotates an interval with where it came from, so " (global)"
// distinguishes a subscription that merely follows the default from one that
// happens to have set the same value.
func intervalOrigin(s config.Subscription) string {
	if s.Interval == "" {
		return " (global default)"
	}
	return " (its own setting)"
}

// subShow prints one subscription's full state.
func subShow(p *paths.Paths, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("sub show: missing <index>")
	}
	index, err := subIndex(cfg, "show", args[0])
	if err != nil {
		return err
	}
	s := cfg.Subscriptions[index]

	line := func(label, value string) string {
		return theme.OutputLabel.Render(label) + theme.OutputValue.Render(value) + "\n"
	}
	state := "enabled"
	if !s.Enabled() {
		state = theme.StatusWarn.Render("disabled")
	}
	if index == cfg.ActiveIndex() {
		state += theme.StatusOk.Render("  (active)")
	}
	auto := "on, every " + s.RefreshIntervalOr(cfg.RefreshInterval()).String() + intervalOrigin(s)
	if !s.AutoUpdate() {
		auto = "off"
	}

	fmt.Print(line("name:     ", s.DisplayName(index)))
	fmt.Print(line("index:    ", strconv.Itoa(index)))
	fmt.Print(line("url:      ", s.URL))
	fmt.Print(line("state:    ", state))
	fmt.Print(line("schedule: ", auto))
	fmt.Print(line("updated:  ", lastUpdated(s)))
	fmt.Print(line("cached:   ", cachedState(p, s)))
	return nil
}

// subRemove deletes a subscription and its cached document. The caller has
// already confirmed when this comes from the menu; from the CLI the index is
// explicit enough to be its own confirmation.
func subRemove(p *paths.Paths, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("sub remove: missing <index>")
	}
	index, err := subIndex(cfg, "remove", args[0])
	if err != nil {
		return err
	}
	removed := cfg.Subscriptions[index]
	name := removed.DisplayName(index)
	wasActive := index == cfg.ActiveIndex()

	if err := cfg.RemoveSubscription(index); err != nil {
		return err
	}
	// Best effort: an orphaned cache file is harmless, and failing the removal
	// over it would leave the config and the cache disagreeing.
	if err := subscription.DropCache(p, removed); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not delete the cached copy: %v\n", err)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("removed subscription %q (%d remaining)\n", name, len(cfg.Subscriptions))

	if !wasActive {
		return nil
	}
	if active := cfg.ActiveIndex(); active >= 0 {
		fmt.Printf("active subscription is now %q\n", cfg.Subscriptions[active].DisplayName(active))
	} else {
		fmt.Println("no subscriptions left; falling back to a direct-only config")
	}
	return applyActive(p, cfg)
}

// lastUpdated renders when a subscription was last fetched, as an age.
func lastUpdated(s config.Subscription) string {
	if s.UpdatedAt.IsZero() {
		return "never"
	}
	return fmt.Sprintf("%s (%s ago)", s.UpdatedAt.Format("2006-01-02 15:04"),
		time.Since(s.UpdatedAt).Round(time.Minute))
}

// cachedState reports whether a subscription can be switched to without network.
func cachedState(p *paths.Paths, s config.Subscription) string {
	if subscription.Cached(p, s) {
		return "yes (switching to it needs no network)"
	}
	return theme.StatusWarn.Render("no (it will be downloaded on first use)")
}

// promptRemoveSubscription lists the configured subscriptions and asks which one
// to delete, then dispatches `sub remove <index>`. It is the interactive-menu
// counterpart to the `sub remove` command.
// removeSubscriptionAt deletes the subscription the user picked in the menu,
// whose index arrives in args. Deleting rewrites the config with no undo, so it
// asks for a second confirmation that names the entry first.
func removeSubscriptionAt(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("sub-remove: expected a subscription index")
	}
	index, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid subscription index %q", args[0])
	}

	cfg, err := loadOrInit(paths.New())
	if err != nil {
		return err
	}
	if index < 0 || index >= len(cfg.Subscriptions) {
		return fmt.Errorf("no subscription at index %d", index)
	}
	s := cfg.Subscriptions[index]

	warning := "This cannot be undone — the entry and its cached copy are deleted from disk."
	if index == cfg.ActiveIndex() {
		warning = "This is the active subscription. Deleting it switches to the next enabled one (or to a direct-only config), and cannot be undone."
	}
	ok, err := runConfirm(confirmModel{
		title:    "Remove this subscription?",
		details:  []string{subscriptionName(index, s), s.URL},
		warning:  warning,
		yesLabel: "Delete it",
		danger:   true,
	})
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("cancelled")
		return nil
	}
	return dispatch("sub", []string{"remove", strconv.Itoa(index)})
}

// printSubscriptions lists the configured subscriptions with their per-entry
// state, marking the active profile, plus the commands that edit them.
func printSubscriptions(p *paths.Paths, cfg *config.Config) {
	line := func(label, value string) string {
		return theme.OutputLabel.Render(label) + theme.OutputValue.Render(value) + "\n"
	}

	fmt.Print(line("config:   ", p.Config))
	fmt.Print(line("interval: ", cfg.RefreshInterval().String()+" (global default)"))
	fmt.Print(line("active:   ", activeSubLabel(cfg)))

	if len(cfg.Subscriptions) == 0 {
		fmt.Print(line("subs:     ", "none"))
		fmt.Println("\nadd one with:  zashhomo sub add <url>")
		return
	}

	fmt.Print(line("subs:     ", fmt.Sprintf("%d", len(cfg.Subscriptions))))
	fmt.Println()

	active := cfg.ActiveIndex()
	for i, s := range cfg.Subscriptions {
		marker := "  "
		if i == active {
			marker = theme.StatusOk.Render("▸ ")
		}
		fmt.Printf("%s[%d] %s%s\n", marker, i, theme.OutputValue.Render(s.DisplayName(i)), subscriptionTags(s, i == active))
		fmt.Printf("      %s\n", theme.Hint.Render(s.URL))
		fmt.Printf("      %s\n", theme.Hint.Render("updated "+lastUpdated(s)+" · "+scheduleSummary(cfg, s)))
	}

	fmt.Println("\nswitch:  zashhomo sub switch <index>          (make it the active profile)")
	fmt.Println("update:  zashhomo sub update [index]          (refresh one, or every enabled one)")
	fmt.Println("pause:   zashhomo sub enable|disable <index>")
	fmt.Println("timer:   zashhomo sub auto <index> on|off")
	fmt.Println("         zashhomo sub interval <index> <dur>  ('default' follows the global one)")
	fmt.Println("remove:  zashhomo sub remove <index>          (delete a subscription)")
	fmt.Println("edit:    zashhomo sub edit                    (open config file)")
}

// scheduleSummary describes a subscription's refresh timer in one phrase.
func scheduleSummary(cfg *config.Config, s config.Subscription) string {
	if !s.AutoUpdate() {
		return "scheduled update off"
	}
	return "every " + s.RefreshIntervalOr(cfg.RefreshInterval()).String() + intervalOrigin(s)
}

// openInEditor opens path in the user's editor: $VISUAL or $EDITOR when set,
// otherwise the platform default (notepad on Windows, `open -t` on macOS,
// xdg-open elsewhere). Stdio is inherited so terminal editors work in place.
func openInEditor(path string) error {
	var name string
	var args []string
	if ed := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR")); ed != "" {
		fields := strings.Fields(ed)
		name, args = fields[0], append(fields[1:], path)
	} else {
		switch runtime.GOOS {
		case "windows":
			name, args = "notepad", []string{path}
		case "darwin":
			name, args = "open", []string{"-t", path}
		default:
			// Linux/BSD: check if xdg-open exists before using it
			if _, err := exec.LookPath("xdg-open"); err == nil {
				name, args = "xdg-open", []string{path}
			} else {
				// No GUI available - provide helpful error message
				return fmt.Errorf("no editor found. Set $VISUAL or $EDITOR, or edit the file directly:\n  %s", path)
			}
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	fmt.Printf("opening %s in %s…\n", path, name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open editor (%s): %w; edit the file directly: %s", name, err, path)
	}
	return nil
}

// popFlag removes every occurrence of name from args, reporting whether it was
// present. Used to lift a boolean flag out of the argument list before passing
// the remainder to a parser that doesn't define it.
func popFlag(args []string, name string) (found bool, rest []string) {
	rest = make([]string, 0, len(args))
	for _, a := range args {
		if a == name {
			found = true
			continue
		}
		rest = append(rest, a)
	}
	return found, rest
}

// firstNonEmpty returns the first non-empty string in vals, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func cmdUninstall(args []string) error {
	if elevated, err := ensureElevated("Uninstalling the service", append([]string{"uninstall"}, args...)); err != nil || elevated {
		return err
	}

	purge := false
	for _, a := range args {
		if a == "--purge" {
			purge = true
		}
	}
	// Stopping the service and waiting out the service manager's deletion can
	// both take seconds, so animate them rather than sitting silent.
	if err := ui.Run("Removing service", "✓", svc.Uninstall); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	if err := ui.Run("Removing global command", "✓", selfinstall.Uninstall); err != nil {
		fmt.Fprintf(os.Stderr, "note: %v (remove it manually if desired)\n", err)
	}
	if purge {
		if err := ui.Run("Removing data and config", "✓", func() error {
			p := paths.New()
			for _, dir := range []string{p.Data, filepath.Dir(p.Config)} {
				if err := os.RemoveAll(dir); err != nil {
					return fmt.Errorf("remove %s: %w", dir, err)
				}
			}
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
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
