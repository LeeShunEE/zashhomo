package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/svc"
	"github.com/LeeShunEE/zashhomo/internal/ui"
)

func TestOnboardStepsOrder(t *testing.T) {
	steps := onboardSteps(svc.State{}, config.Default())
	want := []string{
		"Install the service",
		"Add a subscription",
		"Restart the service",
		"Enable the system proxy",
		"Open the dashboard",
	}
	if len(steps) != len(want) {
		t.Fatalf("got %d steps, want %d", len(steps), len(want))
	}
	for i, w := range want {
		if steps[i].title != w {
			t.Errorf("step %d = %q, want %q", i+1, steps[i].title, w)
		}
		if steps[i].run == nil {
			t.Errorf("step %q has no run func", steps[i].title)
		}
	}
}

func TestOnboardStepsMarkSatisfied(t *testing.T) {
	fresh := config.Default()
	configured := config.Default()
	configured.AddSubscription("home", "https://e.example/sub")
	configured.SystemProxy = true

	tests := []struct {
		name     string
		st       svc.State
		cfg      *config.Config
		wantDone map[string]bool // title -> done
	}{
		{
			name: "nothing set up",
			st:   svc.State{},
			cfg:  fresh,
			wantDone: map[string]bool{
				"Install the service":     false,
				"Add a subscription":      false,
				"Enable the system proxy": false,
			},
		},
		{
			name: "fully set up",
			st:   svc.State{Installed: true, Running: true},
			cfg:  configured,
			wantDone: map[string]bool{
				"Install the service":     true,
				"Add a subscription":      true,
				"Enable the system proxy": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, step := range onboardSteps(tt.st, tt.cfg) {
				want, checked := tt.wantDone[step.title]
				if !checked {
					continue
				}
				if step.done != want {
					t.Errorf("step %q done = %v, want %v", step.title, step.done, want)
				}
				if step.done && step.doneNote == "" {
					t.Errorf("step %q is done but explains nothing", step.title)
				}
			}
		})
	}
}

// The guide is walked in a terminal the user can scroll back through, so the
// panel line must not carry the login token.
func TestOnboardHidesPanelToken(t *testing.T) {
	cfg := &config.Config{WebAddr: "127.0.0.1:9191", Secret: "s3cr3t"}
	for _, step := range onboardSteps(svc.State{Installed: true, Running: true}, cfg) {
		if strings.Contains(step.state, "s3cr3t") {
			t.Errorf("step %q leaks the secret: %q", step.title, step.state)
		}
	}
	if got := panelAddrOnly(cfg); got != "http://127.0.0.1:9191" {
		t.Errorf("panelAddrOnly = %q, want the bare endpoint", got)
	}
	if panelAddrOnly(nil) != "-" {
		t.Error("panelAddrOnly(nil) should render a dash")
	}
}

// Typing is reserved for the one thing that cannot be picked from a list: the
// subscription URL. Every other step is answered with the arrow keys alone.
func TestOnboardSubscriptionStepPrompts(t *testing.T) {
	steps := onboardSteps(svc.State{}, config.Default())
	sub := steps[1]
	if sub.prompt == "" {
		t.Fatal("the subscription step must ask for a URL")
	}
	if !strings.Contains(sub.prompt, "blank to cancel") {
		t.Errorf("prompt should say a blank answer backs out, got %q", sub.prompt)
	}
	for i, step := range steps {
		if i != 1 && step.prompt != "" {
			t.Errorf("step %q should not prompt for input", step.title)
		}
	}
}

// The cursor starts on the harmless answer: run a step still outstanding, leave
// an already-satisfied one alone. Enter alone must never redo finished work.
func TestOnboardOptionsDefaultToTheSafeAnswer(t *testing.T) {
	tests := []struct {
		name string
		step onboardStep
		want onboardAction
	}{
		{"outstanding", onboardStep{runLabel: "Install it now"}, actRun},
		{"already done", onboardStep{runLabel: "Install it now", done: true, doneNote: "registered"}, actSkip},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels, actions := onboardOptions(tt.step)
			if len(labels) != len(actions) {
				t.Fatalf("%d labels but %d actions", len(labels), len(actions))
			}
			if actions[0] != tt.want {
				t.Errorf("default option = %v, want %v (labels %q)", actions[0], tt.want, labels)
			}
			if actions[len(actions)-1] != actQuit {
				t.Error("quitting the guide must be the last option")
			}
			if !slices.Contains(actions, actRun) || !slices.Contains(actions, actSkip) {
				t.Errorf("every step must offer both run and skip, got %q", labels)
			}
			if !slices.Contains(labels, tt.step.runLabel) {
				t.Errorf("the step's own run label %q is missing from %q", tt.step.runLabel, labels)
			}
		})
	}
}

// Esc must not be read as "pick whatever the cursor is on" — the guide treats an
// unanswered chooser as quitting, so the sentinel has to survive.
func TestOnboardChoiceEscapes(t *testing.T) {
	m := onboardChoice{
		heading: "Step 1/5 · Install the service",
		step:    onboardStep{why: "why", state: "service: not installed"},
		options: []string{"Install it now", "Skip this step", "Quit the guide"},
		choice:  -1,
	}

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if final := got.(onboardChoice); final.choice != -1 {
		t.Errorf("Esc chose option %d, want no choice at all", final.choice)
	}

	// Down then Enter picks the second option; the cursor stops at the last one.
	moved, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got, _ = moved.(onboardChoice).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if final := got.(onboardChoice); final.choice != 1 {
		t.Errorf("choice = %d, want 1", final.choice)
	}

	end := m
	for i := 0; i < 5; i++ {
		next, _ := end.Update(tea.KeyMsg{Type: tea.KeyDown})
		end = next.(onboardChoice)
	}
	if end.cursor != len(m.options)-1 {
		t.Errorf("cursor = %d, want it clamped to %d", end.cursor, len(m.options)-1)
	}
}

func TestOnboardChoiceViewShowsContext(t *testing.T) {
	view := onboardChoice{
		heading: "Step 2/5 · Add a subscription",
		step: onboardStep{
			why:      "A subscription supplies the proxy nodes.",
			state:    "subscriptions: 1",
			done:     true,
			doneNote: "1 already configured",
		},
		options: []string{"Skip — already done", "Add another subscription", "Quit the guide"},
	}.View()

	// The alternate screen hides anything printed before it, so the step's
	// explanation has to be inside the chooser itself.
	for _, want := range []string{
		"Step 2/5 · Add a subscription",
		"A subscription supplies the proxy nodes.",
		"subscriptions: 1",
		"1 already configured",
		"Skip — already done",
		"Add another subscription",
		"Quit the guide",
		"↑/↓",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("chooser view missing %q:\n%s", want, view)
		}
	}
}

func TestOnboardMarker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZASHHOMO_STATE_DIR", filepath.Join(dir, "state"))
	p := paths.New()

	if onboardOffered(p) {
		t.Fatal("a fresh state dir must not look already-onboarded")
	}
	markOnboarded(p)
	if !onboardOffered(p) {
		t.Fatal("the marker was not recorded")
	}
	if _, err := os.Stat(p.OnboardMark()); err != nil {
		t.Fatalf("marker file missing: %v", err)
	}

	// Writing it twice is harmless.
	markOnboarded(p)
	if !onboardOffered(p) {
		t.Fatal("re-marking cleared the marker")
	}
}

// offerOnboarding must ask at most once: the second call reports "no prompt
// needed" without touching the terminal, which is what makes it safe to call on
// every menu launch.
func TestOfferOnboardingAsksOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZASHHOMO_STATE_DIR", filepath.Join(dir, "state"))
	p := paths.New()

	markOnboarded(p)
	start, err := offerOnboarding(p)
	if err != nil {
		t.Fatalf("offerOnboarding after marking: %v", err)
	}
	if start {
		t.Fatal("an already-onboarded user was offered the guide again")
	}
}

func TestOnboardNeedsTerminal(t *testing.T) {
	// go test runs with stdin/stdout redirected, so this exercises the non-TTY
	// guard — the guide must refuse rather than treat EOF as "press Enter". Bail
	// out if someone runs the test binary straight from a terminal: there the
	// call would start the real guide and offer to install the service.
	if ui.IsTerminal(os.Stdin) && ui.IsTerminal(os.Stdout) {
		t.Skip("stdio is a terminal; the guide would actually run")
	}
	err := cmdOnboard()
	if err == nil {
		t.Fatal("cmdOnboard should refuse to run without a terminal")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("unexpected error: %v", err)
	}
}
