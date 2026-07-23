// Package ghrelease fetches GitHub release metadata, selects the right asset
// for the current platform, and downloads it.
package ghrelease

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Asset is a single downloadable release asset.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is the subset of the GitHub release API we consume.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

// Latest fetches the latest published release for owner/repo (e.g. "MetaCubeX/mihomo").
func Latest(repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "zashhomo")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github: %s: %s: %s", repo, resp.Status, strings.TrimSpace(string(body)))
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("github: %s: empty release", repo)
	}
	return &rel, nil
}

// FindByExactName returns the asset whose name equals name, or nil.
func (r *Release) FindByExactName(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// FindBest returns the asset scoring highest under score (>0), or nil.
func (r *Release) FindBest(score func(name string) int) *Asset {
	var best *Asset
	bestScore := 0
	for i := range r.Assets {
		s := score(r.Assets[i].Name)
		if s > bestScore {
			bestScore = s
			best = &r.Assets[i]
		}
	}
	return best
}

// MihomoAsset picks the mihomo kernel asset for the current platform from rel.
//
// mihomo names assets like: mihomo-<os>-<arch><variant>-<tag>.<ext>
//   - linux/darwin -> gzip (.gz), windows -> zip (.zip)
//   - amd64 prefers the "-compatible" build (widest CPU support)
func MihomoAsset(rel *Release) (*Asset, error) {
	goos := runtime.GOOS
	ext := "gz"
	if goos == "windows" {
		ext = "zip"
	}

	arch := mihomoArch()
	// Exact preferred name first.
	preferred := fmt.Sprintf("mihomo-%s-%s-%s.%s", goos, arch, rel.TagName, ext)
	if a := rel.FindByExactName(preferred); a != nil {
		return a, nil
	}

	// Fallback: score by matching os/arch tokens + extension, preferring compatible.
	a := rel.FindBest(func(name string) int {
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, "mihomo-"+goos+"-") {
			return 0
		}
		if !strings.HasSuffix(lower, "."+ext) {
			return 0
		}
		if !strings.Contains(lower, archBase()) {
			return 0
		}
		// Reject mismatched arm variants (e.g. armv7 when we want arm64).
		if archBase() == "arm64" && strings.Contains(lower, "armv") {
			return 0
		}
		score := 10
		if strings.Contains(lower, "-compatible") && archBase() == "amd64" {
			score += 5
		}
		// Prefer names carrying the tag (avoids alpha/other stray assets).
		if strings.Contains(lower, strings.ToLower(rel.TagName)) {
			score += 3
		}
		return score
	})
	if a == nil {
		return nil, fmt.Errorf("no mihomo asset for %s/%s in %s", goos, runtime.GOARCH, rel.TagName)
	}
	return a, nil
}

// archBase maps GOARCH to the mihomo arch token base.
func archBase() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "armv7"
	case "386":
		return "386"
	default:
		return runtime.GOARCH
	}
}

// mihomoArch maps GOARCH to the preferred mihomo arch token (with variant).
func mihomoArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64-compatible"
	default:
		return archBase()
	}
}

// PanelAsset picks the zashboard static bundle. Prefers full dist.zip.
func PanelAsset(rel *Release) (*Asset, error) {
	if a := rel.FindByExactName("dist.zip"); a != nil {
		return a, nil
	}
	a := rel.FindBest(func(name string) int {
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".zip") {
			return 0
		}
		switch {
		case lower == "dist.zip":
			return 20
		case strings.HasPrefix(lower, "dist") && !strings.Contains(lower, "no-fonts"):
			return 10
		case strings.Contains(lower, "dist"):
			return 5
		}
		return 0
	})
	if a == nil {
		return nil, fmt.Errorf("no zashboard dist asset in %s", rel.TagName)
	}
	return a, nil
}

// Download fetches url into a temp file next to dest and atomically renames it.
// It returns the final path (== dest).
func Download(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "zashhomo")
	// Downloads use a longer timeout than the API client.
	dlClient := &http.Client{Timeout: 15 * time.Minute}
	resp, err := dlClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpName)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return closeErr
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
