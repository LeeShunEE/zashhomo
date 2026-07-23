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

	// Animate this step: spinner while fetching/extracting, progress bar while
	// downloading. Finalized with the tag on success or "failed" on error.
	st := ui.NewStage("Installing zashboard panel")
	st.Start()
	defer func() {
		if err != nil {
			st.Done("failed")
		} else {
			st.Done(fmt.Sprintf("%s ✓", tag))
		}
	}()

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
