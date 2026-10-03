// ING-058: uploadPhoto and createItem set "item_id" on the gin context, so
// the ING-053 access log carries it on routes that have no :id parameter.
package tests

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"wardrobe/internal/api"
)

// The VLM is a closed port, so the handler fails with 502 after it has
// generated the item id. The staged file is removed on failure, so the id
// cannot be compared with a file stem; it is checked as a UUIDv4 instead.
func TestING058_UploadErrorLogCarriesItemID(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	h := newING031HarnessFor(t, ing031Processor(t, srv))

	buf := &bytes.Buffer{}
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := api.New(h.st, h.photosDir, api.WithTagging(ing031Processor(t, srv), h.stagingDir), api.WithLogger(l))

	rr := ing031Upload(t, e, "photo", "shirt.jpg", []byte("ING058-JPEG-BYTES"))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body:\n%s", rr.Code, rr.Body)
	}
	rec := ing053One(t, buf)
	if rec["msg"] != "http request" || rec["level"] != "ERROR" || rec["route"] != "/api/items/photo" {
		t.Errorf("record = %v, want an ERROR \"http request\" for /api/items/photo", rec)
	}
	if id := rr.Header().Get("X-Request-ID"); id == "" || rec["request_id"] != id {
		t.Errorf("request_id = %v, header = %q, want equal and non-empty", rec["request_id"], id)
	}
	if id, _ := rec["item_id"].(string); !ing031UUIDv4.MatchString(id) {
		t.Errorf("item_id = %v, want a generated UUIDv4", rec["item_id"])
	}
}

// A row with the same id already exists, so catalog.Create fails and the
// handler answers 500. The record must carry the body's item_id.
func TestING058_CreateErrorLogCarriesBodyItemID(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0580-4a01-8580-000000000001"
	if err := h.st.Insert(sampleItem(id)); err != nil {
		t.Fatalf("insert fixture row: %v", err)
	}
	ing032Stage(t, h, id, ".jpg", []byte("ING058-CREATE-FAIL"))

	buf := &bytes.Buffer{}
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := api.New(h.st, h.photosDir, api.WithTagging(nil, h.stagingDir), api.WithLogger(l))

	rr := ing032Post(t, e, ing032ValidBody(t, h, id, id+".jpg"))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body:\n%s", rr.Code, rr.Body)
	}
	rec := ing053One(t, buf)
	if rec["msg"] != "http request" || rec["level"] != "ERROR" || rec["route"] != "/api/items" {
		t.Errorf("record = %v, want an ERROR \"http request\" for /api/items", rec)
	}
	if rid := rr.Header().Get("X-Request-ID"); rid == "" || rec["request_id"] != rid {
		t.Errorf("request_id = %v, header = %q, want equal and non-empty", rec["request_id"], rid)
	}
	if rec["item_id"] != id {
		t.Errorf("item_id = %v, want %q", rec["item_id"], id)
	}
}

// No handler knows an item id here, so the record has request_id and no
// item_id key.
func TestING058_NoItemIDWhenUnknown(t *testing.T) {
	e, buf := ing053Engine(t, false)
	rr := ing053Do(e, http.MethodGet, "/api/weather", "", nil)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	rec := ing053One(t, buf)
	if rec["level"] != "ERROR" || rec["request_id"] == "" || rec["request_id"] == nil {
		t.Errorf("record = %v, want ERROR with a request_id", rec)
	}
	if _, ok := rec["item_id"]; ok {
		t.Errorf("item_id present without a known id: %v", rec)
	}
}
