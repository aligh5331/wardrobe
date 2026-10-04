package recommend

import (
	"reflect"
	"strings"
	"testing"

	"wardrobe/internal/store"
)

// The ING-061 worked example: hot day, every bottom medium.
func hotCatalog() []store.Item {
	return []store.Item{
		it("t", "top", "light", "casual"),
		it("f", "footwear", "light", "casual"),
		it("b1", "bottom", "medium", "formal"),
		it("b2", "bottom", "medium", "smart-casual"),
		it("b3", "bottom", "medium", "casual"),
	}
}

func TestMissingMessage(t *testing.T) {
	hotF := fc(fp(24.8), fp(20.3), fp(30.9), nil)
	coldF := fc(fp(3), nil, fp(6), nil)
	hint := ` Tick "Ignore weather" to skip weather rules.`
	tests := []struct {
		name      string
		items     []store.Item
		ignore    bool
		formality string
		cold      bool
		want      string
	}{
		{"worked example", hotCatalog(), false, "", false,
			"cannot build outfits. bottom: 3 owned, 3 excluded by warmth. Today allows warmth: light (feels-like 24.8 °C, range 20.3–30.9 °C)." + hint},
		{"worked example with formal", hotCatalog(), false, "formal", false,
			"cannot build outfits. top: 1 owned, 1 excluded by formality (formal); bottom: 3 owned, 3 excluded by warmth, 2 excluded by formality (formal); footwear: 1 owned, 1 excluded by formality (formal). Today allows warmth: light (feels-like 24.8 °C, range 20.3–30.9 °C)." + hint},
		{"ignoring weather fills every slot", hotCatalog(), true, "", false, ""},
		{"ignoring weather, formality only, no warmth sentence", hotCatalog(), true, "smart-casual", false,
			"cannot build outfits. top: 1 owned, 1 excluded by formality (smart-casual); footwear: 1 owned, 1 excluded by formality (smart-casual)."},
		{"none in catalog", hotCatalog()[:2], false, "", false,
			"cannot build outfits. bottom: none in catalog."},
		{"required outerwear, null min shows unknown", []store.Item{
			it("t", "top", "heavy", "casual"), it("b", "bottom", "heavy", "casual"),
			it("f", "footwear", "medium", "casual"), it("o", "outerwear", "light", "casual"),
		}, false, "", true,
			"cannot build outfits. outerwear (required on cold days): 1 owned, 1 excluded by warmth. Today allows warmth: medium, heavy (feels-like 3.0 °C, range unknown–6.0 °C)." + hint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hotF
			if tt.cold {
				f = coldF
			}
			r := RulesFor(f)
			if tt.ignore {
				r = r.WithoutWeather()
			}
			if got := MissingMessage(tt.items, r, tt.formality, f); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestWithoutWeather(t *testing.T) {
	// A hot day would drop heavy items and all outerwear.
	r := RulesFor(fc(fp(25), nil, nil, nil)).WithoutWeather()
	catalog := []store.Item{it("top-h", "top", "heavy", "formal"), it("out-h", "outerwear", "heavy", "casual")}
	if got := ids(Filter(catalog, r, "")); !reflect.DeepEqual(got, []string{"top-h", "out-h"}) {
		t.Errorf("Filter = %v, want every item", got)
	}
	if got := ids(Filter(catalog, r, "formal")); !reflect.DeepEqual(got, []string{"top-h"}) {
		t.Errorf("formality still applies: got %v", got)
	}

	_, user := buildPrompt(Input{Forecast: fc(fp(25), fp(21), fp(30), nil), Rules: r})
	for _, want := range []string{
		"Weather rules are off: candidates were not filtered by warmth, so some may be too warm or too cool. Prefer the best fit for this weather.\n",
		"- feels-like: 25.0 C\n",
		"Outerwear: optional.",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(user, "temperature unknown") {
		t.Error("prompt claims the temperature is unknown")
	}
	if _, plain := buildPrompt(Input{Forecast: fc(fp(25), nil, nil, nil), Rules: RulesFor(fc(fp(25), nil, nil, nil))}); strings.Contains(plain, "Weather rules are off") {
		t.Error("rules-off line present without ignoring weather")
	}
}

// Ignoring weather when no temperature is known: the rules-off line replaces
// "temperature unknown", and the summary still prints unknown values.
func TestWithoutWeatherTemperatureUnknown(t *testing.T) {
	f := fc(nil, nil, nil, nil)
	_, user := buildPrompt(Input{Forecast: f, Rules: RulesFor(f).WithoutWeather()})
	for _, want := range []string{"Weather rules are off:", "- feels-like: unknown C\n", "- min: unknown C, max: unknown C\n", "Outerwear: optional."} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(user, "temperature unknown") {
		t.Errorf("prompt has the temperature-unknown line:\n%s", user)
	}
}
