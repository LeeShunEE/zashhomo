package main

import (
	"testing"

	"github.com/LeeShunEE/zashhomo/internal/config"
)

func TestPanelURL(t *testing.T) {
	cfg := &config.Config{WebAddr: "127.0.0.1:9191", Secret: "abc"}
	if got, want := panelURL(cfg), "http://127.0.0.1:9191/?token=abc"; got != want {
		t.Fatalf("panelURL = %q, want %q", got, want)
	}
	// A wildcard listen address is displayed as localhost.
	cfg.WebAddr = "0.0.0.0:9191"
	if got, want := panelURL(cfg), "http://127.0.0.1:9191/?token=abc"; got != want {
		t.Fatalf("wildcard panelURL = %q, want %q", got, want)
	}
	// IPv6 wildcard too.
	cfg.WebAddr = "[::]:9191"
	if got, want := panelURL(cfg), "http://127.0.0.1:9191/?token=abc"; got != want {
		t.Fatalf("ipv6 wildcard panelURL = %q, want %q", got, want)
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
