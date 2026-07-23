// Package subscription turns configured clash subscriptions into a mihomo
// config.yaml and triggers hot reloads via the API.
//
// zashhomo fetches the first subscription's full clash document itself and uses
// it as the base config, preserving the author's proxy-groups and rules (so the
// panel shows the same rich routing other mihomo GUIs do). Only the control
// fields zashhomo owns (mixed-port, external-controller, secret, allow-lan) are
// overridden. Additional subscriptions contribute their proxies to the node
// pool. A node-only subscription (no groups/rules) falls back to a synthesized
// PROXY/AUTO setup so the kernel still routes.
package subscription

import (
	"bytes"
	"context"
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

// GenerateConfig writes config.yaml derived from cfg's subscriptions. With no
// subscriptions it writes a valid direct-only config so the kernel can start
// and the panel can connect. With subscriptions it fetches the first one and
// preserves its full routing (see package doc). If fetching fails it keeps a
// previously written config rather than clobbering a working setup.
func GenerateConfig(p *paths.Paths, cfg *config.Config) error {
	if err := p.EnsureDirs(); err != nil {
		return err
	}

	if len(cfg.Subscriptions) == 0 {
		return writeYAML(p.MihomoConfig(), minimalDirectConfig(cfg))
	}

	// Fetch the first subscription as the base config (full passthrough).
	base, err := fetchClashConfig(cfg.Subscriptions[0].URL)
	if err != nil {
		// Don't clobber a working config; only write a fallback if none exists.
		if _, statErr := os.Stat(p.MihomoConfig()); statErr == nil {
			return fmt.Errorf("fetch subscription %q: %w (kept existing config)", cfg.Subscriptions[0].Name, err)
		}
		if writeErr := writeYAML(p.MihomoConfig(), minimalDirectConfig(cfg)); writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("fetch subscription %q: %w (wrote direct-only fallback)", cfg.Subscriptions[0].Name, err)
	}

	// Merge additional subscriptions' proxies into the node pool.
	var extraNames []string
	for _, s := range cfg.Subscriptions[1:] {
		extra, err := fetchClashConfig(s.URL)
		if err != nil {
			// Skip an unreachable extra subscription; the primary still works.
			continue
		}
		extraNames = append(extraNames, mergeProxies(base, extra)...)
	}
	if len(extraNames) > 0 {
		appendToSelectGroups(base, extraNames)
	}

	ensureRoutable(base, cfg)
	applyOverrides(base, cfg)

	return writeYAML(p.MihomoConfig(), base)
}

// fetchClashConfig downloads url and parses it as a clash YAML document.
func fetchClashConfig(url string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", subUserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
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

// mergeProxies appends src's proxies to dst and returns the names added.
func mergeProxies(dst, src map[string]any) []string {
	srcList, ok := src["proxies"].([]any)
	if !ok {
		return nil
	}
	dstList, _ := dst["proxies"].([]any)
	var names []string
	for _, item := range srcList {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok {
			names = append(names, name)
		}
		dstList = append(dstList, item)
	}
	dst["proxies"] = dstList
	return names
}

// appendToSelectGroups makes extra proxies selectable by adding their names to
// every select-type proxy-group in the base config.
func appendToSelectGroups(base map[string]any, names []string) {
	groups, ok := base["proxy-groups"].([]any)
	if !ok {
		return
	}
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok || gm["type"] != "select" {
			continue
		}
		proxies, _ := gm["proxies"].([]any)
		for _, n := range names {
			proxies = append(proxies, n)
		}
		gm["proxies"] = proxies
	}
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
