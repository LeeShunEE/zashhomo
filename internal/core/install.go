// Package core manages the mihomo kernel lifecycle: install/update and supervise.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// Animate the whole step: spinner while querying/extracting, progress bar
	// while downloading. It starts before the release query because that is a
	// network call which can stall for a long time on a slow or blocked
	// connection — exactly when the user most needs to see progress.
	st := ui.NewStage("Installing mihomo kernel")
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

	dl := filepath.Join(p.Bin, asset.Name)
	if err := st.Download(asset.URL, dl); err != nil {
		return "", false, fmt.Errorf("core: download %s: %w", asset.Name, err)
	}
	defer os.Remove(dl)

	bin := p.MihomoBin()
	extract := func() error {
		switch {
		case strings.HasSuffix(asset.Name, ".gz"):
			if err := archive.GunzipTo(dl, bin); err != nil {
				return fmt.Errorf("core: gunzip: %w", err)
			}
		case strings.HasSuffix(asset.Name, ".zip"):
			if err := archive.UnzipMemberTo(dl, []string{"mihomo", "windows", ".exe"}, bin); err != nil {
				return fmt.Errorf("core: unzip: %w", err)
			}
		default:
			return fmt.Errorf("core: unsupported asset %q", asset.Name)
		}
		return nil
	}

	// The service is normally supervising the kernel while it is updated, and
	// Windows refuses to overwrite a running executable however elevated the
	// caller is. Renaming it *is* allowed, so move it aside to free the name.
	stash, err := stashBinary(bin)
	if err != nil {
		return "", false, fmt.Errorf("core: move the running kernel aside: %w", err)
	}
	if err := extract(); err != nil {
		// Put the working kernel back: a failed update must not leave the service
		// supervising a name with nothing behind it.
		if stash != "" {
			_ = os.Rename(stash, bin)
		}
		return "", false, err
	}
	// Best effort — the displaced file is still mapped by the running process, so
	// it usually cannot be deleted until the service restarts. The next update
	// sweeps whatever is left, and the stale copy is inert in the meantime.
	if stash != "" {
		_ = os.Remove(stash)
	}

	return rel.TagName, true, nil
}

// stashBinary renames an existing binary out of the way so a new one can take
// its place, returning the path it was moved to ("" when there was nothing to
// move). It exists because Windows holds an image section on a running
// executable: the file cannot be replaced or deleted, but it can be renamed,
// and the running process keeps executing from the renamed copy until it is
// restarted.
//
// The stash name is unique per call. Reusing a fixed ".old" would fail the
// second time around, since renaming onto it would hit the same still-running
// file that could not be deleted on the previous update.
func stashBinary(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", nil // nothing installed yet
	}
	sweepStashes(path)
	stash := fmt.Sprintf("%s.old-%d", path, time.Now().UnixNano())
	if err := os.Rename(path, stash); err != nil {
		return "", err
	}
	return stash, nil
}

// sweepStashes deletes the displaced binaries earlier updates left behind. Ones
// still held by a running process simply refuse to go and are tried again next
// time, so failures are ignored.
func sweepStashes(path string) {
	old, err := filepath.Glob(path + ".old-*")
	if err != nil {
		return
	}
	for _, f := range old {
		_ = os.Remove(f)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
