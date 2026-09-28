// ING-043: /api/weather, /api/weather/cities, GET/PUT /api/weather/location.
// All Open-Meteo traffic goes to httptest servers; no real network.
package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

const ing043Forecast = `{"current":{"temperature_2m":21.3,"apparent_temperature":20.1,"weather_code":3,"precipitation":0.0},
 "daily":{"temperature_2m_min":[14.2],"temperature_2m_max":[25.8],"precipitation_probability_max":[10],"weather_code":[3]}}`

// ing043Upstream is a fake Open-Meteo server returning status/body and
// recording the last query and call count.
type ing043Upstream struct {
	url   string
	calls atomic.Int32
	query atomic.Value // url.Values
}

func ing043Fake(t *testing.T, status int, body string) *ing043Upstream {
	t.Helper()
	u := &ing043Upstream{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		u.query.Store(r.URL.Query())
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	u.url = srv.URL
	return u
}

func ing043API(t *testing.T, forecastURL, geoURL string) (*gin.Engine, *store.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return api.New(st, t.TempDir(), api.WithWeather(weather.New(forecastURL, geoURL, time.Second))), st
}

func ing043Do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func ing043JSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("body not a JSON object: %v\n%s", err, rr.Body)
	}
	return m
}

func ing043WantLoc(t *testing.T, got any, name, country string, lat, lon float64) {
	t.Helper()
	m, ok := got.(map[string]any)
	if !ok || len(m) != 4 || m["name"] != name || m["country"] != country || m["latitude"] != lat || m["longitude"] != lon {
		t.Errorf("location = %v, want exactly {name:%q country:%q latitude:%v longitude:%v}", got, name, country, lat, lon)
	}
}

// AC1: default location + exact response shape.
func TestING043_AC1_WeatherDefaultShape(t *testing.T) {
	up := ing043Fake(t, 200, ing043Forecast)
	e, _ := ing043API(t, up.url, "")
	rr := ing043Do(t, e, "GET", "/api/weather", "")
	if rr.Code != 200 {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	m := ing043JSON(t, rr)
	if len(m) != 3 {
		t.Errorf("top-level keys = %v, want location/current/today", m)
	}
	ing043WantLoc(t, m["location"], "Tehran", "Iran", 35.69439, 51.42151)
	cur, _ := m["current"].(map[string]any)
	if len(cur) != 4 || cur["temperature_c"] != 21.3 || cur["apparent_temperature_c"] != 20.1 || cur["weather_code"] != 3.0 || cur["precipitation_mm"] != 0.0 {
		t.Errorf("current = %v", cur)
	}
	td, _ := m["today"].(map[string]any)
	if len(td) != 4 || td["temperature_min_c"] != 14.2 || td["temperature_max_c"] != 25.8 || td["precipitation_probability_max"] != 10.0 || td["weather_code"] != 3.0 {
		t.Errorf("today = %v", td)
	}
	if q, _ := up.query.Load().(url.Values); q.Get("latitude") != "35.69439" || q.Get("longitude") != "51.42151" {
		t.Errorf("upstream query = %v, want Tehran coords", q)
	}
}

// AC2: saved location drives the forecast request and the response.
func TestING043_AC2_WeatherUsesSavedLocation(t *testing.T) {
	up := ing043Fake(t, 200, ing043Forecast)
	e, st := ing043API(t, up.url, "")
	if err := st.SaveWeatherLocation(store.WeatherLocation{Name: "Paris", Country: "France", Latitude: 48.85341, Longitude: 2.3488}); err != nil {
		t.Fatal(err)
	}
	rr := ing043Do(t, e, "GET", "/api/weather", "")
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	ing043WantLoc(t, ing043JSON(t, rr)["location"], "Paris", "France", 48.85341, 2.3488)
	if q, _ := up.query.Load().(url.Values); q.Get("latitude") != "48.85341" || q.Get("longitude") != "2.3488" {
		t.Errorf("upstream query = %v, want Paris coords", q)
	}
}

// AC3: null upstream values are JSON null, not 0.
func TestING043_AC3_NullsPassThrough(t *testing.T) {
	up := ing043Fake(t, 200, `{"current":{"temperature_2m":null,"apparent_temperature":20.1,"weather_code":null,"precipitation":0.0},
 "daily":{"temperature_2m_min":[null],"temperature_2m_max":[25.8],"precipitation_probability_max":[null],"weather_code":[3]}}`)
	e, _ := ing043API(t, up.url, "")
	rr := ing043Do(t, e, "GET", "/api/weather", "")
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	m := ing043JSON(t, rr)
	cur, td := m["current"].(map[string]any), m["today"].(map[string]any)
	for k, v := range map[string]any{"temperature_c": nil, "weather_code": nil, "apparent_temperature_c": 20.1, "precipitation_mm": 0.0} {
		if got, ok := cur[k]; !ok || got != v {
			t.Errorf("current.%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
	for k, v := range map[string]any{"temperature_min_c": nil, "precipitation_probability_max": nil, "temperature_max_c": 25.8, "weather_code": 3.0} {
		if got, ok := td[k]; !ok || got != v {
			t.Errorf("today.%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
}

// AC4 + AC8: upstream failures are 502 with a JSON error on both routes.
func TestING043_AC4_AC8_UpstreamFailures502(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	cases := map[string]string{
		"unreachable": downURL,
		"non-2xx":     ing043Fake(t, 500, `{}`).url,
		"unparseable": ing043Fake(t, 200, `not json`).url,
	}
	for name, u := range cases {
		e, _ := ing043API(t, u, u)
		for _, target := range []string{"/api/weather", "/api/weather/cities?q=par"} {
			rr := ing043Do(t, e, "GET", target, "")
			if rr.Code != http.StatusBadGateway {
				t.Errorf("%s %s status = %d, want 502", name, target, rr.Code)
			}
			if msg, _ := ing043JSON(t, rr)["error"].(string); msg == "" {
				t.Errorf("%s %s: no JSON error message: %s", name, target, rr.Body)
			}
		}
	}
}

// AC5 + AC14: up to 10 cities, missing country/admin1 → "", name/count params only.
func TestING043_AC5_AC14_CitiesSearch(t *testing.T) {
	var res []string
	for i := 0; i < 12; i++ {
		res = append(res, `{"name":"Paris","country":"France","admin1":"Île-de-France","latitude":48.85341,"longitude":2.3488}`)
	}
	res[0] = `{"name":"Parma","latitude":44.8,"longitude":10.3}`
	up := ing043Fake(t, 200, `{"results":[`+strings.Join(res, ",")+`]}`)
	e, _ := ing043API(t, "", up.url)
	rr := ing043Do(t, e, "GET", "/api/weather/cities?q=par", "")
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	var got []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Errorf("results = %d, want 10", len(got))
	}
	if g := got[0]; len(g) != 5 || g["country"] != "" || g["admin1"] != "" || g["name"] != "Parma" {
		t.Errorf("first = %v, want Parma with country/admin1 \"\"", g)
	}
	q, _ := up.query.Load().(url.Values)
	if q.Get("name") != "par" || q.Get("count") != "10" || q.Has("language") || len(q) != 2 {
		t.Errorf("upstream query = %v, want name=par&count=10 only", q)
	}
}

// AC6: zero results → [] not null.
func TestING043_AC6_CitiesEmpty(t *testing.T) {
	for _, body := range []string{`{}`, `{"results":[]}`} {
		e, _ := ing043API(t, "", ing043Fake(t, 200, body).url)
		rr := ing043Do(t, e, "GET", "/api/weather/cities?q=zzzz", "")
		if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "[]" {
			t.Errorf("upstream %s: status %d body %q, want 200 []", body, rr.Code, rr.Body)
		}
	}
}

// AC7: missing/empty/whitespace q → 400, no upstream call.
func TestING043_AC7_CitiesBadQuery(t *testing.T) {
	up := ing043Fake(t, 200, `{}`)
	e, _ := ing043API(t, "", up.url)
	for _, target := range []string{"/api/weather/cities", "/api/weather/cities?q=", "/api/weather/cities?q=%20%09"} {
		if rr := ing043Do(t, e, "GET", target, ""); rr.Code != 400 {
			t.Errorf("%s status = %d, want 400", target, rr.Code)
		}
	}
	if n := up.calls.Load(); n != 0 {
		t.Errorf("upstream calls = %d, want 0", n)
	}
}

// AC9, AC10, AC12: default GET, PUT round-trip, Null Island.
func TestING043_AC9_AC10_AC12_Location(t *testing.T) {
	e, _ := ing043API(t, "", "")
	rr := ing043Do(t, e, "GET", "/api/weather/location", "")
	if rr.Code != 200 {
		t.Fatalf("GET status = %d", rr.Code)
	}
	ing043WantLoc(t, ing043JSON(t, rr), "Tehran", "Iran", 35.69439, 51.42151)

	rr = ing043Do(t, e, "PUT", "/api/weather/location", `{"name":" Paris ","country":"France","latitude":48.85341,"longitude":2.3488}`)
	if rr.Code != 200 {
		t.Fatalf("PUT status = %d body %s", rr.Code, rr.Body)
	}
	ing043WantLoc(t, ing043JSON(t, rr), "Paris", "France", 48.85341, 2.3488)
	ing043WantLoc(t, ing043JSON(t, ing043Do(t, e, "GET", "/api/weather/location", "")), "Paris", "France", 48.85341, 2.3488)

	rr = ing043Do(t, e, "PUT", "/api/weather/location", `{"name":"Null Island","latitude":0,"longitude":0}`)
	if rr.Code != 200 {
		t.Fatalf("Null Island status = %d body %s", rr.Code, rr.Body)
	}
	ing043WantLoc(t, ing043JSON(t, rr), "Null Island", "", 0, 0)
}

// AC11 + AC15: invalid bodies (incl. malformed JSON) → 400 naming the field; saved location unchanged.
func TestING043_AC11_AC15_PutInvalid(t *testing.T) {
	e, st := ing043API(t, "", "")
	if err := st.SaveWeatherLocation(store.WeatherLocation{Name: "Paris", Country: "France", Latitude: 48.85341, Longitude: 2.3488}); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ field, body string }{
		{"name", `{"latitude":1,"longitude":1}`},
		{"name", `{"name":"","latitude":1,"longitude":1}`},
		{"name", `{"name":" \t","latitude":1,"longitude":1}`},
		{"latitude", `{"name":"X","longitude":1}`},
		{"longitude", `{"name":"X","latitude":1}`},
		{"latitude", `{"name":"X","latitude":null,"longitude":1}`},
		{"latitude", `{"name":"X","latitude":90.5,"longitude":1}`},
		{"latitude", `{"name":"X","latitude":-91,"longitude":1}`},
		{"longitude", `{"name":"X","latitude":1,"longitude":180.1}`},
		{"longitude", `{"name":"X","latitude":1,"longitude":-181}`},
		{"", `{"name":"X",`},
		{"", `not json`},
		{"latitude", `{"name":"X","latitude":"1","longitude":1}`},
	}
	for _, c := range cases {
		rr := ing043Do(t, e, "PUT", "/api/weather/location", c.body)
		if rr.Code != 400 {
			t.Errorf("PUT %s status = %d, want 400", c.body, rr.Code)
			continue
		}
		if msg, _ := ing043JSON(t, rr)["error"].(string); !strings.Contains(msg, c.field) || msg == "" {
			t.Errorf("PUT %s error = %q, want it to name %q", c.body, msg, c.field)
		}
		ing043WantLoc(t, ing043JSON(t, ing043Do(t, e, "GET", "/api/weather/location", "")), "Paris", "France", 48.85341, 2.3488)
	}
}

// AC13: a DB failure on any location-touching route is 500.
func TestING043_AC13_DBFailure500(t *testing.T) {
	up := ing043Fake(t, 200, ing043Forecast)
	e, st := ing043API(t, up.url, "")
	_ = st.Close()
	for _, c := range []struct{ method, target, body string }{
		{"GET", "/api/weather", ""},
		{"GET", "/api/weather/location", ""},
		{"PUT", "/api/weather/location", `{"name":"Paris","latitude":1,"longitude":1}`},
	} {
		if rr := ing043Do(t, e, c.method, c.target, c.body); rr.Code != 500 {
			t.Errorf("%s %s status = %d, want 500", c.method, c.target, rr.Code)
		}
	}
	if n := up.calls.Load(); n != 0 {
		t.Errorf("forecast called %d times despite DB failure", n)
	}
}

// AC16: weather upstream down leaves catalog routes unaffected.
func TestING043_AC16_CatalogIsolated(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	e, _ := ing043API(t, down.URL, down.URL)
	for _, target := range []string{"/api/items", "/api/taxonomy"} {
		if rr := ing043Do(t, e, "GET", target, ""); rr.Code != 200 {
			t.Errorf("%s status = %d, want 200", target, rr.Code)
		}
	}
}
