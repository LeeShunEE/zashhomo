package main

import (
	"testing"

	"github.com/LeeShunEE/zashhomo/internal/config"
)

func TestPanelURL(t *testing.T) {
	tests := []struct {
		webAddr string
		want    string
	}{
		{"127.0.0.1:9191", "http://127.0.0.1:9191/?token=abc"},
		{"0.0.0.0:9191", "http://127.0.0.1:9191/?token=abc"},
		{"[::]:9191", "http://127.0.0.1:9191/?token=abc"},
		{"192.168.1.5:8080", "http://192.168.1.5:8080/?token=abc"},
	}

	for _, tt := range tests {
		cfg := &config.Config{WebAddr: tt.webAddr, Secret: "abc"}
		got := panelURL(cfg)
		if got != tt.want {
			t.Errorf("panelURL(%q) = %q, want %q", tt.webAddr, got, tt.want)
		}
	}
}

func TestApplyWebAddr(t *testing.T) {
	// --web-port changes only the port, keeping the configured host.
	cfg := &config.Config{WebAddr: "127.0.0.1:9191"}
	applyWebAddr(cfg, "", 8888)
	if cfg.WebAddr != "127.0.0.1:8888" {
		t.Fatalf("web-port kept host: got %s", cfg.WebAddr)
	}
	// A wildcard host is preserved too.
	cfg.WebAddr = "0.0.0.0:9191"
	applyWebAddr(cfg, "", 7000)
	if cfg.WebAddr != "0.0.0.0:7000" {
		t.Fatalf("web-port kept wildcard host: got %s", cfg.WebAddr)
	}
	// --web-addr wins over --web-port.
	applyWebAddr(cfg, "192.168.1.5:7000", 9999)
	if cfg.WebAddr != "192.168.1.5:7000" {
		t.Fatalf("web-addr should win: got %s", cfg.WebAddr)
	}
	// Neither flag: no change.
	cfg.WebAddr = "127.0.0.1:9191"
	applyWebAddr(cfg, "", 0)
	if cfg.WebAddr != "127.0.0.1:9191" {
		t.Fatalf("noop should not change addr: got %s", cfg.WebAddr)
	}
}

func TestPopFlag(t *testing.T) {
	tests := []struct {
		args     []string
		flag     string
		wantFound bool
		wantRest []string
	}{
		{[]string{"--force", "other"}, "--force", true, []string{"other"}},
		{[]string{"other", "--force"}, "--force", true, []string{"other"}},
		{[]string{"a", "b"}, "--force", false, []string{"a", "b"}},
		{[]string{"--force", "--force", "x"}, "--force", true, []string{"x"}},
	}

	for _, tt := range tests {
		found, rest := popFlag(tt.args, tt.flag)
		if found != tt.wantFound {
			t.Errorf("popFlag(%v, %q) found = %v, want %v", tt.args, tt.flag, found, tt.wantFound)
		}
		if len(rest) != len(tt.wantRest) {
			t.Errorf("popFlag(%v, %q) rest = %v, want %v", tt.args, tt.flag, rest, tt.wantRest)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		vals []string
		want string
	}{
		{[]string{"", "a", "b"}, "a"},
		{[]string{"", "", ""}, ""},
		{[]string{"x", "y"}, "x"},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := firstNonEmpty(tt.vals...)
		if got != tt.want {
			t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.vals, got, tt.want)
		}
	}
}

func TestOrDash(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", "-"},
		{"value", "value"},
	}

	for _, tt := range tests {
		if got := orDash(tt.input); got != tt.want {
			t.Errorf("orDash(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSelfAssetName(t *testing.T) {
	got := selfAssetName("v0.1.0")
	// Just check it contains the expected components
	if got == "" {
		t.Error("selfAssetName returned empty string")
	}
}

func TestClearScreen(t *testing.T) {
	// Just ensure it doesn't panic
	clearScreen()
}

func TestUsage(t *testing.T) {
	// Just ensure it doesn't panic
	usage()
}

func TestDispatchHelp(t *testing.T) {
	// Test that help command doesn't error
	if err := dispatch("help", nil); err != nil {
		t.Errorf("dispatch(help) failed: %v", err)
	}
}

func TestDispatchVersion(t *testing.T) {
	// Test that version command doesn't error
	if err := dispatch("version", nil); err != nil {
		t.Errorf("dispatch(version) failed: %v", err)
	}
}

func TestDispatchUnknown(t *testing.T) {
	err := dispatch("unknown-command", nil)
	if err == nil {
		t.Error("expected error for unknown command")
	}
}
