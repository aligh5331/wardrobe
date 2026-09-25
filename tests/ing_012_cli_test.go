//go:build integration

// Process-level acceptance tests for ING-012: the `cmd/ingest` CLI wires
// config -> VLM client -> ING-005 processor and drives garment photos
// through it. Like the ING-001 integration tests these build the real
// binary and run it against a stub VLM, so they are build-tagged.
//
// The CLI fixes its DB/photo/log paths relative to its working directory
// (data/wardrobe.db, data/photos/, logs/vlm-attempts.jsonl), and post-
// ING-018 it persists every non-flagged outcome. Each test therefore runs
// the binary with its working directory set to an isolated t.TempDir()
// project root, so the run's data/ and logs/ land under that root instead
// of the repo's real data/ and logs/ (personal runtime data). The binary
// itself is still built from the module root.
package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wardrobe/internal/tagging"
)

// ing012Env starts from a fully neutral env contract (so a developer's
// .env / exported vars cannot leak into the child process) and applies
// overrides.
func ing012Env(overrides map[string]string) map[string]string {
	env := map[string]string{
		"VLM_URL":                "http://127.0.0.1:1",
		"VLM_API_KEY":            "",
		"VLM_SERIALIZE_REQUESTS": "false",
		"VLM_REQUEST_DELAY_MS":   "0",
		"VLM_TEMPERATURE":        "0.4",
		"LLM_URL":                "",
		"LLM_API_KEY":            "",
	}
	for k, v := range overrides {
		env[k] = v
	}
	return env
}

// ing012Harness is an isolated project root plus the compiled ingest
// binary that runs with that root as its working directory.
type ing012Harness struct {
	root string
	bin  string
}

// newING012Harness compiles cmd/ingest from the module root and returns a
// harness whose working directory is a fresh t.TempDir() project root, so
// the run's data/wardrobe.db, data/photos/, and logs/vlm-attempts.jsonl
// resolve under that root rather than the repo's real data/ and logs/.
func newING012Harness(t *testing.T) *ing012Harness {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "ingest")
	build := exec.Command("go", "build", "-o", bin, "./cmd/ingest")
	build.Dir = moduleRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/ingest: %v\n%s", err, out)
	}
	return &ing012Harness{root: t.TempDir(), bin: bin}
}

// run executes the documented invocation (`ingest <path...>`) with the
// given env contract and returns stdout, stderr, and the exit code.
func (h *ing012Harness) run(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
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

// ing012Request is one request the stub VLM received.
type ing012Request struct {
	temperature float64
	image       string
	at          time.Time
}

// ing012VLM is a stub VLM recording every request; respond is called
// with the 1-based request number and the parsed request.
type ing012VLM struct {
	mu      sync.Mutex
	reqs    []ing012Request
	respond func(n int, req ing012Request) string
}

func newING012VLM(t *testing.T, respond func(n int, req ing012Request) string) (*httptest.Server, *ing012VLM) {
	t.Helper()

	v := &ing012VLM{respond: respond}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}

		req := ing012Request{at: time.Now()}
		if temp, ok := body["temperature"].(float64); ok {
			req.temperature = temp
		}
		req.image = ing012Image(body)

		v.mu.Lock()
		v.reqs = append(v.reqs, req)
		n := len(v.reqs)
		v.mu.Unlock()

		_, _ = w.Write([]byte(rawEnvelope(v.respond(n, req))))
	}))
	t.Cleanup(srv.Close)
	return srv, v
}

func (v *ing012VLM) all() []ing012Request {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]ing012Request(nil), v.reqs...)
}

func (v *ing012VLM) count() int { return len(v.all()) }

// ing012Image pulls the data URI out of the OpenAI-style chat request.
func ing012Image(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	if len(msgs) < 2 {
		return ""
	}
	user, _ := msgs[1].(map[string]any)
	parts, _ := user["content"].([]any)
	for _, p := range parts {
		part, _ := p.(map[string]any)
		img, _ := part["image_url"].(map[string]any)
		if u, ok := img["url"].(string); ok {
			return u
		}
	}
	return ""
}

var ing012UUIDv4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ing012ParseOutput parses the CLI's one-JSON-object-per-line stdout.
func ing012ParseOutput(t *testing.T, stdout string) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("stdout line %q is not a JSON object: %v", line, err)
		}
		out = append(out, obj)
	}
	return out
}

// ing012Validate runs an emitted record's seven tagging fields back
// through the real validation entry point, so the output is checked
// against 03-taxonomy.md / 04-data-schema.md rather than a hand-copied
// enum list.
func ing012Validate(t *testing.T, obj map[string]any) tagging.TaggingResult {
	t.Helper()

	fields := map[string]any{}
	for _, k := range []string{
		"category", "subcategory", "dominant_color", "secondary_colors",
		"pattern", "warmth_tier", "formality",
	} {
		v, ok := obj[k]
		if !ok {
			t.Fatalf("emitted record is missing field %q: %v", k, obj)
		}
		fields[k] = v
	}
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("re-marshal emitted fields: %v", err)
	}
	res, err := tagging.ParseTaggingResult(string(b))
	if err != nil {
		t.Fatalf("emitted record does not validate against the taxonomy/schema: %v", err)
	}
	return res
}

// ing012DBInfo snapshots the isolated run's data/wardrobe.db, so callers
// read the run's own catalog store without touching the repo's real one.
type ing012DBInfo struct {
	exists bool
	size   int64
	mod    time.Time
}

func ing012DBState(t *testing.T, h *ing012Harness) ing012DBInfo {
	t.Helper()

	info, err := os.Stat(filepath.Join(h.root, "data", "wardrobe.db"))
	if errors.Is(err, os.ErrNotExist) {
		return ing012DBInfo{}
	}
	if err != nil {
		t.Fatalf("stat data/wardrobe.db: %v", err)
	}
	return ing012DBInfo{exists: true, size: info.Size(), mod: info.ModTime()}
}

// ing012LogPath is the attempt log under the isolated harness root.
func ing012LogPath(t *testing.T, h *ing012Harness) string {
	t.Helper()
	return filepath.Join(h.root, "logs", "vlm-attempts.jsonl")
}

// ing012LogSize is the pre-run byte offset of the isolated attempt log.
func ing012LogSize(t *testing.T, h *ing012Harness) int64 {
	t.Helper()

	info, err := os.Stat(ing012LogPath(t, h))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("stat attempt log: %v", err)
	}
	return info.Size()
}

// ing012AppendedLines returns the raw JSONL lines appended after offset.
func ing012AppendedLines(t *testing.T, h *ing012Harness, offset int64) []string {
	t.Helper()

	raw, err := os.ReadFile(ing012LogPath(t, h))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read attempt log: %v", err)
	}
	if int64(len(raw)) < offset {
		t.Fatalf("attempt log shrank below pre-run offset %d (len %d)", offset, len(raw))
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(string(raw[offset:]), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func ing012AppendedAttempts(t *testing.T, h *ing012Harness, offset int64) []tagging.AttemptLog {
	t.Helper()

	var entries []tagging.AttemptLog
	for _, line := range ing012AppendedLines(t, h, offset) {
		var e tagging.AttemptLog
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("appended log line %q is not an AttemptLog: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

func writeIng012Photo(t *testing.T, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write photo %s: %v", path, err)
	}
	return path
}

// AC1: Given a garment photo path and a reachable VLM configured from the
// environment / When the photo is passed through the ingestion pipeline /
// Then a JSON object is emitted with all seven validated tagging fields
// plus the original photo_path.
func TestING012_AC1_SinglePhotoEmitsValidatedJSON(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)
	photo := writePhoto(t, "garment.jpg", []byte("ING012-AC1-JPEG-BYTES"))

	h := newING012Harness(t)
	srv, vlm := newING012VLM(t, func(int, ing012Request) string { return valid })

	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
	}

	out := ing012ParseOutput(t, stdout)
	if len(out) != 1 {
		t.Fatalf("emitted records = %d, want 1; stdout:\n%s", len(out), stdout)
	}
	rec := out[0]

	if got := rec["photo_path"]; got != photo {
		t.Errorf("photo_path = %v, want the original photo path %q", got, photo)
	}
	if flagged, _ := rec["flagged"].(bool); flagged {
		t.Error("flagged = true, want false for a valid response")
	}
	if id, _ := rec["item_id"].(string); !ing012UUIDv4.MatchString(id) {
		t.Errorf("item_id = %q, want a UUIDv4 (ING-005 generates it)", id)
	}
	if got := ing012Validate(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("validated fields = %+v, want %+v", got, want)
	}
	if n := vlm.count(); n != 1 {
		t.Errorf("VLM requests = %d, want 1", n)
	}
	if strings.Contains(stderr, "registry") || strings.Contains(stderr, "wardrobe.db") {
		t.Errorf("stderr mentions the catalog store:\n%s", stderr)
	}
}

// AC2: Given VLM_TEMPERATURE and VLM_SERIALIZE_REQUESTS are set / When the
// pipeline builds its VLM client from config / Then every tagging request
// carries the configured temperature, and VLM_SERIALIZE_REQUESTS=true
// applies the ING-003 serialized queue.
func TestING012_AC2_ConfigClientWiring(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)

	t.Run("configured temperature on every request", func(t *testing.T) {
		photo := writePhoto(t, "garment.jpg", []byte("ING012-AC2-TEMP"))
		// Attempt 1 malformed forces the retry, so both request payloads
		// are observed, not just the first.
		srv, vlm := newING012VLM(t, func(n int, _ ing012Request) string {
			if n == 1 {
				return ing005Malformed
			}
			return valid
		})

		h := newING012Harness(t)
		stdout, stderr, code := h.run(t, ing012Env(map[string]string{
			"VLM_URL":         srv.URL,
			"VLM_TEMPERATURE": "0.9",
		}), photo)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}

		reqs := vlm.all()
		if len(reqs) != 2 {
			t.Fatalf("VLM requests = %d, want 2 (attempt 1 + retry)", len(reqs))
		}
		for i, r := range reqs {
			if r.temperature != 0.9 {
				t.Errorf("request %d temperature = %v, want the configured 0.9", i+1, r.temperature)
			}
		}
		out := ing012ParseOutput(t, stdout)
		if len(out) != 1 {
			t.Fatalf("emitted records = %d, want 1", len(out))
		}
		if got := ing012Validate(t, out[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("validated fields = %+v, want %+v", got, want)
		}
	})

	t.Run("serialization enabled applies the configured delay", func(t *testing.T) {
		dir := t.TempDir()
		writeIng012Photo(t, dir, "a.jpg", []byte("ING012-AC2-SER-A"))
		writeIng012Photo(t, dir, "b.jpg", []byte("ING012-AC2-SER-B"))

		srv, vlm := newING012VLM(t, func(int, ing012Request) string { return valid })
		h := newING012Harness(t)
		_, stderr, code := h.run(t, ing012Env(map[string]string{
			"VLM_URL":                srv.URL,
			"VLM_SERIALIZE_REQUESTS": "true",
			"VLM_REQUEST_DELAY_MS":   "500",
		}), dir)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}

		reqs := vlm.all()
		if len(reqs) != 2 {
			t.Fatalf("VLM requests = %d, want 2", len(reqs))
		}
		// With WithSerialization(delay) wired, the post-response wait is
		// applied between the two requests. Without it there is no wait.
		if gap := reqs[1].at.Sub(reqs[0].at); gap < 300*time.Millisecond {
			t.Errorf("gap between requests = %v, want >= 300ms (VLM_REQUEST_DELAY_MS=500 with serialization on)", gap)
		}
	})

	t.Run("serialization disabled ignores the delay", func(t *testing.T) {
		dir := t.TempDir()
		writeIng012Photo(t, dir, "a.jpg", []byte("ING012-AC2-NOSER-A"))
		writeIng012Photo(t, dir, "b.jpg", []byte("ING012-AC2-NOSER-B"))

		srv, vlm := newING012VLM(t, func(int, ing012Request) string { return valid })
		h := newING012Harness(t)
		_, stderr, code := h.run(t, ing012Env(map[string]string{
			"VLM_URL":                srv.URL,
			"VLM_SERIALIZE_REQUESTS": "false",
			"VLM_REQUEST_DELAY_MS":   "500",
		}), dir)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}

		reqs := vlm.all()
		if len(reqs) != 2 {
			t.Fatalf("VLM requests = %d, want 2", len(reqs))
		}
		if gap := reqs[1].at.Sub(reqs[0].at); gap >= 300*time.Millisecond {
			t.Errorf("gap between requests = %v, want < 300ms (delay only applies when serialization is on)", gap)
		}
		if !strings.Contains(stderr, "will not be applied") {
			t.Errorf("config warning about the ignored delay not logged; stderr:\n%s", stderr)
		}
	})
}

// AC3: Given a photo whose attempt-1 response fails validation (ING-004) /
// When the pipeline processes it / Then the per-photo retry/flag behavior
// and the logs/vlm-attempts.jsonl records are exactly ING-005's, driven
// through this pipeline.
func TestING012_AC3_RetryPolicyDrivenThroughPipeline(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)
	photo := writePhoto(t, "garment.jpg", []byte("ING012-AC3-JPEG-BYTES"))

	srv, vlm := newING012VLM(t, func(n int, _ ing012Request) string {
		if n == 1 {
			return ing005Malformed
		}
		return valid
	})

	h := newING012Harness(t)
	offset := ing012LogSize(t, h)
	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
	}

	out := ing012ParseOutput(t, stdout)
	if len(out) != 1 {
		t.Fatalf("emitted records = %d, want 1; stdout:\n%s", len(out), stdout)
	}
	rec := out[0]
	if flagged, _ := rec["flagged"].(bool); flagged {
		t.Error("flagged = true, want false: a single failure must be retried, not flagged")
	}
	if got := ing012Validate(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("validated fields = %+v, want attempt 2's result %+v", got, want)
	}
	if n := vlm.count(); n != 2 {
		t.Errorf("VLM requests = %d, want exactly 2", n)
	}

	// The log format is ING-005's: exactly its 9 fields per line.
	lines := ing012AppendedLines(t, h, offset)
	if len(lines) != 2 {
		t.Fatalf("appended attempt-log lines = %d, want 2 (failed attempt + success)", len(lines))
	}
	wantKeys := map[string]bool{
		"item_id": true, "timestamp": true, "photo_path": true, "attempt": true,
		"temperature": true, "raw_response": true, "parsed_json": true,
		"failure_type": true, "failure_detail": true,
	}
	for i, line := range lines {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("appended line %d is not a JSON object: %v", i+1, err)
		}
		if len(obj) != len(wantKeys) {
			t.Errorf("line %d has keys %v, want exactly ING-005's 9 fields", i+1, obj)
		}
		for k := range wantKeys {
			if _, ok := obj[k]; !ok {
				t.Errorf("line %d is missing field %q", i+1, k)
			}
		}
	}

	entries := ing012AppendedAttempts(t, h, offset)
	itemID, _ := rec["item_id"].(string)
	if entries[0].Attempt != 1 || entries[0].FailureType != tagging.FailureTypeMalformedJSON {
		t.Errorf("attempt 1 = %+v, want attempt 1 with failure_type %q", entries[0], tagging.FailureTypeMalformedJSON)
	}
	if entries[1].Attempt != 2 || entries[1].FailureType != "" {
		t.Errorf("attempt 2 = %+v, want attempt 2 with no failure", entries[1])
	}
	if entries[0].ItemID != itemID || entries[1].ItemID != itemID {
		t.Errorf("attempt item_ids = %q/%q, want the outcome item_id %q", entries[0].ItemID, entries[1].ItemID, itemID)
	}
	if entries[0].PhotoPath != photo || entries[0].RawResponse != ing005Malformed {
		t.Errorf("attempt 1 = %+v, want photo_path %q and the unmodified raw response", entries[0], photo)
	}
	if entries[0].Temperature != 0.4 || entries[1].Temperature != 0.4 {
		t.Errorf("logged temperatures = %v/%v, want the config default 0.4", entries[0].Temperature, entries[1].Temperature)
	}
}

// AC4: Given a batch in which one photo is flagged for manual review /
// When the batch is processed / Then the remaining photos still emit
// tagged JSON and the flagged photo is reported (flagged=true) separately,
// without aborting the run.
func TestING012_AC4_FlaggedPhotoDoesNotAbortBatch(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)

	dir := t.TempDir()
	flaggedPhoto := writeIng012Photo(t, dir, "a-flagged.jpg", []byte("ING012-AC4-FLAGGED"))
	okPhoto := writeIng012Photo(t, dir, "b-ok.jpg", []byte("ING012-AC4-OK"))

	// First photo: both attempts malformed -> flagged. Second photo: valid.
	srv, vlm := newING012VLM(t, func(n int, _ ing012Request) string {
		if n <= 2 {
			return ing005Malformed
		}
		return valid
	})

	h := newING012Harness(t)
	offset := ing012LogSize(t, h)
	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a flagged photo must not abort the batch); stderr:\n%s", code, stderr)
	}

	out := ing012ParseOutput(t, stdout)
	if len(out) != 2 {
		t.Fatalf("emitted records = %d, want one per photo (2); stdout:\n%s", len(out), stdout)
	}

	first, second := out[0], out[1]
	if firstFlagged, _ := first["flagged"].(bool); !firstFlagged {
		t.Error("first photo flagged = false, want true (both attempts invalid)")
	}
	if got := first["photo_path"]; got != flaggedPhoto {
		t.Errorf("flagged photo_path = %v, want %q", got, flaggedPhoto)
	}
	if secondFlagged, _ := second["flagged"].(bool); secondFlagged {
		t.Error("second photo flagged = true, want false; one bad photo must not poison the batch")
	}
	if got := second["photo_path"]; got != okPhoto {
		t.Errorf("second photo_path = %v, want %q", got, okPhoto)
	}
	if got := ing012Validate(t, second); !reflect.DeepEqual(got, want) {
		t.Errorf("second photo validated fields = %+v, want %+v", got, want)
	}
	if n := vlm.count(); n != 3 {
		t.Errorf("VLM requests = %d, want 3 (2 for the flagged photo + 1 for the rest)", n)
	}
	if entries := ing012AppendedAttempts(t, h, offset); len(entries) != 3 {
		t.Errorf("appended attempt-log lines = %d, want 3", len(entries))
	}
}

// AC5: Given the VLM is unreachable while processing a photo / When the
// pipeline processes it / Then the ING-002 ErrVLMUnreachable error
// surfaces distinctly, is not treated as a malformed-output flag, and is
// not retried into one.
func TestING012_AC5_UnreachableSurfacesDistinctly(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		panic(http.ErrAbortHandler) // drop the connection mid-request
	}))
	t.Cleanup(srv.Close)

	photo := writePhoto(t, "garment.jpg", []byte("ING012-AC5-JPEG-BYTES"))
	h := newING012Harness(t)
	offset := ing012LogSize(t, h)

	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code == 0 {
		t.Fatalf("exit code = 0, want nonzero for an unreachable VLM")
	}
	if !strings.Contains(stderr, "unreachable") {
		t.Errorf("stderr does not surface the unreachable error distinctly:\n%s", stderr)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("VLM requests = %d, want exactly 1 (unreachable must not be retried)", got)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout = %q, want no tagged JSON for a hard failure", stdout)
	}
	if entries := ing012AppendedAttempts(t, h, offset); len(entries) != 0 {
		t.Errorf("appended attempt-log lines = %d, want 0 (unreachable is not a malformed-output flag)", len(entries))
	}
}

// Edge (ticket's open integration point 1): a hard error on attempt 2
// after attempt 1 was already logged. The CLI relies on the log for the
// partial attempt and reports the photo path plus the unreachable error;
// it does not flag the item.
func TestING012_Edge_UnreachableOnAttempt2LeavesAttempt1Log(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(rawEnvelope(ing005Malformed)))
			return
		}
		panic(http.ErrAbortHandler) // attempt 2 drops the connection
	}))
	t.Cleanup(srv.Close)

	photo := writePhoto(t, "garment.jpg", []byte("ING012-EDGE-ATTEMPT2"))
	h := newING012Harness(t)
	offset := ing012LogSize(t, h)

	stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
	if code == 0 {
		t.Fatalf("exit code = 0, want nonzero when the retry is unreachable")
	}
	if !strings.Contains(stderr, "unreachable") {
		t.Errorf("stderr does not name the unreachable error:\n%s", stderr)
	}
	if !strings.Contains(stderr, photo) {
		t.Errorf("stderr does not name the photo path:\n%s", stderr)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("VLM requests = %d, want 2 (one attempt + one retry)", got)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout = %q, want no tagged JSON and no flagged record", stdout)
	}

	entries := ing012AppendedAttempts(t, h, offset)
	if len(entries) != 1 || entries[0].Attempt != 1 || entries[0].FailureType != tagging.FailureTypeMalformedJSON {
		t.Errorf("appended attempts = %+v, want only attempt 1's malformed_json record (the CLI relies on the log for the partial attempt)", entries)
	}
}

// AC6: Given the documented invocation with a photo path or a directory /
// When it is run / Then it prints tagged JSON to stdout and exits 0 on
// success, and exits nonzero on a hard error (VLM unreachable, missing
// photo).
func TestING012_AC6_InvocationAndExitCodes(t *testing.T) {
	t.Run("missing photo exits nonzero naming it", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist.jpg")

		h := newING012Harness(t)
		stdout, stderr, code := h.run(t, ing012Env(nil), missing)
		if code == 0 {
			t.Fatalf("exit code = 0, want nonzero for a missing photo")
		}
		if !strings.Contains(stderr, missing) {
			t.Errorf("stderr does not name the missing photo %q:\n%s", missing, stderr)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("stdout = %q, want no tagged JSON on a hard error", stdout)
		}
	})

	t.Run("missing VLM_URL exits nonzero naming it", func(t *testing.T) {
		photo := writePhoto(t, "garment.jpg", []byte("ING012-AC6-JPEG-BYTES"))

		h := newING012Harness(t)
		_, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": ""}), photo)
		if code == 0 {
			t.Fatalf("exit code = 0, want a startup error for a missing VLM_URL")
		}
		if !strings.Contains(stderr, "VLM_URL") {
			t.Errorf("startup error does not name VLM_URL:\n%s", stderr)
		}
	})

	t.Run("directory path exits 0 with one record per photo", func(t *testing.T) {
		tax := loadTaxonomy(t)
		valid, _ := validTaggingPayload(t, tax)

		dir := t.TempDir()
		writeIng012Photo(t, dir, "one.jpg", []byte("ING012-AC6-DIR-1"))
		writeIng012Photo(t, dir, "two.png", []byte("ING012-AC6-DIR-2"))

		srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })
		h := newING012Harness(t)
		stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), dir)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		if got := len(ing012ParseOutput(t, stdout)); got != 2 {
			t.Errorf("emitted records = %d, want 2", got)
		}
	})
}

// Spec-implied edge cases the ticket did not spell out but 04-data-schema.md
// / 05-vlm-tagging-spec.md imply.
func TestING012_Edges(t *testing.T) {
	tax := loadTaxonomy(t)

	t.Run("empty secondary_colors is emitted as [] and validates", func(t *testing.T) {
		valid := taggingJSON(t, tax, map[string]any{"secondary_colors": []string{}})
		photo := writePhoto(t, "garment.jpg", []byte("ING012-EDGE-EMPTY"))

		srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })
		h := newING012Harness(t)
		stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		out := ing012ParseOutput(t, stdout)
		if len(out) != 1 {
			t.Fatalf("emitted records = %d, want 1", len(out))
		}
		cols, ok := out[0]["secondary_colors"].([]any)
		if !ok {
			t.Fatalf("secondary_colors = %v, want a JSON array", out[0]["secondary_colors"])
		}
		if len(cols) != 0 {
			t.Errorf("secondary_colors = %v, want empty", cols)
		}
		ing012Validate(t, out[0])
	})

	t.Run("a directly-passed uppercase-extension photo is processed", func(t *testing.T) {
		tax := loadTaxonomy(t)
		valid, _ := validTaggingPayload(t, tax)
		photo := filepath.Join(t.TempDir(), "PHOTO.JPG")
		if err := os.WriteFile(photo, []byte("ING012-EDGE-UPPER"), 0o644); err != nil {
			t.Fatalf("write photo: %v", err)
		}

		srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })
		h := newING012Harness(t)
		stdout, stderr, code := h.run(t, ing012Env(map[string]string{"VLM_URL": srv.URL}), photo)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		if got := len(ing012ParseOutput(t, stdout)); got != 1 {
			t.Errorf("emitted records = %d, want 1", got)
		}
	})
}
