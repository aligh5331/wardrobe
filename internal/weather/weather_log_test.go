package weather

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wardrobe/internal/logging"
)

const coordBody = `{"latitude":12.3456,"longitude":65.4321,"current":{"temperature_2m":21.3,"apparent_temperature":20.1,"weather_code":3,"precipitation":0.4},` +
	`"daily":{"temperature_2m_min":[14.2],"temperature_2m_max":[25.8],"precipitation_probability_max":[10],"weather_code":[61]}}`

func logCtx(level slog.Level) (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level}))
	return logging.WithLogger(context.Background(), l), &buf
}

func records(t *testing.T, buf *bytes.Buffer, msg string) []map[string]any {
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
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

func one(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	recs := records(t, buf, "weather attempt")
	if len(recs) != 1 {
		t.Fatalf("got %d attempt records, want 1: %s", len(recs), buf)
	}
	return recs[0]
}

func TestAttemptLogOK(t *testing.T) {
	srv, _, _ := serve(t, 200, coordBody)
	ctx, buf := logCtx(slog.LevelInfo)
	if _, err := New(srv.URL, "", time.Second).Forecast(ctx, 12.3456, 65.4321); err != nil {
		t.Fatal(err)
	}
	rec := one(t, buf)
	if rec["endpoint"] != "forecast" || rec["attempt"] != 1.0 || rec["status"] != 200.0 || rec["outcome"] != "ok" || rec["level"] != "INFO" {
		t.Errorf("record = %v", rec)
	}
	if rec["body_bytes"] != float64(len(coordBody)) || rec["path"] != "/v1/forecast" || rec["host"] != strings.TrimPrefix(srv.URL, "http://") {
		t.Errorf("record = %v", rec)
	}
	if _, ok := rec["elapsed_ms"]; !ok {
		t.Error("no elapsed_ms")
	}
	if _, ok := rec["snippet"]; ok {
		t.Error("ok record has a snippet")
	}
	for _, bad := range []string{"12.3456", "65.4321", "latitude=", "?"} {
		if strings.Contains(buf.String(), bad) {
			t.Errorf("info log contains %q: %s", bad, buf)
		}
	}
}

func TestAttemptLogGeocoding(t *testing.T) {
	srv, _, _ := serve(t, 200, `{"results":[{"name":"Paris"}]}`)
	ctx, buf := logCtx(slog.LevelInfo)
	if _, err := New("", srv.URL, time.Second).SearchCities(ctx, "SENTINEL-CITY-51ab"); err != nil {
		t.Fatal(err)
	}
	rec := one(t, buf)
	if rec["endpoint"] != "geocoding" || rec["outcome"] != "ok" || rec["path"] != "/v1/search" {
		t.Errorf("record = %v", rec)
	}
	if strings.Contains(buf.String(), "SENTINEL-CITY") {
		t.Errorf("search text in info log: %s", buf)
	}
	ctx, buf = logCtx(slog.LevelDebug)
	if _, err := New("", srv.URL, time.Second).SearchCities(ctx, "SENTINEL-CITY-51ab"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "SENTINEL-CITY-51ab") {
		t.Errorf("debug log lacks the full URL: %s", buf)
	}
}

func TestAttemptLogOutcomes(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	drop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		w.Write([]byte(`{"latitude":12.3456,`))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(drop.Close)
	e503, _, _ := serve(t, 503, `{"error":true,"reason":"overloaded"}`)
	garbage, _, _ := serve(t, 200, `not json 12.3456`)
	nocur, _, _ := serve(t, 200, `{"latitude":12.3456,"longitude":65.4321,"daily":{}}`)

	cases := []struct {
		name, base, outcome string
		status              int
		snippet             bool
	}{
		{"unreachable", closed.URL, "unreachable", 0, false},
		{"timeout", slow.URL, "timeout", 0, false},
		{"read_error", drop.URL, "read_error", 200, false},
		{"http_error", e503.URL, "http_error", 503, true},
		{"bad_body", garbage.URL, "bad_body", 200, false},
		{"bad_body_no_current", nocur.URL, "bad_body", 200, false},
	}
	for _, tc := range cases {
		ctx, buf := logCtx(slog.LevelInfo)
		_, err := New(tc.base, tc.base, 100*time.Millisecond).Forecast(ctx, 12.3456, 65.4321)
		if err == nil {
			t.Fatalf("%s: no error", tc.name)
		}
		rec := one(t, buf)
		if rec["outcome"] != tc.outcome || rec["status"] != float64(tc.status) || rec["level"] != "WARN" || rec["attempt"] != 1.0 {
			t.Errorf("%s: record = %v", tc.name, rec)
		}
		if _, ok := rec["snippet"]; ok != tc.snippet {
			t.Errorf("%s: snippet present = %v, want %v", tc.name, ok, tc.snippet)
		}
		if tc.snippet && !strings.Contains(rec["snippet"].(string), "overloaded") {
			t.Errorf("%s: snippet = %v", tc.name, rec["snippet"])
		}
		if _, ok := rec["error"]; !ok {
			t.Errorf("%s: no error attribute", tc.name)
		}
		for _, bad := range []string{"12.3456", "65.4321", "latitude=", "?"} {
			// the read_error body prefix is never logged, only counted
			if strings.Contains(buf.String(), bad) {
				t.Errorf("%s: info log contains %q: %s", tc.name, bad, buf)
			}
		}
	}
}

func TestAttemptLogDebugBody(t *testing.T) {
	srv, _, _ := serve(t, 200, coordBody)
	ctx, buf := logCtx(slog.LevelDebug)
	if _, err := New(srv.URL, "", time.Second).Forecast(ctx, 12.3456, 65.4321); err != nil {
		t.Fatal(err)
	}
	recs := records(t, buf, "weather response")
	if len(recs) != 1 || recs[0]["level"] != "DEBUG" || recs[0]["body"] != coordBody ||
		!strings.Contains(recs[0]["url"].(string), "latitude=12.3456") {
		t.Errorf("debug records = %v", recs)
	}
}

func TestAttemptLogNoLoggerInContext(t *testing.T) {
	srv, _, _ := serve(t, 200, forecastBody)
	if _, err := New(srv.URL, "", time.Second).Forecast(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptLogCanceled(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(slow.Close)
	ctx, buf := logCtx(slog.LevelDebug)
	ctx, cancel := context.WithCancel(ctx)
	time.AfterFunc(30*time.Millisecond, cancel)
	if _, err := New(slow.URL, "", 5*time.Second).Forecast(ctx, 1, 2); err == nil {
		t.Fatal("no error")
	}
	if rec := one(t, buf); rec["outcome"] != "canceled" {
		t.Errorf("record = %v", rec)
	}
	if n := len(records(t, buf, "weather response")); n != 0 {
		t.Errorf("%d debug body records for a call with no response", n)
	}
}
