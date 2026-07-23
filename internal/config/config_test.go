package config

import (
	"os"
	"path/filepath"
	"testing"
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