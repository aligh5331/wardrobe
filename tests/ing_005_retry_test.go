// Package tests holds black-box acceptance tests for backlog tickets.
// ING-005: a malformed or taxonomy-invalid VLM response is retried
// exactly once at the configured (nonzero) temperature, and the item is
// flagged for manual review only if the retry also fails. Every attempt
// is appended to logs/vlm-attempts.jsonl with full diagnostic detail,
// keyed by a shared item_id. VLM-unreachable errors are a distinct
// failure mode and are never retried or flagged.
//
// These tests exercise the exported API of wardrobe/internal/tagging
// only; implementation code is never touched from here. The valid and
// invalid payloads are derived from 03-taxonomy.md at runtime, so the
// suite checks the spec rather than a hand-copied enum list.
package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wardrobe/internal/tagging"
)

// ing005Temperature is the temperature the stub client is configured
// with; 06-decisions.md requires it be nonzero so attempt 2 is an
// independent sample rather than a deterministic repeat.
const ing005Temperature = 0.4

// ing005Malformed is raw model text that is not valid JSON. The trailing
// comma plus prose guarantees json.Unmarshal fails.
const ing005Malformed = `{"category":"bottom", this is not json`

// ing005Request captures the parts of a tagging request the retry policy
// cares about: the spec prompt and the encoded image data URI.
type ing005Request struct {
	system string
	image  string
}

// ing005Capture records every request a stub VLM receives, in order.
type ing005Capture struct {
	mu       sync.Mutex
	requests []ing005Request
}

func (c *ing005Capture) record(r ing005Request) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, r)
	return len(c.requests)
}

func (c *ing005Capture) all() []ing005Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ing005Request(nil), c.requests...)
}

// newING005VLM starts a stub VLM that calls respond with the 1-based
// request number and returns its content wrapped in a valid chat
// response envelope.
func newING005VLM(t *testing.T, respond func(attempt int, req ing005Request) string) (*httptest.Server, *ing005Capture) {
	t.Helper()

	capture := &ing005Capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeRequest(t, r)
		messages := body["messages"].([]any)
		system, _ := messages[0].(map[string]any)["content"].(string)
		user := messages[1].(map[string]any)
		parts := user["content"].([]any)
		image := parts[0].(map[string]any)["image_url"].(map[string]any)["url"].(string)

		req := ing005Request{system: system, image: image}
		attempt := capture.record(req)
		_, _ = w.Write([]byte(rawEnvelope(respond(attempt, req))))
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

// ing005Logger records attempt logs in memory for assertions.
type ing005Logger struct {
	mu      sync.Mutex
	entries []tagging.AttemptLog
}

func (l *ing005Logger) Log(a tagging.AttemptLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, a)
	return nil
}

func (l *ing005Logger) all() []tagging.AttemptLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]tagging.AttemptLog(nil), l.entries...)
}

// ing005Processor builds a processor for the stub with the given
// attempt logger and a stable, nonzero temperature source.
func ing005Processor(t *testing.T, srv *httptest.Server, log tagging.AttemptLogger) *tagging.Processor {
	t.Helper()

	client := tagging.NewClient(srv.URL, "",
		tagging.WithTemperature(func() float64 { return ing005Temperature }))
	return tagging.NewProcessor(client, tagging.WithAttemptLogger(log))
}

// validTaggingPayload returns a fully valid tagging record derived from
// the spec vocabulary, plus the TaggingResult it must validate to.
func validTaggingPayload(t *testing.T, tax taxonomy) (string, tagging.TaggingResult) {
	t.Helper()

	category := tax.categories[0]
	want := tagging.TaggingResult{
		Category:        category,
		Subcategory:     tax.subcategories[category][0],
		DominantColor:   tax.colors[0],
		SecondaryColors: []string{tax.colors[1]},
		Pattern:         tax.patterns[0],
		WarmthTier:      tax.warmthTiers[0],
		Formality:       tax.formalities[0],
	}
	return taggingJSON(t, tax, nil), want
}

// parsedJSONIsNull reports whether an attempt has no parsed object: nil
// before the record is marshalled, the literal null after a JSONL
// round-trip.
func parsedJSONIsNull(a tagging.AttemptLog) bool {
	return len(a.ParsedJSON) == 0 || string(a.ParsedJSON) == "null"
}

// assertING005Entry checks one attempt record carries the ticket's full
// detail: the shared item id, the photo path, the unmodified raw
// response, the attempt number, the temperature used, a UTC RFC3339
// timestamp, the specific failure type/detail, and the parsed_json
// presence rule (null exactly when the raw text was not valid JSON).
func assertING005Entry(t *testing.T, a tagging.AttemptLog, itemID, photo, raw string, attempt int, wantFailureType string, detailFragments ...string) {
	t.Helper()

	if a.ItemID != itemID || itemID == "" {
		t.Errorf("attempt %d item_id = %q, want shared non-empty %q", attempt, a.ItemID, itemID)
	}
	if a.PhotoPath != photo {
		t.Errorf("attempt %d photo_path = %q, want %q", attempt, a.PhotoPath, photo)
	}
	if a.Attempt != attempt {
		t.Errorf("attempt = %d, want %d", a.Attempt, attempt)
	}
	if a.Temperature != ing005Temperature {
		t.Errorf("attempt %d temperature = %v, want %v", attempt, a.Temperature, ing005Temperature)
	}
	if a.RawResponse != raw {
		t.Errorf("attempt %d raw_response = %q, want the unmodified %q", attempt, a.RawResponse, raw)
	}
	if a.FailureType != wantFailureType {
		t.Errorf("attempt %d failure_type = %q, want %q", attempt, a.FailureType, wantFailureType)
	}
	for _, frag := range detailFragments {
		if !strings.Contains(a.FailureDetail, frag) {
			t.Errorf("attempt %d failure_detail = %q, want it to name %q", attempt, a.FailureDetail, frag)
		}
	}
	if a.Timestamp == "" {
		t.Errorf("attempt %d timestamp is empty, want RFC3339 UTC", attempt)
	} else {
		if _, err := time.Parse(time.RFC3339, a.Timestamp); err != nil {
			t.Errorf("attempt %d timestamp = %q, not RFC3339: %v", attempt, a.Timestamp, err)
		}
		if !strings.HasSuffix(a.Timestamp, "Z") {
			t.Errorf("attempt %d timestamp = %q, want UTC (trailing Z)", attempt, a.Timestamp)
		}
	}

	if wantFailureType == tagging.FailureTypeMalformedJSON {
		// In memory the logger leaves ParsedJSON nil; once the record has
		// round-tripped through JSONL it is the literal null. Both mean
		// "no parsed object".
		if !parsedJSONIsNull(a) {
			t.Errorf("attempt %d parsed_json = %s, want null for malformed JSON", attempt, a.ParsedJSON)
		}
		return
	}
	if !json.Valid(a.ParsedJSON) {
		t.Errorf("attempt %d parsed_json = %q, want the parsed JSON object", attempt, a.ParsedJSON)
	}
}

// AC1: Given a VLM response on attempt 1 that fails validation
// (malformed JSON, invalid enum, or a missing required field) / When it
// is processed / Then attempt 1's full detail is logged, and a second
// request is sent for the same photo before anything is flagged.
func TestING005_AC1_Attempt1FailureRetriedAndLogged(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	invalidEnum := taggingJSON(t, tax, map[string]any{
		"category":    "top",
		"subcategory": foreignSubcategory(tax, "top"), // e.g. top/jeans
	})
	missingField := taggingJSON(t, tax, map[string]any{"pattern": omitted})

	cases := []struct {
		name            string
		raw             string
		wantFailureType string
		wantDetail      string
	}{
		{"malformed json", ing005Malformed, tagging.FailureTypeMalformedJSON, "malformed json"},
		{"invalid enum", invalidEnum, tagging.FailureTypeInvalidEnum, foreignSubcategory(tax, "top")},
		{"missing required field", missingField, tagging.FailureTypeMissingRequiredField, "pattern"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING005"))
			srv, capture := newING005VLM(t, func(attempt int, _ ing005Request) string {
				if attempt == 1 {
					return tc.raw
				}
				return valid
			})
			log := &ing005Logger{}
			p := ing005Processor(t, srv, log)

			out, err := p.Process(context.Background(), photo)
			if err != nil {
				t.Fatalf("Process() error = %v, want nil", err)
			}

			reqs := capture.all()
			if len(reqs) != 2 {
				t.Fatalf("VLM requests = %d, want 2 (attempt 2 sent only after attempt 1 failed validation)", len(reqs))
			}
			if reqs[0].image != reqs[1].image {
				t.Error("attempt 2 did not send the same photo as attempt 1")
			}
			if reqs[0].system != reqs[1].system {
				t.Error("attempt 2 did not send the same prompt as attempt 1")
			}
			if out.Flagged {
				t.Error("Flagged = true after attempt 2 passed; a single failure must be retried, not flagged")
			}

			entries := log.all()
			if len(entries) != 2 {
				t.Fatalf("logged attempts = %d, want 2 (attempt 1 failure + attempt 2)", len(entries))
			}
			if len(out.Attempts) != 2 {
				t.Errorf("Outcome.Attempts = %d, want 2", len(out.Attempts))
			}
			assertING005Entry(t, entries[0], out.ItemID, photo, tc.raw, 1, tc.wantFailureType, tc.wantDetail)
			assertING005Entry(t, entries[1], out.ItemID, photo, valid, 2, "")
		})
	}
}

// AC2: Given attempt 2's response passes validation / When it is
// processed / Then the validated result is used normally — the item is
// NOT flagged, even though attempt 1 failed.
func TestING005_AC2_RetrySuccessUsesResultNotFlagged(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING005"))

	srv, capture := newING005VLM(t, func(attempt int, _ ing005Request) string {
		if attempt == 1 {
			return ing005Malformed
		}
		return valid
	})
	log := &ing005Logger{}
	p := ing005Processor(t, srv, log)

	out, err := p.Process(context.Background(), photo)
	if err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}
	if out.Flagged {
		t.Error("Flagged = true, want false when attempt 2 validates")
	}
	if !reflect.DeepEqual(out.Result, want) {
		t.Errorf("Result = %+v, want attempt 2's validated result %+v", out.Result, want)
	}
	if got := len(capture.all()); got != 2 {
		t.Errorf("VLM requests = %d, want 2", got)
	}
	entries := log.all()
	if len(entries) != 2 {
		t.Fatalf("logged attempts = %d, want 2", len(entries))
	}
	if entries[0].FailureType != tagging.FailureTypeMalformedJSON || entries[1].FailureType != "" {
		t.Errorf("failure types = %q,%q; want attempt 1 malformed and attempt 2 clean",
			entries[0].FailureType, entries[1].FailureType)
	}
}

// AC3: Given attempt 2's response also fails validation / When it is
// processed / Then the item is flagged for manual review, with both
// attempt 1's and attempt 2's logged detail attached — not just the
// final failure.
func TestING005_AC3_SecondFailureFlagsWithBothAttempts(t *testing.T) {
	tax := loadTaxonomy(t)
	invalidEnum := taggingJSON(t, tax, map[string]any{
		"category":    "top",
		"subcategory": foreignSubcategory(tax, "top"),
	})
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING005"))

	srv, capture := newING005VLM(t, func(attempt int, _ ing005Request) string {
		if attempt == 1 {
			return ing005Malformed
		}
		return invalidEnum
	})
	log := &ing005Logger{}
	p := ing005Processor(t, srv, log)

	out, err := p.Process(context.Background(), photo)
	if err != nil {
		t.Fatalf("Process() error = %v, want nil for a flag (so a batch continues)", err)
	}
	if !out.Flagged {
		t.Fatal("Flagged = false, want true when both attempts fail validation")
	}
	if !reflect.DeepEqual(out.Result, tagging.TaggingResult{}) {
		t.Errorf("Result = %+v, want zero on a flagged item", out.Result)
	}
	if got := len(capture.all()); got != 2 {
		t.Errorf("VLM requests = %d, want exactly 2 (no third attempt)", got)
	}

	entries := log.all()
	if len(entries) != 2 {
		t.Fatalf("logged attempts = %d, want 2", len(entries))
	}
	if len(out.Attempts) != 2 {
		t.Fatalf("Outcome.Attempts = %d, want 2 attached for manual review", len(out.Attempts))
	}
	// Both attempts' detail must be attached, not just attempt 2's.
	if entries[0].FailureType != tagging.FailureTypeMalformedJSON {
		t.Errorf("attempt 1 failure_type = %q, want %q", entries[0].FailureType, tagging.FailureTypeMalformedJSON)
	}
	if entries[1].FailureType != tagging.FailureTypeInvalidEnum {
		t.Errorf("attempt 2 failure_type = %q, want %q", entries[1].FailureType, tagging.FailureTypeInvalidEnum)
	}
	for i, e := range entries {
		if e.FailureDetail == "" {
			t.Errorf("attempt %d failure_detail empty, want the specific field/value", i+1)
		}
	}
	if entries[0].FailureDetail == entries[1].FailureDetail {
		t.Error("both attempts have identical failure_detail; want each attempt's own detail retained")
	}
	if entries[0].ItemID != entries[1].ItemID || entries[0].ItemID != out.ItemID {
		t.Errorf("attempts not linked by the shared item_id: %q vs %q (outcome %q)",
			entries[0].ItemID, entries[1].ItemID, out.ItemID)
	}
}

// AC4: Given a flagged item / When the ingestion pipeline continues /
// Then processing of other photos in the same batch is not blocked by
// one bad result.
func TestING005_AC4_FlaggedPhotoDoesNotBlockBatch(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)
	photos := []string{
		writePhoto(t, "first.jpg", []byte("JPEG-BYTES-ING005-1")),
		writePhoto(t, "second.jpg", []byte("JPEG-BYTES-ING005-2")),
		writePhoto(t, "third.jpg", []byte("JPEG-BYTES-ING005-3")),
	}

	srv, capture := newING005VLM(t, func(attempt int, _ ing005Request) string {
		if attempt <= 2 { // the first photo's two attempts both fail
			return ing005Malformed
		}
		return valid
	})
	log := &ing005Logger{}
	p := ing005Processor(t, srv, log)

	// A batch loop that keeps going after a flag; a flag returns no error.
	var flagged, tagged int
	for i, photo := range photos {
		out, err := p.Process(context.Background(), photo)
		if err != nil {
			t.Fatalf("photo %d Process() error = %v, want the batch to continue", i, err)
		}
		if out.Flagged {
			flagged++
			continue
		}
		tagged++
		if !reflect.DeepEqual(out.Result, want) {
			t.Errorf("photo %d Result = %+v, want the validated result", i, out.Result)
		}
	}
	if flagged != 1 {
		t.Errorf("flagged photos = %d, want 1", flagged)
	}
	if tagged != 2 {
		t.Errorf("successfully tagged photos = %d, want 2 after the flag", tagged)
	}
	if got := len(capture.all()); got != 4 {
		t.Errorf("VLM requests = %d, want 4 (2 for the flagged photo + 1 each for the rest)", got)
	}
	if got := len(log.all()); got != 4 {
		t.Errorf("logged attempts = %d, want 4", got)
	}
}

// AC5: Given the VLM is unreachable (ING-002's distinct error, not a
// validation failure) / When it occurs on either attempt / Then it is
// NOT treated as a malformed-output case and does NOT count toward the
// two-attempt policy — connectivity surfaces immediately.
func TestING005_AC5_UnreachableNotRetriedOrFlagged(t *testing.T) {
	t.Run("attempt 1", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			panic(http.ErrAbortHandler) // drop the connection mid-request
		}))
		t.Cleanup(srv.Close)

		log := &ing005Logger{}
		p := ing005Processor(t, srv, log)

		out, err := p.Process(context.Background(), writePhoto(t, "photo.jpg", []byte("x")))
		if !errors.Is(err, tagging.ErrVLMUnreachable) {
			t.Fatalf("Process() error = %v, want ErrVLMUnreachable", err)
		}
		if out.Flagged {
			t.Error("Flagged = true, want false for a connectivity failure")
		}
		if got := requests.Load(); got != 1 {
			t.Errorf("VLM requests = %d, want exactly 1 (unreachable must not be retried)", got)
		}
		if got := len(log.all()); got != 0 {
			t.Errorf("logged attempts = %d, want 0 (no raw response to log on a connectivity failure)", got)
		}
	})

	t.Run("attempt 2", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requests.Add(1) == 1 {
				_, _ = w.Write([]byte(rawEnvelope(ing005Malformed)))
				return
			}
			panic(http.ErrAbortHandler) // attempt 2 drops the connection
		}))
		t.Cleanup(srv.Close)

		log := &ing005Logger{}
		p := ing005Processor(t, srv, log)

		out, err := p.Process(context.Background(), writePhoto(t, "photo.jpg", []byte("x")))
		if !errors.Is(err, tagging.ErrVLMUnreachable) {
			t.Fatalf("Process() error = %v, want ErrVLMUnreachable on attempt 2", err)
		}
		if out.Flagged {
			t.Error("Flagged = true; an unreachable retry is not a second malformed-output failure")
		}
		entries := log.all()
		if len(entries) != 1 || entries[0].FailureType != tagging.FailureTypeMalformedJSON {
			t.Errorf("logged attempts = %+v, want only attempt 1's malformed_json record", entries)
		}
	})
}

// Log format: append one JSON object per attempt to
// logs/vlm-attempts.jsonl, with exactly the ticket's 9 fields, a shared
// item_id across a photo's attempts, and parsed_json null exactly when
// the raw text was not valid JSON.
func TestING005_LogFormat_JSONL(t *testing.T) {
	if tagging.DefaultAttemptLogPath != "logs/vlm-attempts.jsonl" {
		t.Errorf("DefaultAttemptLogPath = %q, want logs/vlm-attempts.jsonl", tagging.DefaultAttemptLogPath)
	}

	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING005"))
	path := filepath.Join(t.TempDir(), "logs", "vlm-attempts.jsonl") // parent dir absent on purpose

	srv, _ := newING005VLM(t, func(attempt int, _ ing005Request) string {
		if attempt == 1 {
			return ing005Malformed
		}
		return valid
	})
	logger := tagging.NewJSONLAttemptLogger(path)
	p := ing005Processor(t, srv, logger)

	if _, err := p.Process(context.Background(), photo); err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read attempt log: %v (the logger must create logs/ if absent)", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("attempt-log directory was not created: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("attempt log lines = %d, want one object per attempt (2)", len(lines))
	}

	wantKeys := map[string]bool{
		"item_id": true, "timestamp": true, "photo_path": true, "attempt": true,
		"temperature": true, "raw_response": true, "parsed_json": true,
		"failure_type": true, "failure_detail": true,
	}
	for i, line := range lines {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line %d is not a JSON object: %v", i+1, err)
		}
		if len(obj) != len(wantKeys) {
			t.Errorf("line %d has keys %v, want exactly the ticket's 9 fields", i+1, obj)
		}
		for k := range wantKeys {
			if _, ok := obj[k]; !ok {
				t.Errorf("line %d is missing field %q", i+1, k)
			}
		}
	}

	var first, second tagging.AttemptLog
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 does not decode into AttemptLog: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("line 2 does not decode into AttemptLog: %v", err)
	}

	assertING005Entry(t, first, first.ItemID, photo, ing005Malformed, 1, tagging.FailureTypeMalformedJSON, "malformed json")
	if !parsedJSONIsNull(first) {
		t.Errorf("line 1 parsed_json = %s, want null for malformed JSON", first.ParsedJSON)
	}
	assertING005Entry(t, second, first.ItemID, photo, valid, 2, "")
	if !json.Valid(second.ParsedJSON) {
		t.Errorf("line 2 parsed_json = %q, want the parsed object", second.ParsedJSON)
	}
	if first.FailureDetail == "" {
		t.Error("line 1 failure_detail is empty, want the specific malformed-JSON detail")
	}
}

// ing005OrderLogger records how many VLM requests the stub had already
// received at the moment each attempt was logged.
type ing005OrderLogger struct {
	capture   *ing005Capture
	mu        sync.Mutex
	seenAtLog []int
}

func (l *ing005OrderLogger) Log(tagging.AttemptLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seenAtLog = append(l.seenAtLog, len(l.capture.all()))
	return nil
}

// Spec-implied edge (ticket policy: "log attempt 1's full detail, then
// send attempt 2"): attempt 1 must be logged before the retry request is
// sent, so a crash or crash-loop cannot lose the first failure.
func TestING005_Edge_Attempt1LoggedBeforeRetry(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING005"))

	srv, capture := newING005VLM(t, func(attempt int, _ ing005Request) string {
		if attempt == 1 {
			return ing005Malformed
		}
		return valid
	})
	log := &ing005OrderLogger{capture: capture}
	p := ing005Processor(t, srv, log)

	if _, err := p.Process(context.Background(), photo); err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}
	if len(log.seenAtLog) != 2 || log.seenAtLog[0] != 1 || log.seenAtLog[1] != 2 {
		t.Errorf("requests already sent when each attempt was logged = %v, want [1 2] "+
			"(attempt 1 logged before the retry goes out)", log.seenAtLog)
	}
}

// Spec-implied edge (ticket policy: "no need to burn a second call on a
// success"): a validating attempt 1 stops immediately with one request.
func TestING005_Edge_SuccessOnAttempt1Stops(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, want := validTaggingPayload(t, tax)
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING005"))

	srv, capture := newING005VLM(t, func(int, ing005Request) string { return valid })
	log := &ing005Logger{}
	p := ing005Processor(t, srv, log)

	out, err := p.Process(context.Background(), photo)
	if err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}
	if out.Flagged {
		t.Error("Flagged = true, want false")
	}
	if !reflect.DeepEqual(out.Result, want) {
		t.Errorf("Result = %+v, want %+v", out.Result, want)
	}
	if got := len(capture.all()); got != 1 {
		t.Errorf("VLM requests = %d, want 1 (a success must not burn a second call)", got)
	}
	entries := log.all()
	if len(entries) != 1 {
		t.Fatalf("logged attempts = %d, want 1", len(entries))
	}
	if entries[0].FailureType != "" || entries[0].FailureDetail != "" {
		t.Errorf("successful attempt failure fields = %q/%q, want empty", entries[0].FailureType, entries[0].FailureDetail)
	}
}

// ing005FailLogger always fails, standing in for an unwritable log
// destination. The diagnostic log is the whole point of the retry
// policy, so a log-write failure must surface rather than be swallowed.
type ing005FailLogger struct{}

func (ing005FailLogger) Log(tagging.AttemptLog) error {
	return errors.New("log destination unavailable")
}

func TestING005_Edge_LogWriteFailureSurfaces(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING005"))

	srv, _ := newING005VLM(t, func(int, ing005Request) string { return valid })
	p := ing005Processor(t, srv, ing005FailLogger{})

	out, err := p.Process(context.Background(), photo)
	if err == nil {
		t.Fatal("Process() error = nil, want an error when the attempt log cannot be written")
	}
	if errors.Is(err, tagging.ErrVLMUnreachable) {
		t.Errorf("log-write failure must not be reported as ErrVLMUnreachable, got %v", err)
	}
	if out.Flagged {
		t.Error("Flagged = true, want false; a log failure is not a malformed-output flag")
	}
}
