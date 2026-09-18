// Command ingest runs the Phase 1 ingestion pipeline: it takes a garment
// photo path (or directory of photos), builds the VLM client from config,
// processes each photo through the tagging pipeline with the settled
// retry-once policy, and emits validated tagged JSON to stdout.
//
// Usage: ingest <photo-path> [photo-path...]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"wardrobe/internal/config"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"

	_ "github.com/joho/godotenv/autoload"
)

// photosDir holds the stored copies of ingested garment photos, relative to
// the project root (07-architecture.md "Backend").
const photosDir = "data/photos"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("startup: %v", err)
	}

	for _, warning := range cfg.Warnings() {
		log.Printf("startup warning: %s", warning)
	}

	if len(os.Args) < 2 {
		log.Fatalf("usage: ingest <photo-path> [photo-path...]")
	}

	st, err := store.Open(store.DefaultDBPath)
	if err != nil {
		log.Fatalf("startup: open store: %v", err)
	}
	defer st.Close()

	opts := []tagging.Option{
		tagging.WithTemperature(func() float64 { return cfg.VLMTemperature }),
	}
	if cfg.VLMSerializeRequests {
		opts = append(opts, tagging.WithSerialization(cfg.VLMRequestDelayMS))
	}
	client := tagging.NewClient(cfg.VLMURL, cfg.VLMAPIKey, opts...)

	processor := tagging.NewProcessor(client)

	ctx := context.Background()

	for _, arg := range os.Args[1:] {
		paths, err := expandPaths(arg)
		if err != nil {
			log.Printf("error expanding %s: %v", arg, err)
			os.Exit(1)
		}

		for _, photoPath := range paths {
			outcome, err := processor.Process(ctx, photoPath)
			if err != nil {
				if errors.Is(err, tagging.ErrVLMUnreachable) {
					log.Printf("VLM unreachable for %s: %v", photoPath, err)
					os.Exit(1)
				}
				log.Printf("hard error processing %s: %v", photoPath, err)
				os.Exit(1)
			}

			result := map[string]any{
				"item_id":          outcome.ItemID,
				"photo_path":       outcome.PhotoPath,
				"category":         outcome.Result.Category,
				"subcategory":      outcome.Result.Subcategory,
				"dominant_color":   outcome.Result.DominantColor,
				"secondary_colors": outcome.Result.SecondaryColors,
				"pattern":          outcome.Result.Pattern,
				"warmth_tier":      outcome.Result.WarmthTier,
				"formality":        outcome.Result.Formality,
				"flagged":          outcome.Flagged,
			}

			jsonBytes, err := json.Marshal(result)
			if err != nil {
				log.Printf("encode result for %s: %v", photoPath, err)
				os.Exit(1)
			}
			fmt.Println(string(jsonBytes))

			if outcome.Flagged {
				continue
			}

			if err := persist(st, outcome); err != nil {
				log.Printf("persist %s: %v", photoPath, err)
				os.Exit(1)
			}
		}
	}
}

// persist copies the tagged photo into data/photos/ and writes the matching
// catalog row, reusing the item id the tagging processor already generated.
// It is only called for non-flagged outcomes. A failed copy or insert is
// returned to the caller; retry/rollback/cleanup is deliberately out of scope
// for this sprint.
func persist(st *store.Store, outcome tagging.Outcome) error {
	if err := os.MkdirAll(photosDir, 0o755); err != nil {
		return err
	}

	dest := filepath.Join(photosDir, outcome.ItemID+filepath.Ext(outcome.PhotoPath))
	data, err := os.ReadFile(outcome.PhotoPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return err
	}

	return st.Insert(store.Item{
		ID:              outcome.ItemID,
		Category:        outcome.Result.Category,
		Subcategory:     outcome.Result.Subcategory,
		DominantColor:   outcome.Result.DominantColor,
		SecondaryColors: outcome.Result.SecondaryColors,
		Pattern:         outcome.Result.Pattern,
		WarmthTier:      outcome.Result.WarmthTier,
		Formality:       outcome.Result.Formality,
		PhotoPath:       dest,
		AddedDate:       time.Now(),
		Notes:           "",
	})
}

func expandPaths(arg string) ([]string, error) {
	info, err := os.Stat(arg)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return []string{arg}, nil
	}

	entries, err := os.ReadDir(arg)
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
			paths = append(paths, filepath.Join(arg, entry.Name()))
		}
	}
	return paths, nil
}