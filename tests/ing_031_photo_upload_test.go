// Package tests holds black-box acceptance tests for backlog tickets.
// ING-031: POST /api/items/photo accepts one multipart garment photo
// (field "photo"), rejects any extension outside .jpg/.jpeg/.png/.webp and
// anything over 20 MiB without running the VLM or staging anything, stages
// the accepted upload server-named <item_id>.<ext> under
// data/ingest-staging/ (never the client filename as a path), and runs the
// existing internal/tagging pipeline (retry-once policy included) to return
// a NON-persisted draft: the seven 04-data-schema.md tagging fields plus the
// generated item id and a photo reference the client posts back to persist.
// No catalog row is written and no file reaches data/photos/. A
// validation failure after retry is a non-2xx and removes the staged file;
// an unreachable VLM surfaces without a retry. The route-surface AC is
// asserted by TestING019_AC7_OnlyReadOnlyRoutes in ing_019_api_test.go.
//
// These tests exercise the exported wardrobe/internal/api surface against a
// stub VLM; implementation code is never touched from here. Temp roots
// isolate the store, staging dir, and photos dir, so they never touch the
// repo's real data/.
package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

// ing031DraftKeys is the exact top-level key set the draft contract fixes:
// the seven tagging fields, the generated item id, and the photo reference.
var ing031DraftKeys = []string{
	"item_id", "photo_ref",
	"category", "subcategory", "dominant_color", "secondary_colors",
	"pattern", "warmth_tier", "formality",
}

var ing031UUIDv4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ing031Draft mirrors the POST /api/items/photo success body.
type ing031Draft struct {
	ItemID          string   `json:"item_id"`
	PhotoRef        string   `json:"photo_ref"`
	Category        string   `json:"category"`
	Subcategory     string   `json:"subcategory"`
	DominantColor   string   `json:"dominant_color"`
	SecondaryColors []string `json:"secondary_colors"`
	Pattern         string   `json:"pattern"`
	WarmthTier      string   `json:"warmth_tier"`
	Formality       string   `json:"formality"`
}

// ing031Harness wires the real engine to a stub VLM and isolated temp roots.
type ing031Harness struct {
	engine     *gin.Engine
	st         *store.Store
	stagingDir string
	photosDir  string
	capture    *ing005Capture
	tax        taxonomy
}

// ing031Processor builds the same retry-once processor cmd/server does,
// logging attempts in memory so no logs/ file is written.
func ing031Processor(t *testing.T, srv *httptest.Server) *tagging.Processor {
	t.Helper()
	client := tagging.NewClient(srv.URL, "",
		tagging.WithTemperature(func() float64 { return 0.4 }))
	return tagging.NewProcessor(client, tagging.WithAttemptLogger(&ing005Logger{}))
}

// newING031HarnessFor builds the engine around an already-built processor.
func newING031HarnessFor(t *testing.T, p *tagging.Processor) *ing031Harness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	stagingDir := filepath.Join(t.TempDir(), "ingest-staging")
	photosDir := filepath.Join(t.TempDir(), "photos")
	return &ing031Harness{
		engine:     api.New(st, photosDir, api.WithTagging(p, stagingDir)),
		st:         st,
		stagingDir: stagingDir,
		photosDir:  photosDir,
		tax:        loadTaxonomy(t),
	}
}

// newING031Harness starts a stub VLM whose respond returns the 1-based
// request number's model text, and wires it into the engine.
func newING031Harness(t *testing.T, respond func(n int, req ing005Request) string) *ing031Harness {
	t.Helper()
	srv, capture := newING005VLM(t, respond)
	h := newING031HarnessFor(t, ing031Processor(t, srv))
	h.capture = capture
	return h
}

// ing031Upload posts a multipart body with one file part field/filename.
func ing031Upload(t *testing.T, engine http.Handler, field, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/items/photo", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)
	return rr
}

// ing031Files lists a directory's entry names, treating an absent directory
// as empty (a rejection that stages nothing may never create it).
func ing031Files(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// AC1: Given a multipart upload of one accepted-extension photo / When the
// client POSTs /api/items/photo / Then the backend runs the local tagging
// pipeline and returns a non-persisted draft: the seven tagging fields plus
// the generated item id and a photo reference, with no catalog row and no
// file in data/photos/.
func TestING031_AC1_ReturnsNonPersistedDraft(t *testing.T) {
	valid, want := validTaggingPayload(t, loadTaxonomy(t))
	h := newING031Harness(t, func(int, ing005Request) string { return valid })

	photo := []byte("ING031-JPEG-BYTES")
	rr := ing031Upload(t, h.engine, "photo", "shirt.jpg", photo)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/items/photo status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// Exact contract key set: seven tagging fields + id + reference.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("draft body is not a JSON object: %v\n%s", err, rr.Body)
	}
	if len(raw) != len(ing031DraftKeys) {
		t.Errorf("draft keys = %v, want exactly %v", ing019Keys(raw), ing031DraftKeys)
	}
	for _, k := range ing031DraftKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("draft missing contract key %q; keys = %v", k, ing019Keys(raw))
		}
	}

	var draft ing031Draft
	if err := json.Unmarshal(rr.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}

	// Generated item id (uuid) and the seven validated fields.
	if !ing031UUIDv4.MatchString(draft.ItemID) {
		t.Errorf("item_id = %q, want a UUIDv4", draft.ItemID)
	}
	if draft.Category != want.Category || draft.Subcategory != want.Subcategory ||
		draft.DominantColor != want.DominantColor || draft.Pattern != want.Pattern ||
		draft.WarmthTier != want.WarmthTier || draft.Formality != want.Formality ||
		len(draft.SecondaryColors) != len(want.SecondaryColors) {
		t.Errorf("draft tagging fields = %+v, want %+v", draft, want)
	}
	for i := range want.SecondaryColors {
		if draft.SecondaryColors[i] != want.SecondaryColors[i] {
			t.Errorf("secondary_colors[%d] = %q, want %q", i, draft.SecondaryColors[i], want.SecondaryColors[i])
		}
	}

	// The reference is the staged file's basename, named after the item id,
	// and the staged bytes are the uploaded photo.
	if draft.PhotoRef != draft.ItemID+".jpg" {
		t.Errorf("photo_ref = %q, want %q", draft.PhotoRef, draft.ItemID+".jpg")
	}
	staged := filepath.Join(h.stagingDir, draft.PhotoRef)
	onDisk, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("read staged file %s: %v", staged, err)
	}
	if !bytes.Equal(onDisk, photo) {
		t.Errorf("staged bytes = %q, want the uploaded %q", onDisk, photo)
	}

	// Not persisted: no catalog row, no file in data/photos/.
	items, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(items) != 0 {
		t.Errorf("catalog rows = %d, want 0 for a non-persisted draft", len(items))
	}
	if files := ing031Files(t, h.photosDir); len(files) != 0 {
		t.Errorf("photos dir = %v, want no file", files)
	}

	// A validating response is used as-is: exactly one VLM call.
	if got := len(h.capture.all()); got != 1 {
		t.Errorf("VLM requests = %d, want 1", got)
	}
}

// AC2: Given a valid uploaded photo / When the draft is produced / Then the
// staged file is server-named <item_id>.<ext> under the staging directory,
// and the client-supplied filename is never used as a path — a traversal
// filename cannot escape.
func TestING031_AC2_ServerNamedStagingTraversalSafe(t *testing.T) {
	valid, _ := validTaggingPayload(t, loadTaxonomy(t))

	cases := []struct {
		name     string
		filename string
	}{
		{"plain", "photo.jpg"},
		{"traversal", "../../../../etc/passwd.jpg"},
		{"subdir", "nested/dir/garment.png"},
		{"backslash", `..\..\evil.jpg`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newING031Harness(t, func(int, ing005Request) string { return valid })

			// A canary one level above the staging dir: a naive join of the
			// client path could overwrite it. It must stay untouched.
			canary := filepath.Join(filepath.Dir(h.stagingDir), "passwd.jpg")
			canaryBytes := []byte("ING031-CANARY")
			if err := os.WriteFile(canary, canaryBytes, 0o644); err != nil {
				t.Fatalf("write canary: %v", err)
			}

			rr := ing031Upload(t, h.engine, "photo", tc.filename, []byte("ING031-UPLOAD"))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body:\n%s", rr.Code, rr.Body)
			}
			var draft ing031Draft
			if err := json.Unmarshal(rr.Body.Bytes(), &draft); err != nil {
				t.Fatalf("decode draft: %v", err)
			}

			if strings.ContainsAny(draft.PhotoRef, `/\`) {
				t.Errorf("photo_ref = %q, want a plain basename with no separators", draft.PhotoRef)
			}
			wantName := draft.ItemID + filepath.Ext(tc.filename)
			if draft.PhotoRef != wantName {
				t.Errorf("photo_ref = %q, want the server-named %q", draft.PhotoRef, wantName)
			}
			if draft.PhotoRef != wantName ||
				strings.Contains(draft.PhotoRef, "passwd") ||
				strings.Contains(draft.PhotoRef, "garment") ||
				strings.Contains(draft.PhotoRef, "evil") {
				t.Errorf("photo_ref = %q still carries the client filename", draft.PhotoRef)
			}

			// Exactly one staged file, inside the staging dir.
			files := ing031Files(t, h.stagingDir)
			if len(files) != 1 || files[0] != draft.PhotoRef {
				t.Errorf("staging dir = %v, want exactly [%s]", files, draft.PhotoRef)
			}

			// The canary was not overwritten by the upload.
			if got, err := os.ReadFile(canary); err != nil || !bytes.Equal(got, canaryBytes) {
				t.Errorf("canary outside staging dir changed: got %q err=%v, want %q", got, err, canaryBytes)
			}
		})
	}
}

// AC3: Given an upload whose extension is not .jpg/.jpeg/.png/.webp / When
// the client POSTs /api/items/photo / Then it receives 400, no VLM call is
// made, and nothing is staged.
func TestING031_AC3_RejectsBadExtensionWithoutTagging(t *testing.T) {
	h := newING031Harness(t, func(int, ing005Request) string {
		t.Error("VLM was called for a rejected extension")
		return ""
	})

	cases := []string{"notes.txt", "archive.zip", "image.gif", "photo.bmp", "noextension", "photo."}
	for _, filename := range cases {
		t.Run(filename, func(t *testing.T) {
			rr := ing031Upload(t, h.engine, "photo", filename, []byte("ING031"))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body:\n%s", rr.Code, rr.Body)
			}
			if files := ing031Files(t, h.stagingDir); len(files) != 0 {
				t.Errorf("staging dir = %v, want nothing staged", files)
			}
			if items, _ := h.st.List(); len(items) != 0 {
				t.Errorf("catalog rows = %d, want 0", len(items))
			}
		})
	}

	if got := len(h.capture.all()); got != 0 {
		t.Errorf("VLM requests = %d, want 0 for rejected extensions", got)
	}
}

// AC4: Given an upload larger than 20 MiB / When the client POSTs
// /api/items/photo / Then it is rejected with a 4xx, no VLM call is made,
// and nothing is staged.
func TestING031_AC4_RejectsOversizeWithoutTagging(t *testing.T) {
	h := newING031Harness(t, func(int, ing005Request) string {
		t.Error("VLM was called for an oversize upload")
		return ""
	})

	oversize := bytes.Repeat([]byte("x"), (20<<20)+1)
	rr := ing031Upload(t, h.engine, "photo", "big.jpg", oversize)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Fatalf("status = %d, want 4xx; body:\n%s", rr.Code, rr.Body)
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want nothing staged", files)
	}
	if items, _ := h.st.List(); len(items) != 0 {
		t.Errorf("catalog rows = %d, want 0", len(items))
	}
	if got := len(h.capture.all()); got != 0 {
		t.Errorf("VLM requests = %d, want 0 for an oversize upload", got)
	}
}

// AC5: Given a model response that fails validation even after the
// retry-once policy / When the client POSTs /api/items/photo / Then the
// failure is a non-2xx, nothing is persisted, and the staged upload is
// removed.
func TestING031_AC5_ValidationFailureAfterRetryRemovesStaged(t *testing.T) {
	h := newING031Harness(t, func(int, ing005Request) string { return ing005Malformed })

	rr := ing031Upload(t, h.engine, "photo", "shirt.jpg", []byte("ING031-JPEG-BYTES"))
	if rr.Code < 400 {
		t.Fatalf("status = %d, want a non-2xx error; body:\n%s", rr.Code, rr.Body)
	}

	if got := len(h.capture.all()); got != 2 {
		t.Errorf("VLM requests = %d, want exactly 2 (retry once)", got)
	}
	if items, _ := h.st.List(); len(items) != 0 {
		t.Errorf("catalog rows = %d, want 0", len(items))
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want the failed upload removed", files)
	}
}

// AC6: Given the VLM is unreachable (tagging.ErrVLMUnreachable) / When the
// client POSTs /api/items/photo / Then the error surfaces without a retry
// and no row is written.
func TestING031_AC6_UnreachableSurfacesWithoutRetry(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		panic(http.ErrAbortHandler) // drop the connection mid-request
	}))
	t.Cleanup(srv.Close)

	h := newING031HarnessFor(t, ing031Processor(t, srv))

	rr := ing031Upload(t, h.engine, "photo", "shirt.jpg", []byte("ING031-JPEG-BYTES"))
	if rr.Code < 400 {
		t.Fatalf("status = %d, want a non-2xx error; body:\n%s", rr.Code, rr.Body)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("VLM requests = %d, want exactly 1 (unreachable must not be retried)", got)
	}
	if items, _ := h.st.List(); len(items) != 0 {
		t.Errorf("catalog rows = %d, want 0", len(items))
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want no residue", files)
	}
}

// Edge (trust boundary): an upload so large the request body itself exceeds
// the MaxBytesReader envelope (20 MiB + 1 MiB slack) is rejected before any
// full read, with nothing staged and no VLM call. This reaches the
// body-bound branch the AC4 20 MiB+1 case does not: that one fits inside the
// envelope and is caught by the explicit FileHeader.Size check instead.
func TestING031_Edge_BodyOverEnvelopeRejected(t *testing.T) {
	h := newING031Harness(t, func(int, ing005Request) string {
		t.Error("VLM was called for an oversize request body")
		return ""
	})

	payload := bytes.Repeat([]byte("x"), 25<<20) // > 20 MiB + 1 MiB slack
	rr := ing031Upload(t, h.engine, "photo", "big.jpg", payload)
	t.Logf("body-over-envelope status = %d", rr.Code)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Fatalf("status = %d, want 4xx; body:\n%s", rr.Code, rr.Body)
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want nothing staged", files)
	}
	if items, _ := h.st.List(); len(items) != 0 {
		t.Errorf("catalog rows = %d, want 0", len(items))
	}
	if files := ing031Files(t, h.photosDir); len(files) != 0 {
		t.Errorf("photos dir = %v, want no file", files)
	}
	if got := len(h.capture.all()); got != 0 {
		t.Errorf("VLM requests = %d, want 0 for an oversize request body", got)
	}
}

// Edge (trust boundary / contract): a request missing the "photo" part is a
// 400 with no VLM call and nothing staged, and cmd/server builds a tagging
// Processor from config like cmd/ingest (the wiring AC's static half).
func TestING031_Edge_StaticWiringAndMissingPart(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "cmd", "server", "main.go"))
	if err != nil {
		t.Fatalf("read cmd/server/main.go: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "tagging.NewProcessor(") {
		t.Errorf("cmd/server/main.go does not build a tagging Processor like cmd/ingest")
	}
	if !strings.Contains(s, "api.WithTagging(") {
		t.Errorf("cmd/server/main.go does not wire the processor into api.New")
	}

	h := newING031Harness(t, func(int, ing005Request) string {
		t.Error("VLM was called for a request with no photo part")
		return ""
	})
	rr := ing031Upload(t, h.engine, "file", "shirt.jpg", []byte("ING031"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing photo part status = %d, want 400; body:\n%s", rr.Code, rr.Body)
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want nothing staged", files)
	}
}
