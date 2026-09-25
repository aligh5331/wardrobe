// Package tests holds black-box acceptance tests for backlog tickets.
// ING-032 tester-added edge coverage: the out-of-scope rule that id,
// added_date, and photo_path cannot be supplied as changed values
// (04-data-schema.md "Write-path rules"; ticket "Out of scope"), and the
// "optional notes" / "0 or more secondary_colors" schema rules
// (04-data-schema.md).
//
// Independent of tests/ing_032_create_item_test.go; reuses its harness
// helpers and never touches implementation code or the repo's real data/.
package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Given a valid staged draft whose body also carries id, added_date,
// photo_path, and photo_url / When the client POSTs /api/items / Then the
// server-owned values win: the row and response use the request's item_id,
// a create-time added_date, and the server-derived data/photos path — the
// client fields cannot reach the store (ticket "Out of scope").
func TestING032_Tester_BodyCannotOverrideServerOwnedFields(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0320-4a01-8320-000000000009"
	ing032Stage(t, h, id, ".jpg", []byte("ING032-OWNER"))

	body := ing032ValidBody(t, h, id, id+".jpg")
	body["id"] = "attacker-id"
	body["added_date"] = "1999-01-01"
	body["photo_path"] = "/etc/passwd"
	body["photo_url"] = "/api/photos/attacker.jpg"

	rr := ing032Post(t, h.engine, body)
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("POST /api/items status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}

	got := ing032DecodeItem(t, rr.Body.Bytes())
	if got.ID != id {
		t.Errorf("response id = %q, want the request item_id %q", got.ID, id)
	}
	if got.AddedDate == "1999-01-01" || got.AddedDate == "" {
		t.Errorf("response added_date = %q, want the create-time date, not the body's value", got.AddedDate)
	}
	wantPath := filepath.ToSlash(filepath.Join(h.photosDir, id+".jpg"))
	if got.PhotoPath != wantPath {
		t.Errorf("response photo_path = %q, want the server-derived %q", got.PhotoPath, wantPath)
	}
	if got.PhotoURL != "/api/photos/"+id+".jpg" {
		t.Errorf("response photo_url = %q, want /api/photos/%s.jpg", got.PhotoURL, id)
	}

	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows = %d, want exactly 1", len(rows))
	}
	if rows[0].ID != id || rows[0].PhotoPath != wantPath {
		t.Errorf("row = {id:%q photo_path:%q}, want {id:%q photo_path:%q}", rows[0].ID, rows[0].PhotoPath, id, wantPath)
	}
}

// Given a valid staged draft whose body omits notes and sends an empty
// secondary_colors list (both allowed by 04-data-schema.md) / When it is
// POSTed / Then it persists: notes empty and a 0-length color list, with the
// photo moved and no staged residue.
func TestING032_Tester_OptionalNotesAndEmptySecondaryColorsPersist(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0320-4a01-8320-000000000010"
	ing032Stage(t, h, id, ".png", []byte("ING032-EMPTY-OPTIONALS"))

	body := ing032ValidBody(t, h, id, id+".png")
	delete(body, "notes")
	body["secondary_colors"] = []string{}

	rr := ing032Post(t, h.engine, body)
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("POST /api/items status = %d, want 2xx; body:\n%s", rr.Code, rr.Body)
	}

	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows = %d, want exactly 1", len(rows))
	}
	if rows[0].Notes != "" {
		t.Errorf("Notes = %q, want empty when notes is omitted", rows[0].Notes)
	}
	if len(rows[0].SecondaryColors) != 0 {
		t.Errorf("SecondaryColors = %v, want a 0-length list", rows[0].SecondaryColors)
	}

	if files := ing031Files(t, h.photosDir); len(files) != 1 || files[0] != id+".png" {
		t.Errorf("photos dir = %v, want exactly [%s]", files, id+".png")
	}
	if files := ing031Files(t, h.stagingDir); len(files) != 0 {
		t.Errorf("staging dir = %v, want empty after save", files)
	}
}

// Guard: the malformed-JSON path is a 400 and, consistent with the
// field-validation rule, leaves the staged upload intact (implementation
// note 2; the ticket's "staged kept on a validation rejection" family).
func TestING032_Tester_MalformedJSONKeepsStagedNoWrites(t *testing.T) {
	h := newING032Harness(t)
	id := "0f8c2b1e-0320-4a01-8320-000000000011"
	staged := ing032Stage(t, h, id, ".jpg", []byte("ING032-BAD-JSON"))

	req := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader(`{"item_id":`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.engine.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST malformed JSON status = %d, want 400; body:\n%s", rr.Code, rr.Body)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Errorf("staged upload removed on a malformed-JSON 400: stat err=%v", err)
	}
	rows, err := h.st.List()
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Errorf("catalog rows = %d, want 0 after a malformed-JSON 400", len(rows))
	}
}
