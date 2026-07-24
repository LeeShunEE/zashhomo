// Package config reads and writes zashhomo's own configuration file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Subscription is a single named clash subscription — a profile. Exactly one is
// active at a time and generates config.yaml on its own; the rest sit in the
// local cache ready to be switched to. Each carries its own enable and refresh
// policy so one provider can be paused or polled on a different schedule than
// the others.
type Subscription struct {
	// ID is a stable identifier assigned on first use. It names the cache file
	// holding this subscription's fetched document, so renaming the entry does
	// not orphan what was already downloaded.
	ID string `yaml:"id"`
	// Name is the display name shown in the menu and CLI.
	Name string `yaml:"name"`
	// URL is the subscription endpoint.
	URL string `yaml:"url"`
	// Disabled excludes the subscription from switching and from the timed
	// refresh. The zero value means enabled, so configs written before this
	// field existed keep working unchanged.
	Disabled bool `yaml:"disabled,omitempty"`
	// NoAutoUpdate opts this subscription out of the daemon's timed refresh; an
	// explicit "update now" still works. Zero value means auto-update is on.
	NoAutoUpdate bool `yaml:"no_auto_update,omitempty"`
	// Interval overrides the global refresh interval for this subscription (a Go
	// duration string). Empty means "follow sub_interval".
	Interval string `yaml:"interval,omitempty"`
	// UpdatedAt is when the cached document was last fetched successfully. The
	// zero value means "never", which makes the subscription due immediately.
	UpdatedAt time.Time `yaml:"updated_at,omitempty"`
}

// Enabled reports whether the subscription takes part in switching and refresh.
func (s Subscription) Enabled() bool { return !s.Disabled }

// AutoUpdate reports whether the daemon refreshes this subscription on a timer.
func (s Subscription) AutoUpdate() bool { return !s.NoAutoUpdate }

// RefreshIntervalOr returns this subscription's own refresh interval, falling
// back to global when it does not set one (or set an unusable one).
func (s Subscription) RefreshIntervalOr(global time.Duration) time.Duration {
	d, err := parseInterval(s.Interval)
	if err != nil {
		return global
	}
	return d
}

// DisplayName returns the name to show for the subscription at index i, falling
// back to a positional name for entries that were never named.
func (s Subscription) DisplayName(i int) string {
	if s.Name != "" {
		return s.Name
	}
	return "sub-" + itoa(i)
}

// newSubID returns a fresh 64-bit hex identifier for a subscription.
func newSubID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))[:16]
	}
	return hex.EncodeToString(b)
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
	// ActiveSub is the ID of the subscription currently generating config.yaml.
	// Empty (or pointing at a removed/disabled entry) resolves to the first
	// enabled subscription, so a hand-edited config always has a sane active one.
	ActiveSub string `yaml:"active_sub,omitempty"`
	// SubInterval is the default refresh cadence for subscriptions that do not
	// set their own (Go duration string).
	SubInterval string `yaml:"sub_interval"`
	// SystemProxy records whether zashhomo manages the OS system proxy, pointing
	// it at the mixed-port. When true the daemon enables it on start and clears
	// it on stop.
	SystemProxy bool `yaml:"system_proxy"`
	// CoreVersion / UIVersion record the installed component versions.
	CoreVersion string `yaml:"core_version"`
	UIVersion   string `yaml:"ui_version"`
	// Tun is the mihomo TUN config block zashhomo persists so a toggle made in
	// the panel (which only changes the running kernel, not config.yaml) survives
	// kernel restarts. It is the raw block synced live from the kernel; nil/empty
	// means zashhomo does not manage TUN and leaves the subscription's own to it.
	Tun map[string]any `yaml:"tun,omitempty"`

	path string `yaml:"-"`
}

// Default returns a Config populated with sensible defaults and a fresh secret.
func Default() *Config {
	return &Config{
		ControllerAddr: "127.0.0.1:9090",
		Secret:         randomSecret(),
		WebAddr:        "127.0.0.1:9191",
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
		cfg.WebAddr = "127.0.0.1:9191"
	}
	if cfg.MixedPort == 0 {
		cfg.MixedPort = 9190
	}
	if cfg.SubInterval == "" {
		cfg.SubInterval = "12h"
	}
	cfg.normalize()
	return cfg, nil
}

// normalize backfills subscription IDs (configs written before profiles existed
// have none) and repairs the active pointer, so everything downstream can treat
// both as valid.
func (c *Config) normalize() {
	seen := make(map[string]bool, len(c.Subscriptions))
	for i := range c.Subscriptions {
		s := &c.Subscriptions[i]
		if s.ID == "" || seen[s.ID] {
			s.ID = newSubID()
		}
		seen[s.ID] = true
	}
	c.repairActive()
}

// repairActive points ActiveSub at an existing, enabled subscription: it keeps
// the current choice when still valid, otherwise falls back to the first enabled
// one, and clears it when none is.
func (c *Config) repairActive() {
	for _, s := range c.Subscriptions {
		if s.ID != "" && s.ID == c.ActiveSub && s.Enabled() {
			return
		}
	}
	c.ActiveSub = ""
	for _, s := range c.Subscriptions {
		if s.Enabled() {
			c.ActiveSub = s.ID
			return
		}
	}
}

// ActiveIndex returns the index of the subscription that generates config.yaml,
// or -1 when there is none (no subscriptions, or all of them disabled). A config
// that never recorded a choice resolves to its first enabled subscription.
func (c *Config) ActiveIndex() int {
	for i, s := range c.Subscriptions {
		if s.ID != "" && s.ID == c.ActiveSub {
			return i
		}
	}
	for i, s := range c.Subscriptions {
		if s.Enabled() {
			return i
		}
	}
	return -1
}

// SetActive makes the subscription at index the one config.yaml is built from.
// A disabled subscription is refused: switching to a profile the user has paused
// would silently un-pause it.
func (c *Config) SetActive(index int) error {
	if err := c.checkIndex(index); err != nil {
		return err
	}
	s := c.Subscriptions[index]
	if !s.Enabled() {
		return fmt.Errorf("subscription %q is disabled; enable it first", s.DisplayName(index))
	}
	c.ActiveSub = s.ID
	return nil
}

// SetSubEnabled enables or disables the subscription at index. Disabling the
// active one hands the active slot to the next enabled subscription, so the
// kernel is never left pointing at a paused profile.
func (c *Config) SetSubEnabled(index int, enabled bool) error {
	if err := c.checkIndex(index); err != nil {
		return err
	}
	c.Subscriptions[index].Disabled = !enabled
	c.repairActive()
	return nil
}

// SetSubAutoUpdate turns the timed refresh on or off for the subscription at
// index. It does not affect an explicit "update now".
func (c *Config) SetSubAutoUpdate(index int, on bool) error {
	if err := c.checkIndex(index); err != nil {
		return err
	}
	c.Subscriptions[index].NoAutoUpdate = !on
	return nil
}

// SetSubInterval overrides the refresh interval of the subscription at index
// with a Go duration string. An empty value clears the override, putting the
// subscription back on the global interval.
func (c *Config) SetSubInterval(index int, s string) error {
	if err := c.checkIndex(index); err != nil {
		return err
	}
	if s == "" {
		c.Subscriptions[index].Interval = ""
		return nil
	}
	if _, err := parseInterval(s); err != nil {
		return err
	}
	c.Subscriptions[index].Interval = s
	return nil
}

// checkIndex validates a subscription index against the configured list.
func (c *Config) checkIndex(index int) error {
	if index < 0 || index >= len(c.Subscriptions) {
		return fmt.Errorf("subscription index %d out of range (0-%d)", index, len(c.Subscriptions)-1)
	}
	return nil
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

// parseInterval validates a Go duration string used as a refresh interval,
// rejecting unparseable and non-positive values so no caller can persist one
// that would later be silently discarded.
func parseInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid interval %q (use e.g. 6h, 30m, 90m): %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("interval must be positive, got %q", s)
	}
	return d, nil
}

// RefreshInterval returns the parsed global refresh interval, defaulting to 12h.
// It is the fallback for subscriptions that do not set their own.
func (c *Config) RefreshInterval() time.Duration {
	d, err := parseInterval(c.SubInterval)
	if err != nil {
		return 12 * time.Hour
	}
	return d
}

// SetRefreshInterval validates and stores the global refresh interval as a Go
// duration string (e.g. "6h", "30m", "90m").
func (c *Config) SetRefreshInterval(s string) error {
	if _, err := parseInterval(s); err != nil {
		return err
	}
	c.SubInterval = s
	return nil
}

// AddSubscription appends a subscription, deriving a name if none is given, and
// returns its index. A duplicate URL is not added; the existing entry's index is
// returned instead. The first subscription added becomes the active one.
func (c *Config) AddSubscription(name, url string) int {
	for i, s := range c.Subscriptions {
		if s.URL == url {
			return i
		}
	}
	if name == "" {
		name = "sub-" + itoa(len(c.Subscriptions))
	}
	c.Subscriptions = append(c.Subscriptions, Subscription{ID: newSubID(), Name: name, URL: url})
	c.repairActive()
	return len(c.Subscriptions) - 1
}

// RemoveSubscription deletes the subscription at index, returning an error when
// the index is out of range. Removing the active one promotes the next enabled
// subscription in its place.
func (c *Config) RemoveSubscription(index int) error {
	if err := c.checkIndex(index); err != nil {
		return err
	}
	c.Subscriptions = append(c.Subscriptions[:index], c.Subscriptions[index+1:]...)
	c.repairActive()
	return nil
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
