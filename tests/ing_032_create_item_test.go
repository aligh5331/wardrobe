// Package tests holds black-box acceptance tests for backlog tickets.
// ING-032: POST /api/items persists a confirmed draft. Given an item id
// whose staged photo exists under data/ingest-staging/ and a body carrying
// the seven 04-data-schema.md tagging fields plus optional notes, the
// handler validates the record through internal/tagging (never a second
// enum copy), moves the staged photo into data/photos/<item_id>.<ext>, and
// inserts exactly one row via the shared internal/catalog create (ING-028)
// — the API is a second caller, not a second implementation. The response
// is the created item in the same shape GET /api/items/:id returns.
//
// A field-validation failure is 400 naming the offending field and keeps
// the staged upload (a validation rejection is not a failed create); a
// missing staged photo is 404; a persistence failure at either write leaves
// no orphan photo or dangling row and removes the staged upload. The route
// is asserted by TestING019_AC7_OnlyReadOnlyRoutes in ing_019_api_test.go.
//
// These tests exercise the exported wardrobe/internal/api surface against
// isolated temp roots; implementation code is never touched from here, and
// the repo's real data/ is never read or written.
package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

// ing032Harness wires the real engine to isolated temp staging/photos dirs.
// It passes a nil processor to WithTagging so the create route works without
// a VLM: this endpoint never tags.
type ing032Harness struct {
	engine     *gin.Engine
	st         *store.Store
	stagingDir string
	photosDir  string
	tax        tagging.Taxonomy
}

func newING032Harness(t *testing.T) *ing032Harness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := t.TempDir()
	stagingDir := filepath.Join(root, "ingest-staging")
	photosDir := filepath.Join(root, "photos")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("mkdir staging dir: %v", err)
	}

	return &ing032Harness{
		engine:     api.New(st, photosDir, api.WithTagging(nil, stagingDir)),
		st:         st,
		stagingDir: stagingDir,
		photosDir:  photosDir,
		tax:        tagging.TaxonomyTables(),
	}
}

// ing032ValidBody builds a taxonomy-valid POST /api/items body from the
// tables ParseTaggingResult validates against, so this file never hand-copies
// 03-taxonomy.md.
func ing032ValidBody(t *testing.T, h *ing032Harness, id, photoRef string) map[string]any {
	t.Helper()
	categories := slices.Sorted(maps.Keys(h.tax.Categories))
	if len(categories) == 0 || len(h.tax.Colors) < 2 || len(h.tax.Patterns) == 0 ||
		len(h.tax.WarmthTiers) == 0 || len(h.tax.Formality) == 0 {
		t.Fatalf("tagging tables too small to build a valid body: %+v", h.tax)
	}
	category := categories[0]
	subs := h.tax.Categories[category]
	if len(subs) == 0 {
		t.Fatalf("no subcategories for category %q in the tagging tables", category)
	}
	return map[string]any{
		"item_id":          id,
		"photo_ref":        photoRef,
		"category":         category,
		"subcategory":      subs[0],
		"dominant_color":   h.tax.Colors[0],
		"secondary_colors": []string{h.tax.Colors[1]},
		"pattern":          h.tax.Patterns[0],
		"warmth_tier":      h.tax.WarmthTiers[0],
		"formality":        h.tax.Formality[0],
		"notes":            "confirmed in the UI",
	}
}

// ing032Stage writes the server-named staged upload for id and returns its
// path.
func ing032Stage(t *testing.T, h *ing032Harness, id, ext string, data []byte) string {
	t.Helper()
	p := filepath.Join(h.stagingDir, id+ext)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("stage upload %s: %v", p, err)
	}
	return p
}

// ing032Post marshals body and POSTs it to /api/items.
func ing032Post(t *testing.T, engine http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal POST body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)
	return rr
}

// ing032DecodeItem decodes an ing019Item from a recorder, failing on a
// malformed body.
func ing032DecodeItem(t *testing.T, body []byte) ing019Item {
	t.Helper()
	var item ing019Item
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatalf("decode item: %v\n%s", err, body)
	}
	return item
}

// AC1 + AC2: Given a valid staged draft and a taxonomy-valid body / When the
// client POSTs /api/items / Then it receives a 2xx, the staged photo ends up
// at data/photos/<item_id>.<ext> with the same bytes, exactly one catalog row
// is inserted with that id and photo_path, the response is the created item
// in the same shape GET /api/items/:id returns, and the staged file is gone
// with no duplicate row or photo.
func TestING032_AC1_PersistMovesPhotoInsertsRowReturnsItemShape(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0320-4a01-8320-000000000001"
	stagedBytes := []byte("ING032-STAGED-JPEG-BYTES")
	staged := ing032Stage(t, h, id, ".jpg", stagedBytes)
	body := ing032ValidBody(t, h, id, id+".jpg")

	rr := ing032Post(t, h.engine, body)
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("POST /api/items status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// The response carries exactly the same key set as GET /api/items/:id.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rr.Body)
	}
	if len(raw) != len(ing019ItemKeys) {
		t.Errorf("response keys = %v, want exactly %v", ing019Keys(raw), ing019ItemKeys)
	}
	for _, k := range ing019ItemKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("response missing contract key %q; keys = %v", k, ing019Keys(raw))
		}
	}

	got := ing032DecodeItem(t, rr.Body.Bytes())
	if got.ID != id {
		t.Errorf("response id = %q, want %q", got.ID, id)
	}
	if got.Notes != body["notes"] {
		t.Errorf("response notes = %q, want %q", got.Notes, body["notes"])
	}
	if got.AddedDate == "" {
		t.Errorf("response added_date is empty, want YYYY-MM-DD")
	}

	// Same shape and values as a subsequent GET /api/items/:id.
	rrGet := ing019Do(t, h.engine, http.MethodGet, "/api/items/"+id)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET /api/items/%s status = %d, want 200; body:\n%s", id, rrGet.Code, rrGet.Body)
	}
	if want := ing032DecodeItem(t, rrGet.Body.Bytes()); !reflect.DeepEqual(got, want) {
		t.Errorf("POST response = %+v, want the GET /api/items/:id shape %+v", got, want)
	}

	// Exactly one row, with the id and photo_path at the moved copy.
	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows = %d, want exactly 1", len(rows))
	}
	wantPhoto := filepath.Join(h.photosDir, id+".jpg")
	if rows[0].ID != id || rows[0].PhotoPath != wantPhoto {
		t.Errorf("row = {id:%q photo_path:%q}, want {id:%q photo_path:%q}", rows[0].ID, rows[0].PhotoPath, id, wantPhoto)
	}
	if rows[0].Category != body["category"] || rows[0].Notes != body["notes"] {
		t.Errorf("row fields = %+v, want the body's values", rows[0])
	}

	// The photo moved with the staged bytes; the staged file is gone.
	onDisk, err := os.ReadFile(wantPhoto)
	if err != nil {
		t.Fatalf("read stored photo %s: %v", wantPhoto, err)
	}
	if !bytes.Equal(onDisk, stagedBytes) {
		t.Errorf("stored bytes = %q, want the staged %q", onDisk, stagedBytes)
	}
	if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staged file %s still exists after success (stat err=%v); want it removed", staged, err)
	}

	// Exactly one photo, and nothing left staged.
	if files := ing031Files(t, h.photosDir); len(files) != 1 || files[0] != id+".jpg" {
		t.Errorf("photos dir = %v, want exactly [%s]", files, id+".jpg")
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want empty after save", files)
	}
}

// AC2: Given a create that already succeeded / When the same draft is POSTed
// again / Then no second row or photo appears — the staged upload is gone, so
// the retry is a 404.
func TestING032_AC2_RepostAfterSuccessLeavesSingleRowAndPhoto(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0320-4a01-8320-000000000002"
	ing032Stage(t, h, id, ".png", []byte("ING032-PNG"))
	body := ing032ValidBody(t, h, id, id+".png")

	if rr := ing032Post(t, h.engine, body); rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("first POST status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}
	if rr := ing032Post(t, h.engine, body); rr.Code != http.StatusNotFound {
		t.Errorf("second POST status = %d, want 404 (staged upload already consumed); body:\n%s", rr.Code, rr.Body)
	}

	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Errorf("catalog rows = %d, want exactly 1 (no duplicate)", len(rows))
	}
	if files := ing031Files(t, h.photosDir); len(files) != 1 {
		t.Errorf("photos dir = %v, want exactly one photo (no duplicate)", files)
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want empty", files)
	}
}

// AC3 + AC4: Given a body rejected by field validation / When the client
// POSTs /api/items / Then it receives 400 naming the offending field, no row
// is inserted, no file is placed in data/photos/, and the staged upload is
// kept intact so the draft remains correctable.
func TestING032_AC3_FieldValidation400KeepsStagedNoWrites(t *testing.T) {
	tax := tagging.TaxonomyTables()

	cases := []struct {
		name  string
		field string // must appear in the 400 response
		mut   func(t *testing.T, b map[string]any)
	}{
		{
			name:  "invalid_enum_warmth_tier",
			field: "warmth_tier",
			mut:   func(t *testing.T, b map[string]any) { b["warmth_tier"] = "sweltering" },
		},
		{
			name:  "invalid_enum_dominant_color",
			field: "dominant_color",
			mut:   func(t *testing.T, b map[string]any) { b["dominant_color"] = "chartreuse" },
		},
		{
			name:  "invalid_secondary_color",
			field: "secondary_colors",
			mut: func(t *testing.T, b map[string]any) {
				b["secondary_colors"] = []string{tax.Colors[0], "chartreuse"}
			},
		},
		{
			name:  "invalid_category_subcategory_pair",
			field: "subcategory",
			mut: func(t *testing.T, b map[string]any) {
				// "top" is a category, "jeans" a bottom subcategory: each
				// valid alone, invalid as a pair (03-taxonomy.md).
				b["category"] = "top"
				b["subcategory"] = "jeans"
			},
		},
		{
			name:  "missing_required_field_pattern",
			field: "pattern",
			mut:   func(t *testing.T, b map[string]any) { delete(b, "pattern") },
		},
		{
			name:  "missing_required_field_subcategory",
			field: "subcategory",
			mut:   func(t *testing.T, b map[string]any) { delete(b, "subcategory") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newING032Harness(t)
			id := "0f8c2b1e-0320-4a01-8320-000000000003"
			stagedBytes := []byte("ING032-KEEP-STAGED")
			staged := ing032Stage(t, h, id, ".jpg", stagedBytes)

			body := ing032ValidBody(t, h, id, id+".jpg")
			tc.mut(t, body)

			rr := ing032Post(t, h.engine, body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("POST status = %d, want 400; body:\n%s", rr.Code, rr.Body)
			}
			var resp map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("400 body is not a JSON object: %v\n%s", err, rr.Body)
			}
			if !strings.Contains(resp["error"], tc.field) {
				t.Errorf("400 error %q does not name the offending field %q", resp["error"], tc.field)
			}

			// No row, no photo — but the staged upload is kept.
			rows, err := h.st.List()
			if err != nil {
				t.Fatalf("List() = %v, want nil", err)
			}
			if len(rows) != 0 {
				t.Errorf("catalog rows = %d, want 0 on a validation 400", len(rows))
			}
			if files := ing031Files(t, h.photosDir); len(files) != 0 {
				t.Errorf("photos dir = %v, want no file on a validation 400", files)
			}
			onDisk, err := os.ReadFile(staged)
			if err != nil {
				t.Fatalf("staged upload was removed on a validation 400: %v", err)
			}
			if !bytes.Equal(onDisk, stagedBytes) {
				t.Errorf("staged bytes changed: got %q, want %q", onDisk, stagedBytes)
			}
		})
	}
}

// AC: Given an item id whose staged photo does not exist / When the client
// POSTs /api/items / Then it receives 404 and no row is inserted.
func TestING032_AC4_MissingStagedPhotoIs404NoRow(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0320-4a01-8320-000000000004"
	body := ing032ValidBody(t, h, id, id+".jpg") // staged file never written

	rr := ing032Post(t, h.engine, body)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("POST status = %d, want 404; body:\n%s", rr.Code, rr.Body)
	}

	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Errorf("catalog rows = %d, want 0 after a 404", len(rows))
	}
	if files := ing031Files(t, h.photosDir); len(files) != 0 {
		t.Errorf("photos dir = %v, want no file after a 404", files)
	}
}

// AC5: Given a create failure at either write / When the create returns an
// error / Then no orphan photo remains in data/photos/, no dangling row is
// added, and the staged upload is removed.
func TestING032_AC5_CreateFailureNoOrphanNoRowRemovesStaged(t *testing.T) {
	t.Run("row_insert_failure", func(t *testing.T) {
		h := newING032Harness(t)
		id := "0f8c2b1e-0320-4a01-8320-000000000005"
		// A pre-existing row with the same primary key makes Insert fail
		// after the photo copy succeeded.
		if err := h.st.Insert(sampleItem(id)); err != nil {
			t.Fatalf("insert fixture row: %v", err)
		}
		staged := ing032Stage(t, h, id, ".jpg", []byte("ING032-INSERT-FAIL"))

		rr := ing032Post(t, h.engine, ing032ValidBody(t, h, id, id+".jpg"))
		if rr.Code < 400 {
			t.Fatalf("POST status = %d, want a non-2xx create failure; body:\n%s", rr.Code, rr.Body)
		}

		// The copied photo was rolled back: no orphan file.
		if files := ing031Files(t, h.photosDir); len(files) != 0 {
			t.Errorf("photos dir = %v, want 0 entries (no orphan)", files)
		}
		// Only the pre-existing fixture row remains.
		rows, err := h.st.List()
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		if len(rows) != 1 || rows[0].ID != id {
			t.Errorf("catalog rows = %d (%+v), want exactly the 1 pre-existing fixture row", len(rows), rows)
		}
		assertItemEqual(t, rows[0], sampleItem(id))
		// A failed create removes the staged upload.
		if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("staged file %s still exists after a failed create (stat err=%v)", staged, err)
		}
	})

	t.Run("photo_move_failure", func(t *testing.T) {
		h := newING032Harness(t)
		id := "0f8c2b1e-0320-4a01-8320-000000000006"
		// Occupy the photos path with a regular file so the photo write
		// cannot complete.
		if err := os.WriteFile(h.photosDir, []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("occupy photos path: %v", err)
		}
		staged := ing032Stage(t, h, id, ".jpg", []byte("ING032-MOVE-FAIL"))

		rr := ing032Post(t, h.engine, ing032ValidBody(t, h, id, id+".jpg"))
		if rr.Code < 400 {
			t.Fatalf("POST status = %d, want a non-2xx create failure; body:\n%s", rr.Code, rr.Body)
		}
		rows, err := h.st.List()
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		if len(rows) != 0 {
			t.Errorf("catalog rows = %d, want 0 after a failed photo write", len(rows))
		}
		if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("staged file %s still exists after a failed create (stat err=%v)", staged, err)
		}
	})
}

// AC6 (shared implementation): the create path writes the row and photo
// through internal/catalog.Create — the API is a second caller, not a second
// implementation (06-decisions.md "Second catalog writer exists").
func TestING032_AC6_UsesSharedCatalogCreate(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "api", "api.go"))
	if err != nil {
		t.Fatalf("read internal/api/api.go: %v", err)
	}
	if !strings.Contains(string(src), "catalog.Create(") {
		t.Errorf("internal/api/api.go does not persist through catalog.Create — " +
			"the API must share internal/catalog, not keep a second create copy")
	}
}

// Edge (trust boundary / ING-031 review note): a client-supplied item_id or
// photo_ref carrying separators or traversal is rejected before it can be
// joined to the staging dir, with nothing written and a canary above staging
// untouched.
func TestING032_Edge_TraversalReferencesRejected(t *testing.T) {
	h := newING032Harness(t)
	canary := filepath.Join(filepath.Dir(h.stagingDir), "secret.jpg")
	canaryBytes := []byte("ING032-CANARY")
	if err := os.WriteFile(canary, canaryBytes, 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	id := "0f8c2b1e-0320-4a01-8320-000000000007"
	cases := []struct {
		name string
		id   string
		ref  string
	}{
		{"item_id_slash", "../evil", ""},
		{"item_id_dotdot", "..", ""},
		{"photo_ref_traversal", id, "../../../secret.jpg"},
		{"photo_ref_subdir", id, "nested/dir/" + id + ".jpg"},
		{"photo_ref_backslash", id, `..\..\evil.jpg`},
		{"photo_ref_mismatch", id, "other-id.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := ing032ValidBody(t, h, id, tc.ref)
			body["item_id"] = tc.id
			rr := ing032Post(t, h.engine, body)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("POST status = %d, want 400 for an unsafe reference; body:\n%s", rr.Code, rr.Body)
			}
		})
	}

	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Errorf("catalog rows = %d, want 0", len(rows))
	}
	if files := ing031Files(t, h.photosDir); len(files) != 0 {
		t.Errorf("photos dir = %v, want no file", files)
	}
	if got, err := os.ReadFile(canary); err != nil || !bytes.Equal(got, canaryBytes) {
		t.Errorf("canary outside staging dir changed: got %q err=%v, want %q", got, err, canaryBytes)
	}
}

// Edge (contract): photo_ref is optional; when the client omits it the
// server still locates its own server-named <item_id>.<ext> under staging and
// persists the draft.
func TestING032_Edge_PhotoRefOmittedLocatesByID(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0320-4a01-8320-000000000008"
	ing032Stage(t, h, id, ".webp", []byte("ING032-WEBP"))

	body := ing032ValidBody(t, h, id, "")
	delete(body, "photo_ref")

	rr := ing032Post(t, h.engine, body)
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("POST without photo_ref status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}
	if files := ing031Files(t, h.photosDir); len(files) != 1 || files[0] != id+".webp" {
		t.Errorf("photos dir = %v, want [%s]", files, id+".webp")
	}
}
