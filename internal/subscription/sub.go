// Package subscription turns configured clash subscriptions into a mihomo
// config.yaml (using proxy-providers) and triggers hot reloads via the API.
package subscription

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"gopkg.in/yaml.v3"
)

// healthCheckURL is used by mihomo to test provider proxies.
const healthCheckURL = "https://www.gstatic.com/generate_204"

// mihomoConfig is the generated kernel config. Field order matters for
// readability; yaml.v3 preserves struct field order on marshal.
type mihomoConfig struct {
	MixedPort          int                      `yaml:"mixed-port"`
	AllowLAN           bool                     `yaml:"allow-lan"`
	Mode               string                   `yaml:"mode"`
	LogLevel           string                   `yaml:"log-level"`
	ExternalController string                   `yaml:"external-controller"`
	Secret             string                   `yaml:"secret"`
	ExternalUI         string                   `yaml:"external-ui,omitempty"`
	ProxyProviders     map[string]proxyProvider `yaml:"proxy-providers,omitempty"`
	ProxyGroups        []map[string]any         `yaml:"proxy-groups,omitempty"`
	Rules              []string                 `yaml:"rules"`
}

type proxyProvider struct {
	Type        string      `yaml:"type"`
	URL         string      `yaml:"url"`
	Interval    int         `yaml:"interval"`
	Path        string      `yaml:"path"`
	HealthCheck healthCheck `yaml:"health-check"`
}

type healthCheck struct {
	Enable   bool   `yaml:"enable"`
	URL      string `yaml:"url"`
	Interval int    `yaml:"interval"`
}

// GenerateConfig writes config.yaml derived from cfg's subscriptions. When
// there are no subscriptions it still writes a valid direct-only config so the
// kernel can start and the panel can connect.
func GenerateConfig(p *paths.Paths, cfg *config.Config) error {
	if err := p.EnsureDirs(); err != nil {
		return err
	}

	intervalSecs := int(cfg.RefreshInterval() / time.Second)
	if intervalSecs <= 0 {
		intervalSecs = 12 * 3600
	}

	mc := mihomoConfig{
		MixedPort:          cfg.MixedPort,
		AllowLAN:           false,
		Mode:               "rule",
		LogLevel:           "info",
		ExternalController: cfg.ControllerAddr,
		Secret:             cfg.Secret,
		Rules:              []string{"MATCH,PROXY"},
	}

	if len(cfg.Subscriptions) == 0 {
		// No providers: a selectable DIRECT-only PROXY group keeps rules valid.
		mc.ProxyGroups = []map[string]any{
			{"name": "PROXY", "type": "select", "proxies": []string{"DIRECT"}},
		}
		return writeYAML(p.MihomoConfig(), mc)
	}

	mc.ProxyProviders = map[string]proxyProvider{}
	var providerNames []string
	for i, s := range cfg.Subscriptions {
		name := fmt.Sprintf("sub-%d", i)
		mc.ProxyProviders[name] = proxyProvider{
			Type:     "http",
			URL:      s.URL,
			Interval: intervalSecs,
			Path:     fmt.Sprintf("./providers/%s.yaml", name),
			HealthCheck: healthCheck{
				Enable:   true,
				URL:      healthCheckURL,
				Interval: 300,
			},
		}
		providerNames = append(providerNames, name)
	}

	mc.ProxyGroups = []map[string]any{
		{
			"name": "PROXY",
			"type": "select",
			"use":  providerNames,
			// DIRECT stays selectable as a fallback.
			"proxies": []string{"AUTO", "DIRECT"},
		},
		{
			"name":     "AUTO",
			"type":     "url-test",
			"use":      providerNames,
			"url":      healthCheckURL,
			"interval": 300,
		},
	}

	return writeYAML(p.MihomoConfig(), mc)
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
