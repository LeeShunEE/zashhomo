// Package config reads and writes zashhomo's own configuration file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Subscription is a single named clash subscription URL.
type Subscription struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// Config is zashhomo's persisted configuration (zashhomo.yaml).
type Config struct {
	// ControllerAddr is the mihomo external-controller listen address (loopback only).
	ControllerAddr string `yaml:"controller_addr"`
	// Secret protects the Clash REST API.
	Secret string `yaml:"secret"`
	// WebAddr is the address zashhomo serves the panel + API proxy on.
	WebAddr string `yaml:"web_addr"`
	// MixedPort is the mihomo mixed (http+socks) proxy port.
	MixedPort int `yaml:"mixed_port"`
	// Subscriptions holds the configured clash subscriptions.
	Subscriptions []Subscription `yaml:"subscriptions"`
	// SubInterval is how often subscriptions refresh (Go duration string).
	SubInterval string `yaml:"sub_interval"`
	// CoreVersion / UIVersion record the installed component versions.
	CoreVersion string `yaml:"core_version"`
	UIVersion   string `yaml:"ui_version"`

	path string `yaml:"-"`
}

// Default returns a Config populated with sensible defaults and a fresh secret.
func Default() *Config {
	return &Config{
		ControllerAddr: "127.0.0.1:9090",
		Secret:         randomSecret(),
		WebAddr:        "0.0.0.0:9191",
		MixedPort:      9190,
		SubInterval:    "12h",
	}
}

// randomSecret returns a 128-bit hex secret.
func randomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should not fail; fall back to a timestamp-based value.
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}

// Load reads the config at path, applying defaults for missing fields. If the
// file does not exist, a fresh default config bound to path is returned.
func Load(path string) (*Config, error) {
	cfg := Default()
	cfg.path = path
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.path = path
	// Backfill anything a partial file left empty.
	if cfg.ControllerAddr == "" {
		cfg.ControllerAddr = "127.0.0.1:9090"
	}
	if cfg.Secret == "" {
		cfg.Secret = randomSecret()
	}
	if cfg.WebAddr == "" {
		cfg.WebAddr = "0.0.0.0:9191"
	}
	if cfg.MixedPort == 0 {
		cfg.MixedPort = 9190
	}
	if cfg.SubInterval == "" {
		cfg.SubInterval = "12h"
	}
	return cfg, nil
}

// Save writes the config back to its path (0600, secrets inside).
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config: no path bound")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0o600)
}

// SubInterval returns the parsed refresh interval, defaulting to 12h.
func (c *Config) RefreshInterval() time.Duration {
	d, err := time.ParseDuration(c.SubInterval)
	if err != nil || d <= 0 {
		return 12 * time.Hour
	}
	return d
}

// AddSubscription appends a subscription, deriving a name if none is given.
// Duplicate URLs are ignored.
func (c *Config) AddSubscription(name, url string) {
	for _, s := range c.Subscriptions {
		if s.URL == url {
			return
		}
	}
	if name == "" {
		name = "sub-" + itoa(len(c.Subscriptions))
	}
	c.Subscriptions = append(c.Subscriptions, Subscription{Name: name, URL: url})
}

// itoa is a tiny int->string helper to avoid importing strconv here.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
