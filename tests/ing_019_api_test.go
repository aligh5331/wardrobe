// Package tests holds black-box acceptance tests for backlog tickets.
// ING-019: cmd/server serves a Gin API — GET /api/items returns
// the 04-data-schema.md fields plus a photo URL, GET /api/photos/<file>
// returns stored bytes (rejecting traversal), and exactly the approved
// route set is registered (AC7's assertion grew with the approved write
// routes: ING-036 added GET /api/taxonomy, ING-030 added GET/PUT
// /api/items/:id — still no delete). These tests exercise the exported
// wardrobe/internal/api surface; implementation code is never touched
// from here.
package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/store"
)

// ing019Item mirrors the agreed read-only contract for one GET /api/items
// object: the 04-data-schema.md fields plus photo_url.
type ing019Item struct {
	ID              string   `json:"id"`
	Category        string   `json:"category"`
	Subcategory     string   `json:"subcategory"`
	DominantColor   string   `json:"dominant_color"`
	SecondaryColors []string `json:"secondary_colors"`
	Pattern         string   `json:"pattern"`
	WarmthTier      string   `json:"warmth_tier"`
	Formality       string   `json:"formality"`
	PhotoPath       string   `json:"photo_path"`
	AddedDate       string   `json:"added_date"`
	Notes           string   `json:"notes"`
	PhotoURL        string   `json:"photo_url"`
}

// ing019ItemKeys is the exact key set the contract fixes: the 11 schema
// fields plus photo_url. Anything extra is drift the grid is not told about.
var ing019ItemKeys = []string{
	"id", "category", "subcategory", "dominant_color", "secondary_colors",
	"pattern", "warmth_tier", "formality", "photo_path", "added_date",
	"notes", "photo_url",
}

// newING019API builds the real engine against a throwaway store and a
// throwaway photos directory, so tests never read or write the repo's real
// data/ (personal wardrobe data).
func newING019API(t *testing.T) (*gin.Engine, *store.Store, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	photosDir := filepath.Join(t.TempDir(), "photos")
	if err := os.MkdirAll(photosDir, 0o755); err != nil {
		t.Fatalf("mkdir photos dir: %v", err)
	}
	return api.New(st, photosDir), st, photosDir
}

// ing019Do drives a request through the engine and returns the recorder.
func ing019Do(t *testing.T, engine http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)
	return rr
}

// AC1 (static half): the stdlib http.NewServeMux placeholder is gone and
// main wires the Gin engine through internal/api.
func TestING019_AC1_MainUsesGinNotStdlibMux(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "cmd", "server", "main.go"))
	if err != nil {
		t.Fatalf("read cmd/server/main.go: %v", err)
	}
	s := string(src)
	if strings.Contains(s, "NewServeMux") {
		t.Errorf("cmd/server/main.go still contains the stdlib http.NewServeMux placeholder")
	}
	if !strings.Contains(s, "internal/api") {
		t.Errorf("cmd/server/main.go does not wire wardrobe/internal/api")
	}
	if !strings.Contains(s, "api.New(") {
		t.Errorf("cmd/server/main.go does not call api.New (the Gin engine constructor)")
	}
}

// AC3: Given catalog rows exist / When a client GETs /api/items / Then it
// receives 200 with a JSON array carrying one object per row, each exposing
// the 04-data-schema.md fields plus a photo URL.
func TestING019_AC3_ItemsExposesSchemaFieldsAndPhotoURL(t *testing.T) {
	engine, st, _ := newING019API(t)

	populated := sampleItem("0f8c2b1e-1001-4a01-8101-000000001001")
	empty := sampleItem("0f8c2b1e-1002-4a01-8102-000000001002")
	empty.SecondaryColors = []string{}
	empty.Notes = ""
	empty.Pattern = "solid"

	for _, it := range []store.Item{populated, empty} {
		if err := st.Insert(it); err != nil {
			t.Fatalf("Insert(%s) = %v, want nil", it.ID, err)
		}
	}

	rr := ing019Do(t, engine, http.MethodGet, "/api/items")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/items status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	body := rr.Body.Bytes()

	// Exactly one object per row, with exactly the contract's key set.
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("GET /api/items body is not a JSON array: %v\n%s", err, body)
	}
	if len(raw) != 2 {
		t.Fatalf("GET /api/items returned %d objects, want 2 (one per row)", len(raw))
	}
	for i, obj := range raw {
		if len(obj) != len(ing019ItemKeys) {
			t.Errorf("object %d keys = %v, want exactly %v", i, ing019Keys(obj), ing019ItemKeys)
		}
		for _, k := range ing019ItemKeys {
			if _, ok := obj[k]; !ok {
				t.Errorf("object %d missing contract key %q; keys = %v", i, k, ing019Keys(obj))
			}
		}
	}

	var items []ing019Item
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	byID := map[string]ing019Item{}
	for _, it := range items {
		byID[it.ID] = it
	}

	got, ok := byID[populated.ID]
	if !ok {
		t.Fatalf("item %q missing from /api/items", populated.ID)
	}
	if got.Category != populated.Category ||
		got.Subcategory != populated.Subcategory ||
		got.DominantColor != populated.DominantColor ||
		got.Pattern != populated.Pattern ||
		got.WarmthTier != populated.WarmthTier ||
		got.Formality != populated.Formality ||
		got.PhotoPath != populated.PhotoPath ||
		got.Notes != populated.Notes {
		t.Errorf("item fields mismatch:\n got %+v\nwant %+v", got, populated)
	}
	if len(got.SecondaryColors) != 2 || got.SecondaryColors[0] != "white" || got.SecondaryColors[1] != "red" {
		t.Errorf("secondary_colors = %v, want [white red]", got.SecondaryColors)
	}
	if got.AddedDate != "2026-09-18" {
		t.Errorf("added_date = %q, want %q", got.AddedDate, "2026-09-18")
	}
	if want := "/api/photos/" + populated.ID + ".jpg"; got.PhotoURL != want {
		t.Errorf("photo_url = %q, want %q", got.PhotoURL, want)
	}
	if got.PhotoPath != "data/photos/"+populated.ID+".jpg" {
		t.Errorf("photo_path = %q, want data/photos/<id>.jpg", got.PhotoPath)
	}

	gotEmpty, ok := byID[empty.ID]
	if !ok {
		t.Fatalf("item %q missing from /api/items", empty.ID)
	}
	if gotEmpty.SecondaryColors == nil {
		t.Errorf("empty secondary_colors serialized as null, want []")
	}
	if len(gotEmpty.SecondaryColors) != 0 {
		t.Errorf("empty secondary_colors = %v, want a 0-length list", gotEmpty.SecondaryColors)
	}
	if gotEmpty.Notes != "" {
		t.Errorf("notes = %q, want empty", gotEmpty.Notes)
	}
}

// AC4: Given the catalog is empty / When a client GETs /api/items / Then it
// receives 200 with an empty JSON array, not an error and not null.
func TestING019_AC4_EmptyCatalogIsEmptyArray(t *testing.T) {
	engine, _, _ := newING019API(t)

	rr := ing019Do(t, engine, http.MethodGet, "/api/items")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/items status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "[]" {
		t.Errorf("empty catalog body = %q, want %q", body, "[]")
	}
}

// AC5: Given a stored photo filename / When a client GETs
// /api/photos/<filename> / Then the image bytes for photosDir/<filename>
// are returned with the correct Content-Type.
func TestING019_AC5_ServesStoredPhotoBytesAndContentType(t *testing.T) {
	engine, _, photosDir := newING019API(t)

	pngBytes := []byte("\x89PNG\r\n\x1a\nING019-PNG-BYTES")
	jpgBytes := []byte("\xff\xd8\xff\xe0ING019-JPEG-BYTES")
	if err := os.WriteFile(filepath.Join(photosDir, "shirt.png"), pngBytes, 0o644); err != nil {
		t.Fatalf("write shirt.png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(photosDir, "coat.jpg"), jpgBytes, 0o644); err != nil {
		t.Fatalf("write coat.jpg: %v", err)
	}

	cases := []struct {
		name string
		file string
		want []byte
		mime string
	}{
		{"png", "shirt.png", pngBytes, "image/png"},
		{"jpg", "coat.jpg", jpgBytes, "image/jpeg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := ing019Do(t, engine, http.MethodGet, "/api/photos/"+tc.file)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET /api/photos/%s status = %d, want 200; body:\n%s", tc.file, rr.Code, rr.Body)
			}
			if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, tc.mime) {
				t.Errorf("Content-Type = %q, want %q", ct, tc.mime)
			}
			if got := rr.Body.Bytes(); !bytes.Equal(got, tc.want) {
				t.Errorf("body = %q, want the stored bytes %q", got, tc.want)
			}
		})
	}
}

// AC5 edge (contract): a valid but absent photo is a 404, not an error page
// with a 200.
func TestING019_Edge_AbsentPhotoIs404(t *testing.T) {
	engine, _, _ := newING019API(t)

	rr := ing019Do(t, engine, http.MethodGet, "/api/photos/missing.jpg")
	if rr.Code != http.StatusNotFound {
		t.Errorf("GET /api/photos/missing.jpg status = %d, want 404", rr.Code)
	}
}

// AC6: Given a photo request whose filename is not a plain basename inside
// photosDir (contains "..", a path separator, or is absolute) / When it is
// requested / Then it is rejected without reading or serving any file
// outside photosDir.
func TestING019_AC6_RejectsTraversalFilenames(t *testing.T) {
	engine, _, photosDir := newING019API(t)

	// A canary one level above photosDir. Any traversal that escaped would
	// try to return this file's bytes.
	canary := filepath.Join(filepath.Dir(photosDir), "secret.txt")
	if err := os.WriteFile(canary, []byte("ING019-TOP-SECRET-CANARY"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	cases := []struct {
		name     string
		target   string
		wantCode int // 0 means "any non-200 rejection is acceptable"
	}{
		{"bare_dotdot", "/api/photos/..", http.StatusBadRequest},
		{"bare_dot", "/api/photos/.", http.StatusBadRequest},
		{"embedded_dotdot", "/api/photos/foo..bar", http.StatusBadRequest},
		{"encoded_backslash", "/api/photos/..%5Csecret.txt", http.StatusBadRequest},
		{"encoded_dotdot_slash", "/api/photos/..%2Fsecret.txt", 0},
		{"encoded_slash_prefix", "/api/photos/%2Fetc%2Fpasswd", 0},
		{"encoded_slash_middle", "/api/photos/a%2Fb", 0},
		{"dot_segments", "/api/photos/../../etc/passwd", 0},
		{"double_slash", "/api/photos//etc/passwd", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := ing019Do(t, engine, http.MethodGet, tc.target)
			if tc.wantCode != 0 {
				if rr.Code != tc.wantCode {
					t.Errorf("GET %s status = %d, want %d", tc.target, rr.Code, tc.wantCode)
				}
			} else if rr.Code == http.StatusOK {
				t.Errorf("GET %s status = 200, want the request rejected", tc.target)
			}
			if strings.Contains(rr.Body.String(), "ING019-TOP-SECRET-CANARY") {
				t.Errorf("GET %s leaked the canary outside photosDir:\n%s", tc.target, rr.Body)
			}
		})
	}
}

// AC7: Given the server is running / When its registered routes are
// inspected / Then exactly the approved route set exists (07-architecture.md
// "Backend" + "Catalog write API"): the read routes GET /api/items,
// GET /api/items/:id, GET /api/photos/:filename, GET /api/taxonomy, plus
// the single approved write route PUT /api/items/:id. No delete route and
// no unapproved method exists — any extra or missing route fails here.
// (Assertion converted from the read-only set by ING-030's test note;
// write-route tickets extend the set only when their route is approved.)
func TestING019_AC7_OnlyReadOnlyRoutes(t *testing.T) {
	engine, _, _ := newING019API(t)

	routes := engine.Routes()
	if len(routes) != 5 {
		t.Fatalf("registered routes = %d, want exactly 5: %v", len(routes), ing019RouteNames(routes))
	}
	want := map[string]bool{
		"GET /api/items":            false,
		"GET /api/items/:id":        false,
		"PUT /api/items/:id":        false,
		"GET /api/photos/:filename": false,
		"GET /api/taxonomy":         false,
	}
	for _, r := range routes {
		if r.Method == http.MethodDelete || r.Method == http.MethodPatch {
			t.Errorf("route %s %s uses a method outside the approved set (no delete in Phase 1)", r.Method, r.Path)
		}
		key := r.Method + " " + r.Path
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected route %q; want only %v", key, ing019Keys(want))
		} else {
			want[key] = true
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("expected route %q is not registered", key)
		}
	}
}

// Edge (Coder assumption, contract-adjacent): an empty photo_path must not
// produce the bogus URL /api/photos/. rather than an empty photo_url.
func TestING019_Edge_EmptyPhotoPathYieldsEmptyURL(t *testing.T) {
	engine, st, _ := newING019API(t)

	orphan := sampleItem("0f8c2b1e-1003-4a01-8103-000000001003")
	orphan.PhotoPath = ""
	if err := st.Insert(orphan); err != nil {
		t.Fatalf("Insert(%s) = %v, want nil", orphan.ID, err)
	}

	rr := ing019Do(t, engine, http.MethodGet, "/api/items")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/items status = %d, want 200", rr.Code)
	}
	var items []ing019Item
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].PhotoURL != "" {
		t.Errorf("photo_url = %q for empty photo_path, want \"\"", items[0].PhotoURL)
	}
}

func ing019Keys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ing019RouteNames formats gin routes for failure messages.
func ing019RouteNames(routes gin.RoutesInfo) []string {
	names := make([]string, 0, len(routes))
	for _, r := range routes {
		names = append(names, r.Method+" "+r.Path)
	}
	return names
}
