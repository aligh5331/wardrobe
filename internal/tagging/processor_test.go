package tagging

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
	"sync/atomic"
	"testing"
)

const validTaggingJSON = `{"category":"bottom","subcategory":"jeans","dominant_color":"navy",` +
	`"secondary_colors":[],"pattern":"solid","warmth_tier":"medium","formality":"casual"}`

const invalidEnumJSON = `{"category":"top","subcategory":"jeans","dominant_color":"navy",` +
	`"secondary_colors":[],"pattern":"solid","warmth_tier":"medium","formality":"casual"}`

// captureLogger records attempt logs in memory for assertions.
type captureLogger struct {
	entries []AttemptLog
}

func (c *captureLogger) Log(a AttemptLog) error {
	c.entries = append(c.entries, a)
	return nil
}

// chatBody wraps raw model content in the OpenAI-compatible response envelope
// the client expects.
func chatBody(t *testing.T, content string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
	})
	if err != nil {
		t.Fatalf("marshal canned response: %v", err)
	}
	return b
}

// newVLM returns a client and processor against a server whose handler is fn.
func newVLM(t *testing.T, handler http.HandlerFunc) (*Client, *Processor, *captureLogger) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	log := &captureLogger{}
	client := NewClient(srv.URL, "", WithTemperature(func() float64 { return 0.4 }))
	return client, NewProcessor(client, WithAttemptLogger(log)), log
}

// AC2: attempt 1 fails validation, attempt 2 passes — the result is used and
// the item is not flagged, but both attempts are logged with a shared id.
func TestProcessor_Process_UsesSecondAttemptOnRetrySuccess(t *testing.T) {
	var requests atomic.Int32
	_, p, log := newVLM(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Write(chatBody(t, "this is not json"))
			return
		}
		w.Write(chatBody(t, validTaggingJSON))
	})

	out, err := p.Process(context.Background(), writeTestImage(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if out.Flagged {
		t.Error("Flagged = true, want false when attempt 2 succeeds")
	}
	if requests.Load() != 2 {
		t.Errorf("requests = %d, want 2", requests.Load())
	}
	if out.Result.Category != "bottom" || out.Result.Subcategory != "jeans" {
		t.Errorf("Result = %+v, want attempt 2's validated result", out.Result)
	}
	if len(out.Attempts) != 2 {
		t.Fatalf("Attempts = %d, want 2 logged attempts", len(out.Attempts))
	}

	a1, a2 := out.Attempts[0], out.Attempts[1]
	if a1.FailureType != FailureTypeMalformedJSON {
		t.Errorf("attempt 1 failure_type = %q, want %q", a1.FailureType, FailureTypeMalformedJSON)
	}
	if a1.ParsedJSON != nil {
		t.Errorf("attempt 1 parsed_json = %s, want null for malformed json", a1.ParsedJSON)
	}
	if a2.FailureType != "" || a2.FailureDetail != "" {
		t.Errorf("attempt 2 failure = %q/%q, want empty on success", a2.FailureType, a2.FailureDetail)
	}
	if !json.Valid(a2.ParsedJSON) {
		t.Errorf("attempt 2 parsed_json = %s, want the valid parsed object", a2.ParsedJSON)
	}
	for _, a := range out.Attempts {
		if a.ItemID != out.ItemID {
			t.Errorf("attempt item_id = %q, want shared %q", a.ItemID, out.ItemID)
		}
		if a.Temperature != 0.4 {
			t.Errorf("attempt temperature = %v, want 0.4", a.Temperature)
		}
	}
	if a1.Attempt != 1 || a2.Attempt != 2 {
		t.Errorf("attempt numbers = %d,%d, want 1,2", a1.Attempt, a2.Attempt)
	}
	if len(log.entries) != 2 {
		t.Errorf("logged entries = %d, want 2", len(log.entries))
	}
}

// AC1/AC3: both attempts fail validation — the item is flagged with both
// attempts' detail attached, and the retry was actually sent.
func TestProcessor_Process_FlagsAfterSecondFailure(t *testing.T) {
	var requests atomic.Int32
	_, p, log := newVLM(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write(chatBody(t, invalidEnumJSON))
	})

	out, err := p.Process(context.Background(), writeTestImage(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if !out.Flagged {
		t.Fatal("Flagged = false, want true when both attempts fail")
	}
	if requests.Load() != 2 {
		t.Errorf("requests = %d, want 2", requests.Load())
	}
	if !reflect.DeepEqual(out.Result, TaggingResult{}) {
		t.Errorf("Result = %+v, want zero on flagged item", out.Result)
	}
	if len(out.Attempts) != 2 || len(log.entries) != 2 {
		t.Fatalf("attempts = %d logged %d, want 2 each", len(out.Attempts), len(log.entries))
	}
	for i, a := range out.Attempts {
		if a.FailureType != FailureTypeInvalidEnum {
			t.Errorf("attempt %d failure_type = %q, want %q", i+1, a.FailureType, FailureTypeInvalidEnum)
		}
		if a.FailureDetail == "" {
			t.Errorf("attempt %d failure_detail empty, want the specific field/value", i+1)
		}
	}
	if out.Attempts[0].ItemID != out.Attempts[1].ItemID {
		t.Errorf("attempts not linked by a shared item_id: %q vs %q",
			out.Attempts[0].ItemID, out.Attempts[1].ItemID)
	}
}

// AC5: VLM unreachable on attempt 1 is not retried and not flagged — it
// surfaces immediately as ErrVLMUnreachable.
func TestProcessor_Process_UnreachableFirstAttempt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	log := &captureLogger{}
	p := NewProcessor(NewClient(srv.URL, ""), WithAttemptLogger(log))

	out, err := p.Process(context.Background(), writeTestImage(t))
	if !errors.Is(err, ErrVLMUnreachable) {
		t.Fatalf("Process() error = %v, want ErrVLMUnreachable", err)
	}
	if out.Flagged {
		t.Error("Flagged = true, want false for a connectivity failure")
	}
	if len(log.entries) != 0 {
		t.Errorf("logged entries = %d, want 0 for an unreachable first attempt", len(log.entries))
	}
}

// AC5: unreachable on attempt 2 is not treated as a second malformed-output
// failure and does not turn into a flag.
func TestProcessor_Process_UnreachableSecondAttempt(t *testing.T) {
	var requests atomic.Int32
	log := &captureLogger{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Write(chatBody(t, "not json"))
			return
		}
		panic(http.ErrAbortHandler) // drop the connection mid-request
	}))
	defer srv.Close()

	p := NewProcessor(NewClient(srv.URL, ""), WithAttemptLogger(log))
	out, err := p.Process(context.Background(), writeTestImage(t))
	if !errors.Is(err, ErrVLMUnreachable) {
		t.Fatalf("Process() error = %v, want ErrVLMUnreachable", err)
	}
	if out.Flagged {
		t.Error("Flagged = true, want false — unreachable is not a malformed-output flag")
	}
	if len(log.entries) != 1 || log.entries[0].FailureType != FailureTypeMalformedJSON {
		t.Errorf("logged entries = %+v, want only attempt 1's malformed_json record", log.entries)
	}
}

// AC4: a flagged photo does not stop the next photo in the same batch from
// being tagged (Process returns no error for a flag).
func TestProcessor_Process_FlagDoesNotBlockNextPhoto(t *testing.T) {
	var requests atomic.Int32
	_, p, _ := newVLM(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 2 {
			w.Write(chatBody(t, "not json"))
			return
		}
		w.Write(chatBody(t, validTaggingJSON))
	})

	flagged, err := p.Process(context.Background(), writeTestImage(t))
	if err != nil {
		t.Fatalf("first photo Process() error = %v", err)
	}
	if !flagged.Flagged {
		t.Fatal("first photo Flagged = false, want true")
	}

	tagged, err := p.Process(context.Background(), writeTestImage(t))
	if err != nil {
		t.Fatalf("second photo Process() error = %v, want the batch to continue", err)
	}
	if tagged.Flagged || tagged.Result.Category != "bottom" {
		t.Errorf("second photo = %+v, want an unflagged validated result", tagged)
	}
}

// The JSONL logger appends exactly one JSON object per line, with a null
// parsed_json for a malformed attempt.
func TestJSONLAttemptLogger_Log(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "vlm-attempts.jsonl")
	logger := NewJSONLAttemptLogger(path)

	first := AttemptLog{
		ItemID: "abc", Timestamp: "2026-09-13T14:32:01Z", PhotoPath: "data/photos/x.jpg",
		Attempt: 1, Temperature: 0.4, RawResponse: "garbage",
		FailureType: FailureTypeMalformedJSON, FailureDetail: "malformed json: unexpected token",
	}
	second := AttemptLog{
		ItemID: "abc", Timestamp: "2026-09-13T14:32:02Z", PhotoPath: "data/photos/x.jpg",
		Attempt: 2, Temperature: 0.4, RawResponse: validTaggingJSON,
		ParsedJSON: json.RawMessage(validTaggingJSON),
	}
	for _, a := range []AttemptLog{first, second} {
		if err := logger.Log(a); err != nil {
			t.Fatalf("Log() error = %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2", len(lines))
	}
	var got AttemptLog
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if got.FailureType != FailureTypeMalformedJSON || string(got.ParsedJSON) != "null" {
		t.Errorf("line 1 = %+v, want malformed_json with null parsed_json", got)
	}
	if err := json.Unmarshal([]byte(lines[1]), &got); err != nil {
		t.Fatalf("line 2 not valid JSON: %v", err)
	}
	if !json.Valid(got.ParsedJSON) {
		t.Errorf("line 2 parsed_json = %s, want the parsed object", got.ParsedJSON)
	}
}
