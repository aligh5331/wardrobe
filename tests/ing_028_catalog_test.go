// Package tests holds black-box acceptance tests for backlog tickets.
// ING-028: internal/catalog.Create is the single rollback-safe create —
// photo copy + row insert — shared by cmd/ingest and (later, ING-032) the
// API. These tests exercise the exported catalog API against a real store
// on a throwaway path; the repo's real data/ is never touched.
package tests

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"wardrobe/internal/catalog"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

// ing028Store opens a real store on a throwaway DB path and closes it when
// the test ends.
func ing028Store(t *testing.T) *store.Store {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "data", "wardrobe.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// ing028PhotosDir returns an isolated photos directory (created lazily by
// Create, not by the helper).
func ing028PhotosDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "photos")
}

// ing028Result returns a taxonomy-valid tagging result parsed from
// 03-taxonomy.md via the shared helper.
func ing028Result(t *testing.T) tagging.TaggingResult {
	t.Helper()
	_, result := validTaggingPayload(t, loadTaxonomy(t))
	return result
}

// AC: a create succeeds → the copy holds the source bytes and exactly one
// catalog row exists with that id, the seven fields, added_date at create
// time, empty notes, and photo_path pointing at the copy (ING-018's happy
// path, unchanged).
func TestING028_Create_SuccessCopiesPhotoAndInsertsRow(t *testing.T) {
	src := []byte("ING-028-SOURCE-BYTES")
	photo := writePhoto(t, "garment.jpg", src)
	photos := ing028PhotosDir(t)
	st := ing028Store(t)
	result := ing028Result(t)
	id := "0f0f0f0f-0f0f-4f0f-8f0f-0f0f0f0f0f0f"

	before := time.Now()
	item, err := catalog.Create(st, photos, id, photo, result, "")
	after := time.Now()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	wantCopy := filepath.Join(photos, id+".jpg")
	if item.PhotoPath != wantCopy {
		t.Errorf("item.PhotoPath = %q, want the copy %q", item.PhotoPath, wantCopy)
	}
	if item.Notes != "" {
		t.Errorf("item.Notes = %q, want empty", item.Notes)
	}
	if item.AddedDate.Before(before) || item.AddedDate.After(after) {
		t.Errorf("item.AddedDate = %s, want within [%s, %s]", item.AddedDate, before, after)
	}
	if item.Category != result.Category || item.Subcategory != result.Subcategory ||
		item.DominantColor != result.DominantColor || item.Pattern != result.Pattern ||
		item.WarmthTier != result.WarmthTier || item.Formality != result.Formality {
		t.Errorf("item tagging fields = %+v, want %+v", item, result)
	}
	if !slices.Equal(item.SecondaryColors, result.SecondaryColors) {
		t.Errorf("item.SecondaryColors = %v, want %v", item.SecondaryColors, result.SecondaryColors)
	}

	gotCopy, err := os.ReadFile(wantCopy)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if !bytes.Equal(gotCopy, src) {
		t.Errorf("copy bytes = %q, want source bytes %q", gotCopy, src)
	}
	gotSrc, err := os.ReadFile(photo)
	if err != nil {
		t.Fatalf("re-read source: %v", err)
	}
	if !bytes.Equal(gotSrc, src) {
		t.Errorf("source photo was modified: %q -> %q", src, gotSrc)
	}

	rows, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows = %d, want exactly 1", len(rows))
	}
	assertItemEqual(t, rows[0], item)
}

// AC: the catalog row insert fails after the photo copy succeeded → the
// error is returned, the copied photo is removed (no orphan), and no row
// for that id exists (the pre-existing fixture row is untouched).
// Insert failure induced per the ticket's testability note: a pre-inserted
// row with the same primary key.
func TestING028_Create_InsertFailureRemovesCopiedPhoto(t *testing.T) {
	photo := writePhoto(t, "garment.jpg", []byte("ING-028-INSERT-FAIL"))
	photos := ing028PhotosDir(t)
	st := ing028Store(t)
	result := ing028Result(t)
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	fixture := sampleItem(id)
	if err := st.Insert(fixture); err != nil {
		t.Fatalf("insert fixture row: %v", err)
	}

	item, err := catalog.Create(st, photos, id, photo, result, "")
	if err == nil {
		t.Fatal("Create with duplicate id: want error, got nil")
	}
	if item.ID != "" {
		t.Errorf("returned item on failure = %+v, want the zero Item", item)
	}

	dest := filepath.Join(photos, id+".jpg")
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("copied photo %s still exists after failed insert (stat err=%v); want no orphan", dest, err)
	}
	entries, err := os.ReadDir(photos)
	if err != nil {
		t.Fatalf("read photos dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("photos dir has %d entries, want 0 (no orphan)", len(entries))
	}

	rows, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows = %d, want exactly the 1 pre-existing fixture row", len(rows))
	}
	assertItemEqual(t, rows[0], fixture)
}

// AC: the photo copy fails → no catalog row is inserted for that id.
func TestING028_Create_CopyFailureInsertsNoRow(t *testing.T) {
	photos := ing028PhotosDir(t)
	st := ing028Store(t)
	result := ing028Result(t)
	id := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	missing := filepath.Join(t.TempDir(), "does-not-exist.jpg")

	if _, err := catalog.Create(st, photos, id, missing, result, ""); err == nil {
		t.Fatal("Create with missing source: want error, got nil")
	}

	rows, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("catalog rows = %d, want 0 after a failed copy", len(rows))
	}
	if entries, err := os.ReadDir(photos); err == nil && len(entries) != 0 {
		t.Errorf("photos dir has %d entries, want 0 after a failed copy", len(entries))
	}
}

// AC: a create failed and left no row / the item is processed again with a
// fresh id → the catalog holds exactly one row and the photos directory
// exactly one photo, with no residue from the failed attempt.
func TestING028_Create_RetryAfterFailureLeavesNoResidue(t *testing.T) {
	photos := ing028PhotosDir(t)
	st := ing028Store(t)
	result := ing028Result(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.jpg")

	if _, err := catalog.Create(st, photos, "cccccccc-cccc-4ccc-8ccc-cccccccccccc", missing, result, ""); err == nil {
		t.Fatal("first Create with missing source: want error, got nil")
	}

	src := []byte("ING-028-RETRY")
	photo := writePhoto(t, "retry.jpg", src)
	retryID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	item, err := catalog.Create(st, photos, retryID, photo, result, "")
	if err != nil {
		t.Fatalf("retry Create: %v", err)
	}

	rows, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows = %d, want exactly 1 (the retry)", len(rows))
	}
	if rows[0].ID != retryID {
		t.Errorf("row id = %q, want the retry id %q", rows[0].ID, retryID)
	}

	entries, err := os.ReadDir(photos)
	if err != nil {
		t.Fatalf("read photos dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("photos dir has %d entries, want exactly 1 (the retry's copy)", len(entries))
	}
	if entries[0].Name() != retryID+".jpg" {
		t.Errorf("photo file = %q, want %q", entries[0].Name(), retryID+".jpg")
	}
	if item.ID != retryID {
		t.Errorf("returned item id = %q, want %q", item.ID, retryID)
	}
}
