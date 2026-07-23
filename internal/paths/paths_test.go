package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Data == "" {
		t.Error("Data should not be empty")
	}
	if p.Bin == "" {
		t.Error("Bin should not be empty")
	}
	if p.UI == "" {
		t.Error("UI should not be empty")
	}
	if p.Config == "" {
		t.Error("Config should not be empty")
	}
	if p.Log == "" {
		t.Error("Log should not be empty")
	}
}

func TestNewWithDataEnv(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("ZASHHOMO_DATA", customDir)

	p := New()
	if p.Data != customDir {
		t.Errorf("Data = %q, want %q", p.Data, customDir)
	}
}

func TestEnsureDirs(t *testing.T) {
	p := New()
	// Override data dir to temp
	p.Data = t.TempDir()
	p.Bin = filepath.Join(p.Data, "bin")
	p.UI = filepath.Join(p.Data, "ui")
	p.Config = filepath.Join(p.Data, "zashhomo.yaml")

	if err := p.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	// Verify directories exist
	for _, dir := range []string{p.Data, p.Bin, p.UI, filepath.Dir(p.Config)} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("dir %q not created: %v", dir, err)
		}
	}
}

func TestMihomoBin(t *testing.T) {
	p := &Paths{Bin: "/opt/zashhomo/bin"}
	got := p.MihomoBin()

	// Use filepath.FromSlash for cross-platform comparison
	expected := filepath.FromSlash("/opt/zashhomo/bin/mihomo")
	if runtime.GOOS == "windows" {
		expected = filepath.FromSlash("/opt/zashhomo/bin/mihomo.exe")
	}
	if got != expected {
		t.Errorf("MihomoBin = %q, want %q", got, expected)
	}
}

func TestMihomoConfig(t *testing.T) {
	p := &Paths{Data: "/var/lib/zashhomo"}
	got := p.MihomoConfig()
	want := filepath.FromSlash("/var/lib/zashhomo/config.yaml")
	if got != want {
		t.Errorf("MihomoConfig = %q, want %q", got, want)
	}
}

func TestProvidersDir(t *testing.T) {
	p := &Paths{Data: "/var/lib/zashhomo"}
	got := p.ProvidersDir()
	want := filepath.FromSlash("/var/lib/zashhomo/providers")
	if got != want {
		t.Errorf("ProvidersDir = %q, want %q", got, want)
	}
}

func TestUIIndex(t *testing.T) {
	p := &Paths{UI: "/opt/zashhomo/ui"}
	got := p.UIIndex()
	want := filepath.FromSlash("/opt/zashhomo/ui/index.html")
	if got != want {
		t.Errorf("UIIndex = %q, want %q", got, want)
	}
}

func TestInstallDir(t *testing.T) {
	// Test with env override
	customDir := t.TempDir()
	t.Setenv("ZASHHOMO_BIN", customDir)

	got := InstallDir()
	if got != customDir {
		t.Errorf("InstallDir = %q, want %q", got, customDir)
	}
}

func TestSelfExe(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("ZASHHOMO_BIN", customDir)

	got := SelfExe()
	expected := filepath.Join(customDir, "zashhomo")
	if runtime.GOOS == "windows" {
		expected += ".exe"
	}
	if got != expected {
		t.Errorf("SelfExe = %q, want %q", got, expected)
	}
}

func TestMihomoBinName(t *testing.T) {
	got := mihomoBinName()
	if runtime.GOOS == "windows" {
		if got != "mihomo.exe" {
			t.Errorf("mihomoBinName on Windows = %q, want mihomo.exe", got)
		}
	} else {
		if got != "mihomo" {
			t.Errorf("mihomoBinName = %q, want mihomo", got)
		}
	}
}
