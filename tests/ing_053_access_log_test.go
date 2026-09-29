// ING-053 part 1: access-log middleware, request_id, WithLogger option.
package tests

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/logging"
	"wardrobe/internal/store"
)

// ing053Engine builds the engine with a JSON logger writing to the returned
// buffer. The store is open unless closeStore is set (then every store call
// fails, giving a 500).
func ing053Engine(t *testing.T, closeStore bool) (*gin.Engine, *bytes.Buffer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if closeStore {
		_ = st.Close()
	}
	buf := &bytes.Buffer{}
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return api.New(st, t.TempDir(), api.WithLogger(l)), buf
}

func ing053Do(h http.Handler, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func ing053Records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func ing053One(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	recs := ing053Records(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d log records, want 1: %s", len(recs), buf.String())
	}
	return recs[0]
}

func TestING053_OneRecordWithFieldSet(t *testing.T) {
	e, buf := ing053Engine(t, false)
	rr := ing053Do(e, http.MethodGet, "/api/taxonomy", "", nil)
	rec := ing053One(t, buf)

	want := map[string]any{"method": "GET", "route": "/api/taxonomy", "status": float64(200)}
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("%s = %v, want %v", k, rec[k], v)
		}
	}
	if rec["size"] != float64(rr.Body.Len()) || rec["size"].(float64) == 0 {
		t.Errorf("size = %v, want body length %d", rec["size"], rr.Body.Len())
	}
	for _, k := range []string{"latency", "client_ip", "request_id"} {
		if v, ok := rec[k]; !ok || v == "" {
			t.Errorf("attribute %q missing or empty in %v", k, rec)
		}
	}
	if _, ok := rec["item_id"]; ok {
		t.Errorf("item_id present on a 200 record")
	}
}

func TestING053_LevelPerStatusClass(t *testing.T) {
	cases := []struct {
		name, method, target, body string
		closeStore                 bool
		status                     int
		level, route               string
	}{
		{"2xx taxonomy", "GET", "/api/taxonomy", "", false, 200, "INFO", "/api/taxonomy"},
		{"2xx list", "GET", "/api/items", "", false, 200, "INFO", "/api/items"},
		{"4xx unknown item", "GET", "/api/items/nope", "", false, 404, "WARN", "/api/items/:id"},
		{"4xx bad body", "PUT", "/api/weather/location", "{", false, 400, "WARN", "/api/weather/location"},
		{"5xx unwired weather", "GET", "/api/weather", "", false, 503, "ERROR", "/api/weather"},
		{"5xx unwired recommender", "POST", "/api/recommendations", "{}", false, 503, "ERROR", "/api/recommendations"},
		{"5xx broken store list", "GET", "/api/items", "", true, 500, "ERROR", "/api/items"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, buf := ing053Engine(t, tc.closeStore)
			rr := ing053Do(e, tc.method, tc.target, tc.body, nil)
			rec := ing053One(t, buf)
			if rr.Code != tc.status || rec["status"] != float64(tc.status) {
				t.Errorf("response %d, record status %v, want %d", rr.Code, rec["status"], tc.status)
			}
			if rec["level"] != tc.level {
				t.Errorf("level = %v, want %s", rec["level"], tc.level)
			}
			if rec["route"] != tc.route {
				t.Errorf("route = %v, want %s", rec["route"], tc.route)
			}
		})
	}
}

func TestING053_RequestIDRoundTrip(t *testing.T) {
	e, buf := ing053Engine(t, false)

	rr1 := ing053Do(e, http.MethodGet, "/api/taxonomy", "", nil)
	id1 := rr1.Header().Get("X-Request-ID")
	if id1 == "" {
		t.Fatal("no X-Request-ID response header")
	}
	if got := ing053One(t, buf)["request_id"]; got != id1 {
		t.Errorf("record request_id = %v, header = %q", got, id1)
	}

	buf.Reset()
	rr2 := ing053Do(e, http.MethodGet, "/api/taxonomy", "", map[string]string{"X-Request-ID": "abc"})
	id2 := rr2.Header().Get("X-Request-ID")
	if id2 == "" || id2 == "abc" {
		t.Errorf("response X-Request-ID = %q, want a server-generated id", id2)
	}
	if got := ing053One(t, buf)["request_id"]; got != id2 {
		t.Errorf("record request_id = %v, header = %q", got, id2)
	}
	if id1 == id2 {
		t.Errorf("two requests share request_id %q", id1)
	}
}

func TestING053_ContextLoggerSharesRequestID(t *testing.T) {
	// A record that reached slog.Default() would fail the last check.
	old := slog.Default()
	defBuf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewJSONHandler(defBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	e, buf := ing053Engine(t, false)
	e.GET("/test/ctx", func(c *gin.Context) {
		logging.FromContext(c.Request.Context()).Info("from handler")
		c.Status(http.StatusOK)
	})
	rr := ing053Do(e, http.MethodGet, "/test/ctx", "", nil)

	recs := ing053Records(t, buf)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want handler record + access record: %s", len(recs), buf.String())
	}
	handlerRec, accessRec := recs[0], recs[1]
	if handlerRec["msg"] != "from handler" {
		t.Fatalf("first record msg = %v, want the handler's", handlerRec["msg"])
	}
	id := rr.Header().Get("X-Request-ID")
	if handlerRec["request_id"] != id || accessRec["request_id"] != id || id == "" {
		t.Errorf("request_ids: handler %v, access %v, header %q; want all equal", handlerRec["request_id"], accessRec["request_id"], id)
	}
	if defBuf.Len() != 0 {
		t.Errorf("slog.Default() received records: %s", defBuf.String())
	}
}
