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

// The remove flow must be a submenu the user arrows through, never a prompt that
// deletes on a bare action.
func TestRootMenuRemoveIsSubmenu(t *testing.T) {
	it, ok := findItem(rootMenu(svc.State{Installed: true, Running: true}), "Remove subscription ▸")
	if !ok {
		t.Fatal("subscriptions menu has no 'Remove subscription ▸' entry")
	}
	if len(it.sub) == 0 {
		t.Fatal("'Remove subscription ▸' has no submenu")
	}
	if it.action != "" {
		t.Errorf("submenu item must not carry an action, got %q", it.action)
	}
	for _, row := range it.sub {
		switch {
		case row.action == "noop":
			// placeholder shown when nothing is configured
		case strings.HasPrefix(row.action, "sub-remove "):
			// carries the index to delete
		default:
			t.Errorf("unexpected remove-menu action %q", row.action)
		}
	}
}

// Placeholder rows must never quit the menu: runMenu treats an empty action as
// "exit", so they carry the "noop" sentinel instead.
func TestPlaceholderRowsAreNoop(t *testing.T) {
	for _, items := range [][]menuItem{subscriptionMenu(), removeSubscriptionMenu()} {
		for _, it := range items {
			if it.disabled != "" && it.action == "" && len(it.sub) == 0 {
				t.Errorf("placeholder %q has an empty action and would exit the menu", it.label)
			}
		}
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
