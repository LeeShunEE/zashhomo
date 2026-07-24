package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// The subscription step takes a URL, and a blank answer must mean "skip" rather
// than "add an empty subscription".
func TestOnboardSubscriptionStepPrompts(t *testing.T) {
	steps := onboardSteps(svc.State{}, config.Default())
	sub := steps[1]
	if sub.prompt == "" {
		t.Fatal("the subscription step must ask for a URL")
	}
	if !strings.Contains(sub.prompt, "blank to skip") {
		t.Errorf("prompt should say a blank answer skips, got %q", sub.prompt)
	}
	for i, step := range steps {
		if i != 1 && step.prompt != "" {
			t.Errorf("step %q should not prompt for input", step.title)
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
