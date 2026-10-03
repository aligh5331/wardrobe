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
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"wardrobe/internal/logging"
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
	d := &body.Daily
	check := func() error {
		if body.Current == nil || len(d.Min) == 0 || len(d.Max) == 0 || len(d.PrecipProb) == 0 || len(d.WeatherCode) == 0 {
			return fmt.Errorf("%w: unparseable body: missing current or daily values", ErrUpstream)
		}
		return nil
	}
	if err := c.get(ctx, "forecast", c.forecastBase+"/v1/forecast?"+q.Encode(), &body, check); err != nil {
		return Forecast{}, err
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
	if err := c.get(ctx, "geocoding", c.geocodingBase+"/v1/search?"+q.Encode(), &body, nil); err != nil {
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

// get performs one GET and decodes a 2xx JSON body into out. check, if not
// nil, validates the decoded body; its error is returned and logged as
// "bad_body". Exactly one "weather attempt" record is logged per call.
func (c *Client) get(ctx context.Context, endpoint, u string, out any, check func() error) (err error) {
	start := time.Now()
	status, outcome := 0, "unreachable"
	var body []byte
	var reqURL *url.URL
	var elapsed time.Duration
	defer func() {
		logAttempt(ctx, endpoint, u, reqURL, status, body, elapsed, outcome, err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%w: unreachable: %w", ErrUpstream, err)
	}
	reqURL = req.URL
	resp, err := c.http.Do(req)
	if err != nil {
		elapsed = time.Since(start)
		outcome = logging.Outcome(err, outcome)
		return fmt.Errorf("%w: unreachable or timed out: %w", ErrUpstream, err)
	}
	defer resp.Body.Close()
	status, outcome = resp.StatusCode, "read_error"
	body, err = io.ReadAll(resp.Body)
	elapsed = time.Since(start)
	if err != nil {
		outcome = logging.Outcome(err, outcome)
		return fmt.Errorf("%w: unreachable or timed out: %w", ErrUpstream, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		outcome = "http_error"
		return fmt.Errorf("%w: status %s", ErrUpstream, resp.Status)
	}
	outcome = "bad_body"
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: unparseable body: %w", ErrUpstream, err)
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	outcome = "ok"
	return nil
}

// logAttempt writes the one "weather attempt" record to the context's logger
// (07-architecture.md "Outbound attempt logging"). The request URL carries the
// saved location's coordinates or the search text, so at info only host and
// path are logged; the full URL and body appear in a separate debug record.
// A body snippet is logged only for http_error: a successful forecast body
// starts with the coordinates.
func logAttempt(ctx context.Context, endpoint, rawURL string, reqURL *url.URL, status int, body []byte, elapsed time.Duration, outcome string, err error) {
	l := logging.FromContext(ctx)
	var host, path string
	if reqURL != nil {
		host, path = reqURL.Host, reqURL.Path
	}
	if len(body) > 0 && l.Enabled(ctx, slog.LevelDebug) {
		l.LogAttrs(ctx, slog.LevelDebug, "weather response",
			slog.String("endpoint", endpoint), slog.String("url", rawURL), slog.String("body", string(body)))
	}
	level := slog.LevelInfo
	attrs := []slog.Attr{
		slog.String("endpoint", endpoint),
		slog.Int("attempt", 1),
		slog.Int("status", status),
		slog.Int64("elapsed_ms", elapsed.Milliseconds()),
		slog.Int("body_bytes", len(body)),
		slog.String("outcome", outcome),
		slog.String("host", host),
		slog.String("path", path),
	}
	if outcome == "http_error" {
		attrs = append(attrs, slog.String("snippet", logging.Snippet(body)))
	}
	if outcome != "ok" {
		level = slog.LevelWarn
		if err != nil {
			attrs = append(attrs, slog.String("error", stripURL(err)))
		}
	}
	l.LogAttrs(ctx, level, "weather attempt", attrs...)
}

// stripURL returns err's text without the request URL that *url.Error embeds.
func stripURL(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}
