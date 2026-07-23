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
		return svc.Control(action)
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
  zashhomo system-proxy enable|disable
                              Set or clear the OS system proxy (points at the mixed-port)
  zashhomo update [flags]     Update components (--core --ui --self --all)
  zashhomo sub add <url>      Add a subscription
  zashhomo sub remove <index> Remove the subscription at <index> (see 'sub list')
  zashhomo sub list           List subscriptions (metadata + edit hints)
  zashhomo sub edit           Open the config file in your editor
  zashhomo sub interval [dur] Show or set the global refresh interval (e.g. 6h)
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

	// With --force, remove any existing registration first so the (re)install
	// doesn't fail with "service already exists".
	if force && svc.GetState().Installed {
		fmt.Println("• Removing existing service…")
		if err := svc.Uninstall(); err != nil {
			return err
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
	for {
		// Rebuild the menu (and its banner+status header) from the current state
		// each loop so ordering, greyed items, and the status block reflect
		// installs/starts made moments ago.
		st := svc.GetState()
		it, err := runMenu(st, menuHeader(st))
		if err != nil {
			return err
		}
		if it.action == "" || it.action == "exit" {
			return nil
		}
		clearScreen()
		// A greyed action doesn't apply in the current state; make the user
		// confirm before forcing it through.
		if it.disabled != "" && !confirmForce(it.label, it.disabled) {
			continue
		}
		switch it.action {
		case "sub-add":
			url := promptLine("Subscription URL (blank to cancel): ")
			if url == "" {
				fmt.Println("cancelled")
			} else if err := dispatch("sub", []string{"add", url}); err != nil {
				printCmdError(err)
			}
		case "sub-remove":
			if err := promptRemoveSubscription(); err != nil {
				printCmdError(err)
			}
		case "sub-interval":
			val := promptLine("Refresh interval (e.g. 6h, 30m; blank to cancel): ")
			if val == "" {
				fmt.Println("cancelled")
			} else if err := dispatch("sub", []string{"interval", val}); err != nil {
				printCmdError(err)
			}
		default:
			fields := strings.Fields(it.action)
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
		fmt.Print(line("proxy:   ", systemProxyStatus(cfg)))
		fmt.Print(line("mixed:   ", mixedProxyURL(cfg)))
		fmt.Print(line("tun:     ", tunStatus(cfg)))
		fmt.Print(line("panel:   ", panelURL(cfg)))
		fmt.Print(line("kernel:  ", orDash(cfg.CoreVersion)))
		fmt.Print(line("panelv:  ", orDash(cfg.UIVersion)))
		fmt.Print(line("subs:    ", fmt.Sprintf("%d", len(cfg.Subscriptions))))
	}
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
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}
	switch args[0] {
	case "enable", "on":
		if err := sysproxy.Enable("127.0.0.1", cfg.MixedPort); err != nil {
			return err
		}
		cfg.SystemProxy = true
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("system proxy enabled (127.0.0.1:%d)\n", cfg.MixedPort)
		return nil
	case "disable", "off":
		if err := sysproxy.Disable(); err != nil {
			return err
		}
		cfg.SystemProxy = false
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("system proxy disabled")
		return nil
	default:
		return fmt.Errorf("system-proxy: unknown subcommand %q (want enable|disable)", args[0])
	}
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
		fmt.Println("restart the service to apply:  zashhomo service restart")
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
	fmt.Println("  restart the service to apply:  zashhomo service restart")
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
		return fmt.Errorf("sub: expected 'add <url>', 'remove <index>', 'list', or 'update'")
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
			fmt.Println("run `zashhomo service restart` (or start the service) to apply")
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

	case "list", "ls":
		printSubscriptions(p, cfg)
		return nil

	case "show":
		if len(args) < 2 {
			return fmt.Errorf("sub show: missing <index>")
		}
		var index int
		if _, err := fmt.Sscanf(args[1], "%d", &index); err != nil {
			return fmt.Errorf("sub show: invalid index %q", args[1])
		}
		if index < 0 || index >= len(cfg.Subscriptions) {
			return fmt.Errorf("sub show: index %d out of range (0-%d)", index, len(cfg.Subscriptions)-1)
		}
		sub := cfg.Subscriptions[index]
		name := sub.Name
		if name == "" {
			name = fmt.Sprintf("sub-%d", index)
		}

		// Display subscription details with themed output
		line := func(label, value string) string {
			return theme.OutputLabel.Render(label) + theme.OutputValue.Render(value) + "\n"
		}

		fmt.Print(line("name:  ", name))
		fmt.Print(line("url:   ", sub.URL))
		fmt.Print(line("index: ", fmt.Sprintf("%d", index)))
		return nil

	case "remove", "rm", "del", "delete":
		if len(args) < 2 {
			return fmt.Errorf("sub remove: missing <index>")
		}
		var index int
		if _, err := fmt.Sscanf(args[1], "%d", &index); err != nil {
			return fmt.Errorf("sub remove: invalid index %q", args[1])
		}
		if index < 0 || index >= len(cfg.Subscriptions) {
			return fmt.Errorf("sub remove: index %d out of range (0-%d)", index, len(cfg.Subscriptions)-1)
		}
		removed := cfg.Subscriptions[index]
		name := removed.Name
		if name == "" {
			name = fmt.Sprintf("sub-%d", index)
		}
		if err := cfg.RemoveSubscription(index); err != nil {
			return err
		}
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("removed subscription %q (%d remaining)\n", name, len(cfg.Subscriptions))
		// Try a live reload; ignore errors when the service isn't running.
		if err := subscription.Reload(context.Background(), cfg, p.MihomoConfig()); err == nil {
			fmt.Println("kernel reloaded")
		} else {
			fmt.Println("run `zashhomo service restart` (or start the service) to apply")
		}
		return nil

	case "edit":
		return openInEditor(p.Config)

	case "interval":
		if len(args) < 2 {
			fmt.Printf("refresh interval: %s\n", cfg.RefreshInterval())
			fmt.Println("set with:  zashhomo sub interval <duration>   (e.g. 6h, 30m, 90m)")
			return nil
		}
		if err := cfg.SetRefreshInterval(args[1]); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		// Regenerate config so the new interval reaches the proxy-providers, then
		// hot-reload if the kernel is up. The daemon's own refresh loop reads the
		// interval at startup, so a restart is still needed to change its cadence.
		if err := subscription.GenerateConfig(p, cfg); err != nil {
			return err
		}
		fmt.Printf("refresh interval set to %s\n", cfg.RefreshInterval())
		if err := subscription.Reload(context.Background(), cfg, p.MihomoConfig()); err == nil {
			fmt.Println("kernel reloaded")
		}
		fmt.Println("restart the service to apply the daemon refresh cycle:  zashhomo service restart")
		return nil

	default:
		return fmt.Errorf("sub: unknown subcommand %q", args[0])
	}
}

// promptRemoveSubscription lists the configured subscriptions and asks which one
// to delete, then dispatches `sub remove <index>`. It is the interactive-menu
// counterpart to the `sub remove` command.
func promptRemoveSubscription() error {
	p := paths.New()
	cfg, err := loadOrInit(p)
	if err != nil {
		return err
	}
	if len(cfg.Subscriptions) == 0 {
		fmt.Println("no subscriptions to remove")
		return nil
	}
	for i, s := range cfg.Subscriptions {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("sub-%d", i)
		}
		fmt.Printf("  [%d] %s\n", i, theme.OutputValue.Render(name))
		fmt.Printf("      %s\n", theme.Hint.Render(s.URL))
	}
	in := promptLine("\nIndex to remove (blank to cancel): ")
	if in == "" {
		fmt.Println("cancelled")
		return nil
	}
	var index int
	if _, err := fmt.Sscanf(in, "%d", &index); err != nil {
		return fmt.Errorf("invalid index %q", in)
	}
	return dispatch("sub", []string{"remove", fmt.Sprintf("%d", index)})
}

// printSubscriptions lists the configured subscriptions with their metadata and
// the config path, plus the commands that edit them.
func printSubscriptions(p *paths.Paths, cfg *config.Config) {
	line := func(label, value string) string {
		return theme.OutputLabel.Render(label) + theme.OutputValue.Render(value) + "\n"
	}

	fmt.Print(line("config:   ", p.Config))
	fmt.Print(line("interval: ", cfg.RefreshInterval().String()))

	if len(cfg.Subscriptions) == 0 {
		fmt.Print(line("subs:     ", "none"))
		fmt.Println("\nadd one with:  zashhomo sub add <url>")
		return
	}

	fmt.Print(line("subs:     ", fmt.Sprintf("%d", len(cfg.Subscriptions))))
	fmt.Println()

	for i, s := range cfg.Subscriptions {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("sub-%d", i)
		}
		fmt.Printf("  [%d] %s\n", i, theme.OutputValue.Render(name))
		fmt.Printf("      %s\n", theme.Hint.Render(s.URL))
	}

	fmt.Println("\nedit:    zashhomo sub edit             (open config file)")
	fmt.Println("         zashhomo sub interval <dur>  (set refresh interval, e.g. 6h)")
	fmt.Println("remove:  zashhomo sub remove <index>  (delete a subscription)")
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
