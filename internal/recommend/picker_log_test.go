package recommend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"wardrobe/internal/logging"
)

// logCtx returns a context whose logger writes JSON lines to the buffer.
func logCtx(level slog.Level) (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level}))
	return logging.WithLogger(context.Background(), l), &buf
}

// attempts returns the parsed "llm attempt" records.
func attempts(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if rec["msg"] == "llm attempt" {
			out = append(out, rec)
		}
	}
	return out
}

func wantAttempt(t *testing.T, rec map[string]any, n, status int, outcome, level string) {
	t.Helper()
	if rec["attempt"] != float64(n) || rec["status"] != float64(status) || rec["outcome"] != outcome || rec["level"] != level {
		t.Errorf("attempt %d: got %v, want status=%d outcome=%s level=%s", n, rec, status, outcome, level)
	}
}

func TestAttemptLogOK(t *testing.T) {
	srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) { reply(w, good) })
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	if _, err := p.Pick(ctx, pickIn()); err != nil {
		t.Fatal(err)
	}
	recs := attempts(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d attempt records, want 1: %s", len(recs), buf)
	}
	wantAttempt(t, recs[0], 1, 200, "ok", "INFO")
	for _, k := range []string{"elapsed_ms", "body_bytes", "snippet"} {
		if _, ok := recs[0][k]; !ok {
			t.Errorf("record lacks %s", k)
		}
	}
	if recs[0]["body_bytes"].(float64) < 1 {
		t.Errorf("body_bytes = %v", recs[0]["body_bytes"])
	}
}

func TestAttemptLogSSE(t *testing.T) {
	srv, calls := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[]}\n\n"))
	})
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	_, err := p.Pick(ctx, pickIn())
	if err == nil || !errors.Is(err, ErrLLM) || !strings.Contains(err.Error(), "invalid output after retry: bad llm output: response is not valid JSON") {
		t.Fatalf("err = %v", err)
	}
	recs := attempts(t, buf)
	if calls.Load() != 2 || len(recs) != 2 {
		t.Fatalf("calls=%d records=%d, want 2 and 2", calls.Load(), len(recs))
	}
	for i, r := range recs {
		wantAttempt(t, r, i+1, 200, "bad_envelope", "WARN")
		if !strings.HasPrefix(r["snippet"].(string), "data: {") {
			t.Errorf("snippet = %q", r["snippet"])
		}
	}
}

func TestAttemptLogBodyLimitAndDebug(t *testing.T) {
	const marker = "MARKER-after-300"
	body := strings.Repeat("x", 310) + marker
	srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}

	ctx, buf := logCtx(slog.LevelInfo)
	p.Pick(ctx, pickIn())
	recs := attempts(t, buf)
	if len(recs) == 0 || len(recs[0]["snippet"].(string)) > 300 || recs[0]["body_bytes"] != float64(len(body)) {
		t.Errorf("info records: %v", recs)
	}
	if strings.Contains(buf.String(), marker) {
		t.Error("marker in info-level log")
	}

	ctx, buf = logCtx(slog.LevelDebug)
	p.Pick(ctx, pickIn())
	if !strings.Contains(buf.String(), marker) {
		t.Error("full body missing at debug level")
	}
}

func TestAttemptLogInvalidThenOK(t *testing.T) {
	srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			reply(w, "nope")
			return
		}
		reply(w, good)
	})
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	if _, err := p.Pick(ctx, pickIn()); err != nil {
		t.Fatal(err)
	}
	recs := attempts(t, buf)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	wantAttempt(t, recs[0], 1, 200, "invalid_output", "WARN")
	wantAttempt(t, recs[1], 2, 200, "ok", "INFO")
}

func TestAttemptLogNoChoices(t *testing.T) {
	srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"choices":[]}`)) })
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	p.Pick(ctx, pickIn())
	recs := attempts(t, buf)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	for _, r := range recs {
		if r["outcome"] != "bad_envelope" {
			t.Errorf("outcome = %v", r["outcome"])
		}
	}
}

func TestAttemptLogHTTPError(t *testing.T) {
	srv, calls := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("model required"))
	})
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	p.Pick(ctx, pickIn())
	recs := attempts(t, buf)
	if calls.Load() != 1 || len(recs) != 1 {
		t.Fatalf("calls=%d records=%d", calls.Load(), len(recs))
	}
	wantAttempt(t, recs[0], 1, 400, "http_error", "WARN")
	if recs[0]["snippet"] != "model required" {
		t.Errorf("snippet = %v", recs[0]["snippet"])
	}
}

func TestAttemptLogUnreachable(t *testing.T) {
	srv, calls := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {})
	srv.Close()
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	p.Pick(ctx, pickIn())
	recs := attempts(t, buf)
	if calls.Load() != 0 || len(recs) != 1 {
		t.Fatalf("calls=%d records=%d", calls.Load(), len(recs))
	}
	wantAttempt(t, recs[0], 1, 0, "unreachable", "WARN")
	if recs[0]["body_bytes"] != float64(0) {
		t.Errorf("body_bytes = %v", recs[0]["body_bytes"])
	}
}

func TestAttemptLogTimeout(t *testing.T) {
	srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 50 * time.Millisecond}
	p.Pick(ctx, pickIn())
	recs := attempts(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	wantAttempt(t, recs[0], 1, 0, "timeout", "WARN")
}

func TestAttemptLogReadError(t *testing.T) {
	srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	})
	ctx, buf := logCtx(slog.LevelInfo)
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	p.Pick(ctx, pickIn())
	recs := attempts(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	wantAttempt(t, recs[0], 1, 200, "read_error", "WARN")
}

// TestAttemptLogNeverLogsSecrets covers the API key and the request body at
// debug level, for success, non-2xx, unreachable and bad-envelope answers.
func TestAttemptLogNeverLogsSecrets(t *testing.T) {
	const key, note = "SENTINEL-KEY-9f3a", "SENTINEL-NOTE-77c1"
	answers := map[string]func(w http.ResponseWriter){
		"ok":       func(w http.ResponseWriter) { reply(w, good) },
		"non2xx":   func(w http.ResponseWriter) { w.WriteHeader(500); w.Write([]byte("boom")) },
		"envelope": func(w http.ResponseWriter) { w.Write([]byte("not json")) },
		"down":     nil,
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			var auth string
			srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
				auth = r.Header.Get("Authorization")
				if answer != nil {
					answer(w)
				}
			})
			if answer == nil {
				srv.Close()
			}
			ctx, buf := logCtx(slog.LevelDebug)
			in := pickIn()
			in.Note = note
			p := Picker{URL: srv.URL, APIKey: key, Timeout: 5 * time.Second}
			p.Pick(ctx, in)
			if answer != nil && auth != "Bearer "+key {
				t.Errorf("Authorization = %q", auth)
			}
			out := buf.String()
			for _, s := range []string{key, note, "id=t1", "You pick outfits"} {
				if strings.Contains(out, s) {
					t.Errorf("log contains %q", s)
				}
			}
		})
	}
}

func TestAttemptLogNoLoggerInContext(t *testing.T) {
	srv, _ := serve(t, func(n int, w http.ResponseWriter, r *http.Request) { reply(w, good) })
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	if _, err := p.Pick(context.Background(), pickIn()); err != nil {
		t.Fatal(err)
	}
	if len(attempts(t, &buf)) != 1 {
		t.Errorf("default logger got: %s", buf.String())
	}
}
