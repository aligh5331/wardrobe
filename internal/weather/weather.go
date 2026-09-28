// Package weather is the backend-only Open-Meteo client: today's forecast
// for a coordinate and city search via geocoding (07-architecture.md
// "Weather (Phase 2)", 06-decisions.md "Weather provider: Open-Meteo,
// backend-proxied, no API key"). No API key, no cache.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Production defaults (06-decisions.md, 07-architecture.md).
const (
	DefaultForecastBaseURL  = "https://api.open-meteo.com"
	DefaultGeocodingBaseURL = "https://geocoding-api.open-meteo.com"
	DefaultTimeout          = 10 * time.Second
)

// maxCities is the geocoding result cap (07-architecture.md).
const maxCities = 10

// ErrUpstream marks every Open-Meteo failure: unreachable/timeout, non-2xx
// status, or unparseable body. The wrapped message says which.
var ErrUpstream = errors.New("weather upstream failure")

// Current is the current-conditions block; weather_code is the raw WMO code.
// nil = Open-Meteo returned null/absent (missing data, not an error;
// 07-architecture.md) and serializes as JSON null, never 0.
type Current struct {
	TemperatureC         *float64 `json:"temperature_c"`
	ApparentTemperatureC *float64 `json:"apparent_temperature_c"`
	WeatherCode          *int     `json:"weather_code"`
	PrecipitationMM      *float64 `json:"precipitation_mm"`
}

// Today is today's daily forecast (Open-Meteo daily index 0); nil as in Current.
type Today struct {
	TemperatureMinC             *float64 `json:"temperature_min_c"`
	TemperatureMaxC             *float64 `json:"temperature_max_c"`
	PrecipitationProbabilityMax *int     `json:"precipitation_probability_max"`
	WeatherCode                 *int     `json:"weather_code"`
}

// Forecast is the current + today pair returned by Client.Forecast.
type Forecast struct {
	Current Current `json:"current"`
	Today   Today   `json:"today"`
}

// City is one geocoding search result; missing country/admin1 are "".
type City struct {
	Name      string  `json:"name"`
	Country   string  `json:"country"`
	Admin1    string  `json:"admin1"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Client calls Open-Meteo. Safe for concurrent use.
type Client struct {
	forecastBase  string
	geocodingBase string
	http          *http.Client
}

// New returns a Client for the given base URLs (scheme+host, no path) with
// the given outbound timeout. Production: New(DefaultForecastBaseURL,
// DefaultGeocodingBaseURL, DefaultTimeout).
func New(forecastBase, geocodingBase string, timeout time.Duration) *Client {
	return &Client{
		forecastBase:  strings.TrimRight(forecastBase, "/"),
		geocodingBase: strings.TrimRight(geocodingBase, "/"),
		http:          &http.Client{Timeout: timeout},
	}
}

// Forecast fetches current conditions and today's forecast for lat/lon.
func (c *Client) Forecast(ctx context.Context, lat, lon float64) (Forecast, error) {
	q := url.Values{
		"latitude":      {strconv.FormatFloat(lat, 'f', -1, 64)},
		"longitude":     {strconv.FormatFloat(lon, 'f', -1, 64)},
		"current":       {"temperature_2m,apparent_temperature,weather_code,precipitation"},
		"daily":         {"temperature_2m_min,temperature_2m_max,precipitation_probability_max,weather_code"},
		"timezone":      {"auto"},
		"forecast_days": {"1"},
	}
	var body struct {
		Current *struct {
			Temperature2m       *float64 `json:"temperature_2m"`
			ApparentTemperature *float64 `json:"apparent_temperature"`
			WeatherCode         *int     `json:"weather_code"`
			Precipitation       *float64 `json:"precipitation"`
		} `json:"current"`
		Daily struct {
			Min         []*float64 `json:"temperature_2m_min"`
			Max         []*float64 `json:"temperature_2m_max"`
			PrecipProb  []*int     `json:"precipitation_probability_max"`
			WeatherCode []*int     `json:"weather_code"`
		} `json:"daily"`
	}
	if err := c.get(ctx, c.forecastBase+"/v1/forecast?"+q.Encode(), &body); err != nil {
		return Forecast{}, err
	}
	d := body.Daily
	if body.Current == nil || len(d.Min) == 0 || len(d.Max) == 0 || len(d.PrecipProb) == 0 || len(d.WeatherCode) == 0 {
		return Forecast{}, fmt.Errorf("%w: unparseable body: missing current or daily values", ErrUpstream)
	}
	cur := body.Current
	return Forecast{
		Current: Current{cur.Temperature2m, cur.ApparentTemperature, cur.WeatherCode, cur.Precipitation},
		Today:   Today{d.Min[0], d.Max[0], d.PrecipProb[0], d.WeatherCode[0]},
	}, nil
}

// SearchCities returns up to 10 geocoding matches for text; zero matches is
// an empty, non-nil slice. Input validation (empty q) is the caller's job.
func (c *Client) SearchCities(ctx context.Context, text string) ([]City, error) {
	q := url.Values{"name": {text}, "count": {strconv.Itoa(maxCities)}}
	var body struct {
		Results []City `json:"results"`
	}
	if err := c.get(ctx, c.geocodingBase+"/v1/search?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	if len(body.Results) > maxCities {
		body.Results = body.Results[:maxCities]
	}
	if body.Results == nil {
		return []City{}, nil
	}
	return body.Results, nil
}

// get performs one GET and decodes a 2xx JSON body into out.
func (c *Client) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%w: unreachable: %w", ErrUpstream, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: unreachable or timed out: %w", ErrUpstream, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: unreachable or timed out: %w", ErrUpstream, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: status %s", ErrUpstream, resp.Status)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%w: unparseable body: %w", ErrUpstream, err)
	}
	return nil
}
