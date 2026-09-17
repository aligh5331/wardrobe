//go:build integration

// Live E2E acceptance test for ING-004 against a real llama.cpp VLM.
// Opt-in: run with
//
//	VLM_URL=http://192.168.0.100:2525 go test -tags=integration ./tests/ -run ING004
//
// Skips when VLM_URL is unset, so unit/CI runs never touch the network.
package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wardrobe/internal/tagging"
)

// findSamplePhoto returns the first image in <root>/temp/, the sample
// garment photo the live E2E test tags.
func findSamplePhoto(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(moduleRoot(t), "temp")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp/: %v", err)
	}
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".jpg", ".jpeg", ".png", ".webp":
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("no sample image found in %s", dir)
	return ""
}

// AC1 live: a real request against the running VLM returns raw text that
// ParseTaggingResult accepts, and the resulting typed record matches the
// vocabulary in 03-taxonomy.md. This checks the parser against real model
// output, not just hand-built JSON.
func TestING004_Live_AC1_RealVLMOutputValidates(t *testing.T) {
	vlmURL := os.Getenv("VLM_URL")
	if vlmURL == "" {
		t.Skip("VLM_URL not set; live E2E skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := tagging.NewClient(vlmURL, "")
	raw, err := client.Tag(ctx, findSamplePhoto(t))
	if err != nil {
		t.Fatalf("Tag() against %s error = %v", vlmURL, err)
	}
	t.Logf("raw VLM response:\n%s", raw)

	got, err := tagging.ParseTaggingResult(raw)
	if err != nil {
		t.Fatalf("ParseTaggingResult() rejected real VLM output: %v\nraw: %s", err, raw)
	}
	t.Logf("validated result: %+v", got)

	tax := loadTaxonomy(t)
	if !containsStr(tax.categories, got.Category) {
		t.Errorf("category %q is not in 03-taxonomy.md's category list", got.Category)
	}
	if !containsStr(tax.subcategories[got.Category], got.Subcategory) {
		t.Errorf("subcategory %q is not valid for category %q", got.Subcategory, got.Category)
	}
	if !containsStr(tax.colors, got.DominantColor) {
		t.Errorf("dominant_color %q is not in 03-taxonomy.md's palette", got.DominantColor)
	}
	for _, c := range got.SecondaryColors {
		if !containsStr(tax.colors, c) {
			t.Errorf("secondary_colors contains %q, not in 03-taxonomy.md's palette", c)
		}
	}
	if !containsStr(tax.patterns, got.Pattern) {
		t.Errorf("pattern %q is not in 03-taxonomy.md", got.Pattern)
	}
	if !containsStr(tax.warmthTiers, got.WarmthTier) {
		t.Errorf("warmth_tier %q is not in 03-taxonomy.md", got.WarmthTier)
	}
	if !containsStr(tax.formalities, got.Formality) {
		t.Errorf("formality %q is not in 03-taxonomy.md", got.Formality)
	}
}

func containsStr(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
