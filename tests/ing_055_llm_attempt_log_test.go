// ING-055: the recommendations 502 path logs "recommendation failed", and it
// shares one request_id with the access log and the "llm attempt" record.
package tests

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

func TestING055_RecommendationFailedSharesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, it := range g48Items() {
		if err := st.Insert(it); err != nil {
			t.Fatal(err)
		}
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()

	buf := &bytes.Buffer{}
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	e := api.New(st, t.TempDir(), api.WithLogger(l),
		api.WithWeather(weather.New(ing043Fake(t, 200, e49Mild).url, "", time.Second)),
		api.WithRecommender(&recommend.Picker{URL: dead.URL, Timeout: 5 * time.Second}))

	rr := e49Post(e, strings.NewReader("{}"))
	msg := e49Err(t, rr, http.StatusBadGateway, "llm failure")
	if got := ing043JSON(t, rr)["error"]; got != msg {
		t.Fatalf("body error changed: %v", got)
	}

	rid := rr.Header().Get("X-Request-ID")
	if rid == "" {
		t.Fatal("no X-Request-ID header")
	}
	seen := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		seen[rec["msg"].(string)] = rec
	}
	for _, m := range []string{"llm attempt", "recommendation failed", "http request"} {
		rec := seen[m]
		if rec == nil {
			t.Errorf("no %q record in:\n%s", m, buf)
			continue
		}
		if rec["request_id"] != rid {
			t.Errorf("%q request_id = %v, want %q", m, rec["request_id"], rid)
		}
	}
	if rec := seen["recommendation failed"]; rec != nil && (rec["level"] != "ERROR" || rec["error"] != msg) {
		t.Errorf("recommendation failed record = %v", rec)
	}
	if rec := seen["llm attempt"]; rec != nil && rec["outcome"] != "unreachable" {
		t.Errorf("llm attempt record = %v", rec)
	}
}
