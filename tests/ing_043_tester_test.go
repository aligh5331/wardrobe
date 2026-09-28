// ING-043 Tester edge cases implied by 07-architecture.md "Weather (Phase 2)"
// and 04-data-schema.md "Settings — weather location". Reuses ing043* helpers.
package tests

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Country trimmed on save; inclusive range boundaries accepted.
func TestING043_Tester_PutTrimAndBoundaries(t *testing.T) {
	e, _ := ing043API(t, "", "")
	rr := ing043Do(t, e, "PUT", "/api/weather/location", `{"name":"\tParis ","country":"  France\n","latitude":48.85341,"longitude":2.3488}`)
	if rr.Code != 200 {
		t.Fatalf("status = %d body %s", rr.Code, rr.Body)
	}
	ing043WantLoc(t, ing043JSON(t, rr), "Paris", "France", 48.85341, 2.3488)
	ing043WantLoc(t, ing043JSON(t, ing043Do(t, e, "GET", "/api/weather/location", "")), "Paris", "France", 48.85341, 2.3488)

	for _, c := range []struct {
		body     string
		lat, lon float64
	}{
		{`{"name":"N","latitude":90,"longitude":180}`, 90, 180},
		{`{"name":"S","latitude":-90,"longitude":-180}`, -90, -180},
		{`{"name":"Z","latitude":0,"longitude":51.4}`, 0, 51.4},
		{`{"name":"Z","latitude":35.6,"longitude":0}`, 35.6, 0},
	} {
		rr := ing043Do(t, e, "PUT", "/api/weather/location", c.body)
		if rr.Code != 200 {
			t.Errorf("PUT %s status = %d body %s, want 200", c.body, rr.Code, rr.Body)
			continue
		}
		m := ing043JSON(t, rr)
		ing043WantLoc(t, m, m["name"].(string), "", c.lat, c.lon)
	}
	// explicit 0 latitude + absent longitude: still names longitude
	if rr := ing043Do(t, e, "PUT", "/api/weather/location", `{"name":"X","latitude":0}`); rr.Code != 400 {
		t.Errorf("lat 0, lon absent status = %d, want 400", rr.Code)
	} else if msg, _ := ing043JSON(t, rr)["error"].(string); !strings.Contains(msg, "longitude") {
		t.Errorf("lat 0, lon absent error = %q, want it to name longitude", msg)
	}
}

// A client-supplied id is ignored, never echoed; the body is exactly the 4 keys.
// Wrong-typed name is 400 naming name.
func TestING043_Tester_PutIgnoresIDAndTypeErrors(t *testing.T) {
	e, _ := ing043API(t, "", "")
	rr := ing043Do(t, e, "PUT", "/api/weather/location", `{"id":7,"ID":7,"name":"Oslo","country":"Norway","latitude":59.9,"longitude":10.7}`)
	if rr.Code != 200 {
		t.Fatalf("status = %d body %s", rr.Code, rr.Body)
	}
	ing043WantLoc(t, ing043JSON(t, rr), "Oslo", "Norway", 59.9, 10.7) // exactly 4 keys, no id
	for _, body := range []string{`{"name":5,"latitude":1,"longitude":1}`, `null`, ``} {
		rr := ing043Do(t, e, "PUT", "/api/weather/location", body)
		if rr.Code != 400 {
			t.Errorf("PUT %q status = %d, want 400", body, rr.Code)
		}
	}
	if msg, _ := ing043JSON(t, ing043Do(t, e, "PUT", "/api/weather/location", `{"name":5,"latitude":1,"longitude":1}`))["error"].(string); !strings.Contains(msg, "name") {
		t.Errorf("name type error = %q, want it to name name", msg)
	}
	ing043WantLoc(t, ing043JSON(t, ing043Do(t, e, "GET", "/api/weather/location", "")), "Oslo", "Norway", 59.9, 10.7)
}

// 502 bodies are JSON; weather routes run concurrently with PUT (exercised under -race).
func TestING043_Tester_ConcurrentAndJSON502(t *testing.T) {
	up := ing043Fake(t, 200, ing043Forecast)
	e, _ := ing043API(t, up.url, up.url)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); ing043Do(t, e, "GET", "/api/weather", "") }()
		go func() {
			defer wg.Done()
			ing043Do(t, e, "PUT", "/api/weather/location", `{"name":"Oslo","latitude":59.9,"longitude":10.7}`)
		}()
	}
	wg.Wait()

	bad, _ := ing043API(t, ing043Fake(t, 503, "").url, "")
	rr := ing043Do(t, bad, "GET", "/api/weather", "")
	if rr.Code != 502 || !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
		t.Errorf("status %d Content-Type %q, want 502 application/json", rr.Code, rr.Header().Get("Content-Type"))
	}
}

// Startup: cmd/server builds the client but never calls Open-Meteo, and adds no env var.
func TestING043_Tester_ServerStartupNoWeatherCall(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "api.WithWeather(weather.New(weather.DefaultForecastBaseURL, weather.DefaultGeocodingBaseURL, weather.DefaultTimeout))") {
		t.Error("cmd/server does not wire the production weather client with the Default* constants")
	}
	for _, bad := range []string{".Forecast(", ".SearchCities(", "Getenv", "WEATHER"} {
		if strings.Contains(s, bad) {
			t.Errorf("cmd/server/main.go contains %q (startup weather call or env var)", bad)
		}
	}
}

// Whitespace around q is trimmed before the upstream call.
func TestING043_Tester_CitiesTrimmedQuery(t *testing.T) {
	up := ing043Fake(t, 200, `{}`)
	e, _ := ing043API(t, "", up.url)
	if rr := ing043Do(t, e, "GET", "/api/weather/cities?q=%20par%09", ""); rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	if q, _ := up.query.Load().(url.Values); q.Get("name") != "par" {
		t.Errorf("upstream name = %q, want %q", q.Get("name"), "par")
	}
}

// Missing current / empty daily is malformed (502); an absent variable is JSON null.
func TestING043_Tester_ForecastMalformedAndAbsent(t *testing.T) {
	for _, body := range []string{
		`{"daily":{"temperature_2m_min":[1],"temperature_2m_max":[2],"precipitation_probability_max":[3],"weather_code":[4]}}`,
		`{"current":{"temperature_2m":1},"daily":{"temperature_2m_min":[],"temperature_2m_max":[],"precipitation_probability_max":[],"weather_code":[]}}`,
	} {
		e, _ := ing043API(t, ing043Fake(t, 200, body).url, "")
		rr := ing043Do(t, e, "GET", "/api/weather", "")
		if rr.Code != 502 {
			t.Errorf("upstream %s status = %d, want 502", body, rr.Code)
		} else if msg, _ := ing043JSON(t, rr)["error"].(string); msg == "" {
			t.Errorf("502 without error message: %s", rr.Body)
		}
	}

	e, _ := ing043API(t, ing043Fake(t, 200, `{"current":{"apparent_temperature":20.1},
 "daily":{"temperature_2m_min":[14.2],"temperature_2m_max":[null],"precipitation_probability_max":[10],"weather_code":[null]}}`).url, "")
	rr := ing043Do(t, e, "GET", "/api/weather", "")
	if rr.Code != 200 {
		t.Fatalf("status = %d body %s", rr.Code, rr.Body)
	}
	m := ing043JSON(t, rr)
	cur, td := m["current"].(map[string]any), m["today"].(map[string]any)
	for _, k := range []string{"temperature_c", "weather_code", "precipitation_mm"} {
		if v, ok := cur[k]; !ok || v != nil {
			t.Errorf("current.%s = %v (present %v), want null", k, v, ok)
		}
	}
	if cur["apparent_temperature_c"] != 20.1 || td["temperature_min_c"] != 14.2 || td["precipitation_probability_max"] != 10.0 {
		t.Errorf("non-null fields changed: current=%v today=%v", cur, td)
	}
	for _, k := range []string{"temperature_max_c", "weather_code"} {
		if v, ok := td[k]; !ok || v != nil {
			t.Errorf("today.%s = %v (present %v), want null", k, v, ok)
		}
	}
}
