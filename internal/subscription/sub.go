// Package subscription turns the active clash subscription into a mihomo
// config.yaml and triggers hot reloads via the API.
//
// Subscriptions are profiles: every one is downloaded to its own cache file
// under <data>/subs, but only the active one generates config.yaml. Its full
// clash document is used as the base, preserving the author's proxy-groups and
// rules (so the panel shows the same rich routing other mihomo GUIs do); only
// the control fields zashhomo owns (mixed-port, external-controller, secret,
// allow-lan) are overridden. A node-only subscription (no groups/rules) falls
// back to a synthesized PROXY/AUTO setup so the kernel still routes.
//
// Because every profile is cached, switching between them is a local operation:
// no network, and it works while the provider is unreachable.
package subscription

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"gopkg.in/yaml.v3"
)

// healthCheckURL is used by mihomo to test proxies in synthesized groups.
const healthCheckURL = "https://www.gstatic.com/generate_204"

// fetchTimeout bounds a single subscription download.
const fetchTimeout = 30 * time.Second

// subUserAgent identifies zashhomo to subscription servers. Many providers only
// return a clash YAML document when the User-Agent looks like a clash client.
const subUserAgent = "clash.meta/zashhomo"

// httpClient is the client used to fetch subscriptions (overridable in tests).
var httpClient = &http.Client{Timeout: fetchTimeout}

// GenerateConfig writes config.yaml from the active subscription's cached
// document, preserving its full routing (see package doc). With no active
// subscription — none configured, or all of them disabled — it writes a valid
// direct-only config so the kernel can still start and the panel can connect.
//
// It prefers the cache and only reaches for the network when the active
// subscription has never been fetched, which is what makes switching profiles
// instant. If that fetch fails it keeps a previously written config rather than
// clobbering a working setup.
func GenerateConfig(p *paths.Paths, cfg *config.Config) error {
	if err := p.EnsureDirs(); err != nil {
		return err
	}

	idx := cfg.ActiveIndex()
	if idx < 0 {
		return writeYAML(p.MihomoConfig(), minimalDirectConfig(cfg))
	}
	sub := &cfg.Subscriptions[idx]

	base, err := loadCached(p, *sub)
	if err != nil {
		// Never fetched (fresh install, or added while offline): go get it.
		base, err = Fetch(p, sub)
		if err != nil {
			return fallbackConfig(p, cfg, sub.DisplayName(idx), err)
		}
	}

	ensureRoutable(base, cfg)
	applyOverrides(base, cfg)

	return writeYAML(p.MihomoConfig(), base)
}

// fallbackConfig answers a failed fetch. An existing config.yaml is a working
// setup and must not be clobbered; with none on disk the kernel needs something
// to start from, so a direct-only config is written. Either way the cause is
// reported, since the user asked for a subscription and did not get it.
func fallbackConfig(p *paths.Paths, cfg *config.Config, name string, cause error) error {
	if _, err := os.Stat(p.MihomoConfig()); err == nil {
		return fmt.Errorf("subscription %q: %w (kept existing config)", name, cause)
	}
	if err := writeYAML(p.MihomoConfig(), minimalDirectConfig(cfg)); err != nil {
		return err
	}
	return fmt.Errorf("subscription %q: %w (wrote direct-only fallback)", name, cause)
}

// Fetch downloads sub's document into the local cache and stamps UpdatedAt on
// success, returning the parsed document. The caller owns persisting cfg.
func Fetch(p *paths.Paths, sub *config.Subscription) (map[string]any, error) {
	doc, raw, err := fetchClashConfig(sub.URL)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.SubsDir(), 0o755); err != nil {
		return nil, err
	}
	// The raw bytes are cached rather than a re-marshalled document, so a later
	// switch replays exactly what the provider served.
	if err := os.WriteFile(p.SubFile(cacheName(*sub)), raw, 0o600); err != nil {
		return nil, err
	}
	sub.UpdatedAt = time.Now()
	return doc, nil
}

// Refresh re-downloads the subscription at index into the cache. Regenerating
// config.yaml and saving cfg (Fetch stamps UpdatedAt) are left to the caller,
// which knows whether the refreshed subscription is the active one.
func Refresh(p *paths.Paths, cfg *config.Config, index int) error {
	if index < 0 || index >= len(cfg.Subscriptions) {
		return fmt.Errorf("subscription index %d out of range (0-%d)", index, len(cfg.Subscriptions)-1)
	}
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	_, err := Fetch(p, &cfg.Subscriptions[index])
	return err
}

// RefreshAll re-downloads every enabled subscription. It tries all of them and
// reports the first failure, so one dead provider doesn't stop the rest.
func RefreshAll(p *paths.Paths, cfg *config.Config) error {
	var firstErr error
	for i := range cfg.Subscriptions {
		if !cfg.Subscriptions[i].Enabled() {
			continue
		}
		if err := Refresh(p, cfg, i); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("subscription %q: %w", cfg.Subscriptions[i].DisplayName(i), err)
		}
	}
	return firstErr
}

// Due returns the indices of the subscriptions whose timed refresh has come due
// at now: enabled, opted into auto-update, and last fetched longer ago than
// their own interval (or the global one when they don't set one). A subscription
// that has never been fetched is always due.
func Due(cfg *config.Config, now time.Time) []int {
	global := cfg.RefreshInterval()
	var due []int
	for i, s := range cfg.Subscriptions {
		if !s.Enabled() || !s.AutoUpdate() {
			continue
		}
		if now.Sub(s.UpdatedAt) >= s.RefreshIntervalOr(global) {
			due = append(due, i)
		}
	}
	return due
}

// DropCache deletes sub's cached document. It is called when a subscription is
// removed so the cache doesn't accumulate files no config refers to. A missing
// file is not an error.
func DropCache(p *paths.Paths, sub config.Subscription) error {
	if err := os.Remove(p.SubFile(cacheName(sub))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Cached reports whether sub has a document on disk, i.e. whether switching to
// it would need the network.
func Cached(p *paths.Paths, sub config.Subscription) bool {
	_, err := os.Stat(p.SubFile(cacheName(sub)))
	return err == nil
}

// cacheName is the file stem holding sub's fetched document. It prefers the
// stable ID; a subscription that predates IDs (or one built in code) falls back
// to a hash of its URL, which is stable across runs just the same.
func cacheName(sub config.Subscription) string {
	if sub.ID != "" {
		return sub.ID
	}
	sum := sha256.Sum256([]byte(sub.URL))
	return hex.EncodeToString(sum[:8])
}

// loadCached parses sub's cached document. A missing or unusable cache is an
// error, which callers answer by fetching.
func loadCached(p *paths.Paths, sub config.Subscription) (map[string]any, error) {
	data, err := os.ReadFile(p.SubFile(cacheName(sub)))
	if err != nil {
		return nil, err
	}
	return parseClashConfig(data)
}

// fetchClashConfig downloads url, returning the parsed document alongside the
// bytes as served so they can be cached verbatim.
func fetchClashConfig(url string) (map[string]any, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", subUserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, err
	}
	doc, err := parseClashConfig(data)
	if err != nil {
		return nil, nil, err
	}
	return doc, data, nil
}

// parseClashConfig parses data as a clash YAML document, rejecting anything that
// couldn't drive the kernel (many providers answer an expired token with an HTML
// or plain-text error page, which would otherwise be cached as a valid profile).
func parseClashConfig(data []byte) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("empty document")
	}
	if _, ok := doc["proxies"]; !ok {
		return nil, fmt.Errorf("no proxies in subscription")
	}
	return doc, nil
}

// ensureRoutable synthesizes a PROXY/AUTO setup for a node-only subscription so
// the kernel has something to route through. It only acts when the subscription
// carried neither proxy-groups nor rules.
func ensureRoutable(base map[string]any, cfg *config.Config) {
	groups, _ := base["proxy-groups"].([]any)
	rules, _ := base["rules"].([]any)
	if len(groups) > 0 || len(rules) > 0 {
		return
	}
	names := proxyNames(base)
	proxyGroup := []any{"AUTO", "DIRECT"}
	for _, n := range names {
		proxyGroup = append(proxyGroup, n)
	}
	base["proxy-groups"] = []any{
		map[string]any{"name": "PROXY", "type": "select", "proxies": proxyGroup},
		map[string]any{
			"name": "AUTO", "type": "url-test",
			"proxies":  namesAsAny(names),
			"url":      healthCheckURL,
			"interval": 300,
		},
	}
	base["rules"] = []any{"MATCH,PROXY"}
}

// proxyNames returns the name of every proxy in base.
func proxyNames(base map[string]any) []string {
	list, ok := base["proxies"].([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

// namesAsAny converts a []string to []any for YAML list fields.
func namesAsAny(names []string) []any {
	out := make([]any, len(names))
	for i, n := range names {
		out[i] = n
	}
	return out
}

// applyOverrides forces the control/security fields zashhomo owns onto base,
// regardless of what the subscription set, and strips conflicting listeners.
func applyOverrides(base map[string]any, cfg *config.Config) {
	base["mixed-port"] = cfg.MixedPort
	base["allow-lan"] = false
	base["external-controller"] = cfg.ControllerAddr
	base["secret"] = cfg.Secret
	if _, ok := base["mode"]; !ok {
		base["mode"] = "rule"
	}
	if _, ok := base["log-level"]; !ok {
		base["log-level"] = "info"
	}
	// zashhomo owns the proxy port and serves its own panel; drop any separate
	// listeners or external-ui the subscription tried to configure.
	for _, k := range []string{"port", "socks-port", "redir-port", "tproxy-port", "external-ui"} {
		delete(base, k)
	}
	// When zashhomo manages TUN (synced from a panel toggle), its block wins over
	// whatever the subscription set so the user's choice persists across restarts.
	if len(cfg.Tun) > 0 {
		base["tun"] = cfg.Tun
	}
}

// minimalDirectConfig is the DIRECT-only config used when there are no
// subscriptions or a fetch fails with no prior config to fall back on.
func minimalDirectConfig(cfg *config.Config) map[string]any {
	m := map[string]any{
		"mixed-port":          cfg.MixedPort,
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "info",
		"external-controller": cfg.ControllerAddr,
		"secret":              cfg.Secret,
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"DIRECT"}},
		},
		"rules": []any{"MATCH,PROXY"},
	}
	if len(cfg.Tun) > 0 {
		m["tun"] = cfg.Tun
	}
	return m
}

// writeYAML marshals v and writes it to path (0600; may contain the secret).
func writeYAML(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Reload asks a running kernel to reload config.yaml (PUT /configs?force=true).
func Reload(ctx context.Context, cfg *config.Config, configPath string) error {
	url := "http://" + cfg.ControllerAddr + "/configs?force=true"
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// The Clash API expects a JSON body naming the config path.
	jsonBody := []byte(fmt.Sprintf(`{"path":%q}`, configPath))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPut, url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("reload: %s", resp.Status)
	}
	return nil
}

// ApplyTun patches config.yaml's `tun` block in place to match cfg.Tun, without
// refetching subscriptions. This lets a persisted panel toggle take effect on
// the next kernel start even between subscription refreshes (config.yaml is not
// regenerated on a plain restart). When cfg.Tun is empty zashhomo does not
// manage TUN, so the existing file (including any subscription-provided tun) is
// left untouched.
func ApplyTun(p *paths.Paths, cfg *config.Config) error {
	if len(cfg.Tun) == 0 {
		return nil
	}
	data, err := os.ReadFile(p.MihomoConfig())
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	doc["tun"] = cfg.Tun
	return writeYAML(p.MihomoConfig(), doc)
}

// FetchTun returns the running kernel's live `tun` config block via
// GET /configs. The panel toggles TUN by patching the running kernel only, so
// this is how zashhomo observes that choice to persist it. It returns (nil, nil)
// when the kernel reports no tun block.
func FetchTun(ctx context.Context, cfg *config.Config) (map[string]any, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+cfg.ControllerAddr+"/configs", nil)
	if err != nil {
		return nil, err
	}
	if cfg.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get configs: %s", resp.Status)
	}
	var doc struct {
		Tun map[string]any `json:"tun"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode configs: %w", err)
	}
	return doc.Tun, nil
}
