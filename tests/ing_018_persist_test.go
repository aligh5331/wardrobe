//go:build integration

// Process-level acceptance tests for ING-018: the `cmd/ingest` CLI persists
// a non-flagged Processor.Outcome as one internal/store catalog row, copies
// the tagged photo into data/photos/, and leaves flagged outcomes untouched.
//
// The CLI fixes its DB/photo/log paths relative to the working directory
// (store.DefaultDBPath is "data/wardrobe.db", photos are "data/photos/",
// the attempt log is "logs/vlm-attempts.jsonl"). These tests therefore build
// the real cmd/ingest binary and run it with its working directory set to an
// isolated temp root, so the run writes under that root instead of the
// repo's real data/ (which holds personal wardrobe data).
package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"wardrobe/internal/store"
)

// ing018Harness is an isolated project root plus the compiled CLI that runs
// with that root as its working directory.
type ing018Harness struct {
	root string
	bin  string
}

func newING018Harness(t *testing.T) *ing018Harness {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "ingest")
	build := exec.Command("go", "build", "-o", bin, "./cmd/ingest")
	build.Dir = moduleRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/ingest: %v\n%s", err, out)
	}
	return &ing018Harness{root: t.TempDir(), bin: bin}
}

// run executes the CLI from the harness root (its working directory), so all
// relative paths resolve under the temp root.
func (h *ing018Harness) run(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	cmd := exec.Command(h.bin, args...)
	cmd.Dir = h.root
	cmd.Env = envWith(env)

	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
		t.Fatalf("run ingest: %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("ingest process produced no exit state")
	}
	return out.String(), errBuf.String(), cmd.ProcessState.ExitCode()
}

func (h *ing018Harness) dbPath() string {
	return filepath.Join(h.root, "data", "wardrobe.db")
}

func (h *ing018Harness) photosDir() string {
	return filepath.Join(h.root, "data", "photos")
}

func (h *ing018Harness) logPath() string {
	return filepath.Join(h.root, "logs", "vlm-attempts.jsonl")
}

func (h *ing018Harness) copyPath(p string) string {
	return filepath.Join(h.root, p)
}

// list reads the catalog rows the CLI wrote, through the real store package.
func (h *ing018Harness) list(t *testing.T) []store.Item {
	t.Helper()

	s, err := store.Open(h.dbPath())
	if err != nil {
		t.Fatalf("open store at %s: %v", h.dbPath(), err)
	}
	defer s.Close()

	items, err := s.List()
	if err != nil {
		t.Fatalf("list store: %v", err)
	}
	return items
}

// ing018ParseStdout parses the CLI's one-JSON-object-per-line stdout. It
// fails the test if any non-empty line is not a JSON object: the CLI must
// keep stdout JSON-only (ING-012), which matters now that the persist path
// also opens the store. It returns the parsed records so callers can keep
// checking the rest of the behavior.
func ing018ParseStdout(t *testing.T, stdout string) []map[string]any {
	t.Helper()

	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("stdout line is not a JSON object; the CLI must emit only JSON records on stdout (ING-012):\n  %s", line)
			continue
		}
		records = append(records, obj)
	}
	return records
}

// ing018AssertStoreRowFields compares every persisted field to the tagging
// result the CLI emitted, so "the row's seven tagging fields equal
// Outcome.Result" is checked against real output rather than a hand copy.
func ing018AssertStoreRowFields(t *testing.T, row store.Item, wantTagging map[string]any) {
	t.Helper()

	if got := wantTagging["category"]; row.Category != got {
		t.Errorf("row.Category = %q, want %v", row.Category, got)
	}
	if got := wantTagging["subcategory"]; row.Subcategory != got {
		t.Errorf("row.Subcategory = %q, want %v", row.Subcategory, got)
	}
	if got := wantTagging["dominant_color"]; row.DominantColor != got {
		t.Errorf("row.DominantColor = %q, want %v", row.DominantColor, got)
	}
	if got := wantTagging["pattern"]; row.Pattern != got {
		t.Errorf("row.Pattern = %q, want %v", row.Pattern, got)
	}
	if got := wantTagging["warmth_tier"]; row.WarmthTier != got {
		t.Errorf("row.WarmthTier = %q, want %v", row.WarmthTier, got)
	}
	if got := wantTagging["formality"]; row.Formality != got {
		t.Errorf("row.Formality = %q, want %v", row.Formality, got)
	}

	wantColors, _ := wantTagging["secondary_colors"].([]any)
	gotColors := make([]any, len(row.SecondaryColors))
	for i, c := range row.SecondaryColors {
		gotColors[i] = c
	}
	if len(wantColors) != len(gotColors) {
		t.Fatalf("row.SecondaryColors = %v (%d), want %v (%d)",
			row.SecondaryColors, len(gotColors), wantColors, len(wantColors))
	}
	for i := range wantColors {
		if wantColors[i] != gotColors[i] {
			t.Errorf("row.SecondaryColors[%d] = %v, want %v", i, gotColors[i], wantColors[i])
		}
	}
}

// AC1: Given a photo whose tagging outcome is not flagged / When cmd/ingest
// processes it / Then it writes exactly one catalog row whose id is
// Outcome.ItemID and whose seven tagging fields equal Outcome.Result, with
// added_date the cataloging date and notes empty / And the tagged photo is
// copied into data/photos/ and the row's photo_path points at that copy, not
// the original source.
func TestING018_AC1_NonFlaggedOutcomePersistsRowAndPhotoCopy(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	src := []byte("ING018-AC1-SOURCE-BYTES")
	photo := writePhoto(t, "garment.jpg", src)

	h := newING018Harness(t)
	srv, vlm := newING012VLM(t, func(int, ing012Request) string { return valid })

	before := time.Now()
	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	after := time.Now()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
	}

	out := ing018ParseStdout(t, stdout)
	if len(out) != 1 {
		t.Fatalf("emitted records = %d, want 1; stdout:\n%s", len(out), stdout)
	}
	rec := out[0]
	itemID, _ := rec["item_id"].(string)
	if !ing012UUIDv4.MatchString(itemID) {
		t.Fatalf("item_id = %q, want UUIDv4", itemID)
	}
	if flagged, _ := rec["flagged"].(bool); flagged {
		t.Fatal("flagged = true, want false for a valid response")
	}
	if n := vlm.count(); n != 1 {
		t.Errorf("VLM requests = %d, want 1", n)
	}
	ing012Validate(t, rec)

	items := h.list(t)
	if len(items) != 1 {
		t.Fatalf("catalog rows = %d, want exactly 1", len(items))
	}
	row := items[0]

	if row.ID != itemID {
		t.Errorf("row.ID = %q, want Outcome.ItemID %q (no new uuid)", row.ID, itemID)
	}
	ing018AssertStoreRowFields(t, row, rec)
	if row.AddedDate.Before(before) || row.AddedDate.After(after) {
		t.Errorf("row.AddedDate = %s, want the cataloging instant within [%s, %s]",
			row.AddedDate, before, after)
	}
	if row.Notes != "" {
		t.Errorf("row.Notes = %q, want empty", row.Notes)
	}

	wantCopy := filepath.Join("data", "photos", itemID+".jpg")
	if row.PhotoPath != wantCopy {
		t.Errorf("row.PhotoPath = %q, want the stored copy %q", row.PhotoPath, wantCopy)
	}
	if row.PhotoPath == photo {
		t.Errorf("row.PhotoPath = the original source path %q; must be the copy", photo)
	}

	gotCopy, err := os.ReadFile(h.copyPath(row.PhotoPath))
	if err != nil {
		t.Fatalf("read stored copy %s: %v", row.PhotoPath, err)
	}
	if !bytes.Equal(gotCopy, src) {
		t.Errorf("stored copy bytes = %q, want the source bytes %q", gotCopy, src)
	}

	entries, err := os.ReadDir(h.photosDir())
	if err != nil {
		t.Fatalf("read data/photos: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("data/photos has %d entries, want exactly the one copy", len(entries))
	}
}

// AC2: Given a photo whose tagging outcome is flagged / When cmd/ingest
// processes it / Then no catalog row is written, nothing is copied into
// data/photos/, and its reporting (stdout JSON + vlm-attempts.jsonl trail)
// is unchanged.
func TestING018_AC2_FlaggedOutcomeWritesNoRowOrPhoto(t *testing.T) {
	photo := writePhoto(t, "garment.jpg", []byte("ING018-AC2-FLAGGED"))

	h := newING018Harness(t)
	srv, vlm := newING012VLM(t, func(int, ing012Request) string { return "{not valid json" })

	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a flag is not a hard error); stderr:\n%s", code, stderr)
	}
	if n := vlm.count(); n != 2 {
		t.Errorf("VLM requests = %d, want 2 (retry once then flag)", n)
	}

	out := ing018ParseStdout(t, stdout)
	if len(out) != 1 {
		t.Fatalf("emitted records = %d, want 1; stdout:\n%s", len(out), stdout)
	}
	rec := out[0]
	if flagged, _ := rec["flagged"].(bool); !flagged {
		t.Fatal("flagged = false, want true")
	}
	if rec["photo_path"] != photo {
		t.Errorf("flagged stdout photo_path = %v, want the original %q", rec["photo_path"], photo)
	}
	itemID, _ := rec["item_id"].(string)

	if items := h.list(t); len(items) != 0 {
		t.Errorf("catalog rows = %d, want 0 for a flagged outcome", len(items))
	}

	if _, err := os.Stat(h.photosDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("data/photos exists (stat err=%v) after a flagged-only run; nothing should be copied", err)
	}

	raw, err := os.ReadFile(h.logPath())
	if err != nil {
		t.Fatalf("read attempt log %s: %v", h.logPath(), err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("attempt log lines = %d, want 2 for a flagged photo:\n%s", len(lines), raw)
	}
	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("attempt log line %d is not JSON: %v\n%s", i+1, err, line)
		}
		if entry["item_id"] != itemID {
			t.Errorf("attempt log line %d item_id = %v, want the emitted %q", i+1, entry["item_id"], itemID)
		}
	}
}

// AC3: Given a successful ingest / When it completes / Then the original
// source photo is untouched and the existing ING-012 stdout record is emitted
// unchanged.
func TestING018_AC3_SourcePhotoUntouchedAndStdoutPreserved(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)
	src := []byte("ING018-AC3-ORIGINAL-SOURCE")
	photo := writePhoto(t, "shirt.png", src)

	before, err := os.Stat(photo)
	if err != nil {
		t.Fatalf("stat source photo: %v", err)
	}

	h := newING018Harness(t)
	srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })

	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
	}

	out := ing018ParseStdout(t, stdout)
	if len(out) != 1 {
		t.Fatalf("emitted records = %d, want 1", len(out))
	}
	rec := out[0]

	// ING-012 record shape: the seven tagging fields + item_id + original
	// photo_path + flagged, no more and no less.
	wantKeys := []string{
		"item_id", "photo_path", "category", "subcategory", "dominant_color",
		"secondary_colors", "pattern", "warmth_tier", "formality", "flagged",
	}
	if len(rec) != len(wantKeys) {
		t.Errorf("stdout record keys = %v, want exactly %v", ing018Keys(rec), wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := rec[k]; !ok {
			t.Errorf("stdout record missing key %q", k)
		}
	}
	if rec["photo_path"] != photo {
		t.Errorf("stdout photo_path = %v, want the original ingestion path %q", rec["photo_path"], photo)
	}
	if got := ing012Validate(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("emitted tagging fields = %+v, want %+v", got, want)
	}

	// Source photo bytes and mtime unchanged (only ever read).
	gotSrc, err := os.ReadFile(photo)
	if err != nil {
		t.Fatalf("re-read source photo: %v", err)
	}
	if !bytes.Equal(gotSrc, src) {
		t.Errorf("source photo bytes changed: %q -> %q", src, gotSrc)
	}
	after, err := os.Stat(photo)
	if err != nil {
		t.Fatalf("re-stat source photo: %v", err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("source photo stat changed: %+v -> %+v", before, after)
	}

	// The persisted row points at the copy, not the source.
	items := h.list(t)
	if len(items) != 1 {
		t.Fatalf("catalog rows = %d, want 1", len(items))
	}
	if items[0].PhotoPath == photo {
		t.Errorf("row.PhotoPath = source path %q, want the data/photos copy", photo)
	}
}

// AC4: Given the CLI has already ingested a photo and is run again on a new
// one / When it completes / Then the new row is added without overwriting or
// mutating previously written rows or stored photos.
func TestING018_AC4_SecondRunAddsWithoutMutatingFirst(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	photoA := writePhoto(t, "first.jpg", []byte("ING018-AC4-FIRST"))
	photoB := writePhoto(t, "second.jpg", []byte("ING018-AC4-SECOND"))

	h := newING018Harness(t)
	srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })
	env := ing012Env(map[string]string{"VLM_URL": srv.URL})

	if _, stderr, code := h.run(t, env, photoA); code != 0 {
		t.Fatalf("first run exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	first := h.list(t)
	if len(first) != 1 {
		t.Fatalf("rows after first run = %d, want 1", len(first))
	}
	rowA := first[0]
	copyA := h.copyPath(rowA.PhotoPath)
	copyABytes, err := os.ReadFile(copyA)
	if err != nil {
		t.Fatalf("read first stored copy: %v", err)
	}
	copyAInfo, err := os.Stat(copyA)
	if err != nil {
		t.Fatalf("stat first stored copy: %v", err)
	}

	if _, stderr, code := h.run(t, env, photoB); code != 0 {
		t.Fatalf("second run exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	second := h.list(t)
	if len(second) != 2 {
		t.Fatalf("rows after second run = %d, want 2", len(second))
	}

	var gotA, gotB *store.Item
	for i := range second {
		switch second[i].ID {
		case rowA.ID:
			gotA = &second[i]
		default:
			gotB = &second[i]
		}
	}
	if gotA == nil {
		t.Fatal("first row is missing after the second run")
	}
	if gotB == nil {
		t.Fatal("second run did not add a new row")
	}
	if !reflect.DeepEqual(*gotA, rowA) {
		t.Errorf("first row mutated by the second run:\n got %+v\nwant %+v", *gotA, rowA)
	}
	if gotB.PhotoPath == rowA.PhotoPath {
		t.Errorf("both rows share photo_path %q; the second run overwrote the first copy", gotB.PhotoPath)
	}

	gotCopyA, err := os.ReadFile(copyA)
	if err != nil {
		t.Fatalf("re-read first stored copy: %v", err)
	}
	if !bytes.Equal(gotCopyA, copyABytes) {
		t.Errorf("first stored photo was mutated: %q -> %q", copyABytes, gotCopyA)
	}
	afterInfo, err := os.Stat(copyA)
	if err != nil {
		t.Fatalf("re-stat first stored copy: %v", err)
	}
	if !afterInfo.ModTime().Equal(copyAInfo.ModTime()) {
		t.Errorf("first stored photo mtime changed: %s -> %s", copyAInfo.ModTime(), afterInfo.ModTime())
	}
}

// Edge (spec-implied): 04-data-schema.md allows an empty secondary_colors
// list, and the stored filename preserves the source extension from the
// ticket's copy-naming rule.
func TestING018_Edge_EmptySecondaryColorsAndExtensionPreserved(t *testing.T) {
	tax := loadTaxonomy(t)
	payload := taggingJSON(t, tax, map[string]any{"secondary_colors": []string{}})
	photo := writePhoto(t, "jacket.PNG", []byte("ING018-EDGE"))

	h := newING018Harness(t)
	srv, _ := newING012VLM(t, func(int, ing012Request) string { return payload })

	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
	}
	out := ing018ParseStdout(t, stdout)
	if len(out) != 1 {
		t.Fatalf("emitted records = %d, want 1", len(out))
	}
	itemID, _ := out[0]["item_id"].(string)

	items := h.list(t)
	if len(items) != 1 {
		t.Fatalf("catalog rows = %d, want 1", len(items))
	}
	if got := items[0].SecondaryColors; len(got) != 0 {
		t.Errorf("row.SecondaryColors = %v, want an empty list", got)
	}
	wantCopy := filepath.Join("data", "photos", itemID+".PNG")
	if items[0].PhotoPath != wantCopy {
		t.Errorf("row.PhotoPath = %q, want the source extension preserved as %q", items[0].PhotoPath, wantCopy)
	}
}

// Edge (ticket context): a failed copy/open/insert must surface as an error
// in the existing CLI style rather than silently producing a half-written
// row. data/photos/ is pre-occupied by a regular file so persist's MkdirAll
// fails; the success-path stdout JSON must still be the only stdout output,
// and the process must exit non-zero with the failure named on stderr.
func TestING018_Edge_PersistFailureSurfacesNonZero(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	photo := writePhoto(t, "garment.jpg", []byte("ING018-EDGE-FAIL"))

	h := newING018Harness(t)
	if err := os.MkdirAll(filepath.Join(h.root, "data"), 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.WriteFile(h.photosDir(), []byte("occupies the photos path"), 0o644); err != nil {
		t.Fatalf("occupy data/photos with a file: %v", err)
	}

	srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })

	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code == 0 {
		t.Errorf("exit code = 0, want non-zero: a failed persist must surface, not be silent")
	}
	if out := ing018ParseStdout(t, stdout); len(out) != 1 {
		t.Errorf("emitted records = %d, want 1 (the stdout record precedes persist); stdout:\n%s", len(out), stdout)
	}
	if !strings.Contains(stderr, "persist") {
		t.Errorf("stderr does not name the persist failure:\n%s", stderr)
	}
}

func ing018Keys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
