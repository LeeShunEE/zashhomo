package subscription

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/paths"
)

// sampleClashConfig is a full clash document with the author's own groups/rules.
const sampleClashConfig = `
mixed-port: 7890
allow-lan: true
external-controller: 127.0.0.1:9090
secret: sub-secret
proxies:
  - {name: "HK-1", type: ss, server: a.example.com, port: 443, cipher: aes-128-gcm, password: x}
  - {name: "US-1", type: ss, server: b.example.com, port: 443, cipher: aes-128-gcm, password: y}
proxy-groups:
  - {name: "Streaming", type: select, proxies: ["HK-1", "US-1"]}
  - {name: "PROXY", type: select, proxies: ["HK-1", "US-1", "DIRECT"]}
rules:
  - "DOMAIN-SUFFIX,youtube.com,Streaming"
  - "GEOIP,CN,DIRECT"
  - "MATCH,PROXY"
`

// nodeOnlyConfig has proxies but no groups or rules.
const nodeOnlyConfig = `
proxies:
  - {name: "N-1", type: ss, server: a.example.com, port: 443, cipher: aes-128-gcm, password: x}
`

func newTestPaths(t *testing.T) *paths.Paths {
	dir := t.TempDir()
	return &paths.Paths{
		Data:   dir,
		Bin:    filepath.Join(dir, "bin"),
		UI:     filepath.Join(dir, "ui"),
		Config: filepath.Join(dir, "zashhomo.yaml"),
	}
}

// serveConfig starts an httptest server returning body for every request.
func serveConfig(t *testing.T, body string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGenerateConfigNoSubscriptions(t *testing.T) {
	p := newTestPaths(t)
	cfg := &config.Config{MixedPort: 9190, ControllerAddr: "127.0.0.1:9090", Secret: "test-secret"}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	data, err := os.ReadFile(p.MihomoConfig())
	if err != nil {
		t.Fatalf("mihomo config not created: %v", err)
	}
	if !strings.Contains(string(data), "MATCH,PROXY") {
		t.Errorf("expected direct-only fallback rule, got:\n%s", data)
	}
}

func TestGenerateConfigPassthrough(t *testing.T) {
	p := newTestPaths(t)
	url := serveConfig(t, sampleClashConfig)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9099",
		Secret:         "zashhomo-secret",
		Subscriptions:  []config.Subscription{{Name: "primary", URL: url}},
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	got := readConfig(t, p)

	// The subscription's groups and rules survive.
	for _, want := range []string{"Streaming", "DOMAIN-SUFFIX,youtube.com,Streaming", "GEOIP,CN,DIRECT", "HK-1", "US-1"} {
		if !strings.Contains(got, want) {
			t.Errorf("passthrough missing %q in:\n%s", want, got)
		}
	}
	// Control fields are overridden to zashhomo's values.
	if !strings.Contains(got, "127.0.0.1:9099") {
		t.Errorf("external-controller not overridden:\n%s", got)
	}
	if !strings.Contains(got, "zashhomo-secret") || strings.Contains(got, "sub-secret") {
		t.Errorf("secret not overridden:\n%s", got)
	}
	if !strings.Contains(got, "mixed-port: 9190") {
		t.Errorf("mixed-port not overridden:\n%s", got)
	}
	if strings.Contains(got, "allow-lan: true") {
		t.Errorf("allow-lan should be forced false:\n%s", got)
	}
}

func TestGenerateConfigNodeOnly(t *testing.T) {
	p := newTestPaths(t)
	url := serveConfig(t, nodeOnlyConfig)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		Subscriptions:  []config.Subscription{{Name: "primary", URL: url}},
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	got := readConfig(t, p)
	// A node-only subscription gets a synthesized PROXY group + MATCH rule.
	if !strings.Contains(got, "MATCH,PROXY") {
		t.Errorf("node-only config missing synthesized rule:\n%s", got)
	}
	if !strings.Contains(got, "N-1") {
		t.Errorf("node-only config missing proxy N-1:\n%s", got)
	}
}

// Subscriptions are profiles: only the active one reaches config.yaml, so an
// inactive subscription's nodes must stay out of it.
func TestGenerateConfigUsesOnlyTheActiveSubscription(t *testing.T) {
	p := newTestPaths(t)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		Subscriptions: []config.Subscription{
			{ID: "a", Name: "primary", URL: serveConfig(t, sampleClashConfig)},
			{ID: "b", Name: "other", URL: serveConfig(t, nodeOnlyConfig)},
		},
		ActiveSub: "a",
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	got := readConfig(t, p)
	if !strings.Contains(got, "HK-1") {
		t.Errorf("active subscription's node missing:\n%s", got)
	}
	if strings.Contains(got, "N-1") {
		t.Errorf("inactive subscription's node leaked into config.yaml:\n%s", got)
	}
}

// Switching profiles must not touch the network: once a subscription has been
// fetched, activating it replays the cached document.
func TestSwitchActiveUsesCacheWithoutNetwork(t *testing.T) {
	p := newTestPaths(t)
	other := serveConfig(t, nodeOnlyConfig)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		Subscriptions: []config.Subscription{
			{ID: "a", Name: "primary", URL: serveConfig(t, sampleClashConfig)},
			{ID: "b", Name: "other", URL: other},
		},
		ActiveSub: "a",
	}

	// Cache both profiles, then make the second one unreachable.
	if err := RefreshAll(p, cfg); err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}
	if !Cached(p, cfg.Subscriptions[1]) {
		t.Fatal("second subscription was not cached by RefreshAll")
	}
	cfg.Subscriptions[1].URL = "http://127.0.0.1:0/gone"

	cfg.ActiveSub = "b"
	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig after switch: %v", err)
	}
	got := readConfig(t, p)
	if !strings.Contains(got, "N-1") {
		t.Errorf("switched-to profile not applied from cache:\n%s", got)
	}
	if strings.Contains(got, "HK-1") {
		t.Errorf("previous profile's nodes survived the switch:\n%s", got)
	}
}

func TestRefreshStampsUpdatedAtAndDropCache(t *testing.T) {
	p := newTestPaths(t)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		Subscriptions:  []config.Subscription{{ID: "a", Name: "primary", URL: serveConfig(t, sampleClashConfig)}},
	}

	before := time.Now()
	if err := Refresh(p, cfg, 0); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if cfg.Subscriptions[0].UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt = %v, want at or after %v", cfg.Subscriptions[0].UpdatedAt, before)
	}
	if !Cached(p, cfg.Subscriptions[0]) {
		t.Fatal("Refresh did not write a cache file")
	}

	if err := DropCache(p, cfg.Subscriptions[0]); err != nil {
		t.Fatalf("DropCache: %v", err)
	}
	if Cached(p, cfg.Subscriptions[0]) {
		t.Error("cache file survived DropCache")
	}
	// Dropping an already-absent cache is not an error.
	if err := DropCache(p, cfg.Subscriptions[0]); err != nil {
		t.Errorf("second DropCache: %v", err)
	}
}

// A provider answering with an error page must not be cached as a valid profile.
func TestRefreshRejectsNonClashDocument(t *testing.T) {
	p := newTestPaths(t)
	cfg := &config.Config{
		Subscriptions: []config.Subscription{{ID: "a", Name: "expired", URL: serveConfig(t, "token expired")}},
	}
	if err := Refresh(p, cfg, 0); err == nil {
		t.Fatal("expected an error for a document with no proxies")
	}
	if Cached(p, cfg.Subscriptions[0]) {
		t.Error("a rejected document was still cached")
	}
	if !cfg.Subscriptions[0].UpdatedAt.IsZero() {
		t.Error("a failed fetch stamped UpdatedAt")
	}
}

func TestDueRespectsPerSubscriptionPolicy(t *testing.T) {
	now := time.Now()
	cfg := &config.Config{
		SubInterval: "12h",
		Subscriptions: []config.Subscription{
			{ID: "never", Name: "never fetched"},
			{ID: "fresh", Name: "fetched an hour ago", UpdatedAt: now.Add(-time.Hour)},
			{ID: "short", Name: "own short interval", Interval: "30m", UpdatedAt: now.Add(-time.Hour)},
			{ID: "manual", Name: "auto-update off", NoAutoUpdate: true},
			{ID: "paused", Name: "disabled", Disabled: true},
		},
	}

	got := Due(cfg, now)
	want := []int{0, 2}
	if len(got) != len(want) {
		t.Fatalf("Due = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Due = %v, want %v", got, want)
		}
	}
}

func TestGenerateConfigFetchFailKeepsExisting(t *testing.T) {
	p := newTestPaths(t)
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// Pre-write a "good" config that must not be clobbered.
	good := "mixed-port: 1234\nrules:\n  - MATCH,PROXY\n# sentinel-good-config\n"
	if err := os.WriteFile(p.MihomoConfig(), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		// Unroutable URL forces a fetch error.
		Subscriptions: []config.Subscription{{Name: "bad", URL: "http://127.0.0.1:0/nope"}},
	}

	if err := GenerateConfig(p, cfg); err == nil {
		t.Error("expected an error when fetch fails")
	}
	got := readConfig(t, p)
	if !strings.Contains(got, "sentinel-good-config") {
		t.Errorf("existing config was clobbered on fetch failure:\n%s", got)
	}
}

func TestGenerateConfigFetchFailWritesFallback(t *testing.T) {
	p := newTestPaths(t)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		Subscriptions:  []config.Subscription{{Name: "bad", URL: "http://127.0.0.1:0/nope"}},
	}

	// Error is expected, but a direct-only fallback must be written so the
	// kernel can still start.
	_ = GenerateConfig(p, cfg)
	got := readConfig(t, p)
	if !strings.Contains(got, "MATCH,PROXY") {
		t.Errorf("expected direct-only fallback, got:\n%s", got)
	}
}

func TestGenerateConfigCreatesProvidersDir(t *testing.T) {
	p := newTestPaths(t)
	cfg := config.Default()

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if _, err := os.Stat(p.ProvidersDir()); err != nil {
		t.Errorf("providers dir not created: %v", err)
	}
}

func TestWriteYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	if err := writeYAML(path, map[string]string{"key": "value"}); err != nil {
		t.Fatalf("writeYAML failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestGenerateConfigInjectsTunDirect(t *testing.T) {
	p := newTestPaths(t)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		Tun:            map[string]any{"enable": true, "stack": "gVisor"},
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	got := readConfig(t, p)
	for _, want := range []string{"tun:", "enable: true", "stack: gVisor"} {
		if !strings.Contains(got, want) {
			t.Errorf("direct config missing %q in:\n%s", want, got)
		}
	}
}

func TestGenerateConfigTunOverridesSubscription(t *testing.T) {
	p := newTestPaths(t)
	url := serveConfig(t, sampleClashConfig)
	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "s",
		Subscriptions:  []config.Subscription{{Name: "primary", URL: url}},
		Tun:            map[string]any{"enable": false},
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	got := readConfig(t, p)
	if !strings.Contains(got, "tun:") || !strings.Contains(got, "enable: false") {
		t.Errorf("managed tun block not injected over subscription:\n%s", got)
	}
}

func TestApplyTunPatchesExistingConfig(t *testing.T) {
	p := newTestPaths(t)
	if err := p.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	// A pre-existing config.yaml with no tun block.
	base := map[string]any{"mixed-port": 9190, "rules": []any{"MATCH,PROXY"}}
	if err := writeYAML(p.MihomoConfig(), base); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// Empty cfg.Tun leaves the file untouched (unmanaged).
	cfg := &config.Config{}
	if err := ApplyTun(p, cfg); err != nil {
		t.Fatalf("ApplyTun (empty): %v", err)
	}
	if strings.Contains(readConfig(t, p), "tun:") {
		t.Errorf("unmanaged tun should not be injected:\n%s", readConfig(t, p))
	}

	// A managed cfg.Tun is patched in without regenerating from a subscription.
	cfg.Tun = map[string]any{"enable": true, "stack": "gVisor"}
	if err := ApplyTun(p, cfg); err != nil {
		t.Fatalf("ApplyTun (managed): %v", err)
	}
	got := readConfig(t, p)
	for _, want := range []string{"tun:", "enable: true", "stack: gVisor", "MATCH,PROXY"} {
		if !strings.Contains(got, want) {
			t.Errorf("patched config missing %q in:\n%s", want, got)
		}
	}
}

func readConfig(t *testing.T, p *paths.Paths) string {
	t.Helper()
	data, err := os.ReadFile(p.MihomoConfig())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}
