// Package paths resolves cross-platform data/config directories for zashhomo.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths holds the resolved locations zashhomo uses at runtime.
type Paths struct {
	Data   string // root data dir (holds bin/, ui/, providers/, config.yaml)
	Bin    string // mihomo binary directory
	UI     string // zashboard static site directory
	Config string // zashhomo self config (zashhomo.yaml)
	Log    string // daemon log file
}

// mihomoBinName returns the platform binary file name for the mihomo kernel.
func mihomoBinName() string {
	if runtime.GOOS == "windows" {
		return "mihomo.exe"
	}
	return "mihomo"
}

// isRoot reports whether the process runs as the superuser (always false on Windows).
func isRoot() bool {
	return runtime.GOOS != "windows" && os.Geteuid() == 0
}

// dataDir picks the platform data directory root.
func dataDir() string {
	if v := os.Getenv("ZASHHOMO_DATA"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "zashhomo")
	default:
		if isRoot() {
			return "/var/lib/zashhomo"
		}
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "zashhomo")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "zashhomo")
	}
}

// configDir picks the platform config directory for zashhomo.yaml.
func configDir() string {
	if v := os.Getenv("ZASHHOMO_CONFIG_DIR"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		return dataDir() // keep everything together under ProgramData on Windows
	default:
		if isRoot() {
			return "/etc/zashhomo"
		}
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "zashhomo")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "zashhomo")
	}
}

// New resolves all paths. It does not create directories; call EnsureDirs.
func New() *Paths {
	d := dataDir()
	return &Paths{
		Data:   d,
		Bin:    filepath.Join(d, "bin"),
		UI:     filepath.Join(d, "ui"),
		Config: filepath.Join(configDir(), "zashhomo.yaml"),
		Log:    filepath.Join(d, "zashhomo.log"),
	}
}

// EnsureDirs creates the directories zashhomo writes into.
func (p *Paths) EnsureDirs() error {
	dirs := []string{
		p.Data,
		p.Bin,
		p.UI,
		filepath.Join(p.Data, "providers"),
		filepath.Dir(p.Config),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// MihomoBin returns the full path to the mihomo binary.
func (p *Paths) MihomoBin() string {
	return filepath.Join(p.Bin, mihomoBinName())
}

// MihomoConfig returns the full path to the generated mihomo config.yaml.
func (p *Paths) MihomoConfig() string {
	return filepath.Join(p.Data, "config.yaml")
}

// ProvidersDir returns the directory mihomo proxy-providers are cached in.
func (p *Paths) ProvidersDir() string {
	return filepath.Join(p.Data, "providers")
}

// UIIndex returns the expected zashboard entrypoint file.
func (p *Paths) UIIndex() string {
	return filepath.Join(p.UI, "index.html")
}

// selfBinName is the canonical name of the installed zashhomo executable.
func selfBinName() string {
	if runtime.GOOS == "windows" {
		return "zashhomo.exe"
	}
	return "zashhomo"
}

// InstallDir returns the directory the zashhomo executable is installed into and
// which should be on the user's PATH. Can be overridden with ZASHHOMO_BIN.
func InstallDir() string {
	if v := os.Getenv("ZASHHOMO_BIN"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		la := os.Getenv("LOCALAPPDATA")
		if la == "" {
			la = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(la, "Programs", "zashhomo")
	default:
		if isRoot() {
			return "/usr/local/bin"
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "bin")
	}
}

// SelfExe returns the canonical full path of the installed zashhomo executable.
func SelfExe() string {
	return filepath.Join(InstallDir(), selfBinName())
}
