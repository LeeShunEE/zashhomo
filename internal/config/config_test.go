package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.ControllerAddr != "127.0.0.1:9090" {
		t.Errorf("ControllerAddr = %q, want 127.0.0.1:9090", cfg.ControllerAddr)
	}
	if cfg.WebAddr != "127.0.0.1:9191" {
		t.Errorf("WebAddr = %q, want 127.0.0.1:9191", cfg.WebAddr)
	}
	if cfg.MixedPort != 9190 {
		t.Errorf("MixedPort = %d, want 9190", cfg.MixedPort)
	}
	if cfg.Secret == "" {
		t.Error("Secret should not be empty")
	}
	if cfg.SubInterval != "12h" {
		t.Errorf("SubInterval = %q, want 12h", cfg.SubInterval)
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Secret == "" {
		t.Error("expected auto-generated secret")
	}
}

func TestLoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `
controller_addr: 192.168.1.1:9090
secret: my-secret
web_addr: 0.0.0.0:8080
mixed_port: 7890
sub_interval: 6h
subscriptions:
  - name: sub1
    url: https://example.com/sub1
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ControllerAddr != "192.168.1.1:9090" {
		t.Errorf("ControllerAddr = %q", cfg.ControllerAddr)
	}
	if cfg.Secret != "my-secret" {
		t.Errorf("Secret = %q", cfg.Secret)
	}
	if cfg.WebAddr != "0.0.0.0:8080" {
		t.Errorf("WebAddr = %q", cfg.WebAddr)
	}
	if cfg.MixedPort != 7890 {
		t.Errorf("MixedPort = %d", cfg.MixedPort)
	}
	if len(cfg.Subscriptions) != 1 {
		t.Errorf("Subscriptions length = %d", len(cfg.Subscriptions))
	}
}

func TestLoadPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.yaml")

	// Only partial fields, others should use defaults
	content := `secret: partial-secret
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Secret != "partial-secret" {
		t.Errorf("Secret = %q", cfg.Secret)
	}
	if cfg.ControllerAddr != "127.0.0.1:9090" {
		t.Errorf("ControllerAddr should default: %q", cfg.ControllerAddr)
	}
	if cfg.MixedPort != 9190 {
		t.Errorf("MixedPort should default: %d", cfg.MixedPort)
	}
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save-test.yaml")

	cfg := Default()
	cfg.path = path
	cfg.AddSubscription("test-sub", "https://example.com/test")

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Reload and verify
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}
	if len(loaded.Subscriptions) != 1 {
		t.Errorf("subscriptions length = %d", len(loaded.Subscriptions))
	}
	if loaded.Secret != cfg.Secret {
		t.Errorf("Secret mismatch")
	}
}

func TestSaveWithoutPath(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Save(); err == nil {
		t.Error("expected error for Save without path")
	}
}

func TestRefreshInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected int // hours
	}{
		{"6h", 6},
		{"30m", 0}, // less than 1 hour
		{"12h", 12},
		{"24h", 24},
		{"", 12}, // default
	}

	for _, tt := range tests {
		cfg := &Config{SubInterval: tt.input}
		got := cfg.RefreshInterval()
		if tt.expected > 0 && got.Hours() != float64(tt.expected) {
			t.Errorf("RefreshInterval(%q) = %v, want %dh", tt.input, got, tt.expected)
		}
	}
}

func TestSetRefreshInterval(t *testing.T) {
	cfg := &Config{}

	// Valid values
	valid := []string{"6h", "30m", "90m", "1h30m"}
	for _, v := range valid {
		if err := cfg.SetRefreshInterval(v); err != nil {
			t.Errorf("SetRefreshInterval(%q) failed: %v", v, err)
		}
	}

	// Invalid values
	invalid := []string{"invalid", "-1h", "0h", "0m"}
	for _, v := range invalid {
		if err := cfg.SetRefreshInterval(v); err == nil {
			t.Errorf("SetRefreshInterval(%q) should fail", v)
		}
	}
}

func TestAddSubscription(t *testing.T) {
	cfg := Default()

	// Add first
	cfg.AddSubscription("", "https://example.com/sub1")
	if len(cfg.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription")
	}
	if cfg.Subscriptions[0].Name != "sub-0" {
		t.Errorf("auto name = %q", cfg.Subscriptions[0].Name)
	}

	// Add with custom name
	cfg.AddSubscription("custom", "https://example.com/sub2")
	if len(cfg.Subscriptions) != 2 {
		t.Fatalf("expected 2 subscriptions")
	}
	if cfg.Subscriptions[1].Name != "custom" {
		t.Errorf("custom name = %q", cfg.Subscriptions[1].Name)
	}

	// Duplicate URL should be ignored
	cfg.AddSubscription("", "https://example.com/sub1")
	if len(cfg.Subscriptions) != 2 {
		t.Errorf("duplicate URL should not add")
	}
}

func TestRemoveSubscription(t *testing.T) {
	cfg := Default()
	cfg.AddSubscription("a", "https://example.com/a")
	cfg.AddSubscription("b", "https://example.com/b")
	cfg.AddSubscription("c", "https://example.com/c")

	// Out-of-range indexes are rejected without mutating the slice.
	if err := cfg.RemoveSubscription(-1); err == nil {
		t.Errorf("negative index should error")
	}
	if err := cfg.RemoveSubscription(3); err == nil {
		t.Errorf("too-large index should error")
	}
	if len(cfg.Subscriptions) != 3 {
		t.Fatalf("expected 3 subscriptions after failed removes, got %d", len(cfg.Subscriptions))
	}

	// Removing the middle element shifts the rest down.
	if err := cfg.RemoveSubscription(1); err != nil {
		t.Fatalf("RemoveSubscription: %v", err)
	}
	if len(cfg.Subscriptions) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(cfg.Subscriptions))
	}
	if cfg.Subscriptions[0].Name != "a" || cfg.Subscriptions[1].Name != "c" {
		t.Errorf("unexpected order after remove: %q, %q", cfg.Subscriptions[0].Name, cfg.Subscriptions[1].Name)
	}
}

// The first subscription added becomes the active one, and adding more does not
// steal the slot from it.
func TestAddSubscriptionSetsActive(t *testing.T) {
	cfg := Default()
	if cfg.ActiveIndex() != -1 {
		t.Errorf("empty config ActiveIndex = %d, want -1", cfg.ActiveIndex())
	}

	cfg.AddSubscription("a", "https://example.com/a")
	if got := cfg.ActiveIndex(); got != 0 {
		t.Fatalf("ActiveIndex after first add = %d, want 0", got)
	}
	cfg.AddSubscription("b", "https://example.com/b")
	if got := cfg.ActiveIndex(); got != 0 {
		t.Errorf("ActiveIndex after second add = %d, want it to stay 0", got)
	}
	if cfg.Subscriptions[0].ID == "" || cfg.Subscriptions[0].ID == cfg.Subscriptions[1].ID {
		t.Errorf("subscriptions must get distinct non-empty IDs, got %q and %q",
			cfg.Subscriptions[0].ID, cfg.Subscriptions[1].ID)
	}
}

func TestSetActive(t *testing.T) {
	cfg := Default()
	cfg.AddSubscription("a", "https://example.com/a")
	cfg.AddSubscription("b", "https://example.com/b")

	if err := cfg.SetActive(1); err != nil {
		t.Fatalf("SetActive(1): %v", err)
	}
	if got := cfg.ActiveIndex(); got != 1 {
		t.Errorf("ActiveIndex = %d, want 1", got)
	}
	if err := cfg.SetActive(5); err == nil {
		t.Error("out-of-range SetActive should fail")
	}

	// Switching to a paused profile would silently un-pause it; refuse instead.
	if err := cfg.SetSubEnabled(0, false); err != nil {
		t.Fatalf("SetSubEnabled: %v", err)
	}
	if err := cfg.SetActive(0); err == nil {
		t.Error("SetActive on a disabled subscription should fail")
	}
}

// Disabling or removing the active subscription hands the slot to the next
// enabled one, so the kernel is never pointed at a paused or missing profile.
func TestActiveFollowsEnabledSubscriptions(t *testing.T) {
	cfg := Default()
	cfg.AddSubscription("a", "https://example.com/a")
	cfg.AddSubscription("b", "https://example.com/b")

	if err := cfg.SetSubEnabled(0, false); err != nil {
		t.Fatalf("SetSubEnabled: %v", err)
	}
	if got := cfg.ActiveIndex(); got != 1 {
		t.Errorf("ActiveIndex after disabling the active one = %d, want 1", got)
	}

	// Re-enabling does not take the slot back; that is an explicit switch.
	if err := cfg.SetSubEnabled(0, true); err != nil {
		t.Fatalf("SetSubEnabled: %v", err)
	}
	if got := cfg.ActiveIndex(); got != 1 {
		t.Errorf("ActiveIndex after re-enabling = %d, want it to stay 1", got)
	}

	if err := cfg.RemoveSubscription(1); err != nil {
		t.Fatalf("RemoveSubscription: %v", err)
	}
	if got := cfg.ActiveIndex(); got != 0 {
		t.Errorf("ActiveIndex after removing the active one = %d, want 0", got)
	}

	// With every subscription disabled there is no active one at all.
	if err := cfg.SetSubEnabled(0, false); err != nil {
		t.Fatalf("SetSubEnabled: %v", err)
	}
	if got := cfg.ActiveIndex(); got != -1 {
		t.Errorf("ActiveIndex with all disabled = %d, want -1", got)
	}
}

func TestSubscriptionRefreshPolicy(t *testing.T) {
	cfg := Default() // global interval 12h
	cfg.AddSubscription("a", "https://example.com/a")

	global := cfg.RefreshInterval()
	if got := cfg.Subscriptions[0].RefreshIntervalOr(global); got != 12*time.Hour {
		t.Errorf("default interval = %v, want the global 12h", got)
	}

	if err := cfg.SetSubInterval(0, "30m"); err != nil {
		t.Fatalf("SetSubInterval: %v", err)
	}
	if got := cfg.Subscriptions[0].RefreshIntervalOr(global); got != 30*time.Minute {
		t.Errorf("own interval = %v, want 30m", got)
	}
	if err := cfg.SetSubInterval(0, "nonsense"); err == nil {
		t.Error("an unparseable interval should be rejected")
	}
	if got := cfg.Subscriptions[0].Interval; got != "30m" {
		t.Errorf("a rejected interval overwrote the stored one: %q", got)
	}

	// An empty value clears the override, putting it back on the global one.
	if err := cfg.SetSubInterval(0, ""); err != nil {
		t.Fatalf("SetSubInterval(clear): %v", err)
	}
	if got := cfg.Subscriptions[0].RefreshIntervalOr(global); got != 12*time.Hour {
		t.Errorf("cleared interval = %v, want the global 12h", got)
	}

	if !cfg.Subscriptions[0].AutoUpdate() {
		t.Error("auto-update should default to on")
	}
	if err := cfg.SetSubAutoUpdate(0, false); err != nil {
		t.Fatalf("SetSubAutoUpdate: %v", err)
	}
	if cfg.Subscriptions[0].AutoUpdate() {
		t.Error("auto-update should be off after SetSubAutoUpdate(false)")
	}
}

// A config written before profiles existed has neither IDs nor an active
// pointer; loading it must backfill both rather than leaving them unusable.
func TestLoadBackfillsIDsAndActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zashhomo.yaml")
	legacy := "subscriptions:\n" +
		"  - name: a\n    url: https://example.com/a\n" +
		"  - name: b\n    url: https://example.com/b\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for i, s := range cfg.Subscriptions {
		if s.ID == "" {
			t.Errorf("subscription %d got no ID", i)
		}
		if !s.Enabled() || !s.AutoUpdate() {
			t.Errorf("subscription %d should default to enabled with auto-update on", i)
		}
	}
	if got := cfg.ActiveIndex(); got != 0 {
		t.Errorf("ActiveIndex = %d, want the first subscription", got)
	}
	if cfg.ActiveSub != cfg.Subscriptions[0].ID {
		t.Errorf("ActiveSub = %q, want %q", cfg.ActiveSub, cfg.Subscriptions[0].ID)
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
	}
	for _, tt := range tests {
		if got := itoa(tt.in); got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
