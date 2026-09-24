// Package tests holds black-box acceptance tests for backlog tickets.
package tests

import (
	"errors"
	"testing"
	"time"

	"wardrobe/internal/store"
)

// AC1: Given an item id that exists / When Get(id) is called / Then the full
// item is returned with all 04-data-schema.md fields intact, including empty
// notes and a 0-length secondary_colors list.
func TestING029_AC1_GetReturnsFullItem(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	t.Run("all_fields_populated", func(t *testing.T) {
		want := sampleItem("0f8c2b1e-0100-4a01-8100-000000000001")
		if err := s.Insert(want); err != nil {
			t.Fatalf("Insert() = %v, want nil", err)
		}
		got, err := s.Get(want.ID)
		if err != nil {
			t.Fatalf("Get(%q) = %v, want nil", want.ID, err)
		}
		assertItemEqual(t, got, want)
	})

	t.Run("empty_notes_and_zero_secondary_colors", func(t *testing.T) {
		want := sampleItem("0f8c2b1e-0101-4a01-8101-000000000002")
		want.Notes = ""
		want.SecondaryColors = []string{}
		want.Pattern = "solid"
		if err := s.Insert(want); err != nil {
			t.Fatalf("Insert() = %v, want nil", err)
		}
		got, err := s.Get(want.ID)
		if err != nil {
			t.Fatalf("Get(%q) = %v, want nil", want.ID, err)
		}
		if got.Notes != "" {
			t.Errorf("Notes = %q, want \"\"", got.Notes)
		}
		if len(got.SecondaryColors) != 0 {
			t.Errorf("SecondaryColors = %v, want a list of 0 values", got.SecondaryColors)
		}
		assertItemEqual(t, got, want)
	})
}

// AC2: Given an item id that does not exist / When Get(id) is called / Then
// it returns an error the caller can identify as "not found" without
// importing gorm. This test file deliberately does not import gorm — it
// identifies the error solely via store.ErrNotFound.
func TestING029_AC2_GetNotFoundIsIdentifiable(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	got, err := s.Get("no-such-id")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want errors.Is(..., store.ErrNotFound)", err)
	}
	if got.ID != "" {
		t.Errorf("Get(missing) item.ID = %q, want empty", got.ID)
	}
}

// AC3: Given an existing item and new values for the mutable fields (the
// seven tagging fields plus notes) / When Update is called / Then exactly
// those fields are persisted and read back changed, while id, added_date,
// and photo_path are unchanged. The passed item carries different
// added_date/photo_path values than the stored row to prove Update ignores
// them.
func TestING029_AC3_UpdatePersistsMutableFieldsOnly(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	original := sampleItem("0f8c2b1e-0110-4a01-8110-000000000003")
	if err := s.Insert(original); err != nil {
		t.Fatalf("Insert() = %v, want nil", err)
	}

	updated := original
	updated.Category = "bottom"
	updated.Subcategory = "jeans"
	updated.DominantColor = "navy"
	updated.SecondaryColors = []string{"white"}
	updated.Pattern = "solid"
	updated.WarmthTier = "heavy"
	updated.Formality = "smart-casual"
	updated.Notes = "hemmed once"
	// Immutable fields set to different values: Update must not persist them.
	updated.AddedDate = time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	updated.PhotoPath = "data/photos/attacker-controlled.jpg"

	if err := s.Update(updated); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.Get(original.ID)
	if err != nil {
		t.Fatalf("Get(%q) = %v, want nil", original.ID, err)
	}
	if got.Category != "bottom" || got.Subcategory != "jeans" ||
		got.DominantColor != "navy" || got.Pattern != "solid" ||
		got.WarmthTier != "heavy" || got.Formality != "smart-casual" ||
		got.Notes != "hemmed once" {
		t.Errorf("mutable fields not persisted: %+v", got)
	}
	if len(got.SecondaryColors) != 1 || got.SecondaryColors[0] != "white" {
		t.Errorf("SecondaryColors = %v, want [white]", got.SecondaryColors)
	}
	if got.ID != original.ID {
		t.Errorf("ID = %q, want %q", got.ID, original.ID)
	}
	if !got.AddedDate.Equal(original.AddedDate) {
		t.Errorf("AddedDate = %v, want immutable %v", got.AddedDate, original.AddedDate)
	}
	if got.PhotoPath != original.PhotoPath {
		t.Errorf("PhotoPath = %q, want immutable %q", got.PhotoPath, original.PhotoPath)
	}
}

// AC4: Given an item id that does not exist / When Update is called / Then
// it returns the same identifiable "not found" error and writes nothing.
func TestING029_AC4_UpdateNotFoundWritesNothing(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	before, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}

	missing := sampleItem("0f8c2b1e-0120-4a01-8120-000000000004")
	err = s.Update(missing)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Update(missing) error = %v, want errors.Is(..., store.ErrNotFound)", err)
	}
	// The not-found error must be the same sentinel Get returns.
	if _, gerr := s.Get(missing.ID); !errors.Is(err, store.ErrNotFound) || !errors.Is(gerr, store.ErrNotFound) {
		t.Errorf("Update/Get not-found errors differ: Update=%v Get=%v", err, gerr)
	}

	after, err := s.List()
	if err != nil {
		t.Fatalf("List() after failed Update = %v, want nil", err)
	}
	if len(after) != len(before) {
		t.Errorf("List() grew from %d to %d rows — Update wrote nothing", len(before), len(after))
	}
}

// AC5: Given empty notes and an empty secondary_colors list / When an update
// writes them / Then they round-trip as "" and [] (not null), matching
// ING-017's read shape.
func TestING029_AC5_UpdateEmptyRoundTrip(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	original := sampleItem("0f8c2b1e-0130-4a01-8130-000000000005")
	original.Notes = "old note"
	original.SecondaryColors = []string{"white", "red"}
	if err := s.Insert(original); err != nil {
		t.Fatalf("Insert() = %v, want nil", err)
	}

	update := original
	update.Notes = ""
	update.SecondaryColors = []string{}
	update.Pattern = "solid" // keep a valid distinct mutable value
	if err := s.Update(update); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.Get(original.ID)
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got.Notes != "" {
		t.Errorf("Notes = %q, want \"\" (not null / not sentinel)", got.Notes)
	}
	if got.SecondaryColors == nil {
		// nil is acceptable only if it reads as a 0-length list per ING-017;
		// but reject the literal string "null" sentinel if it ever leaks.
		t.Errorf("SecondaryColors = nil, want a 0-length list value")
	}
	if len(got.SecondaryColors) != 0 {
		t.Errorf("SecondaryColors = %v, want [] (length 0)", got.SecondaryColors)
	}

	// List must agree with Get on the same row (ING-017's read shape).
	items, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	listed := findItem(t, items, original.ID)
	if listed.Notes != "" || len(listed.SecondaryColors) != 0 {
		t.Errorf("List row = notes %q, colors %v; want \"\" and []", listed.Notes, listed.SecondaryColors)
	}
}

// Edge (spec-implied, AC5): "empty secondary_colors list" may arrive as a nil
// slice — Go's zero value, e.g. Get→mutate→Update on a row that was inserted
// with nil (ING-017's insert path stores SQL NULL for nil). The read shape
// must still be a 0-length list matching ING-017, never a leaked sentinel,
// and notes must be "".
func TestING029_Edge_NilSecondaryColorsOnUpdate(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	original := sampleItem("0f8c2b1e-0150-4a01-8150-000000000008")
	original.SecondaryColors = nil // ING-017 insert path: SQL NULL
	original.Pattern = "solid"
	if err := s.Insert(original); err != nil {
		t.Fatalf("Insert() = %v, want nil", err)
	}

	update := original
	update.Notes = ""
	update.SecondaryColors = nil // empty list as nil
	update.WarmthTier = "medium"
	if err := s.Update(update); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.Get(original.ID)
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got.Notes != "" {
		t.Errorf("Notes = %q, want \"\"", got.Notes)
	}
	if len(got.SecondaryColors) != 0 {
		t.Errorf("SecondaryColors = %v, want a 0-length list (ING-017 read shape)", got.SecondaryColors)
	}
	if got.WarmthTier != "medium" {
		t.Errorf("WarmthTier = %q, want \"medium\"", got.WarmthTier)
	}
	items, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	listed := findItem(t, items, original.ID)
	if listed.Notes != "" || len(listed.SecondaryColors) != 0 {
		t.Errorf("List row = notes %q, colors %v; want \"\" and len 0", listed.Notes, listed.SecondaryColors)
	}
}

// Edge (AC4): Update keyed by the empty id — no row can have id "", so it
// must hit the same not-found sentinel and write nothing (no insert of a new
// row, no mutation of existing ones).
func TestING029_Edge_UpdateEmptyIDNotFoundWritesNothing(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	keep := sampleItem("0f8c2b1e-0160-4a01-8160-000000000009")
	if err := s.Insert(keep); err != nil {
		t.Fatalf("Insert() = %v, want nil", err)
	}

	err = s.Update(store.Item{Category: "bottom", Subcategory: "jeans"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Update(empty id) error = %v, want errors.Is(..., store.ErrNotFound)", err)
	}
	items, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(items) != 1 {
		t.Fatalf("List() = %d rows, want 1 (empty-id Update wrote nothing)", len(items))
	}
	assertItemEqual(t, findItem(t, items, keep.ID), keep)
}

// Edge (spec-implied, 04-data-schema.md write-path rules + AC3): immutable
// fields passed as zero values must also be ignored — not only "different"
// values. An API Get→mutate→Update that forgets to copy PhotoPath/AddedDate
// would pass zero values; the stored row must keep the originals.
func TestING029_Edge_ImmutableFieldsZeroValueIgnored(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	original := sampleItem("0f8c2b1e-0170-4a01-8170-000000000010")
	if err := s.Insert(original); err != nil {
		t.Fatalf("Insert() = %v, want nil", err)
	}

	update := original
	update.Notes = "edited"
	update.PhotoPath = ""
	update.AddedDate = time.Time{}
	if err := s.Update(update); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.Get(original.ID)
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got.PhotoPath != original.PhotoPath {
		t.Errorf("PhotoPath = %q, want immutable %q (zero-value pass-through must be ignored)", got.PhotoPath, original.PhotoPath)
	}
	if !got.AddedDate.Equal(original.AddedDate) {
		t.Errorf("AddedDate = %v, want immutable %v (zero-value pass-through must be ignored)", got.AddedDate, original.AddedDate)
	}
	if got.Notes != "edited" {
		t.Errorf("Notes = %q, want \"edited\"", got.Notes)
	}
	if got.ID != original.ID {
		t.Errorf("ID = %q, want %q", got.ID, original.ID)
	}
}

// AC6 is covered by the unchanged Insert/List surface: the pre-existing
// TestING017_* suite must still pass against this same store. This smoke
// check re-exercises Insert+List alongside Get/Update so a regression in the
// shared connection path fails here too.
func TestING029_AC6_InsertListUnchangedAlongsideGetUpdate(t *testing.T) {
	s, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	defer s.Close()

	a := sampleItem("0f8c2b1e-0140-4a01-8140-000000000006")
	b := sampleItem("0f8c2b1e-0141-4a01-8141-000000000007")
	for _, it := range []store.Item{a, b} {
		if err := s.Insert(it); err != nil {
			t.Fatalf("Insert(%s) = %v, want nil", it.ID, err)
		}
	}

	b.Notes = "updated via Get/Update path"
	if err := s.Update(b); err != nil {
		t.Fatalf("Update(%s) = %v, want nil", b.ID, err)
	}

	items, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(items) != 2 {
		t.Fatalf("List() = %d items, want 2 (Insert/List unchanged)", len(items))
	}
	assertItemEqual(t, findItem(t, items, a.ID), a)
	gotB, err := s.Get(b.ID)
	if err != nil {
		t.Fatalf("Get(%s) = %v, want nil", b.ID, err)
	}
	if gotB.Notes != b.Notes {
		t.Errorf("Get after Update Notes = %q, want %q", gotB.Notes, b.Notes)
	}
}
