package tests

import (
	"reflect"
	"sort"
	"testing"

	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

func f47(v float64) *float64 { return &v }

func fc47(feels, mn, mx *float64, pop *int) weather.Forecast {
	return weather.Forecast{
		Current: weather.Current{ApparentTemperatureC: feels},
		Today:   weather.Today{TemperatureMinC: mn, TemperatureMaxC: mx, PrecipitationProbabilityMax: pop},
	}
}

func tiers47(r recommend.Rules) []string {
	out := []string{}
	for t := range r.Tiers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// ING-047 gaps the Coder tests leave open, derived from the 06-decisions.md
// table: feels-like present with a partial min/max, 9.99 and 20.01 edges,
// range 20..30, outerwear rule unmoved by a wide range, the Tiers nil iff
// TempUnknown invariant, rain hint with unknown temperature, and Filter
// results (non-nil empty slice, unknown formality, formality on an excluded
// outerwear day, Filter into MissingSlots).
func TestING047_Gaps(t *testing.T) {
	L, M, H := []string{"light"}, []string{"light", "medium"}, []string{"heavy", "medium"}
	all := []string{"heavy", "light", "medium"}
	pop := 60

	rules := []struct {
		name    string
		f       weather.Forecast
		tiers   []string
		outer   recommend.Outerwear
		unknown bool
	}{
		{"feels 15, only min 5 widens to cold", fc47(f47(15), f47(5), nil, nil), all, recommend.OuterwearOptional, false},
		{"feels 15, only max 25 adds hot band, tiers stay light and medium", fc47(f47(15), nil, f47(25), nil), M, recommend.OuterwearOptional, false},
		{"feels 15, only max 9 widens to cold", fc47(f47(15), nil, f47(9), nil), all, recommend.OuterwearOptional, false},
		{"feels 15, range 20..30 touches mild and hot", fc47(f47(15), f47(20), f47(30), nil), M, recommend.OuterwearOptional, false},
		{"feels 25, range 0..30 widens, outerwear stays excluded", fc47(f47(25), f47(0), f47(30), nil), all, recommend.OuterwearExcluded, false},
		{"feels 5, range 0..30 widens, outerwear stays required", fc47(f47(5), f47(0), f47(30), nil), all, recommend.OuterwearRequired, false},
		{"feels 9.99 is cold", fc47(f47(9.99), nil, nil, nil), H, recommend.OuterwearRequired, false},
		{"feels 20.01 is hot", fc47(f47(20.01), nil, nil, nil), L, recommend.OuterwearExcluded, false},
		{"rain hint set even when temperature unknown", fc47(nil, nil, nil, &pop), nil, recommend.OuterwearOptional, true},
	}
	for _, tt := range rules {
		t.Run(tt.name, func(t *testing.T) {
			r := recommend.RulesFor(tt.f)
			if r.Outerwear != tt.outer || r.TempUnknown != tt.unknown {
				t.Errorf("outerwear=%v unknown=%v, want %v %v", r.Outerwear, r.TempUnknown, tt.outer, tt.unknown)
			}
			if (r.Tiers == nil) != r.TempUnknown {
				t.Errorf("Tiers nil = %v but TempUnknown = %v", r.Tiers == nil, r.TempUnknown)
			}
			if tt.unknown {
				if !r.RainHint {
					t.Error("RainHint = false, want true for 60%")
				}
				return
			}
			if got := tiers47(r); !reflect.DeepEqual(got, tt.tiers) {
				t.Errorf("tiers = %v, want %v", got, tt.tiers)
			}
		})
	}

	item := func(id, cat, warmth, formality string) store.Item {
		return store.Item{ID: id, Category: cat, WarmthTier: warmth, Formality: formality}
	}
	catalog := []store.Item{
		item("top", "top", "medium", "casual"),
		item("out-light-formal", "outerwear", "light", "formal"),
		item("out-heavy", "outerwear", "heavy", "casual"),
		item("shoe", "footwear", "light", "casual"),
	}
	hot := recommend.RulesFor(fc47(f47(25), nil, nil, nil))
	cold := recommend.RulesFor(fc47(f47(5), nil, nil, nil))

	t.Run("no match returns non-nil empty slice", func(t *testing.T) {
		got := recommend.Filter(catalog, hot, "")
		if len(got) != 1 || got[0].ID != "shoe" {
			t.Fatalf("hot day kept %v, want only shoe", got)
		}
		got = recommend.Filter(catalog, hot, "formal")
		if got == nil || len(got) != 0 {
			t.Errorf("got %#v, want non-nil empty slice", got)
		}
		if got = recommend.Filter(nil, cold, ""); got == nil {
			t.Error("empty input gave nil, want non-nil empty slice")
		}
	})
	t.Run("unknown formality string matches nothing", func(t *testing.T) {
		if got := recommend.Filter(catalog, recommend.RulesFor(fc47(nil, nil, nil, nil)), "bogus"); got == nil || len(got) != 0 {
			t.Errorf("got %#v, want non-nil empty slice", got)
		}
	})
	t.Run("formal outerwear still dropped on an excluded day", func(t *testing.T) {
		for _, it := range recommend.Filter(catalog, hot, "formal") {
			t.Errorf("kept %s, want nothing", it.ID)
		}
	})
	t.Run("Filter output drives MissingSlots", func(t *testing.T) {
		// Cold day: heavy outerwear is casual, so formal leaves no outerwear,
		// and the light formal coat fails the warmth filter.
		got := recommend.MissingSlots(recommend.Filter(catalog, cold, "formal"), cold)
		want := []string{"top", "bottom", "footwear", "outerwear"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("missing = %v, want %v", got, want)
		}
	})
}
