package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	// runLabel names the "do it" option, phrased for this step rather than as a
	// generic "run this step".
	runLabel string
	// prompt asks for free-form input before running; an empty answer cancels the
	// step. Empty for steps that need no input — which is most of them, because
	// every other decision is made with the arrow keys.
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
		title:    "Install the service",
		why:      "Downloads the mihomo kernel and the zashboard panel, writes the config, then registers and starts the background service.",
		state:    "service: not installed",
		runLabel: "Install it now",
		run:      func(string) error { return dispatch("install", nil) },
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
		title:    "Add a subscription",
		why:      "A subscription supplies the proxy nodes. Without one the kernel starts but has nothing to route through.",
		state:    fmt.Sprintf("subscriptions: %d", subs),
		runLabel: "Add a subscription",
		// The only step that needs the keyboard: a URL cannot be picked from a list.
		prompt: "Subscription URL (blank to cancel): ",
		run: func(url string) error {
			return dispatch("sub", []string{"add", url})
		},
	}
	if subs > 0 {
		subscribe.done = true
		subscribe.doneNote = fmt.Sprintf("%d already configured", subs)
		subscribe.runLabel = "Add another subscription"
	}

	restart := onboardStep{
		title:    "Restart the service",
		why:      "Restarting makes the kernel pick up the generated config. Adding a subscription already tries a live reload, so this is a safety net.",
		state:    "service: not installed",
		runLabel: "Restart it now",
		run:      func(string) error { return dispatch("service", []string{"restart"}) },
	}
	if st.Installed {
		restart.state = "service: stopped"
		if st.Running {
			restart.state = "service: running"
		}
	}

	proxy := onboardStep{
		title:    "Enable the system proxy",
		why:      "Points the OS at the local mixed port so ordinary apps go through mihomo without per-app configuration.",
		state:    "system proxy: off",
		runLabel: "Enable it now",
		run:      func(string) error { return dispatch("system-proxy", []string{"enable"}) },
	}
	if proxyOn {
		proxy.done = true
		proxy.doneNote = "zashhomo already manages the system proxy"
		proxy.state = "system proxy: on"
	}

	panelStep := onboardStep{
		title:    "Open the dashboard",
		why:      "Opens zashboard in your default browser, already logged in via the token in the URL.",
		state:    "panel: " + panelAddrOnly(cfg),
		runLabel: "Open it now",
		run:      func(string) error { return dispatch("dashboard", nil) },
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
	fmt.Println("Five steps take you from nothing to a working proxy. Pick each answer with the")
	fmt.Println("arrow keys — every step is optional, and you can quit from any of them.")
	fmt.Println()

	ran, skipped := 0, 0
	for i, step := range steps {
		outcome, err := runOnboardStep(i+1, len(steps), step)
		if err != nil {
			return err
		}
		switch outcome {
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

// onboardAction is what the user picked for a step.
type onboardAction int

const (
	actRun onboardAction = iota
	actSkip
	actQuit
)

// onboardOptions lays out the choices for a step, with the safe answer first so
// the cursor starts there: a step still to do defaults to running it, one already
// satisfied defaults to leaving it alone.
func onboardOptions(step onboardStep) ([]string, []onboardAction) {
	runLabel := step.runLabel
	if runLabel == "" {
		runLabel = "Run this step"
	}
	if step.done {
		return []string{"Skip — already done", runLabel, "Quit the guide"},
			[]onboardAction{actSkip, actRun, actQuit}
	}
	return []string{runLabel, "Skip this step", "Quit the guide"},
		[]onboardAction{actRun, actSkip, actQuit}
}

// runOnboardStep asks what to do with one step and carries it out. The question
// is answered with the arrow keys; only a step whose prompt is set (the
// subscription URL) ever asks the user to type, and only after they chose to run
// it. A failing step is reported but does not end the guide unless the user says
// so.
func runOnboardStep(n, total int, step onboardStep) (onboardOutcome, error) {
	heading := fmt.Sprintf("Step %d/%d · %s", n, total, step.title)
	labels, actions := onboardOptions(step)

	choice, err := runOnboardChoice(onboardChoice{
		heading: heading,
		step:    step,
		options: labels,
	})
	if err != nil {
		return onboardQuit, err
	}

	// Escaping out of the chooser leaves the guide, same as picking "Quit".
	action := actQuit
	if choice >= 0 {
		action = actions[choice]
	}
	switch action {
	case actQuit:
		return onboardQuit, nil
	case actSkip:
		fmt.Printf("%s\n\n", theme.Hint.Render(heading+" — skipped"))
		return onboardSkipped, nil
	}

	// The alternate screen is gone by now, so reprint the heading: what follows is
	// command output and it needs to say which step produced it.
	fmt.Println(theme.Title.Render(heading))

	answer := ""
	if step.prompt != "" {
		answer = strings.TrimSpace(promptLine("  " + step.prompt))
		if answer == "" {
			fmt.Printf("%s\n\n", theme.Hint.Render("  nothing entered — step skipped"))
			return onboardSkipped, nil
		}
	}

	fmt.Println()
	if err := step.run(answer); err != nil {
		printCmdError(err)
		fmt.Println()
		carryOn, cerr := runConfirm(confirmModel{
			title:    "That step failed",
			details:  []string{step.title + " did not complete.", "The remaining steps do not depend on it finishing."},
			noLabel:  "Quit the guide",
			yesLabel: "Continue with the guide",
			cursor:   1,
		})
		if cerr != nil {
			return onboardQuit, cerr
		}
		if !carryOn {
			return onboardQuit, nil
		}
	}
	fmt.Println()
	return onboardRan, nil
}

// onboardChoice is the arrow-key prompt for a single step. The step's
// explanation rides inside the model rather than being printed beforehand,
// because the alternate screen would hide anything printed first.
type onboardChoice struct {
	heading string
	step    onboardStep
	options []string
	cursor  int
	choice  int // index picked; -1 until Enter, and left there on Esc
}

func (m onboardChoice) Init() tea.Cmd { return nil }

func (m onboardChoice) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "up", "k", "left", "h":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "right", "l", "tab":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case "enter":
		m.choice = m.cursor
		return m, tea.Quit
	}
	return m, nil
}

func (m onboardChoice) View() string {
	var card strings.Builder
	card.WriteString(theme.Title.Render(m.heading))
	// why is the only thing the run/skip decision rests on, so it is body text
	// rather than an aside: left unstyled, the brightest thing in the card.
	card.WriteString("\n\n  " + m.step.why)
	card.WriteString("\n  " + theme.OutputValue.Render(m.step.state))
	if m.step.done {
		card.WriteString("\n  " + theme.StatusOk.Render("✓ already done — "+m.step.doneNote))
	}

	var b strings.Builder
	b.WriteString(theme.Card.Render(card.String()))
	b.WriteString("\n\n")

	for i, opt := range m.options {
		prefix := "    "
		if i == m.cursor {
			prefix = "❯ "
			b.WriteString(theme.Selected.Render(prefix + opt))
		} else {
			b.WriteString(theme.MenuItem.Render(prefix + opt))
		}
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString("  " + theme.Hint.Render("↑/↓ move · enter select · esc quit the guide"))
	b.WriteByte('\n')
	return b.String()
}

// runOnboardChoice shows m on the alternate screen and returns the picked index,
// or -1 when the user escaped out.
func runOnboardChoice(m onboardChoice) (int, error) {
	m.choice = -1
	res, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return -1, err
	}
	final, ok := res.(onboardChoice)
	if !ok {
		return -1, nil
	}
	return final.choice, nil
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
