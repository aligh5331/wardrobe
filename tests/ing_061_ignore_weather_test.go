// ING-061: explained missing-slot 422 and the ignore_weather request field.
// Reuses the ING-049 harness (e49*) and the g48 fixtures.
package tests

import (
	"strings"
	"testing"
	"time"

	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
)

// Feels-like 24.8 C with a 20.3..30.9 range: hot band, only light allowed.
const e61Hot = `{"current":{"temperature_2m":28.5,"apparent_temperature":24.8,"weather_code":0,"precipitation":0.0},
 "daily":{"temperature_2m_min":[20.3],"temperature_2m_max":[30.9],"precipitation_probability_max":[3],"weather_code":[3]}}`

// e61Items is the g48 catalog with every top, bottom and footwear set to the
// given warmth tier.
func e61Items(warmth string) []store.Item {
	var out []store.Item
	for _, it := range g48Items() {
		switch it.Category {
		case "top", "bottom", "footwear":
			it.WarmthTier = warmth
		}
		out = append(out, it)
	}
	return out
}

func TestING061_Explained422(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	items := e61Items("light")
	for i := range items {
		if items[i].Category == "bottom" {
			items[i].WarmthTier = "medium"
		}
	}
	e, _ := e49Engine(t, items, ing043Fake(t, 200, e61Hot).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	want := `cannot build outfits. bottom: 2 owned, 2 excluded by warmth. Today allows warmth: light (feels-like 24.8 °C, range 20.3–30.9 °C). Tick "Ignore weather" to skip weather rules.`
	if msg := e49Err(t, e49Post(e, strings.NewReader(`{}`)), 422, "bottom"); msg != want {
		t.Errorf("error = %q\nwant    %q", msg, want)
	}
	e49Calls(t, calls, 0)
}

func TestING061_IgnoreWeather(t *testing.T) {
	// Every top, bottom and footwear is heavy and outerwear is in the
	// catalog: a hot day would exclude all of it.
	answer := g48Raw(g48O("a", "t1", "b1", "f1", "o1"), g48O("b", "t2", "b2", "f2"), g48O("c", "t1", "b2", "f1"))
	url, calls, body := e49Fake(t, answer)
	e, _ := e49Engine(t, e61Items("heavy"), ing043Fake(t, 200, e61Hot).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})

	e49Err(t, e49Post(e, strings.NewReader(`{}`)), 422, "excluded by warmth")
	e49Calls(t, calls, 0)

	rr := e49Post(e, strings.NewReader(`{"ignore_weather":true}`))
	if rr.Code != 200 {
		t.Fatalf("ignore_weather: status = %d, body %s", rr.Code, rr.Body)
	}
	if m := ing043JSON(t, rr); m["weather"] == nil || m["outfits"] == nil {
		t.Errorf("response keys = %v, want weather and outfits", ing019Keys(m))
	}
	prompt := e49Prompt(t, body)
	for _, s := range []string{"Weather rules are off:", "- feels-like: 24.8 C\n", "Outerwear: optional.", "id=o1 ", "id=t1 "} {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
	if strings.Contains(prompt, "temperature unknown") {
		t.Error("prompt claims the temperature is unknown")
	}
}

// G1: on a cold day ignoring weather makes outerwear optional, so a catalog
// with no outerwear still gets outfits, and an answer without outerwear is
// accepted.
func TestING061_IgnoreWeatherColdDayOuterwearOptional(t *testing.T) {
	answer := g48Raw(g48O("a", "t1", "b1", "f1"), g48O("b", "t2", "b2", "f2"), g48O("c", "t1", "b2", "f1"))
	url, calls, body := e49Fake(t, answer)
	e, _ := e49Engine(t, e49Items(nil, "o1", "o2"), ing043Fake(t, 200, e49Cold).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})

	e49Err(t, e49Post(e, strings.NewReader(`{}`)), 422, "outerwear (required")
	e49Calls(t, calls, 0)

	rr := e49Post(e, strings.NewReader(`{"ignore_weather":true}`))
	if rr.Code != 200 {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	e49Calls(t, calls, 1)
	if p := e49Prompt(t, body); !strings.Contains(p, "Outerwear: optional.") {
		t.Errorf("prompt does not make outerwear optional:\n%s", p)
	}
}

// G2: an explicit false is the same as leaving the field out.
func TestING061_IgnoreWeatherFalseIsDefault(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	e, _ := e49Engine(t, e61Items("heavy"), ing043Fake(t, 200, e61Hot).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	absent := e49Err(t, e49Post(e, strings.NewReader(`{}`)), 422, "excluded by warmth")
	if explicit := e49Err(t, e49Post(e, strings.NewReader(`{"ignore_weather":false}`)), 422, "excluded by warmth"); explicit != absent {
		t.Errorf("ignore_weather:false error = %q\nwant same as absent: %q", explicit, absent)
	}
	e49Calls(t, calls, 0)
}

func TestING061_IgnoreWeatherBadValueAndWeatherFailure(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	for _, b := range []string{`{"ignore_weather":"yes"}`, `{"ignore_weather":1}`, `{"ignore_weather":null}`} {
		e49Err(t, e49Post(e, strings.NewReader(b)), 400, "ignore_weather")
	}

	down, _ := e49Engine(t, g48Items(), ing043Fake(t, 500, `{}`).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	e49Err(t, e49Post(down, strings.NewReader(`{"ignore_weather":true}`)), 502, "weather")
	e49Calls(t, calls, 0)
}
