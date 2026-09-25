// Package tests holds black-box acceptance tests for backlog tickets.
// Kept under tests/ (never under internal/) per the Tester role: this
// agent does not modify implementation code.
package tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"wardrobe/internal/store"
)

// schemaColumns is every column 04-data-schema.md requires on the wardrobe
// item table, in the snake_case form GORM derives from store.Item's fields.
var schemaColumns = []string{
	"id",
	"category",
	"subcategory",
	"dominant_color",
	"secondary_colors",
	"pattern",
	"warmth_tier",
	"formality",
	"photo_path",
	"added_date",
	"notes",
}

// newStorePath returns a DB path whose parent data/ directory does not yet
// exist, inside a per-test scratch dir, so Open must create the parent.
func newStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "data", "wardrobe.db")
}

// sampleItem returns a fully populated store.Item using valid 03-taxonomy.md
// values, with every 04-data-schema.md field set to a distinct value.
func sampleItem(id string) store.Item {
	return store.Item{
		ID:              id,
		Category:        "top",
		Subcategory:     "t-shirt",
		DominantColor:   "black",
		SecondaryColors: []string{"white", "red"},
		Pattern:         "striped",
		WarmthTier:      "light",
		Formality:       "casual",
		PhotoPath:       "data/photos/" + id + ".jpg",
		AddedDate:       time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
		Notes:           "collar slightly frayed",
	}
}

// assertItemEqual compares every 04-data-schema.md field. added_date uses
// time.Equal so location/monotonic differences from the DB round-trip do not
// create false failures.
func assertItemEqual(t *testing.T, got, want store.Item) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Category != want.Category {
		t.Errorf("Category = %q, want %q", got.Category, want.Category)
	}
	if got.Subcategory != want.Subcategory {
		t.Errorf("Subcategory = %q, want %q", got.Subcategory, want.Subcategory)
	}
	if got.DominantColor != want.DominantColor {
		t.Errorf("DominantColor = %q, want %q", got.DominantColor, want.DominantColor)
	}
	if got.Pattern != want.Pattern {
		t.Errorf("Pattern = %q, want %q", got.Pattern, want.Pattern)
	}
	if got.WarmthTier != want.WarmthTier {
		t.Errorf("WarmthTier = %q, want %q", got.WarmthTier, want.WarmthTier)
	}
	if got.Formality != want.Formality {
		t.Errorf("Formality = %q, want %q", got.Formality, want.Formality)
	}
	if got.PhotoPath != want.PhotoPath {
		t.Errorf("PhotoPath = %q, want %q", got.PhotoPath, want.PhotoPath)
	}
	if got.Notes != want.Notes {
		t.Errorf("Notes = %q, want %q", got.Notes, want.Notes)
	}
	if !got.AddedDate.Equal(want.AddedDate) {
		t.Errorf("AddedDate = %v, want %v", got.AddedDate, want.AddedDate)
	}
	if len(got.SecondaryColors) != len(want.SecondaryColors) {
		t.Errorf("SecondaryColors = %v, want %v", got.SecondaryColors, want.SecondaryColors)
		return
	}
	for i := range want.SecondaryColors {
		if got.SecondaryColors[i] != want.SecondaryColors[i] {
			t.Errorf("SecondaryColors[%d] = %q, want %q", i, got.SecondaryColors[i], want.SecondaryColors[i])
		}
	}
}

// findItem returns the listed item with the given id, or fails.
func findItem(t *testing.T, items []store.Item, id string) store.Item {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("item %q not found in %d listed items", id, len(items))
	return store.Item{}
}

// rawSchemaColumns opens the SQLite file with an independent connection and
// returns each user table's column names, so the on-disk schema can be checked
// without touching the store's unexported db handle.
func rawSchemaColumns(t *testing.T, path string) map[string][]string {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open raw sqlite at %s: %v", path, err)
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}

	var tables []struct{ Name string }
	if err := db.Raw(
		"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'",
	).Scan(&tables).Error; err != nil {
		t.Fatalf("list tables: %v", err)
	}

	cols := map[string][]string{}
	for _, tbl := range tables {
		var info []struct{ Name string }
		if err := db.Raw("PRAGMA table_info(" + tbl.Name + ")").Scan(&info).Error; err != nil {
			t.Fatalf("PRAGMA table_info(%s): %v", tbl.Name, err)
		}
		for _, c := range info {
			cols[tbl.Name] = append(cols[tbl.Name], c.Name)
		}
	}
	return cols
}

// AC1: Given data/wardrobe.db does not exist / When the store is opened /
// Then data/ is created if absent, and the DB holds a table whose columns
// cover every field in 04-data-schema.md.
func TestING017_AC1_OpenCreatesDataDirAndColumns(t *testing.T) {
	path := newStorePath(t)
	dir := filepath.Dir(path)

	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("scratch parent %q unexpectedly exists before Open", dir)
	}

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open(%q) = %v, want nil", path, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Store.Close() = %v, want nil", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("parent dir %q not created by Open: %v", dir, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("db file %q not created by Open: %v", path, err)
	}

	tables := rawSchemaColumns(t, path)
	if len(tables) != 1 {
		t.Fatalf("want exactly 1 user table in %s, got %d: %v", path, len(tables), tables)
	}
	var cols []string
	for name, c := range tables {
		t.Logf("table %q columns: %v", name, c)
		cols = c
	}
	have := map[string]bool{}
	for _, c := range cols {
		have[c] = true
	}
	for _, want := range schemaColumns {
		if !have[want] {
			t.Errorf("table missing schema column %q; columns = %v", want, cols)
		}
	}
}

// AC2: Given an item with a caller-supplied uuid id and all 04-data-schema.md
// fields / When it is inserted and read back / Then every field round-trips
// unchanged, secondary_colors returns as a list of 0 or more palette values,
// and added_date is the cataloging date supplied.
func TestING017_AC2_RoundTripAllFields(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	t.Run("all_fields_populated", func(t *testing.T) {
		want := sampleItem("0f8c2b1e-0001-4a01-8001-000000000001")
		if err := s.Insert(want); err != nil {
			t.Fatalf("Insert() = %v, want nil", err)
		}
		items, err := s.List()
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		assertItemEqual(t, findItem(t, items, want.ID), want)
	})

	t.Run("empty_secondary_colors", func(t *testing.T) {
		want := sampleItem("0f8c2b1e-0002-4a01-8002-000000000002")
		want.SecondaryColors = nil
		want.Pattern = "solid"
		if err := s.Insert(want); err != nil {
			t.Fatalf("Insert() = %v, want nil", err)
		}
		items, err := s.List()
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		got := findItem(t, items, want.ID)
		if len(got.SecondaryColors) != 0 {
			t.Errorf("SecondaryColors = %v, want a list of 0 values", got.SecondaryColors)
		}
		assertItemEqual(t, got, want)
	})

	// added_date carries its calendar day through the round-trip (the schema
	// calls it a date; the API layer formats it).
	items, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	got := findItem(t, items, "0f8c2b1e-0001-4a01-8001-000000000001")
	if d := got.AddedDate.Format("2006-01-02"); d != "2026-09-18" {
		t.Errorf("AddedDate calendar day = %s, want 2026-09-18", d)
	}
}

// AC3: Given several items have been inserted / When all items are listed /
// Then every inserted item is returned and no schema field is dropped — notes
// and secondary_colors may be empty, but neither is replaced by an unexpected
// value.
func TestING017_AC3_ListReturnsEveryItemNoFieldDropped(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	populated := sampleItem("0f8c2b1e-0010-4a01-8010-000000000010")
	empty := sampleItem("0f8c2b1e-0011-4a01-8011-000000000011")
	empty.SecondaryColors = []string{}
	empty.Notes = ""
	empty.Pattern = "solid"
	third := sampleItem("0f8c2b1e-0012-4a01-8012-000000000012")
	third.SecondaryColors = []string{"navy"}
	third.Notes = ""

	for _, it := range []store.Item{populated, empty, third} {
		if err := s.Insert(it); err != nil {
			t.Fatalf("Insert(%s) = %v, want nil", it.ID, err)
		}
	}

	items, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(items) != 3 {
		t.Fatalf("List() returned %d items, want 3", len(items))
	}
	assertItemEqual(t, findItem(t, items, populated.ID), populated)

	gotEmpty := findItem(t, items, empty.ID)
	if len(gotEmpty.SecondaryColors) != 0 {
		t.Errorf("empty item SecondaryColors = %v, want 0 values", gotEmpty.SecondaryColors)
	}
	if gotEmpty.Notes != "" {
		t.Errorf("empty item Notes = %q, want \"\"", gotEmpty.Notes)
	}
	assertItemEqual(t, gotEmpty, empty)

	gotThird := findItem(t, items, third.ID)
	if len(gotThird.SecondaryColors) != 1 || gotThird.SecondaryColors[0] != "navy" {
		t.Errorf("third item SecondaryColors = %v, want [navy]", gotThird.SecondaryColors)
	}
	assertItemEqual(t, gotThird, third)
}

// AC4: Given the store is opened twice against the same file / When it is
// opened again / Then re-running the migration is a no-op and previously
// written rows are intact.
func TestING017_AC4_ReopenIsNoOpRowsIntact(t *testing.T) {
	path := newStorePath(t)

	first, err := store.Open(path)
	if err != nil {
		t.Fatalf("first Open() = %v, want nil", err)
	}
	original := sampleItem("0f8c2b1e-0020-4a01-8020-000000000020")
	if err := first.Insert(original); err != nil {
		t.Fatalf("Insert() = %v, want nil", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() = %v, want nil", err)
	}

	second, err := store.Open(path)
	if err != nil {
		t.Fatalf("second Open() = %v, want nil (migration must be a no-op replay)", err)
	}
	defer second.Close()

	items, err := second.List()
	if err != nil {
		t.Fatalf("List() after reopen = %v, want nil", err)
	}
	if len(items) != 1 {
		t.Fatalf("List() after reopen returned %d items, want the 1 pre-existing row", len(items))
	}
	assertItemEqual(t, findItem(t, items, original.ID), original)

	// The re-migrated table still accepts new rows.
	added := sampleItem("0f8c2b1e-0021-4a01-8021-000000000021")
	if err := second.Insert(added); err != nil {
		t.Fatalf("Insert() after reopen = %v, want nil", err)
	}
	items, err = second.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(items) != 2 {
		t.Fatalf("List() returned %d items after post-reopen insert, want 2", len(items))
	}
	assertItemEqual(t, findItem(t, items, original.ID), original)
	assertItemEqual(t, findItem(t, items, added.ID), added)
}

// Required exported surface: the fixed DB path constant callers use.
func TestING017_Surface_DefaultDBPath(t *testing.T) {
	if store.DefaultDBPath != "data/wardrobe.db" {
		t.Errorf("DefaultDBPath = %q, want %q", store.DefaultDBPath, "data/wardrobe.db")
	}
}

// Edge (spec-implied): 04-data-schema.md calls notes optional free text and
// the ticket requires support for a non-empty future value, so punctuation,
// newlines, and non-ASCII must survive the round-trip.
func TestING017_Edge_NotesFreeTextRoundTrips(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	want := sampleItem("0f8c2b1e-0030-4a01-8030-000000000030")
	want.Notes = "tag says \"dry clean only\"\nlost a button — resewn café 🔧"
	if err := s.Insert(want); err != nil {
		t.Fatalf("Insert() = %v, want nil", err)
	}
	items, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	assertItemEqual(t, findItem(t, items, want.ID), want)
}

// Edge (spec-implied): "data/ is created if absent" generalizes to any absent
// parent; the DB is opened from the project root, which may be nested deeper.
func TestING017_Edge_NestedParentDirCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "wardrobe.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open(%q) = %v, want nil", path, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("nested parent dir not created: %v", err)
	}
}
