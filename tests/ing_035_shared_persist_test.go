//go:build integration

// Package tests holds black-box acceptance tests for backlog tickets.
// ING-035 (process-level half): the shared-persistence equivalence contract
// (06-decisions.md "Second catalog writer exists", ING-028). cmd/ingest and
// the API each persist the same item id and tagging fields into separate
// isolated roots; this test runs the real cmd/ingest binary and the real Gin
// engine and checks both produce an equivalent row and stored photo —
// confirming one internal/catalog implementation rather than two copies.
//
// It is build-tagged `integration` because it builds and runs the real
// binary, matching ing_012/ing_018/ing_021. The in-process round-trip,
// create-failure, and write-path-validation acceptance tests live in
// ing_035_create_edit_test.go.
package tests

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"wardrobe/internal/store"
)

// ing035SameTagging checks the seven 04-data-schema.md tagging fields plus
// notes of two persisted rows are identical, ignoring the fields that are
// deliberately per-writer/per-root (id is the same by construction,
// added_date is asserted separately, photo_path is a different root).
func ing035SameTagging(t *testing.T, cliRow, apiRow store.Item) {
	t.Helper()
	if cliRow.Category != apiRow.Category ||
		cliRow.Subcategory != apiRow.Subcategory ||
		cliRow.DominantColor != apiRow.DominantColor ||
		cliRow.Pattern != apiRow.Pattern ||
		cliRow.WarmthTier != apiRow.WarmthTier ||
		cliRow.Formality != apiRow.Formality {
		t.Errorf("tagging fields differ:\n cli=%+v\n api=%+v", cliRow, apiRow)
	}
	if !slices.Equal(cliRow.SecondaryColors, apiRow.SecondaryColors) {
		t.Errorf("secondary_colors differ: cli=%v api=%v", cliRow.SecondaryColors, apiRow.SecondaryColors)
	}
	if cliRow.Notes != apiRow.Notes {
		t.Errorf("notes differ: cli=%q api=%q", cliRow.Notes, apiRow.Notes)
	}
}

// AC3: Given the shared persistence contract (ING-028) / When cmd/ingest and
// the API each persist the same item id and tagging fields into separate
// isolated roots / Then both produce an equivalent result — same seven field
// values, added_date-at-create semantics, empty notes, and the same
// data/photos/<item_id>.<ext> photo_path shape and stored bytes — confirming
// one internal/catalog implementation.
func TestING035_AC3_CLIAndAPISharedPersistence(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)
	src := []byte("ING035-SHARED-PERSIST-BYTES")
	photo := writePhoto(t, "garment.jpg", src)

	start := time.Now()

	// cmd/ingest persists into its own isolated root, generating the item id.
	cli := newING018Harness(t)
	srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })
	stdout, stderr, code := cli.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code != 0 {
		t.Fatalf("ingest exit code = %d, want 0; stderr:\n%s", code, stderr)
	}
	out := ing018ParseStdout(t, stdout)
	if len(out) != 1 {
		t.Fatalf("ingest records = %d, want 1; stdout:\n%s", len(out), stdout)
	}
	itemID, _ := out[0]["item_id"].(string)
	if itemID == "" {
		t.Fatal("ingest emitted no item_id")
	}
	cliRows := cli.list(t)
	if len(cliRows) != 1 {
		t.Fatalf("cli catalog rows = %d, want exactly 1", len(cliRows))
	}
	cliRow := cliRows[0]
	if cliRow.ID != itemID {
		t.Errorf("cli row id = %q, want the emitted item_id %q", cliRow.ID, itemID)
	}
	if cliRow.Category != want.Category || cliRow.Subcategory != want.Subcategory {
		t.Errorf("cli row differs from the VLM result: got %+v, want %+v", cliRow, want)
	}

	// The API persists the same id + fields in a separate isolated root:
	// stage the upload under the CLI's id and POST it through the real engine.
	h := newING035Harness(t, func(int, ing005Request) string { return valid })
	staged := filepath.Join(h.stagingDir, itemID+".jpg")
	if err := os.WriteFile(staged, src, 0o644); err != nil {
		t.Fatalf("stage upload for the API: %v", err)
	}
	body := map[string]any{
		"item_id":          itemID,
		"photo_ref":        itemID + ".jpg",
		"category":         cliRow.Category,
		"subcategory":      cliRow.Subcategory,
		"dominant_color":   cliRow.DominantColor,
		"secondary_colors": cliRow.SecondaryColors,
		"pattern":          cliRow.Pattern,
		"warmth_tier":      cliRow.WarmthTier,
		"formality":        cliRow.Formality,
		"notes":            "",
	}
	rr := ing035DoJSON(t, h.engine, http.MethodPost, "/api/items", body)
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("API POST /api/items status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}
	apiRows, err := h.st.List()
	if err != nil {
		t.Fatalf("api List() = %v, want nil", err)
	}
	if len(apiRows) != 1 {
		t.Fatalf("api catalog rows = %d, want exactly 1", len(apiRows))
	}
	apiRow := apiRows[0]
	end := time.Now()

	if apiRow.ID != itemID {
		t.Errorf("api row id = %q, want the same id %q", apiRow.ID, itemID)
	}

	// Same seven field values and the same (empty) notes.
	ing035SameTagging(t, cliRow, apiRow)

	// Same added_date-at-create semantics: both set within the create window.
	for name, row := range map[string]store.Item{"cli": cliRow, "api": apiRow} {
		if row.AddedDate.Before(start) || row.AddedDate.After(end) {
			t.Errorf("%s row added_date = %s, want within [%s, %s]", name, row.AddedDate, start, end)
		}
	}

	// Same photo_path shape and stored bytes: data/photos/<item_id>.<ext>
	// under each writer's own root.
	for name, p := range map[string]string{"cli": cli.copyPath(cliRow.PhotoPath), "api": apiRow.PhotoPath} {
		if !strings.HasSuffix(filepath.ToSlash(p), "data/photos/"+itemID+".jpg") {
			t.Errorf("%s photo_path = %q, want the data/photos/%s.jpg shape", name, p, itemID)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s stored photo %s: %v", name, p, err)
		}
		if !bytes.Equal(got, src) {
			t.Errorf("%s stored bytes = %q, want the source %q", name, got, src)
		}
	}

	// Each writer persisted exactly one row and exactly one photo.
	if files := ing031Files(t, cli.photosDir()); len(files) != 1 || files[0] != itemID+".jpg" {
		t.Errorf("cli photos dir = %v, want exactly [%s]", files, itemID+".jpg")
	}
	if files := ing031Files(t, h.photosDir); len(files) != 1 || files[0] != itemID+".jpg" {
		t.Errorf("api photos dir = %v, want exactly [%s]", files, itemID+".jpg")
	}

	// One shared implementation: both writers persist through internal/catalog.
	for _, rel := range []string{"cmd/ingest/main.go", "internal/api/api.go"} {
		raw, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(raw), "catalog.Create(") {
			t.Errorf("%s does not persist through catalog.Create — shared persistence is required", rel)
		}
	}
}
