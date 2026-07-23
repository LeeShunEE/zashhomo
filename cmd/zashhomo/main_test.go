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
