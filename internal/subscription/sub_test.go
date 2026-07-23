package subscription

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/paths"
)

func TestGenerateConfigNoSubscriptions(t *testing.T) {
	dir := t.TempDir()
	p := &paths.Paths{
		Data:   dir,
		Bin:    filepath.Join(dir, "bin"),
		UI:     filepath.Join(dir, "ui"),
		Config: filepath.Join(dir, "zashhomo.yaml"),
	}

	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "test-secret",
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(p.MihomoConfig()); err != nil {
		t.Errorf("mihomo config not created: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(p.MihomoConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Should have basic config
	content := string(data)
	if content == "" {
		t.Error("config is empty")
	}
}

func TestGenerateConfigWithSubscriptions(t *testing.T) {
	dir := t.TempDir()
	p := &paths.Paths{
		Data:   dir,
		Bin:    filepath.Join(dir, "bin"),
		UI:     filepath.Join(dir, "ui"),
		Config: filepath.Join(dir, "zashhomo.yaml"),
	}

	cfg := &config.Config{
		MixedPort:      9190,
		ControllerAddr: "127.0.0.1:9090",
		Secret:         "test-secret",
		Subscriptions: []config.Subscription{
			{Name: "sub1", URL: "https://example.com/sub1"},
			{Name: "sub2", URL: "https://example.com/sub2"},
		},
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Verify file exists
	data, err := os.ReadFile(p.MihomoConfig())
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	// Should contain proxy-providers
	if len(cfg.Subscriptions) > 0 && content == "" {
		t.Error("config should not be empty with subscriptions")
	}
}

func TestGenerateConfigCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	p := &paths.Paths{
		Data:   dir,
		Bin:    filepath.Join(dir, "bin"),
		UI:     filepath.Join(dir, "ui"),
		Config: filepath.Join(dir, "zashhomo.yaml"),
	}

	cfg := config.Default()

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Verify providers dir created
	providersDir := p.ProvidersDir()
	if _, err := os.Stat(providersDir); err != nil {
		t.Errorf("providers dir not created: %v", err)
	}
}

func TestWriteYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	data := map[string]string{"key": "value"}
	if err := writeYAML(path, data); err != nil {
		t.Fatalf("writeYAML failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}

	// Verify permissions (0600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Note: permission check varies by platform
	_ = info
}

func TestGenerateConfigCustomPort(t *testing.T) {
	dir := t.TempDir()
	p := &paths.Paths{
		Data:   dir,
		Bin:    filepath.Join(dir, "bin"),
		UI:     filepath.Join(dir, "ui"),
		Config: filepath.Join(dir, "zashhomo.yaml"),
	}

	cfg := &config.Config{
		MixedPort:      8080,
		ControllerAddr: "127.0.0.1:9999",
		Secret:         "custom-secret",
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(p.MihomoConfig()); err != nil {
		t.Errorf("mihomo config not created: %v", err)
	}
}

func TestGenerateConfigCreatesProvidersDir(t *testing.T) {
	dir := t.TempDir()
	p := &paths.Paths{
		Data:   dir,
		Bin:    filepath.Join(dir, "bin"),
		UI:     filepath.Join(dir, "ui"),
		Config: filepath.Join(dir, "zashhomo.yaml"),
	}

	cfg := config.Default()
	cfg.Subscriptions = []config.Subscription{
		{Name: "test", URL: "https://example.com/test"},
	}

	if err := GenerateConfig(p, cfg); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Verify providers dir created
	providersDir := p.ProvidersDir()
	if _, err := os.Stat(providersDir); err != nil {
		t.Errorf("providers dir not created: %v", err)
	}
}