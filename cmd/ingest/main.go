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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"wardrobe/internal/catalog"
	"wardrobe/internal/config"
	"wardrobe/internal/logging"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	// Config errors go through slog.Default() (text, stderr) because the
	// configured logger needs a valid config to exist. stdout stays reserved
	// for the one-JSON-object-per-line result stream (ING-012).
	cfg, err := config.Load()
	if err != nil {
		slog.Error("startup: " + err.Error())
		os.Exit(1)
	}
	logger, _, err := logging.New(cfg.LogLevel, cfg.LogFormat, "logs/app.log", os.Stderr)
	if err != nil {
		slog.Error("startup: " + err.Error())
		os.Exit(1)
	}

	for _, warning := range cfg.Warnings() {
		logger.Warn("startup warning: " + warning)
	}

	if len(os.Args) < 2 {
		logger.Error("usage: ingest <photo-path> [photo-path...]")
		os.Exit(1)
	}

	st, err := store.Open(store.DefaultDBPath)
	if err != nil {
		logger.Error("startup: open store: " + err.Error())
		os.Exit(1)
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
			logger.Error("error expanding path", "path", arg, "error", err.Error())
			os.Exit(1)
		}

		for _, photoPath := range paths {
			outcome, err := processor.Process(ctx, photoPath)
			if err != nil {
				if errors.Is(err, tagging.ErrVLMUnreachable) {
					logger.Error("VLM unreachable", "path", photoPath, "error", err.Error())
					os.Exit(1)
				}
				logger.Error("hard error processing photo", "path", photoPath, "error", err.Error())
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
				logger.Error("encode result failed", "path", photoPath, "error", err.Error())
				os.Exit(1)
			}
			fmt.Println(string(jsonBytes))

			if outcome.Flagged {
				continue
			}

			// Shared rollback-safe create (ING-028); the ingest path
			// supplies no notes yet. Failures still surface in the
			// existing CLI style: log "persist" with the path and exit 1.
			if _, err := catalog.Create(st, catalog.PhotosDir, outcome.ItemID, outcome.PhotoPath, outcome.Result, ""); err != nil {
				logger.Error("persist failed", "path", photoPath, "error", err.Error())
				os.Exit(1)
			}
		}
	}
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
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
			paths = append(paths, filepath.Join(arg, entry.Name()))
		}
	}
	return paths, nil
}
