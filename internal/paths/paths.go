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
	State  string // per-user UI state dir (onboarding marker)
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

// The root-owned locations a system-wide Unix install uses. They are named
// because two things need them: resolving the paths when running as root, and
// spotting an unprivileged session that is about to edit a *different* config
// than the installed service reads (see ForeignSystemInstall).
const (
	systemDataDir   = "/var/lib/zashhomo"
	systemConfigDir = "/etc/zashhomo"
)

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
			return systemDataDir
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
			return systemConfigDir
		}
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "zashhomo")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "zashhomo")
	}
}

// stateDir picks the directory for per-user interface state — things the person
// at the keyboard owns rather than the daemon, such as "the onboarding guide has
// already been offered". It deliberately does not follow dataDir: on Windows the
// data dir lives under ProgramData, which an unelevated session cannot write to,
// and this state must be writable by whoever runs the menu.
func stateDir() string {
	if v := os.Getenv("ZASHHOMO_STATE_DIR"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		// No usable per-user location; fall back to the shared config dir, which
		// at worst makes the marker unwritable and the prompt reappear.
		return configDir()
	}
	return filepath.Join(dir, "zashhomo")
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
		State:  stateDir(),
	}
}

// OnboardMark returns the marker file recording that the guided setup has been
// offered to this user. Its presence — not its contents — is the signal.
func (p *Paths) OnboardMark() string {
	return filepath.Join(p.State, "onboarded")
}

// Writable reports whether this process can modify the file at path, or create
// it when it does not exist yet. Callers use it to decide whether an operation
// has to be re-run elevated.
//
// The probe deliberately targets the file itself rather than its directory. On
// Windows the data lives under ProgramData, whose default ACL lets a standard
// user *create* new files while denying writes to the files an elevated install
// already wrote — so probing the directory with a fresh temp file would report
// "writable" on precisely the case that needs elevation.
func Writable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return true
	}
	if !os.IsNotExist(err) {
		return false
	}
	// Nothing there yet, so the real question is whether the tree would accept a
	// new file. Walk up to the nearest directory that exists — on a first run the
	// parents have to be created too — and probe there instead.
	dir := filepath.Dir(path)
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false // walked to the root without finding anything
		}
		dir = parent
	}
	probe, err := os.CreateTemp(dir, ".zashhomo-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return true
}

// ManagedWritable reports whether this session can modify the files zashhomo
// manages on the user's behalf. It probes the self config and the generated
// mihomo config separately: they are written by different code paths, so after a
// mixed sequence of elevated and plain runs their ownership can genuinely
// differ, and being able to write one says nothing about the other.
func (p *Paths) ManagedWritable() bool {
	return Writable(p.Config) && Writable(p.MihomoConfig())
}

// ForeignSystemInstall returns the config path of a root-owned install that this
// unprivileged session is *not* looking at, or "" when there is nothing to warn
// about. It is the Unix counterpart to the Windows elevation check: there, a
// write to ProgramData fails loudly; here New() silently resolves to the user's
// own ~/.config copy instead, so an unprivileged `zashhomo sub add` would report
// success while the installed service keeps reading /etc/zashhomo.
//
// An explicit ZASHHOMO_CONFIG_DIR means the caller has chosen a location on
// purpose, so it is left alone.
func ForeignSystemInstall() string {
	return foreignSystemInstall(filepath.Join(systemConfigDir, "zashhomo.yaml"))
}

// foreignSystemInstall is ForeignSystemInstall with the system config location
// as a parameter, so the decision can be tested without a root-owned /etc.
func foreignSystemInstall(sysConfig string) string {
	if runtime.GOOS == "windows" || isRoot() {
		return ""
	}
	if os.Getenv("ZASHHOMO_CONFIG_DIR") != "" {
		return ""
	}
	if _, err := os.Stat(sysConfig); err != nil {
		return ""
	}
	return sysConfig
}

// EnsureDirs creates the directories zashhomo writes into.
func (p *Paths) EnsureDirs() error {
	dirs := []string{
		p.Data,
		p.Bin,
		p.UI,
		filepath.Join(p.Data, "providers"),
		filepath.Join(p.Data, "subs"),
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

// SubsDir returns the directory each subscription's fetched clash document is
// cached in, one file per subscription. Keeping every profile on disk is what
// lets zashhomo switch between them without going back to the network.
func (p *Paths) SubsDir() string {
	return filepath.Join(p.Data, "subs")
}

// SubFile returns the cache file holding the document last fetched for the
// subscription identified by id.
func (p *Paths) SubFile(id string) string {
	return filepath.Join(p.SubsDir(), id+".yaml")
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
