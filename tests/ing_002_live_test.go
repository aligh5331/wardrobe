//go:build integration

// Live E2E acceptance test for ING-002 against a real llama.cpp VLM.
// Opt-in: run with
//
//	VLM_URL=http://192.168.0.100:2525 go test -tags=integration ./tests/ -run ING002
//
// Skips when VLM_URL is unset, so unit/CI runs never touch the network.
package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wardrobe/internal/tagging"
)

// taxonomyCategories and taxonomyColors mirror 03-taxonomy.md; the live
// response is checked against them rather than trusting the ticket text
// alone.
var taxonomyCategories = map[string]bool{
	"top": true, "bottom": true, "outerwear": true,
	"footwear": true, "headwear": true, "accessory": true,
}

var taxonomyColors = map[string]bool{
	"black": true, "white": true, "gray": true, "navy": true, "blue": true,
	"red": true, "green": true, "olive": true, "brown": true, "tan": true,
	"beige": true, "burgundy": true, "pink": true, "purple": true,
	"yellow": true, "orange": true,
}

// findSamplePhoto returns the first image in <root>/temp/, per the
// sample image Ali left there for this ticket.
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

// AC1 live: a real request against the running VLM returns the raw
// model text, which is a JSON record validating against
// 03-taxonomy.md. Empty VLM_API_KEY means no auth header (the server is
// keyless).
func TestING002_Live_AC1_RealVLMTagging(t *testing.T) {
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
	if strings.TrimSpace(raw) == "" {
		t.Fatal("Tag() returned empty raw content from a reachable VLM")
	}
	t.Logf("raw VLM response:\n%s", raw)

	var rec struct {
		Category       string   `json:"category"`
		Subcategory    string   `json:"subcategory"`
		DominantColor  string   `json:"dominant_color"`
		SecondaryColor []string `json:"secondary_colors"`
		Pattern        string   `json:"pattern"`
		WarmthTier     string   `json:"warmth_tier"`
		Formality      string   `json:"formality"`
	}
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatalf("raw response is not valid JSON (05-vlm-tagging-spec.md Output validation): %v\nraw: %s", err, raw)
	}
	if !taxonomyCategories[rec.Category] {
		t.Errorf("category %q is not in 03-taxonomy.md's category list", rec.Category)
	}
	if !taxonomyColors[rec.DominantColor] {
		t.Errorf("dominant_color %q is not in 03-taxonomy.md's color palette", rec.DominantColor)
	}
	for _, c := range rec.SecondaryColor {
		if !taxonomyColors[c] {
			t.Errorf("secondary_colors contains %q, not in 03-taxonomy.md's color palette", c)
		}
	}
}
