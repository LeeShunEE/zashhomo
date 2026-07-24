package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/svc"
)

func TestSubscriptionName(t *testing.T) {
	tests := []struct {
		name string
		i    int
		sub  config.Subscription
		want string
	}{
		{"named", 0, config.Subscription{Name: "home", URL: "https://e.example/x"}, "home"},
		{"unnamed falls back to index", 2, config.Subscription{URL: "https://e.example/x"}, "sub-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subscriptionName(tt.i, tt.sub); got != tt.want {
				t.Errorf("subscriptionName(%d, %+v) = %q, want %q", tt.i, tt.sub, got, tt.want)
			}
		})
	}
}

// findItem returns the item whose label matches, searching one submenu deep.
func findItem(items []menuItem, label string) (menuItem, bool) {
	for _, it := range items {
		if it.label == label {
			return it, true
		}
		if found, ok := findItem(it.sub, label); ok {
			return found, true
		}
	}
	return menuItem{}, false
}

func TestRootMenuDashboardEntry(t *testing.T) {
	tests := []struct {
		name         string
		st           svc.State
		wantDisabled bool
	}{
		{"running", svc.State{Installed: true, Running: true}, false},
		{"stopped", svc.State{Installed: true}, true},
		{"not installed", svc.State{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it, ok := findItem(rootMenu(tt.st), "Open dashboard")
			if !ok {
				t.Fatal("root menu has no 'Open dashboard' entry")
			}
			if it.action != "dashboard" {
				t.Errorf("action = %q, want %q", it.action, "dashboard")
			}
			if got := it.disabled != ""; got != tt.wantDisabled {
				t.Errorf("disabled = %q, want disabled: %v", it.disabled, tt.wantDisabled)
			}
		})
	}
}

// The guided setup sits at the bottom of the root menu, above Version/Help/Exit,
// and stays available in every state.
func TestRootMenuGuidedSetup(t *testing.T) {
	states := []svc.State{
		{},
		{Installed: true},
		{Installed: true, Running: true},
	}
	for _, st := range states {
		root := rootMenu(st)
		idx, verIdx := -1, -1
		for i, it := range root {
			switch it.label {
			case "Guided setup":
				idx = i
			case "Version":
				verIdx = i
			}
		}
		if idx < 0 {
			t.Fatalf("state %+v: root menu has no 'Guided setup' entry", st)
		}
		if root[idx].action != "onboard" {
			t.Errorf("state %+v: action = %q, want %q", st, root[idx].action, "onboard")
		}
		if root[idx].disabled != "" {
			t.Errorf("state %+v: guided setup should never be greyed, got %q", st, root[idx].disabled)
		}
		if idx != verIdx-1 {
			t.Errorf("state %+v: guided setup at %d, want directly above Version at %d", st, idx, verIdx)
		}
	}
}

func TestRootMenuUpdateLabel(t *testing.T) {
	root := rootMenu(svc.State{Installed: true, Running: true})
	if _, ok := findItem(root, "Software Update ▸"); !ok {
		t.Error("root menu has no 'Software Update ▸' entry")
	}
	if _, ok := findItem(root, "Update ▸"); ok {
		t.Error("root menu still carries the old 'Update ▸' label")
	}
}

// testConfig is a three-subscription config covering every per-entry state the
// menu renders: active, plain enabled, disabled, and auto-update off.
func testConfig() *config.Config {
	return &config.Config{
		SubInterval: "12h",
		Subscriptions: []config.Subscription{
			{ID: "a", Name: "alpha", URL: "https://e.example/a"},
			{ID: "b", Name: "beta", URL: "https://e.example/b", NoAutoUpdate: true},
			{ID: "c", Name: "gamma", URL: "https://e.example/c", Disabled: true},
		},
		ActiveSub: "a",
	}
}

// The Subscriptions menu must lead with the active profile, since every other
// action on the page is relative to it.
func TestSubscriptionsMenuLeadsWithActive(t *testing.T) {
	items := subscriptionsMenu(testConfig())
	if len(items) == 0 {
		t.Fatal("subscriptions menu is empty")
	}
	first := items[0]
	if !first.info {
		t.Errorf("first row %q is selectable; the active line must be an info row", first.label)
	}
	if !strings.Contains(first.label, "Current active:") || !strings.Contains(first.label, "alpha") {
		t.Errorf("first row = %q, want the active subscription named", first.label)
	}
}

// Switching is an arrow-key pick. The active row and any disabled row are inert:
// they carry the "noop" sentinel and say why they can't be chosen, rather than
// dispatching a command that would fail.
func TestSwitchSubscriptionMenuGreysUnavailableRows(t *testing.T) {
	items := switchSubscriptionMenu(testConfig())
	if len(items) != 3 {
		t.Fatalf("got %d rows, want one per subscription", len(items))
	}

	if items[0].action != "noop" {
		t.Errorf("active row action = %q, want the inert sentinel", items[0].action)
	}
	if !strings.Contains(items[0].disabled, "already active") {
		t.Errorf("active row reason = %q, want 'already active'", items[0].disabled)
	}
	if items[1].action != "sub switch 1" {
		t.Errorf("switchable row action = %q, want %q", items[1].action, "sub switch 1")
	}
	if items[1].disabled != "" {
		t.Errorf("an enabled, inactive subscription must be selectable, got reason %q", items[1].disabled)
	}
	if items[2].action != "noop" || items[2].disabled == "" {
		t.Errorf("disabled row = %+v, want an inert, greyed row", items[2])
	}
}

// Everything the user can do to a single subscription lives on its detail page.
func TestSubscriptionDetailMenuActions(t *testing.T) {
	cfg := testConfig()

	// The active subscription cannot be re-activated, and its auto-update is on,
	// so the toggle offers to turn it off.
	active := subscriptionDetailMenu(cfg, 0)
	wantActive := map[string]string{
		"Set as active":             "noop",
		"Update now":                "sub update 0",
		"Disable this subscription": "sub disable 0",
		"Turn scheduled update off": "sub auto 0 off",
		"Change update interval…":   "sub-interval-at 0",
		"Delete this subscription":  "sub-remove 0",
	}
	assertActions(t, "active detail", active, wantActive)

	// A disabled subscription offers to be enabled and cannot be activated.
	disabled := subscriptionDetailMenu(cfg, 2)
	assertActions(t, "disabled detail", disabled, map[string]string{
		"Set as active":             "noop",
		"Enable this subscription":  "sub enable 2",
		"Turn scheduled update off": "sub auto 2 off",
	})
	if it, _ := findItem(disabled, "Set as active"); it.disabled == "" {
		t.Error("'Set as active' must be greyed for a disabled subscription")
	}

	// One with auto-update off offers to turn it back on.
	if it, ok := findItem(subscriptionDetailMenu(cfg, 1), "Turn scheduled update on"); !ok {
		t.Error("detail page of a manual subscription has no 'Turn scheduled update on'")
	} else if it.action != "sub auto 1 on" {
		t.Errorf("action = %q, want %q", it.action, "sub auto 1 on")
	}
}

// assertActions checks that each labelled row exists and carries want[label].
func assertActions(t *testing.T, ctx string, items []menuItem, want map[string]string) {
	t.Helper()
	for label, action := range want {
		it, ok := findItem(items, label)
		if !ok {
			t.Errorf("%s: no row labelled %q", ctx, label)
			continue
		}
		if it.action != action {
			t.Errorf("%s: %q action = %q, want %q", ctx, label, it.action, action)
		}
	}
}

// The detail page opens with the subscription's state, which the cursor skips.
func TestSubscriptionDetailMenuOpensWithInfoRows(t *testing.T) {
	items := subscriptionDetailMenu(testConfig(), 0)
	if !items[0].info {
		t.Fatalf("first detail row %q is not an info row", items[0].label)
	}
	if got := firstSelectable(items); items[got].label != "Set as active" {
		t.Errorf("cursor starts on %q, want the first real action", items[got].label)
	}
}

// Placeholder rows must never quit the menu: runMenu treats an empty action as
// "exit", so they carry the "noop" sentinel instead.
func TestPlaceholderRowsAreNoop(t *testing.T) {
	empty := &config.Config{}
	for _, items := range [][]menuItem{subscriptionMenu(empty), switchSubscriptionMenu(empty), subscriptionsMenu(empty)} {
		for _, it := range items {
			if it.info {
				continue
			}
			if it.disabled != "" && it.action == "" && len(it.sub) == 0 {
				t.Errorf("placeholder %q has an empty action and would exit the menu", it.label)
			}
		}
	}
}

// The cursor must never land on an info row, in either direction or on wrap.
func TestMoveCursorSkipsInfoRows(t *testing.T) {
	items := []menuItem{
		{label: "info", info: true},
		{label: "first", action: "a"},
		{label: "second", action: "b"},
	}
	if got := firstSelectable(items); got != 1 {
		t.Errorf("firstSelectable = %d, want 1", got)
	}
	// Up from the first action wraps past the info row to the last action.
	if got := moveCursor(items, 1, -1); got != 2 {
		t.Errorf("moveCursor(up from 1) = %d, want 2", got)
	}
	// Down from the last action wraps past the info row to the first action.
	if got := moveCursor(items, 2, 1); got != 1 {
		t.Errorf("moveCursor(down from 2) = %d, want 1", got)
	}
	// A list of nothing but info rows leaves the cursor where it is.
	if got := moveCursor([]menuItem{{info: true}}, 0, 1); got != 0 {
		t.Errorf("moveCursor(all info) = %d, want 0", got)
	}
}

func confirmKey(m confirmModel, key string) confirmModel {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return next.(confirmModel)
}

func TestConfirmModelDefaultsToCancel(t *testing.T) {
	m := confirmModel{title: "Remove this subscription?", yesLabel: "Delete it"}

	// Enter without moving the cursor must not confirm — this is the second gate
	// in front of an irreversible delete.
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got.(confirmModel).answer {
		t.Fatal("Enter on the default cursor confirmed the delete")
	}

	// Moving down and pressing Enter confirms.
	moved, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got, _ = moved.(confirmModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !got.(confirmModel).answer {
		t.Fatal("Enter on the destructive option did not confirm")
	}

	// Esc cancels even from the destructive option.
	got, _ = moved.(confirmModel).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got.(confirmModel).answer {
		t.Fatal("Esc confirmed the delete")
	}
}

// A benign prompt (the onboarding welcome) opts out of the destructive default:
// it starts on the confirming option so Enter accepts.
func TestConfirmModelBenignDefaultsToYes(t *testing.T) {
	m := confirmModel{
		title:    "Welcome to zashhomo",
		noLabel:  "Skip, go to the menu",
		yesLabel: "Start the guide",
		cursor:   1,
	}
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !got.(confirmModel).answer {
		t.Fatal("Enter on a benign prompt should accept")
	}
	view := m.View()
	if !strings.Contains(view, "Skip, go to the menu") {
		t.Errorf("custom decline label missing from view:\n%s", view)
	}
	// Esc still declines.
	got, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got.(confirmModel).answer {
		t.Fatal("Esc should decline even when the cursor starts on yes")
	}
}

func TestConfirmModelViewWarnsIrreversible(t *testing.T) {
	m := confirmModel{
		title:    "Remove this subscription?",
		details:  []string{"home", "https://e.example/sub"},
		warning:  "This cannot be undone.",
		yesLabel: "Delete it",
		danger:   true,
	}
	view := m.View()
	for _, want := range []string{"Remove this subscription?", "home", "https://e.example/sub", "cannot be undone", "Delete it", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirm view missing %q:\n%s", want, view)
		}
	}
	// j/k move the cursor like the main menu does.
	if confirmKey(m, "j").cursor != 1 {
		t.Error("j did not move the cursor onto the destructive option")
	}
}
