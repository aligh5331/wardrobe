// ING-056: Open-Meteo attempt records share the request_id of the access log.
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

func ing056Records(t *testing.T, buf *bytes.Buffer) map[string]map[string]any {
	t.Helper()
	seen := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		seen[rec["msg"].(string)] = rec
	}
	return seen
}

func ing056Engine(t *testing.T, w *weather.Client, opts ...api.Option) (*gin.Engine, *bytes.Buffer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	buf := &bytes.Buffer{}
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	for _, it := range g48Items() {
		if err := st.Insert(it); err != nil {
			t.Fatal(err)
		}
	}
	return api.New(st, t.TempDir(), append([]api.Option{api.WithLogger(l), api.WithWeather(w)}, opts...)...), buf
}

func TestING056_WeatherAttemptSharesRequestID(t *testing.T) {
	forecast := ing043Fake(t, 200, ing043Forecast)
	geo := ing043Fake(t, 200, `{"results":[{"name":"Paris","country":"France","latitude":48.85,"longitude":2.35}]}`)
	for _, tc := range []struct{ target, endpoint string }{
		{"/api/weather", "forecast"},
		{"/api/weather/cities?q=paris", "geocoding"},
	} {
		e, buf := ing056Engine(t, weather.New(forecast.url, geo.url, time.Second))
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.target, nil))
		if rr.Code != 200 {
			t.Fatalf("%s: status %d: %s", tc.target, rr.Code, rr.Body)
		}
		rid := rr.Header().Get("X-Request-ID")
		if rid == "" {
			t.Fatalf("%s: no X-Request-ID", tc.target)
		}
		seen := ing056Records(t, buf)
		for _, m := range []string{"weather attempt", "http request"} {
			rec := seen[m]
			if rec == nil {
				t.Errorf("%s: no %q record in:\n%s", tc.target, m, buf)
			} else if rec["request_id"] != rid {
				t.Errorf("%s: %q request_id = %v, want %q", tc.target, m, rec["request_id"], rid)
			}
		}
		if rec := seen["weather attempt"]; rec != nil && (rec["endpoint"] != tc.endpoint || rec["outcome"] != "ok") {
			t.Errorf("%s: weather attempt = %v", tc.target, rec)
		}
	}
}

func TestING056_RecommendationsWeatherAttemptSharesRequestID(t *testing.T) {
	e, buf := ing056Engine(t, weather.New(ing043Fake(t, 503, `{"error":true}`).url, "", time.Second),
		api.WithRecommender(&recommend.Picker{URL: "http://127.0.0.1:1"}))
	rr := e49Post(e, strings.NewReader("{}"))
	rid := rr.Header().Get("X-Request-ID")
	rec := ing056Records(t, buf)["weather attempt"]
	if rec == nil {
		t.Fatalf("no weather attempt record: %s (status %d)", buf, rr.Code)
	}
	if rid == "" || rec["request_id"] != rid || rec["outcome"] != "http_error" {
		t.Errorf("request_id = %v, want %q; record %v", rec["request_id"], rid, rec)
	}
}
