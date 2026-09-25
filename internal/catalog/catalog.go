// Package catalog is the one shared photo-copy + row-insert create for the
// wardrobe catalog. cmd/ingest and the API both call Create — neither keeps
// its own copy of the logic (06-decisions.md "Second catalog writer exists:
// extract shared persistence out of cmd/ingest").
package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

// PhotosDir is where stored photo copies live, relative to the project root
// (07-architecture.md "Backend"). Callers pass it into Create so tests can
// point at an isolated directory instead of the real data/photos/.
const PhotosDir = "data/photos"

// Create copies sourcePhoto to photosDir/<id><source-ext> and inserts the
// matching catalog row: the given id, the seven tagging fields, added_date
// set at create time, the given notes, and photo_path at the copy — never
// the source path (04-data-schema.md "Write-path rules"). The source photo
// is only read, never modified.
//
// Rollback-safe per 06-decisions.md "Catalog writes must not leave orphan
// photos or dangling rows": a failed copy inserts no row; a failed insert
// removes the copy it just made. On any failure a zero Item and the error
// are returned, so a retry (which the pipelines give a fresh id) leaves no
// residue.
func Create(st *store.Store, photosDir, id, sourcePhoto string, result tagging.TaggingResult, notes string) (store.Item, error) {
	item := store.Item{
		ID:              id,
		Category:        result.Category,
		Subcategory:     result.Subcategory,
		DominantColor:   result.DominantColor,
		SecondaryColors: result.SecondaryColors,
		Pattern:         result.Pattern,
		WarmthTier:      result.WarmthTier,
		Formality:       result.Formality,
		AddedDate:       time.Now(),
		Notes:           notes,
	}

	if err := os.MkdirAll(photosDir, 0o755); err != nil {
		return store.Item{}, err
	}

	dest := filepath.Join(photosDir, id+filepath.Ext(sourcePhoto))
	data, err := os.ReadFile(sourcePhoto)
	if err != nil {
		return store.Item{}, err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return store.Item{}, rollback(dest, err)
	}
	item.PhotoPath = dest

	if err := st.Insert(item); err != nil {
		return store.Item{}, rollback(dest, err)
	}
	return item, nil
}

// rollback removes the copy a failed create must not leave behind, keeping
// cause as the returned error. A removal failure is joined in, never
// swallowed; a copy that was never created (ErrNotExist) is not an error.
func rollback(dest string, cause error) error {
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(cause, err)
	}
	return cause
}
