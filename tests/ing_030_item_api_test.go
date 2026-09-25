// Package tests holds black-box acceptance tests for backlog tickets.
// ING-030: GET /api/items/:id serves one item in the exact JSON shape
// GET /api/items serves per element (the 04-data-schema.md fields plus
// photo_url); PUT /api/items/:id accepts the seven tagging fields plus
// notes, validates them through internal/tagging's ParseTaggingResult
// (never a second enum copy of 03-taxonomy.md), persists them, and never
// touches id, added_date, photo_path, or the photo file. Unknown ids are
// 404 on both methods; an invalid body is 400 naming the offending field
// with the item left unchanged. The route-surface AC is asserted by
// TestING019_AC7_OnlyReadOnlyRoutes in ing_019_api_test.go, which this
// ticket converted to the approved route set (still no delete) per its
// test note.
package tests

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

// ing030ID is the single item every test in this file inserts.
const ing030ID = "0f8c2b1e-0300-4a01-8300-000000000001"

// ing030Put sends a raw JSON body to target and returns the recorder.
func ing030Put(t *testing.T, engine http.Handler, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)
	return rr
}

// ing030ValidBody builds a taxonomy-valid PUT body from
// tagging.TaxonomyTables() — the very tables ParseTaggingResult validates
// against — so this file never hand-copies 03-taxonomy.md. Every value is
// picked to differ from sampleItem's, so a successful PUT visibly changes
// all seven tagging fields plus notes.
func ing030ValidBody(t *testing.T) map[string]any {
	t.Helper()
	tax := tagging.TaxonomyTables()
	categories := slices.Sorted(maps.Keys(tax.Categories))
	if len(categories) == 0 || len(tax.Colors) < 3 || len(tax.Patterns) < 3 ||
		len(tax.WarmthTiers) < 2 || len(tax.Formality) < 2 {
		t.Fatalf("tagging tables too small to build a valid body: %+v", tax)
	}
	category := categories[0]
	subs := tax.Categories[category]
	if len(subs) == 0 {
		t.Fatalf("no subcategories for category %q in the tagging tables", category)
	}
	return map[string]any{
		"category":         category,
		"subcategory":      subs[0],
		"dominant_color":   tax.Colors[1],
		"secondary_colors": []string{tax.Colors[0], tax.Colors[2]},
		"pattern":          tax.Patterns[2],
		"warmth_tier":      tax.WarmthTiers[1],
		"formality":        tax.Formality[1],
		"notes":            "edited via PUT",
	}
}

// ing030Marshal renders a PUT body or fails the test.
func ing030Marshal(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal PUT body: %v", err)
	}
	return string(raw)
}

// ing030Insert seeds the store with sampleItem(ing030ID) and returns it.
func ing030Insert(t *testing.T, st *store.Store) store.Item {
	t.Helper()
	original := sampleItem(ing030ID)
	if err := st.Insert(original); err != nil {
		t.Fatalf("Insert(%s) = %v, want nil", original.ID, err)
	}
	return original
}

// AC1: Given a catalog item id that exists / When a client GETs
// /api/items/:id / Then it receives 200 with the same JSON object shape
// GET /api/items returns for one item — the 04-data-schema.md fields plus
// photo_url.
func TestING030_AC1_GetOneItemMatchesListShape(t *testing.T) {
	engine, st, _ := newING019API(t)
	item := ing030Insert(t, st)

	rrList := ing019Do(t, engine, http.MethodGet, "/api/items")
	if rrList.Code != http.StatusOK {
		t.Fatalf("GET /api/items status = %d, want 200", rrList.Code)
	}
	var list []ing019Item
	if err := json.Unmarshal(rrList.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v\n%s", err, rrList.Body)
	}

	rr := ing019Do(t, engine, http.MethodGet, "/api/items/"+item.ID)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/items/:id status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// Same key set as one element of GET /api/items: the contract keys,
	// nothing extra, nothing missing.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("GET /api/items/:id body is not a JSON object: %v\n%s", err, rr.Body)
	}
	if len(raw) != len(ing019ItemKeys) {
		t.Errorf("keys = %v, want exactly %v", ing019Keys(raw), ing019ItemKeys)
	}
	for _, k := range ing019ItemKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing contract key %q; keys = %v", k, ing019Keys(raw))
		}
	}

	var got ing019Item
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	var want ing019Item
	found := false
	for _, listed := range list {
		if listed.ID == item.ID {
			want, found = listed, true
			break
		}
	}
	if !found {
		t.Fatalf("item %q missing from GET /api/items", item.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GET /api/items/:id = %+v, want the GET /api/items element %+v", got, want)
	}
}

// AC2: Given an id that does not exist / When a client GETs or PUTs
// /api/items/:id / Then it receives 404. The PUT cases also pin the
// lookup order: an unknown id is 404 regardless of body validity (the id
// is checked before the body is read).
func TestING030_AC2_UnknownIDIs404ForGetAndPut(t *testing.T) {
	engine, st, _ := newING019API(t)
	ing030Insert(t, st) // catalog is non-empty; only the requested id is unknown

	validBody := ing030Marshal(t, ing030ValidBody(t))
	cases := []struct {
		name   string
		method string
		body   string
	}{
		{"get", http.MethodGet, ""},
		{"put_valid_body", http.MethodPut, validBody},
		{"put_invalid_body", http.MethodPut, `{"category":"nope","subcategory":"jeans"}`},
		{"put_empty_body", http.MethodPut, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/items/no-such-id", strings.NewReader(tc.body))
			if tc.method == http.MethodPut {
				req.Header.Set("Content-Type", "application/json")
			}
			rr := httptest.NewRecorder()
			engine.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Errorf("%s /api/items/no-such-id status = %d, want 404; body:\n%s",
					tc.method, rr.Code, rr.Body)
			}
		})
	}

	// No row was created or mutated by any of the rejected requests.
	items, err := st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(items) != 1 || items[0].ID != ing030ID {
		t.Errorf("catalog after 404s = %d rows, want the 1 untouched seeded row", len(items))
	}
}

// AC3: Given an existing item and a PUT body carrying the seven tagging
// fields plus notes, all taxonomy-valid / When the client PUTs
// /api/items/:id / Then it receives a 2xx, the mutable fields are
// persisted, and a subsequent GET /api/items/:id reflects the new values.
func TestING030_AC3_PutValidBodyPersistsAndGetReflects(t *testing.T) {
	engine, st, _ := newING019API(t)
	original := ing030Insert(t, st)

	body := ing030ValidBody(t)
	rr := ing030Put(t, engine, "/api/items/"+ing030ID, ing030Marshal(t, body))
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("PUT /api/items/:id status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}

	// Persisted in the store.
	got, err := st.Get(ing030ID)
	if err != nil {
		t.Fatalf("Get(%s) = %v, want nil", ing030ID, err)
	}
	wantCategory, _ := body["category"].(string)
	wantSub, _ := body["subcategory"].(string)
	wantColor, _ := body["dominant_color"].(string)
	wantPattern, _ := body["pattern"].(string)
	wantWarmth, _ := body["warmth_tier"].(string)
	wantFormality, _ := body["formality"].(string)
	wantNotes, _ := body["notes"].(string)
	wantSecondary := body["secondary_colors"].([]string)
	if got.Category != wantCategory || got.Subcategory != wantSub ||
		got.DominantColor != wantColor || got.Pattern != wantPattern ||
		got.WarmthTier != wantWarmth || got.Formality != wantFormality ||
		got.Notes != wantNotes || !slices.Equal(got.SecondaryColors, wantSecondary) {
		t.Errorf("stored mutable fields = %+v, want the PUT body values", got)
	}
	if got.ID != original.ID || got.PhotoPath != original.PhotoPath ||
		!got.AddedDate.Equal(original.AddedDate) {
		t.Errorf("immutable fields changed on PUT: id=%q photo_path=%q added_date=%v",
			got.ID, got.PhotoPath, got.AddedDate)
	}

	// A subsequent GET reflects them, in the same shape as GET /api/items.
	rrGet := ing019Do(t, engine, http.MethodGet, "/api/items/"+ing030ID)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET after PUT status = %d, want 200; body:\n%s", rrGet.Code, rrGet.Body)
	}
	var item ing019Item
	if err := json.Unmarshal(rrGet.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item after PUT: %v\n%s", err, rrGet.Body)
	}
	if item.Category != wantCategory || item.Subcategory != wantSub ||
		item.DominantColor != wantColor || item.Pattern != wantPattern ||
		item.WarmthTier != wantWarmth || item.Formality != wantFormality ||
		item.Notes != wantNotes || !slices.Equal(item.SecondaryColors, wantSecondary) {
		t.Errorf("GET after PUT = %+v, want the PUT body values", item)
	}
	if item.ID != original.ID || item.PhotoPath != original.PhotoPath ||
		item.AddedDate != "2026-09-18" {
		t.Errorf("GET after PUT immutable fields: id=%q photo_path=%q added_date=%q, want the stored values",
			item.ID, item.PhotoPath, item.AddedDate)
	}
}

// AC4: Given a PUT body with an invalid enum value, an invalid
// category/subcategory pair, or a missing required tagging field / When
// the client PUTs /api/items/:id / Then it receives 400, the response
// names the offending field, and the item is left unchanged.
func TestING030_AC4_InvalidBodyIs400NamingFieldItemUnchanged(t *testing.T) {
	engine, st, _ := newING019API(t)
	original := ing030Insert(t, st)
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
			name:  "invalid_enum_secondary_color",
			field: "secondary_colors",
			mut: func(t *testing.T, b map[string]any) {
				b["secondary_colors"] = []string{tax.Colors[0], "chartreuse"}
			},
		},
		{
			name:  "invalid_category_subcategory_pair",
			field: "subcategory",
			mut: func(t *testing.T, b map[string]any) {
				// Valid values individually, invalid as a pair: "top" is a
				// category, "jeans" a bottom subcategory (03-taxonomy.md).
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
			body := ing030ValidBody(t)
			tc.mut(t, body)
			rr := ing030Put(t, engine, "/api/items/"+ing030ID, ing030Marshal(t, body))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT status = %d, want 400; body:\n%s", rr.Code, rr.Body)
			}
			if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var resp map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("400 body is not a JSON object: %v\n%s", err, rr.Body)
			}
			if !strings.Contains(resp["error"], tc.field) {
				t.Errorf("400 error %q does not name the offending field %q", resp["error"], tc.field)
			}

			// The item is left unchanged.
			got, err := st.Get(ing030ID)
			if err != nil {
				t.Fatalf("Get(%s) = %v, want nil", ing030ID, err)
			}
			assertItemEqual(t, got, original)
		})
	}
}

// AC5: Given a PUT body that also carries id, added_date, or photo_path
// with different values / When the client PUTs /api/items/:id / Then
// those values do not change: the stored id, added_date, and photo_path
// are unchanged and the photo file is untouched.
func TestING030_AC5_ImmutableFieldsAndPhotoUntouched(t *testing.T) {
	engine, st, photosDir := newING019API(t)
	original := ing030Insert(t, st)

	// The actual photo file behind the item's photo_url.
	photoBytes := []byte("\xff\xd8\xff\xe0ING030-PHOTO-BYTES")
	photoName := filepath.Base(original.PhotoPath)
	if err := os.WriteFile(filepath.Join(photosDir, photoName), photoBytes, 0o644); err != nil {
		t.Fatalf("write photo: %v", err)
	}

	body := ing030ValidBody(t)
	body["id"] = "attacker-chosen-id"
	body["added_date"] = "1999-01-01"
	body["photo_path"] = "data/photos/attacker-controlled.jpg"

	rr := ing030Put(t, engine, "/api/items/"+ing030ID, ing030Marshal(t, body))
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("PUT status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}

	got, err := st.Get(ing030ID)
	if err != nil {
		t.Fatalf("Get(%s) = %v, want nil", ing030ID, err)
	}
	if got.ID != original.ID {
		t.Errorf("stored id = %q, want immutable %q", got.ID, original.ID)
	}
	if !got.AddedDate.Equal(original.AddedDate) {
		t.Errorf("stored added_date = %v, want immutable %v", got.AddedDate, original.AddedDate)
	}
	if got.PhotoPath != original.PhotoPath {
		t.Errorf("stored photo_path = %q, want immutable %q", got.PhotoPath, original.PhotoPath)
	}

	// The GET shape reports the stored immutable values, not the body's.
	rrGet := ing019Do(t, engine, http.MethodGet, "/api/items/"+ing030ID)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET after PUT status = %d, want 200", rrGet.Code)
	}
	var item ing019Item
	if err := json.Unmarshal(rrGet.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if item.ID != ing030ID || item.AddedDate != "2026-09-18" ||
		item.PhotoPath != "data/photos/"+ing030ID+".jpg" ||
		item.PhotoURL != "/api/photos/"+photoName {
		t.Errorf("GET immutable fields: id=%q added_date=%q photo_path=%q photo_url=%q, want the stored values",
			item.ID, item.AddedDate, item.PhotoPath, item.PhotoURL)
	}

	// The photo file is untouched and still served.
	onDisk, err := os.ReadFile(filepath.Join(photosDir, photoName))
	if err != nil {
		t.Fatalf("read photo after PUT: %v", err)
	}
	if string(onDisk) != string(photoBytes) {
		t.Errorf("photo file changed on PUT: got %q, want %q", onDisk, photoBytes)
	}
	rrPhoto := ing019Do(t, engine, http.MethodGet, "/api/photos/"+photoName)
	if rrPhoto.Code != http.StatusOK {
		t.Fatalf("GET /api/photos/%s status = %d, want 200", photoName, rrPhoto.Code)
	}
	if rrPhoto.Body.String() != string(photoBytes) {
		t.Errorf("served photo bytes changed after PUT")
	}
}

// AC6: Given the corrected record / When it is validated / Then it
// validates against the same 03-taxonomy.md enums as a model output,
// reusing internal/tagging's taxonomy tables (ParseTaggingResult) rather
// than a second enum copy. Behavior: every category/subcategory/color the
// tables hold is accepted, and a pair or value outside them is rejected —
// so the accepted set is exactly tagging.TaxonomyTables(), not a
// hand-maintained subset. Static half: internal/api routes validation
// through tagging.ParseTaggingResult (same source-check style as
// TestING019_AC1).
func TestING030_AC6_ValidationReusesTaggingTables(t *testing.T) {
	engine, st, _ := newING019API(t)
	ing030Insert(t, st)
	tax := tagging.TaxonomyTables()
	categories := slices.Sorted(maps.Keys(tax.Categories))
	if len(categories) < 2 {
		t.Fatalf("need at least 2 categories to test pairs, got %v", categories)
	}

	// Static half: the handler calls the shared validator.
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "api", "api.go"))
	if err != nil {
		t.Fatalf("read internal/api/api.go: %v", err)
	}
	if !strings.Contains(string(src), "tagging.ParseTaggingResult(") {
		t.Errorf("internal/api/api.go does not validate through tagging.ParseTaggingResult — " +
			"PUT must reuse internal/tagging, not a second enum copy")
	}

	// Every category with its own first subcategory is accepted (values
	// drawn from the tables, never spelled out here).
	for _, category := range categories {
		body := ing030ValidBody(t)
		body["category"] = category
		body["subcategory"] = tax.Categories[category][0]
		rr := ing030Put(t, engine, "/api/items/"+ing030ID, ing030Marshal(t, body))
		if rr.Code < 200 || rr.Code >= 300 {
			t.Errorf("PUT with tables-derived pair %q/%q status = %d, want 2xx; body:\n%s",
				category, tax.Categories[category][0], rr.Code, rr.Body)
		}
	}

	// A subcategory that belongs to a different category is rejected.
	outer := categories[0]
	badSub := ""
	for _, other := range categories[1:] {
		for _, sub := range tax.Categories[other] {
			if !slices.Contains(tax.Categories[outer], sub) {
				badSub = sub
				break
			}
		}
		if badSub != "" {
			break
		}
	}
	if badSub == "" {
		t.Fatalf("taxonomy holds no cross-category subcategory to test an invalid pair")
	}
	pairBody := ing030ValidBody(t)
	pairBody["category"] = outer
	pairBody["subcategory"] = badSub
	rrPair := ing030Put(t, engine, "/api/items/"+ing030ID, ing030Marshal(t, pairBody))
	if rrPair.Code != http.StatusBadRequest {
		t.Errorf("PUT with pair %q/%q status = %d, want 400; body:\n%s",
			outer, badSub, rrPair.Code, rrPair.Body)
	}

	// A value in no table (a color outside the palette) is rejected.
	colorBody := ing030ValidBody(t)
	colorBody["dominant_color"] = "chartreuse"
	rrColor := ing030Put(t, engine, "/api/items/"+ing030ID, ing030Marshal(t, colorBody))
	if rrColor.Code != http.StatusBadRequest {
		t.Errorf("PUT with a color outside the palette status = %d, want 400; body:\n%s",
			rrColor.Code, rrColor.Body)
	}

	// The item still holds the last accepted values from the loop above.
	got, err := st.Get(ing030ID)
	if err != nil {
		t.Fatalf("Get(%s) = %v, want nil", ing030ID, err)
	}
	lastCategory := categories[len(categories)-1]
	lastSub := tax.Categories[lastCategory][0]
	if got.Category != lastCategory || got.Subcategory != lastSub {
		t.Errorf("stored pair = %q/%q, want the last accepted tables-derived pair %q/%q",
			got.Category, got.Subcategory, lastCategory, lastSub)
	}
}

// Edge (spec-implied, ticket did not name it): 04-data-schema.md types
// secondary_colors as "0 or more" and notes as free text with no enum.
// A corrected record that clears the secondary colors or carries notes that
// contain (or look like) taxonomy words must be accepted and round-trip:
// the list serializes as [] not null, and notes are never enum-checked.
func TestING030_Edge_EmptySecondaryColorsAndFreeTextNotes(t *testing.T) {
	engine, st, _ := newING019API(t)
	ing030Insert(t, st) // sampleItem has 2 secondary colors and non-empty notes

	body := ing030ValidBody(t)
	body["secondary_colors"] = []string{}
	body["notes"] = "solid mood — not a pattern: hoodie-ish, smart-casual at best"
	rr := ing030Put(t, engine, "/api/items/"+ing030ID, ing030Marshal(t, body))
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("PUT with empty secondary_colors / free-text notes status = %d, want 2xx; body:\n%s",
			rr.Code, rr.Body)
	}

	got, err := st.Get(ing030ID)
	if err != nil {
		t.Fatalf("Get(%s) = %v, want nil", ing030ID, err)
	}
	if len(got.SecondaryColors) != 0 {
		t.Errorf("stored secondary_colors = %v, want cleared to an empty list", got.SecondaryColors)
	}
	if got.Notes != body["notes"] {
		t.Errorf("stored notes = %q, want the free text %q", got.Notes, body["notes"])
	}

	rrGet := ing019Do(t, engine, http.MethodGet, "/api/items/"+ing030ID)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET after PUT status = %d, want 200; body:\n%s", rrGet.Code, rrGet.Body)
	}
	var item ing019Item
	if err := json.Unmarshal(rrGet.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item: %v\n%s", err, rrGet.Body)
	}
	if item.SecondaryColors == nil || len(item.SecondaryColors) != 0 {
		t.Errorf("GET secondary_colors = %v, want [] (never null)", item.SecondaryColors)
	}
	if item.Notes != body["notes"] {
		t.Errorf("GET notes = %q, want %q", item.Notes, body["notes"])
	}
}

// Edge (Coder assumption 4 + a required tagging field the AC4 table omits):
// a malformed JSON body and an omitted secondary_colors are 400 with the
// guard/field named, and the item is left unchanged either way.
func TestING030_Edge_BadBodyIs400AndItemUnchanged(t *testing.T) {
	engine, st, _ := newING019API(t)
	original := ing030Insert(t, st)

	cases := []struct {
		name string
		body string
		want string // substring the 400 error must contain
	}{
		{"malformed_json", `{"category": "top",`, "invalid JSON body"},
		{"omitted_secondary_colors", "", "secondary_colors"}, // marshaled below
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bodyStr := tc.body
			if bodyStr == "" {
				b := ing030ValidBody(t)
				delete(b, "secondary_colors")
				bodyStr = ing030Marshal(t, b)
			}
			rr := ing030Put(t, engine, "/api/items/"+ing030ID, bodyStr)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT status = %d, want 400; body:\n%s", rr.Code, rr.Body)
			}
			var resp map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("400 body is not a JSON object: %v\n%s", err, rr.Body)
			}
			if !strings.Contains(resp["error"], tc.want) {
				t.Errorf("400 error %q does not contain %q", resp["error"], tc.want)
			}
			got, err := st.Get(ing030ID)
			if err != nil {
				t.Fatalf("Get(%s) = %v, want nil", ing030ID, err)
			}
			assertItemEqual(t, got, original)
		})
	}
}
