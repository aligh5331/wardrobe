// ING-053: access-log middleware, request_id, WithLogger option.
package tests

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/logging"
	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

// ing053Engine builds the engine with a JSON logger writing to the returned
// buffer. The store is open unless closeStore is set (then every store call
// fails, giving a 500).
func ing053Engine(t *testing.T, closeStore bool, opts ...api.Option) (*gin.Engine, *bytes.Buffer) {
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
	return api.New(st, t.TempDir(), append(opts, api.WithLogger(l))...), buf
}

// ing053DeadWeather points the weather client at a closed port, so upstream
// calls fail fast with a 502.
func ing053DeadWeather() api.Option {
	return api.WithWeather(weather.New("http://127.0.0.1:1", "http://127.0.0.1:1", time.Second))
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

// Tester addition: the level cases above have no 3xx, so "2xx or 3xx is info"
// was only asserted for 2xx.
func TestING053_RedirectIsInfo(t *testing.T) {
	e, buf := ing053Engine(t, false)
	e.GET("/test/redirect", func(c *gin.Context) { c.Redirect(http.StatusFound, "/api/taxonomy") })
	rr := ing053Do(e, http.MethodGet, "/test/redirect", "", nil)
	rec := ing053One(t, buf)
	if rr.Code != 302 || rec["status"] != float64(302) || rec["level"] != "INFO" {
		t.Errorf("response %d, record status %v level %v, want 302 INFO", rr.Code, rec["status"], rec["level"])
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

// The rest of the 12-route table. Part 1 covered GET /api/items,
// GET /api/items/:id, GET /api/taxonomy, GET /api/weather (503),
// PUT /api/weather/location (400) and POST /api/recommendations (503).
// POST /api/items/photo needs a live VLM for anything past the 503, and
// POST /api/items needs a staged upload, so those get the cheap 503 / 400.
func TestING053_RouteTableRest(t *testing.T) {
	deadLLM := func() api.Option {
		return api.WithRecommender(&recommend.Picker{URL: "http://127.0.0.1:1", Timeout: time.Second})
	}
	const loc = `{"name":"Tehran","country":"IR","latitude":35.69,"longitude":51.42}`
	cases := []struct {
		name, method, target, body string
		opts                       []api.Option
		closeStore                 bool
		status                     int
		level, route               string
	}{
		{"photo missing file", "GET", "/api/photos/nope.jpg", "", nil, false, 404, "WARN", "/api/photos/:filename"},
		{"photo bad name", "GET", "/api/photos/a%5Cb.jpg", "", nil, false, 400, "WARN", "/api/photos/:filename"},
		{"cities no q", "GET", "/api/weather/cities", "", nil, false, 400, "WARN", "/api/weather/cities"},
		{"cities unwired", "GET", "/api/weather/cities?q=x", "", nil, false, 503, "ERROR", "/api/weather/cities"},
		{"cities dead upstream", "GET", "/api/weather/cities?q=x", "", []api.Option{ing053DeadWeather()}, false, 502, "ERROR", "/api/weather/cities"},
		{"location get", "GET", "/api/weather/location", "", nil, false, 200, "INFO", "/api/weather/location"},
		{"location get broken store", "GET", "/api/weather/location", "", nil, true, 500, "ERROR", "/api/weather/location"},
		{"location put ok", "PUT", "/api/weather/location", loc, nil, false, 200, "INFO", "/api/weather/location"},
		{"location put broken store", "PUT", "/api/weather/location", loc, nil, true, 500, "ERROR", "/api/weather/location"},
		{"weather dead upstream", "GET", "/api/weather", "", []api.Option{ing053DeadWeather()}, false, 502, "ERROR", "/api/weather"},
		{"recommendations bad body", "POST", "/api/recommendations", "{", []api.Option{deadLLM()}, false, 400, "WARN", "/api/recommendations"},
		{"items create bad body", "POST", "/api/items", "{", nil, false, 400, "WARN", "/api/items"},
		{"items photo unwired", "POST", "/api/items/photo", "", nil, false, 503, "ERROR", "/api/items/photo"},
		{"item put broken store", "PUT", "/api/items/abc", "{}", nil, true, 500, "ERROR", "/api/items/:id"},
		{"item get broken store", "GET", "/api/items/abc", "", nil, true, 500, "ERROR", "/api/items/:id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, buf := ing053Engine(t, tc.closeStore, tc.opts...)
			rr := ing053Do(e, tc.method, tc.target, tc.body, nil)
			rec := ing053One(t, buf)
			if rr.Code != tc.status || rec["status"] != float64(tc.status) {
				t.Errorf("response %d, record status %v, want %d (body %s)", rr.Code, rec["status"], tc.status, rr.Body.String())
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

func TestING053_NonAPIRequestsLogged(t *testing.T) {
	e, buf := ing053Engine(t, false)
	api.ServeFrontend(e, fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	cases := []struct {
		name, target string
		status       int
		level        string
	}{
		{"static asset", "/assets/app.js", 200, "INFO"},
		{"SPA fallback path", "/wardrobe/closet", 200, "INFO"},
		{"unknown api path", "/api/nope", 404, "WARN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			rr := ing053Do(e, http.MethodGet, tc.target, "", nil)
			rec := ing053One(t, buf)
			if rr.Code != tc.status || rec["status"] != float64(tc.status) || rec["level"] != tc.level {
				t.Errorf("response %d, record status %v level %v, want %d %s", rr.Code, rec["status"], rec["level"], tc.status, tc.level)
			}
			if rec["request_id"] == "" || rec["request_id"] != rr.Header().Get("X-Request-ID") {
				t.Errorf("request_id %v, header %q", rec["request_id"], rr.Header().Get("X-Request-ID"))
			}
			if rec["route"] != "" {
				t.Errorf("route = %v, want empty on the NoRoute path", rec["route"])
			}
		})
	}
}

func TestING053_ItemID(t *testing.T) {
	t.Run("path id on GET and PUT", func(t *testing.T) {
		for _, method := range []string{"GET", "PUT"} {
			e, buf := ing053Engine(t, true)
			rr := ing053Do(e, method, "/api/items/item-xyz", "{}", nil)
			rec := ing053One(t, buf)
			if rr.Code != 500 || rec["level"] != "ERROR" {
				t.Fatalf("%s: response %d level %v, want 500 ERROR", method, rr.Code, rec["level"])
			}
			if rec["item_id"] != "item-xyz" || rec["request_id"] == "" {
				t.Errorf("%s: item_id %v request_id %v, want item-xyz and an id", method, rec["item_id"], rec["request_id"])
			}
		}
	})
	t.Run("c.Set fallback", func(t *testing.T) {
		e, buf := ing053Engine(t, false)
		e.GET("/test/set", func(c *gin.Context) {
			c.Set("item_id", "x")
			c.Status(http.StatusInternalServerError)
		})
		ing053Do(e, http.MethodGet, "/test/set", "", nil)
		rec := ing053One(t, buf)
		if rec["item_id"] != "x" || rec["request_id"] == "" || rec["level"] != "ERROR" {
			t.Errorf("record = %v, want ERROR with item_id x and a request_id", rec)
		}
	})
	t.Run("absent without Set", func(t *testing.T) {
		e, buf := ing053Engine(t, false)
		rr := ing053Do(e, http.MethodGet, "/api/weather", "", nil)
		rec := ing053One(t, buf)
		if rr.Code != 503 {
			t.Fatalf("status = %d, want 503", rr.Code)
		}
		if _, ok := rec["item_id"]; ok {
			t.Errorf("item_id present without c.Set: %v", rec)
		}
	})
}

func TestING053_PanicLoggedAs500(t *testing.T) {
	e, buf := ing053Engine(t, false)
	e.GET("/test/panic", func(c *gin.Context) { panic("boom") })
	rr := ing053Do(e, http.MethodGet, "/test/panic", "", nil)
	if rr.Code != 500 {
		t.Errorf("client status = %d, want 500", rr.Code)
	}
	rec := ing053One(t, buf)
	if rec["status"] != float64(500) || rec["level"] != "ERROR" || rec["route"] != "/test/panic" {
		t.Errorf("record = %v, want ERROR status 500 route /test/panic", rec)
	}
}

func TestING053_DefaultsToSlogDefault(t *testing.T) {
	old := slog.Default()
	defBuf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewJSONHandler(defBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	gin.SetMode(gin.TestMode)
	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := api.New(st, t.TempDir()) // no WithLogger

	if n := len(e.Routes()); n != 12 {
		t.Errorf("routes = %d, want 12", n)
	}
	rr := ing053Do(e, http.MethodGet, "/api/taxonomy", "", nil)
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	rec := ing053One(t, defBuf)
	if rec["msg"] != "http request" || rec["request_id"] != rr.Header().Get("X-Request-ID") {
		t.Errorf("default-logger record = %v, want msg \"http request\" and the response request_id", rec)
	}
}
