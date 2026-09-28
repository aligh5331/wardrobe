package weather

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const forecastBody = `{
 "current":{"temperature_2m":21.3,"apparent_temperature":20.1,"weather_code":3,"precipitation":0.4},
 "daily":{"temperature_2m_min":[14.2],"temperature_2m_max":[25.8],"precipitation_probability_max":[10],"weather_code":[61]}}`

func serve(t *testing.T, status int, body string) (*httptest.Server, *int32, *url.URL) {
	t.Helper()
	var hits int32
	var got url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		got = *r.URL
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &got
}

func TestForecast(t *testing.T) {
	srv, hits, got := serve(t, 200, forecastBody)
	f, err := New(srv.URL, "", time.Second).Forecast(context.Background(), 35.69439, 51.42151)
	if err != nil {
		t.Fatal(err)
	}
	if *hits != 1 || got.Path != "/v1/forecast" {
		t.Fatalf("hits=%d path=%s", *hits, got.Path)
	}
	want := url.Values{
		"latitude":      {"35.69439"},
		"longitude":     {"51.42151"},
		"current":       {"temperature_2m,apparent_temperature,weather_code,precipitation"},
		"daily":         {"temperature_2m_min,temperature_2m_max,precipitation_probability_max,weather_code"},
		"timezone":      {"auto"},
		"forecast_days": {"1"},
	}
	if q := got.Query(); q.Encode() != want.Encode() {
		t.Errorf("query = %s\nwant    %s", q.Encode(), want.Encode())
	}
	exp := Forecast{Current{p(21.3), p(20.1), p(3), p(0.4)}, Today{p(14.2), p(25.8), p(10), p(61)}}
	if !reflect.DeepEqual(f, exp) {
		t.Errorf("got %+v, want %+v", f, exp)
	}
}

func p[T any](v T) *T { return &v }

// null/absent variables are missing data, not an error: nil, JSON null (07-architecture.md).
func TestForecastNulls(t *testing.T) {
	srv, _, _ := serve(t, 200, `{
 "current":{"temperature_2m":null,"weather_code":3},
 "daily":{"temperature_2m_min":[null],"temperature_2m_max":[25.8],"precipitation_probability_max":[null],"weather_code":[null]}}`)
	f, err := New(srv.URL, "", time.Second).Forecast(context.Background(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	exp := Forecast{Current{nil, nil, p(3), nil}, Today{nil, p(25.8), nil, nil}}
	if !reflect.DeepEqual(f, exp) {
		t.Errorf("got %+v, want %+v", f, exp)
	}
	b, _ := json.Marshal(f)
	if want := `{"current":{"temperature_c":null,"apparent_temperature_c":null,"weather_code":3,"precipitation_mm":null},"today":{"temperature_min_c":null,"temperature_max_c":25.8,"precipitation_probability_max":null,"weather_code":null}}`; string(b) != want {
		t.Errorf("json = %s\nwant   %s", b, want)
	}
}

func TestSearchCities(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"results":[{"name":"Tehran","country":"Iran","admin1":"Tehran","latitude":35.69439,"longitude":51.42151,"id":1}`)
	for i := 0; i < 11; i++ {
		sb.WriteString(`,{"name":"X","latitude":1,"longitude":2}`)
	}
	sb.WriteString(`]}`)
	srv, _, got := serve(t, 200, sb.String())
	cs, err := New("", srv.URL, time.Second).SearchCities(context.Background(), "Teh ran")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/v1/search" || got.Query().Encode() != "count=10&name=Teh+ran" {
		t.Errorf("request = %s?%s", got.Path, got.RawQuery)
	}
	if len(cs) != 10 {
		t.Fatalf("len = %d, want 10", len(cs))
	}
	if cs[0] != (City{"Tehran", "Iran", "Tehran", 35.69439, 51.42151}) || cs[1] != (City{"X", "", "", 1, 2}) {
		t.Errorf("got %+v, %+v", cs[0], cs[1])
	}
}

func TestSearchCitiesEmpty(t *testing.T) {
	for _, body := range []string{`{}`, `{"results":[]}`, `{"generationtime_ms":0.1}`} {
		srv, _, _ := serve(t, 200, body)
		cs, err := New("", srv.URL, time.Second).SearchCities(context.Background(), "zzz")
		if err != nil || cs == nil || len(cs) != 0 {
			t.Errorf("body %s: cs=%#v err=%v", body, cs, err)
		}
	}
}

func TestUpstreamErrors(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	bad, _, _ := serve(t, 500, `{"error":true}`)
	garbage, _, _ := serve(t, 200, `not json`)
	partial, _, _ := serve(t, 200, `{"current":{"temperature_2m":1},"daily":{}}`)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)

	cases := []struct {
		name, base, msg string
		forecastOnly    bool
	}{
		{"closed", closed.URL, "unreachable", false},
		{"non2xx", bad.URL, "status 500", false},
		{"garbage", garbage.URL, "unparseable", false},
		{"partial", partial.URL, "unparseable", true},
		{"timeout", slow.URL, "timed out", false},
	}
	for _, tc := range cases {
		c := New(tc.base, tc.base, 100*time.Millisecond)
		start := time.Now()
		f, err := c.Forecast(context.Background(), 1, 2)
		check(t, tc.name+"/forecast", err, tc.msg)
		if !reflect.DeepEqual(f, Forecast{}) {
			t.Errorf("%s/forecast: partial result %+v", tc.name, f)
		}
		if tc.forecastOnly {
			continue
		}
		cs, err := c.SearchCities(context.Background(), "x")
		check(t, tc.name+"/search", err, tc.msg)
		if cs != nil {
			t.Errorf("%s/search: partial result %+v", tc.name, cs)
		}
		if tc.name == "timeout" && time.Since(start) > time.Second {
			t.Errorf("timeout not enforced: %v", time.Since(start))
		}
	}
}

func check(t *testing.T, name string, err error, msg string) {
	t.Helper()
	if !errors.Is(err, ErrUpstream) || !strings.Contains(err.Error(), msg) {
		t.Errorf("%s: err = %v, want ErrUpstream containing %q", name, err, msg)
	}
}

func TestDefaults(t *testing.T) {
	if DefaultTimeout != 10*time.Second ||
		DefaultForecastBaseURL != "https://api.open-meteo.com" ||
		DefaultGeocodingBaseURL != "https://geocoding-api.open-meteo.com" {
		t.Error("production defaults drifted from 06-decisions.md / 07-architecture.md")
	}
}
