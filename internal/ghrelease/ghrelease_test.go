package ghrelease

import (
	"testing"
)

func TestReleaseFindByExactName(t *testing.T) {
	rel := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "mihomo-linux-amd64.gz", URL: "url1", Size: 1000},
			{Name: "mihomo-windows-amd64.zip", URL: "url2", Size: 2000},
		},
	}

	// Found
	a := rel.FindByExactName("mihomo-linux-amd64.gz")
	if a == nil {
		t.Fatal("expected to find asset")
	}
	if a.Name != "mihomo-linux-amd64.gz" {
		t.Errorf("name = %q", a.Name)
	}

	// Not found
	a = rel.FindByExactName("nonexistent")
	if a != nil {
		t.Error("expected nil for nonexistent asset")
	}
}

func TestReleaseFindBest(t *testing.T) {
	rel := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "a.txt", URL: "url1", Size: 100},
			{Name: "b.txt", URL: "url2", Size: 200},
			{Name: "c.txt", URL: "url3", Size: 300},
		},
	}

	// Find highest score
	a := rel.FindBest(func(name string) int {
		if name == "b.txt" {
			return 10
		}
		return 1
	})
	if a == nil || a.Name != "b.txt" {
		t.Errorf("expected b.txt, got %v", a)
	}

	// No match (all scores <= 0)
	a = rel.FindBest(func(name string) int {
		return 0
	})
	if a != nil {
		t.Error("expected nil when all scores are 0")
	}
}

func TestArchBase(t *testing.T) {
	// Just verify function doesn't panic and returns non-empty
	got := archBase()
	if got == "" {
		t.Error("archBase returned empty string")
	}
}

func TestMihomoArch(t *testing.T) {
	// Just verify function doesn't panic
	got := mihomoArch()
	if got == "" {
		t.Error("mihomoArch returned empty string")
	}
}

func TestPanelAssetFindsDistZip(t *testing.T) {
	rel := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "dist.zip", URL: "url1", Size: 1000},
			{Name: "other.txt", URL: "url2", Size: 100},
		},
	}

	a, err := PanelAsset(rel)
	if err != nil {
		t.Fatalf("PanelAsset failed: %v", err)
	}
	if a.Name != "dist.zip" {
		t.Errorf("name = %q, want dist.zip", a.Name)
	}
}

func TestPanelAssetNoAsset(t *testing.T) {
	rel := &Release{
		TagName: "v1.0.0",
		Assets:  []Asset{},
	}

	_, err := PanelAsset(rel)
	if err == nil {
		t.Error("expected error for empty assets")
	}
}