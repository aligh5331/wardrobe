// Package tests holds black-box acceptance tests for backlog tickets.
// ING-035 (in-process half): one black-box acceptance flow drives the real
// Gin engine from internal/api against isolated temp roots and a local stub
// VLM, exercising the four approved write/read routes together
// (07-architecture.md "Catalog write API"):
//
//   - POST /api/items/photo returns a non-persisted draft (upload + VLM)
//   - POST /api/items persists the corrected draft through the shared
//     rollback-safe internal/catalog create (ING-028/032)
//   - GET /api/items/:id reads it back
//   - PUT /api/items/:id edits the mutable fields
//
// It also covers the create-failure cleanup contract (06-decisions.md
// "Catalog writes must not leave orphan photos or dangling rows") at
// filesystem level, and the write-path validation contract (400 naming the
// field, row unchanged) at integration level (04-data-schema.md
// "Write-path rules", 05-vlm-tagging-spec.md "Interactive tagging").
//
// The cross-writer CLI equivalence (ING-035's shared-persistence AC) lives
// in ing_035_shared_persist_test.go behind the `integration` build tag,
// because it builds and runs cmd/ingest, mirroring the ING-019 split
// (in-process engine tests untagged, process-level tests tagged).
//
// These tests are stdlib-only (testing / net/http / net/http/httptest) plus
// the already-present Gin and internal packages: no test framework, runtime
// dependency, or model is added. Every root is a t.TempDir(), so the repo's
// real data/ and logs/ (personal runtime data) are never read or written.
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
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

// ing035Harness wires the real engine to isolated temp store/staging/photos
// paths and a local stub VLM, so no real data/ or logs/ is touched.
type ing035Harness struct {
	engine     *gin.Engine
	st         *store.Store
	root       string
	photosDir  string
	stagingDir string
	capture    *ing005Capture
}

// newING035Harness builds the same engine cmd/server builds, against a
// throwaway store and throwaway data/{ingest-staging,photos} directories,
// with the retry-once tagging processor wired to an in-process stub VLM.
func newING035Harness(t *testing.T, respond func(int, ing005Request) string) *ing035Harness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := t.TempDir()
	stagingDir := filepath.Join(root, "data", "ingest-staging")
	photosDir := filepath.Join(root, "data", "photos")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("mkdir staging dir: %v", err)
	}

	srv, capture := newING005VLM(t, respond)
	p := ing031Processor(t, srv)
	return &ing035Harness{
		engine:     api.New(st, photosDir, api.WithTagging(p, stagingDir)),
		st:         st,
		root:       root,
		photosDir:  photosDir,
		stagingDir: stagingDir,
		capture:    capture,
	}
}

// ing035ValidBody builds a taxonomy-valid create/update body from the same
// tables ParseTaggingResult validates against, so this file never hand-copies
// 03-taxonomy.md. shift selects which valid values to use; distinct shifts
// give distinct-but-valid records (used to prove an update changed the row).
func ing035ValidBody(t *testing.T, id, photoRef, notes string, shift int) map[string]any {
	t.Helper()
	tax := tagging.TaxonomyTables()
	categories := slices.Sorted(maps.Keys(tax.Categories))
	if len(categories) == 0 || len(tax.Colors) < 2 || len(tax.Patterns) == 0 ||
		len(tax.WarmthTiers) == 0 || len(tax.Formality) == 0 {
		t.Fatalf("tagging tables too small to build a valid body: %+v", tax)
	}
	category := categories[shift%len(categories)]
	subs := tax.Categories[category]
	if len(subs) == 0 {
		t.Fatalf("no subcategories for category %q in the tagging tables", category)
	}
	return map[string]any{
		"item_id":          id,
		"photo_ref":        photoRef,
		"category":         category,
		"subcategory":      subs[shift%len(subs)],
		"dominant_color":   tax.Colors[shift%len(tax.Colors)],
		"secondary_colors": []string{tax.Colors[(shift+1)%len(tax.Colors)]},
		"pattern":          tax.Patterns[shift%len(tax.Patterns)],
		"warmth_tier":      tax.WarmthTiers[shift%len(tax.WarmthTiers)],
		"formality":        tax.Formality[shift%len(tax.Formality)],
		"notes":            notes,
	}
}

// ing035DoJSON marshals body and drives one JSON request through the engine.
func ing035DoJSON(t *testing.T, engine http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s %s body: %v", method, target, err)
	}
	req := httptest.NewRequest(method, target, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)
	return rr
}

// ing035Upload posts one accepted-extension garment photo and returns the
// decoded draft, failing the test on a non-200.
func ing035Upload(t *testing.T, h *ing035Harness, photo []byte) ing031Draft {
	t.Helper()
	rr := ing031Upload(t, h.engine, "photo", "garment.jpg", photo)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/items/photo status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	var draft ing031Draft
	if err := json.Unmarshal(rr.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v\n%s", err, rr.Body)
	}
	return draft
}

// ing035Create uploads a photo, corrects the draft, and persists it,
// returning the item id and the created API item.
func ing035Create(t *testing.T, h *ing035Harness, photo []byte, shift int, notes string) (string, ing019Item) {
	t.Helper()
	draft := ing035Upload(t, h, photo)
	body := ing035ValidBody(t, draft.ItemID, draft.PhotoRef, notes, shift)
	rr := ing035DoJSON(t, h.engine, http.MethodPost, "/api/items", body)
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("POST /api/items status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}
	return draft.ItemID, ing032DecodeItem(t, rr.Body.Bytes())
}

// ing035AssertBodyMatchesItem checks the seven tagging fields plus notes of a
// decoded API item against a body built by ing035ValidBody.
func ing035AssertBodyMatchesItem(t *testing.T, got ing019Item, body map[string]any) {
	t.Helper()
	str := func(k string) string {
		v, _ := body[k].(string)
		return v
	}
	if got.Category != str("category") {
		t.Errorf("category = %q, want %q", got.Category, str("category"))
	}
	if got.Subcategory != str("subcategory") {
		t.Errorf("subcategory = %q, want %q", got.Subcategory, str("subcategory"))
	}
	if got.DominantColor != str("dominant_color") {
		t.Errorf("dominant_color = %q, want %q", got.DominantColor, str("dominant_color"))
	}
	if got.Pattern != str("pattern") {
		t.Errorf("pattern = %q, want %q", got.Pattern, str("pattern"))
	}
	if got.WarmthTier != str("warmth_tier") {
		t.Errorf("warmth_tier = %q, want %q", got.WarmthTier, str("warmth_tier"))
	}
	if got.Formality != str("formality") {
		t.Errorf("formality = %q, want %q", got.Formality, str("formality"))
	}
	wantColors, _ := body["secondary_colors"].([]string)
	if !slices.Equal(got.SecondaryColors, wantColors) {
		t.Errorf("secondary_colors = %v, want %v", got.SecondaryColors, wantColors)
	}
	if got.Notes != str("notes") {
		t.Errorf("notes = %q, want %q", got.Notes, str("notes"))
	}
}

// ing035AssertBodyMatchesRow checks the same fields on a store row.
func ing035AssertBodyMatchesRow(t *testing.T, got store.Item, body map[string]any) {
	t.Helper()
	str := func(k string) string {
		v, _ := body[k].(string)
		return v
	}
	if got.Category != str("category") ||
		got.Subcategory != str("subcategory") ||
		got.DominantColor != str("dominant_color") ||
		got.Pattern != str("pattern") ||
		got.WarmthTier != str("warmth_tier") ||
		got.Formality != str("formality") {
		t.Errorf("row tagging fields = %+v, want the body's values %v", got, body)
	}
	wantColors, _ := body["secondary_colors"].([]string)
	if !slices.Equal(got.SecondaryColors, wantColors) {
		t.Errorf("row secondary_colors = %v, want %v", got.SecondaryColors, wantColors)
	}
	if got.Notes != str("notes") {
		t.Errorf("row notes = %q, want %q", got.Notes, str("notes"))
	}
}

// AC1: Given the real engine against an isolated root and a stub VLM / When
// the test uploads a photo, corrects the draft, persists it, fetches it, and
// updates it / Then the round trip succeeds: exactly one row with the
// corrected values, exactly one photo at data/photos/<item_id>.<ext>, the
// staged upload gone, and the before/after reads reflect the changes.
func TestING035_AC1_UploadCorrectPersistFetchUpdateRoundTrip(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	h := newING035Harness(t, func(int, ing005Request) string { return valid })

	photoBytes := []byte("ING035-ROUNDTRIP-PHOTO-BYTES")
	draft := ing035Upload(t, h, photoBytes)

	// The draft is not persisted; the upload is staged under the server name.
	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Errorf("catalog rows after draft = %d, want 0 (not persisted)", len(rows))
	}
	if files := ing031Files(t, h.photosDir); len(files) != 0 {
		t.Errorf("photos dir after draft = %v, want no file", files)
	}
	staged := filepath.Join(h.stagingDir, draft.PhotoRef)
	if got, err := os.ReadFile(staged); err != nil || !bytes.Equal(got, photoBytes) {
		t.Fatalf("staged upload %s = %q (err %v), want the uploaded bytes", staged, got, err)
	}

	id := draft.ItemID
	createBody := ing035ValidBody(t, id, draft.PhotoRef, "corrected in the UI", 1)
	rrCreate := ing035DoJSON(t, h.engine, http.MethodPost, "/api/items", createBody)
	if rrCreate.Code < 200 || rrCreate.Code >= 300 {
		t.Fatalf("POST /api/items status = %d, want 2xx; body:\n%s", rrCreate.Code, rrCreate.Body)
	}

	// Exactly one row, with the corrected values and the moved photo.
	wantPhoto := filepath.ToSlash(filepath.Join(h.photosDir, id+".jpg"))
	rows, err = h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows after create = %d, want exactly 1", len(rows))
	}
	if rows[0].ID != id || rows[0].PhotoPath != wantPhoto {
		t.Errorf("row = {id:%q photo_path:%q}, want {id:%q photo_path:%q}", rows[0].ID, rows[0].PhotoPath, id, wantPhoto)
	}
	ing035AssertBodyMatchesRow(t, rows[0], createBody)

	if got, err := os.ReadFile(wantPhoto); err != nil || !bytes.Equal(got, photoBytes) {
		t.Fatalf("stored photo %s = %q (err %v), want the uploaded bytes", wantPhoto, got, err)
	}
	if files := ing031Files(t, h.photosDir); len(files) != 1 || files[0] != id+".jpg" {
		t.Errorf("photos dir = %v, want exactly [%s]", files, id+".jpg")
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want empty after save", files)
	}

	// The earlier read reflects the corrected values.
	rrGet := ing019Do(t, h.engine, http.MethodGet, "/api/items/"+id)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET /api/items/%s status = %d, want 200; body:\n%s", id, rrGet.Code, rrGet.Body)
	}
	created := ing032DecodeItem(t, rrGet.Body.Bytes())
	ing035AssertBodyMatchesItem(t, created, createBody)
	if created.PhotoPath != wantPhoto {
		t.Errorf("photo_path = %q, want %q", created.PhotoPath, wantPhoto)
	}
	if want := "/api/photos/" + id + ".jpg"; created.PhotoURL != want {
		t.Errorf("photo_url = %q, want %q", created.PhotoURL, want)
	}
	if created.AddedDate == "" {
		t.Errorf("added_date is empty, want the create date")
	}
	createdDate := created.AddedDate

	// The update changes every editable field.
	updateBody := ing035ValidBody(t, id, draft.PhotoRef, "updated in the UI", 2)
	rrPut := ing035DoJSON(t, h.engine, http.MethodPut, "/api/items/"+id, updateBody)
	if rrPut.Code != http.StatusOK {
		t.Fatalf("PUT /api/items/%s status = %d, want 200; body:\n%s", id, rrPut.Code, rrPut.Body)
	}
	put := ing032DecodeItem(t, rrPut.Body.Bytes())
	ing035AssertBodyMatchesItem(t, put, updateBody)
	if put.AddedDate != createdDate {
		t.Errorf("added_date changed on edit: %q -> %q (immutable)", createdDate, put.AddedDate)
	}
	if put.PhotoPath != wantPhoto {
		t.Errorf("photo_path changed on edit: %q -> %q (immutable)", wantPhoto, put.PhotoPath)
	}

	// The updated read reflects the new values; id/date/photo are unchanged.
	rrGet2 := ing019Do(t, h.engine, http.MethodGet, "/api/items/"+id)
	if rrGet2.Code != http.StatusOK {
		t.Fatalf("GET after update status = %d, want 200; body:\n%s", rrGet2.Code, rrGet2.Body)
	}
	updated := ing032DecodeItem(t, rrGet2.Body.Bytes())
	ing035AssertBodyMatchesItem(t, updated, updateBody)
	if updated.AddedDate != createdDate {
		t.Errorf("added_date = %q after update, want the create date %q", updated.AddedDate, createdDate)
	}
	if updated.PhotoPath != wantPhoto {
		t.Errorf("photo_path = %q after update, want %q", updated.PhotoPath, wantPhoto)
	}

	// Still exactly one row and one photo.
	rows, err = h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows after update = %d, want exactly 1", len(rows))
	}
	ing035AssertBodyMatchesRow(t, rows[0], updateBody)
	if got := rows[0].AddedDate.Format("2006-01-02"); got != createdDate {
		t.Errorf("row added_date = %q after update, want %q", got, createdDate)
	}
	if got, err := os.ReadFile(wantPhoto); err != nil || !bytes.Equal(got, photoBytes) {
		t.Errorf("stored photo changed on edit: %q (err %v), want the uploaded bytes", got, err)
	}
	if files := ing031Files(t, h.photosDir); len(files) != 1 || files[0] != id+".jpg" {
		t.Errorf("photos dir = %v, want exactly [%s]", files, id+".jpg")
	}
}

// AC2: Given a draft-ready upload / When the persist fails at the
// photo/filesystem write (data/photos pre-occupied so the move cannot
// complete) / Then no row exists for the id, no orphan photo is left, and
// the failure surfaces rather than being swallowed.
func TestING035_AC2_CreateFailureLeavesNoRowOrOrphanAndSurfaces(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	h := newING035Harness(t, func(int, ing005Request) string { return valid })

	draft := ing035Upload(t, h, []byte("ING035-FAIL-PHOTO"))
	id := draft.ItemID
	createBody := ing035ValidBody(t, id, draft.PhotoRef, "should not persist", 1)
	staged := filepath.Join(h.stagingDir, draft.PhotoRef)

	// Pre-occupy data/photos with a regular file so catalog.Create's
	// MkdirAll cannot complete the photo write.
	occupier := []byte("ING035-OCCUPIES-PHOTOS-DIR")
	if err := os.WriteFile(h.photosDir, occupier, 0o644); err != nil {
		t.Fatalf("occupy photos path: %v", err)
	}

	rr := ing035DoJSON(t, h.engine, http.MethodPost, "/api/items", createBody)
	if rr.Code < 400 {
		t.Fatalf("POST /api/items status = %d, want a surfaced failure; body:\n%s", rr.Code, rr.Body)
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failure body is not a JSON object: %v\n%s", err, rr.Body)
	}
	if strings.TrimSpace(resp["error"]) == "" {
		t.Errorf("failure body has no error message; the failure must surface, not be swallowed:\n%s", rr.Body)
	}

	// No row for that id, and no dangling row at all.
	if _, err := h.st.Get(id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get(%s) err = %v, want store.ErrNotFound (no row for a failed create)", id, err)
	}
	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Errorf("catalog rows = %d, want 0 after a failed create", len(rows))
	}

	// No orphan photo: the path is still the occupying regular file, unchanged.
	info, err := os.Stat(h.photosDir)
	if err != nil {
		t.Fatalf("stat occupied photos path: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("photos path mode = %v, want the occupying regular file (no directory created)", info.Mode())
	}
	if got, err := os.ReadFile(h.photosDir); err != nil || !bytes.Equal(got, occupier) {
		t.Errorf("photos path content = %q (err %v), want the untouched occupier %q", got, err, occupier)
	}

	// A failed create removes the staged upload.
	if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staged file %s still exists after a failed create (stat err=%v)", staged, err)
	}
}

// AC4: Given a corrected record that fails taxonomy validation / When it is
// PUT to an existing id / Then the response is 400 naming the field and the
// stored row is unchanged (04-data-schema.md "Write-path rules" /
// 05-vlm-tagging-spec.md "Interactive tagging").
func TestING035_AC4_InvalidUpdateIs400NamingFieldAndLeavesRowUnchanged(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)

	cases := []struct {
		name  string
		field string // must appear in the 400 response
		mut   func(b map[string]any)
	}{
		{
			name:  "invalid_enum_warmth_tier",
			field: "warmth_tier",
			mut:   func(b map[string]any) { b["warmth_tier"] = "sweltering" },
		},
		{
			name:  "invalid_category_subcategory_pair",
			field: "subcategory",
			mut: func(b map[string]any) {
				// "top" is a category and "jeans" a bottom subcategory: each
				// valid alone, invalid as a pair (03-taxonomy.md).
				b["category"] = "top"
				b["subcategory"] = "jeans"
			},
		},
		{
			name:  "missing_required_field_pattern",
			field: "pattern",
			mut:   func(b map[string]any) { delete(b, "pattern") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newING035Harness(t, func(int, ing005Request) string { return valid })
			id, _ := ing035Create(t, h, []byte("ING035-TAXONOMY-PHOTO"), 1, "valid before the bad edit")
			before, err := h.st.Get(id)
			if err != nil {
				t.Fatalf("Get(%s) = %v, want the created row", id, err)
			}

			body := ing035ValidBody(t, id, "", "invalid edit", 2)
			tc.mut(body)
			rr := ing035DoJSON(t, h.engine, http.MethodPut, "/api/items/"+id, body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT status = %d, want 400; body:\n%s", rr.Code, rr.Body)
			}
			var resp map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("400 body is not a JSON object: %v\n%s", err, rr.Body)
			}
			if !strings.Contains(resp["error"], tc.field) {
				t.Errorf("400 error %q does not name the offending field %q", resp["error"], tc.field)
			}

			// The stored row is unchanged.
			after, err := h.st.Get(id)
			if err != nil {
				t.Fatalf("Get(%s) after rejected update = %v", id, err)
			}
			assertItemEqual(t, after, before)

			// A subsequent read still returns the pre-edit values.
			rrGet := ing019Do(t, h.engine, http.MethodGet, "/api/items/"+id)
			if rrGet.Code != http.StatusOK {
				t.Fatalf("GET after rejected update status = %d, want 200", rrGet.Code)
			}
			got := ing032DecodeItem(t, rrGet.Body.Bytes())
			if got.WarmthTier != before.WarmthTier || got.Pattern != before.Pattern ||
				got.Category != before.Category || got.Subcategory != before.Subcategory {
				t.Errorf("read after rejected update = %+v, want the unchanged row %+v", got, before)
			}
		})
	}
}
