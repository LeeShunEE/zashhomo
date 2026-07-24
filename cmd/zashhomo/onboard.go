package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/svc"
	"github.com/LeeShunEE/zashhomo/internal/ui"
)

// onboardStep is one stage of the guided setup. done reports that the current
// state already satisfies the step, in which case doneNote says why and Enter
// skips instead of running it.
type onboardStep struct {
	title    string
	why      string // one line on what this step buys the user
	state    string // where things stand right now
	done     bool
	doneNote string
	// prompt asks for free-form input before running; an empty answer skips the
	// step. Nil for steps that need no input.
	prompt string
	// run executes the step. answer holds the prompt reply (empty when prompt is
	// unset).
	run func(answer string) error
}

// onboardSteps builds the five-stage guide for the current state. It is pure so
// the state-derived wording and skip decisions can be tested without touching
// the service manager.
func onboardSteps(st svc.State, cfg *config.Config) []onboardStep {
	subs := 0
	proxyOn := false
	if cfg != nil {
		subs = len(cfg.Subscriptions)
		proxyOn = cfg.SystemProxy
	}

	installed := onboardStep{
		title: "Install the service",
		why:   "Downloads the mihomo kernel and the zashboard panel, writes the config, then registers and starts the background service.",
		state: "service: not installed",
		run:   func(string) error { return dispatch("install", nil) },
	}
	if st.Installed {
		installed.done = true
		installed.doneNote = "the service is already registered"
		installed.state = "service: installed"
		if st.Running {
			installed.state = "service: installed and running"
		}
	}

	subscribe := onboardStep{
		title:  "Add a subscription",
		why:    "A subscription supplies the proxy nodes. Without one the kernel starts but has nothing to route through.",
		state:  fmt.Sprintf("subscriptions: %d", subs),
		prompt: "Subscription URL (blank to skip): ",
		run: func(url string) error {
			return dispatch("sub", []string{"add", url})
		},
	}
	if subs > 0 {
		subscribe.done = true
		subscribe.doneNote = fmt.Sprintf("%d already configured — run it again to add another", subs)
	}

	restart := onboardStep{
		title: "Restart the service",
		why:   "Restarting makes the kernel pick up the generated config. Adding a subscription already tries a live reload, so this is a safety net.",
		state: "service: not installed",
		run:   func(string) error { return dispatch("service", []string{"restart"}) },
	}
	if st.Installed {
		restart.state = "service: stopped"
		if st.Running {
			restart.state = "service: running"
		}
	}

	proxy := onboardStep{
		title: "Enable the system proxy",
		why:   "Points the OS at the local mixed port so ordinary apps go through mihomo without per-app configuration.",
		state: "system proxy: off",
		run:   func(string) error { return dispatch("system-proxy", []string{"enable"}) },
	}
	if proxyOn {
		proxy.done = true
		proxy.doneNote = "zashhomo already manages the system proxy"
		proxy.state = "system proxy: on"
	}

	panelStep := onboardStep{
		title: "Open the dashboard",
		why:   "Opens zashboard in your default browser, already logged in via the token in the URL.",
		state: "panel: " + panelAddrOnly(cfg),
		run:   func(string) error { return dispatch("dashboard", nil) },
	}

	return []onboardStep{installed, subscribe, restart, proxy, panelStep}
}

// panelAddrOnly renders the panel endpoint without its login token, which has no
// business being echoed into a terminal the user may scroll back through.
func panelAddrOnly(cfg *config.Config) string {
	if cfg == nil {
		return "-"
	}
	url := panelURL(cfg)
	if i := strings.Index(url, "/?token="); i >= 0 {
		return url[:i]
	}
	return url
}

// cmdOnboard walks the guided setup. Every step delegates to the same command
// the menu would run, so the guide teaches the real commands rather than a
// parallel implementation.
func cmdOnboard() error {
	// Each step prompts, and promptLine reads EOF as an empty line — which the
	// wizard treats as "go ahead". Behind a pipe that would silently install the
	// service, so require a real terminal.
	if !ui.IsTerminal(os.Stdin) || !ui.IsTerminal(os.Stdout) {
		return fmt.Errorf("the guided setup needs an interactive terminal; run the steps directly instead (see 'zashhomo help')")
	}

	p := paths.New()
	// The marker is written up front: having seen the guide once is what silences
	// the welcome prompt, whether or not the user finishes it.
	markOnboarded(p)

	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	steps := onboardSteps(svc.GetState(), cfg)

	fmt.Println(theme.OutputTitle.Render("zashhomo guided setup"))
	fmt.Println("Five steps take you from nothing to a working proxy. Every step is optional —")
	fmt.Println("skip anything you have already done, and quit whenever you like.")
	fmt.Println()

	ran, skipped := 0, 0
	for i, step := range steps {
		switch outcome := runOnboardStep(i+1, len(steps), step); outcome {
		case onboardRan:
			ran++
		case onboardSkipped:
			skipped++
		case onboardQuit:
			fmt.Println("\nguide stopped — reopen it any time from the menu or with `zashhomo onboard`")
			return nil
		}
	}

	fmt.Printf("\n%s %d step(s) run, %d skipped.\n", theme.StatusOk.Render("✓ Done."), ran, skipped)
	fmt.Println("The menu's status block at the top shows where things stand.")
	return nil
}

// onboardOutcome is what happened to a single step.
type onboardOutcome int

const (
	onboardRan onboardOutcome = iota
	onboardSkipped
	onboardQuit
)

// runOnboardStep prints one step, asks what to do, and carries it out. A failing
// step is reported but does not end the guide unless the user says so.
func runOnboardStep(n, total int, step onboardStep) onboardOutcome {
	fmt.Println(theme.Title.Render(fmt.Sprintf("Step %d/%d · %s", n, total, step.title)))
	fmt.Printf("  %s\n", theme.Hint.Render(step.why))
	fmt.Printf("  %s\n", theme.OutputValue.Render(step.state))
	if step.done {
		fmt.Printf("  %s\n", theme.StatusOk.Render("✓ already done — "+step.doneNote))
	}
	fmt.Println()

	answer := ""
	if step.prompt != "" {
		// The input doubles as the decision: a blank URL means "skip this step".
		fmt.Println(theme.Hint.Render("  [q] quit the guide"))
		answer = promptLine("  " + step.prompt)
		switch {
		case strings.EqualFold(answer, "q"):
			return onboardQuit
		case answer == "":
			fmt.Println(theme.Hint.Render("  skipped"))
			fmt.Println()
			return onboardSkipped
		}
	} else {
		hint := "  [Enter] run this step   [s] skip   [q] quit the guide"
		if step.done {
			// Enter leaves a satisfied step alone; redoing it takes a deliberate key.
			hint = "  [Enter] skip   [r] run it anyway   [q] quit the guide"
		}
		fmt.Println(theme.Hint.Render(hint))

		reply := strings.ToLower(promptLine("  > "))
		if reply == "q" {
			return onboardQuit
		}
		runIt := reply == "" || reply == "y"
		if step.done {
			runIt = reply == "r" || reply == "y"
		}
		if !runIt {
			fmt.Println(theme.Hint.Render("  skipped"))
			fmt.Println()
			return onboardSkipped
		}
	}

	fmt.Println()
	if err := step.run(answer); err != nil {
		printCmdError(err)
		fmt.Println()
		if strings.EqualFold(promptLine("That step failed. Continue with the guide? [Y/n]: "), "n") {
			return onboardQuit
		}
	}
	fmt.Println()
	return onboardRan
}

// onboardOffered reports whether this user has already been offered the guide.
func onboardOffered(p *paths.Paths) bool {
	_, err := os.Stat(p.OnboardMark())
	return err == nil
}

// markOnboarded records that the guide has been offered. Failing to write the
// marker only means the welcome prompt appears again next time, so the error is
// deliberately swallowed rather than pushed into the user's face.
func markOnboarded(p *paths.Paths) {
	if err := os.MkdirAll(filepath.Dir(p.OnboardMark()), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p.OnboardMark(), []byte("1\n"), 0o644)
}

// offerOnboarding shows the one-time welcome prompt and reports whether the user
// wants the guide. It is asked once per user: the marker is written whatever the
// answer is, so declining is remembered too.
func offerOnboarding(p *paths.Paths) (bool, error) {
	if onboardOffered(p) {
		return false, nil
	}
	markOnboarded(p)
	return runConfirm(confirmModel{
		title: "Welcome to zashhomo",
		details: []string{
			"The guided setup walks you through installing the service, adding a",
			"subscription, starting it up, and opening the dashboard.",
			"",
			"You can reopen it later from 'Guided setup' at the bottom of the menu.",
		},
		noLabel:  "Skip, go to the menu",
		yesLabel: "Start the guide",
		cursor:   1,
	})
}
