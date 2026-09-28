// ING-041: internal/weather Open-Meteo client. Tester gap-fill on top of
// internal/weather/weather_test.go: (1) daily values come from index 0
// even when upstream returns more than one day; (2) the Forecast and City
// JSON key sets are exactly the 07-architecture.md "Weather (Phase 2)"
// contract. All HTTP goes to httptest; no real network.
package tests

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"wardrobe/internal/weather"
)

func ing041Server(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func ing041Keys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return slices.Sorted(maps.Keys(m))
}

func TestING041_DailyIndexZeroAndWireKeys(t *testing.T) {
	base := ing041Server(t, `{
 "current":{"temperature_2m":21.3,"apparent_temperature":20.1,"weather_code":3,"precipitation":0.0},
 "daily":{"temperature_2m_min":[14.2,1],"temperature_2m_max":[25.8,2],
          "precipitation_probability_max":[10,99],"weather_code":[3,95]}}`)
	f, err := weather.New(base, "", time.Second).Forecast(context.Background(), 35.69439, 51.42151)
	if err != nil {
		t.Fatal(err)
	}
	td := f.Today
	if td.TemperatureMinC == nil || *td.TemperatureMinC != 14.2 || td.TemperatureMaxC == nil || *td.TemperatureMaxC != 25.8 ||
		td.PrecipitationProbabilityMax == nil || *td.PrecipitationProbabilityMax != 10 || td.WeatherCode == nil || *td.WeatherCode != 3 {
		t.Errorf("today = %+v, want index 0 {14.2 25.8 10 3}", td)
	}
	if got, want := ing041Keys(t, f.Current), []string{"apparent_temperature_c", "precipitation_mm", "temperature_c", "weather_code"}; !slices.Equal(got, want) {
		t.Errorf("current keys = %v, want %v", got, want)
	}
	if got, want := ing041Keys(t, f.Today), []string{"precipitation_probability_max", "temperature_max_c", "temperature_min_c", "weather_code"}; !slices.Equal(got, want) {
		t.Errorf("today keys = %v, want %v", got, want)
	}
}

func TestING041_CityWireKeys(t *testing.T) {
	base := ing041Server(t, `{"results":[{"id":1,"name":"Tehran","country":"Iran","admin1":"Tehran","latitude":35.69439,"longitude":51.42151,"timezone":"Asia/Tehran"}]}`)
	cs, err := weather.New("", base, time.Second).SearchCities(context.Background(), "Tehran")
	if err != nil || len(cs) != 1 {
		t.Fatalf("cs=%v err=%v", cs, err)
	}
	if got, want := ing041Keys(t, cs[0]), []string{"admin1", "country", "latitude", "longitude", "name"}; !slices.Equal(got, want) {
		t.Errorf("city keys = %v, want %v", got, want)
	}
}
