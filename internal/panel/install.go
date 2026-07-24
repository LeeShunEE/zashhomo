// Package panel downloads and installs the zashboard static web panel.
package panel

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LeeShunEE/zashhomo/internal/archive"
	"github.com/LeeShunEE/zashhomo/internal/ghrelease"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/ui"
)

// ZashboardRepo is the upstream zashboard repository.
const ZashboardRepo = "Zephyruso/zashboard"

// Install downloads (or updates) the zashboard bundle into p.UI and returns the
// installed release tag. If currentVersion matches the latest tag and the panel
// is already present, the download is skipped (updated=false).
func Install(p *paths.Paths, currentVersion string) (tag string, updated bool, err error) {
	// Animate the whole step: spinner while querying/extracting, progress bar
	// while downloading. It starts before the release query because that is a
	// network call which can stall for a long time on a slow or blocked
	// connection — exactly when the user most needs to see progress.
	st := ui.NewStage("Installing zashboard panel")
	st.Start()
	defer func() {
		switch {
		case err != nil:
			st.Done("failed")
		case !updated:
			st.Done(fmt.Sprintf("%s (up to date)", tag))
		default:
			st.Done(fmt.Sprintf("%s ✓", tag))
		}
	}()

	rel, err := ghrelease.Latest(ZashboardRepo)
	if err != nil {
		return "", false, fmt.Errorf("panel: fetch release: %w", err)
	}
	if rel.TagName == currentVersion && fileExists(p.UIIndex()) {
		return rel.TagName, false, nil
	}

	asset, err := ghrelease.PanelAsset(rel)
	if err != nil {
		return "", false, err
	}
	if err := p.EnsureDirs(); err != nil {
		return "", false, err
	}

	dl := filepath.Join(p.Data, asset.Name)
	if err := st.Download(asset.URL, dl); err != nil {
		return "", false, fmt.Errorf("panel: download %s: %w", asset.Name, err)
	}
	defer os.Remove(dl)

	// Replace the UI directory contents.
	if err := os.RemoveAll(p.UI); err != nil {
		return "", false, err
	}
	if err := archive.UnzipAllTo(dl, p.UI); err != nil {
		return "", false, fmt.Errorf("panel: unzip: %w", err)
	}
	if !fileExists(p.UIIndex()) {
		return "", false, fmt.Errorf("panel: index.html missing after extract")
	}

	return rel.TagName, true, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
