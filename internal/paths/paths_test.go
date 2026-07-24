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

// The onboarding marker records per-user interface state, so it must not land in
// the shared data dir — on Windows that is ProgramData, which an unelevated
// session cannot write to.
func TestStateDirIsIndependentOfDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ZASHHOMO_DATA", dataDir)

	p := New()
	if p.State == "" {
		t.Fatal("State should not be empty")
	}
	if p.State == p.Data {
		t.Error("State must not share the data dir")
	}
	if want := filepath.Join(p.State, "onboarded"); p.OnboardMark() != want {
		t.Errorf("OnboardMark = %q, want %q", p.OnboardMark(), want)
	}
}

func TestStateDirEnvOverride(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("ZASHHOMO_STATE_DIR", custom)

	if got := New().State; got != custom {
		t.Errorf("State = %q, want %q", got, custom)
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

func TestWritable(t *testing.T) {
	dir := t.TempDir()

	existing := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(existing, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !Writable(existing) {
		t.Error("Writable on a file this process owns = false, want true")
	}

	// Not created yet: the answer must come from whether the directory takes a
	// new file, which is what writing the config for the first time does.
	if !Writable(filepath.Join(dir, "not-there-yet.yaml")) {
		t.Error("Writable on a missing file in a writable dir = false, want true")
	}

}

// The whole point of the probe is catching a file this process may read but not
// modify — the state an elevated install leaves behind under ProgramData.
func TestWritableRejectsReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	// Restore write access so the temp dir can be cleaned up on Windows, where a
	// read-only attribute makes the file undeletable.
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if Writable(path) {
		t.Error("Writable on a read-only file = true, want false")
	}
}

func TestForeignSystemInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := ForeignSystemInstall(); got != "" {
			t.Errorf("ForeignSystemInstall on Windows = %q, want \"\" (elevation covers it there)", got)
		}
		return
	}
	if os.Geteuid() == 0 {
		t.Skip("a root session already resolves to the system paths")
	}

	dir := t.TempDir()
	sysConfig := filepath.Join(dir, "zashhomo.yaml")

	if got := foreignSystemInstall(sysConfig); got != "" {
		t.Errorf("foreignSystemInstall with no system install = %q, want \"\"", got)
	}

	if err := os.WriteFile(sysConfig, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := foreignSystemInstall(sysConfig); got != sysConfig {
		t.Errorf("foreignSystemInstall with a system install = %q, want %q", got, sysConfig)
	}

	// An explicit override means the user picked this location deliberately.
	t.Setenv("ZASHHOMO_CONFIG_DIR", dir)
	if got := foreignSystemInstall(sysConfig); got != "" {
		t.Errorf("foreignSystemInstall with ZASHHOMO_CONFIG_DIR set = %q, want \"\"", got)
	}
}

// A first run has to create the config's parent directories, so "the parent does
// not exist yet" must not read as "needs elevation" — that would send a plain
// Unix user chasing a sudo prompt for a file in their own home.
func TestWritableWalksUpToAnExistingParent(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c", "zashhomo.yaml")
	if !Writable(deep) {
		t.Error("Writable through missing-but-creatable parents = false, want true")
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Error("the probe created directories it should have left alone")
	}
}

func TestManagedWritable(t *testing.T) {
	t.Setenv("ZASHHOMO_DATA", t.TempDir())
	t.Setenv("ZASHHOMO_CONFIG_DIR", t.TempDir())
	if !New().ManagedWritable() {
		t.Error("ManagedWritable over temp dirs = false, want true")
	}
}

// Both files are probed, because an elevated install writes them by different
// code paths and a session may be able to write one but not the other.
func TestManagedWritableRejectsUnwritableDataDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	data := t.TempDir()
	t.Setenv("ZASHHOMO_DATA", data)
	t.Setenv("ZASHHOMO_CONFIG_DIR", t.TempDir())

	p := New()
	if err := os.WriteFile(p.MihomoConfig(), []byte("mixed-port: 9190\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.MihomoConfig(), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p.MihomoConfig(), 0o600) })

	if p.ManagedWritable() {
		t.Error("ManagedWritable with a read-only mihomo config = true, want false")
	}
}
