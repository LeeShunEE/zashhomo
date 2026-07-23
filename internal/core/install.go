// Package core manages the mihomo kernel lifecycle: install/update and supervise.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LeeShunEE/zashhomo/internal/archive"
	"github.com/LeeShunEE/zashhomo/internal/ghrelease"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/ui"
)

// MihomoRepo is the upstream mihomo kernel repository.
const MihomoRepo = "MetaCubeX/mihomo"

// Install downloads (or updates) the mihomo kernel into p.Bin and returns the
// installed release tag. If currentVersion matches the latest tag the download
// is skipped and (tag, false, nil) is returned with updated=false.
func Install(p *paths.Paths, currentVersion string) (tag string, updated bool, err error) {
	rel, err := ghrelease.Latest(MihomoRepo)
	if err != nil {
		return "", false, fmt.Errorf("core: fetch release: %w", err)
	}
	if rel.TagName == currentVersion && fileExists(p.MihomoBin()) {
		return rel.TagName, false, nil
	}

	asset, err := ghrelease.MihomoAsset(rel)
	if err != nil {
		return "", false, err
	}
	if err := p.EnsureDirs(); err != nil {
		return "", false, err
	}

	// Animate this step: spinner while fetching/extracting, progress bar while
	// downloading. Finalized with the tag on success or "failed" on error.
	st := ui.NewStage("Installing mihomo kernel")
	st.Start()
	defer func() {
		if err != nil {
			st.Done("failed")
		} else {
			st.Done(fmt.Sprintf("%s ✓", tag))
		}
	}()

	dl := filepath.Join(p.Bin, asset.Name)
	if err := st.Download(asset.URL, dl); err != nil {
		return "", false, fmt.Errorf("core: download %s: %w", asset.Name, err)
	}
	defer os.Remove(dl)

	switch {
	case strings.HasSuffix(asset.Name, ".gz"):
		if err := archive.GunzipTo(dl, p.MihomoBin()); err != nil {
			return "", false, fmt.Errorf("core: gunzip: %w", err)
		}
	case strings.HasSuffix(asset.Name, ".zip"):
		if err := archive.UnzipMemberTo(dl, []string{"mihomo", "windows", ".exe"}, p.MihomoBin()); err != nil {
			return "", false, fmt.Errorf("core: unzip: %w", err)
		}
	default:
		return "", false, fmt.Errorf("core: unsupported asset %q", asset.Name)
	}

	return rel.TagName, true, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
