package recommend

import (
	"reflect"
	"sort"
	"testing"

	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }

func fc(feels, mn, mx *float64, pop *int) weather.Forecast {
	return weather.Forecast{
		Current: weather.Current{ApparentTemperatureC: feels},
		Today:   weather.Today{TemperatureMinC: mn, TemperatureMaxC: mx, PrecipitationProbabilityMax: pop},
	}
}

func tiers(r Rules) []string {
	if r.Tiers == nil {
		return nil
	}
	out := []string{}
	for t := range r.Tiers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func TestRulesFor(t *testing.T) {
	L, M, H := []string{"light"}, []string{"light", "medium"}, []string{"heavy", "medium"}
	all := []string{"heavy", "light", "medium"}
	tests := []struct {
		name    string
		f       weather.Forecast
		tiers   []string
		outer   Outerwear
		unknown bool
	}{
		{"cold band", fc(fp(5), fp(3), fp(8), nil), H, OuterwearRequired, false},
		{"mild band", fc(fp(15), fp(12), fp(18), nil), M, OuterwearOptional, false},
		{"hot band", fc(fp(25), fp(21), fp(30), nil), L, OuterwearExcluded, false},
		{"feels-like 10 is mild", fc(fp(10), nil, nil, nil), M, OuterwearOptional, false},
		{"feels-like 20 is mild", fc(fp(20), nil, nil, nil), M, OuterwearOptional, false},
		{"just below 10 is cold", fc(fp(9.9), nil, nil, nil), H, OuterwearRequired, false},
		{"just above 20 is hot", fc(fp(20.1), nil, nil, nil), L, OuterwearExcluded, false},
		{"range crosses both", fc(fp(15), fp(8), fp(24), nil), all, OuterwearOptional, false},
		{"range 5..10 touches cold and mild", fc(fp(15), fp(5), fp(10), nil), all, OuterwearOptional, false},
		{"range starting at 10 does not touch cold", fc(fp(15), fp(10), fp(18), nil), M, OuterwearOptional, false},
		{"range ending at 20 does not touch hot", fc(fp(15), fp(12), fp(20), nil), M, OuterwearOptional, false},
		{"range crosses 10 only", fc(fp(5), fp(3), fp(12), nil), []string{"heavy", "light", "medium"}, OuterwearRequired, false},
		{"range crosses 20 only, hot feels-like", fc(fp(25), fp(18), fp(30), nil), M, OuterwearExcluded, false},
		{"feels-like null, midpoint", fc(nil, fp(2), fp(6), nil), H, OuterwearRequired, false},
		{"feels-like null, midpoint on edge", fc(nil, fp(0), fp(20), nil), all, OuterwearOptional, false},
		{"feels-like null, only min", fc(nil, fp(4), nil, nil), H, OuterwearRequired, false},
		{"feels-like null, only max", fc(nil, nil, fp(25), nil), L, OuterwearExcluded, false},
		{"all null", fc(nil, nil, nil, nil), nil, OuterwearOptional, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RulesFor(tt.f)
			if got := tiers(r); !reflect.DeepEqual(got, tt.tiers) {
				t.Errorf("tiers = %v, want %v", got, tt.tiers)
			}
			if r.Outerwear != tt.outer {
				t.Errorf("outerwear = %v, want %v", r.Outerwear, tt.outer)
			}
			if r.TempUnknown != tt.unknown {
				t.Errorf("TempUnknown = %v, want %v", r.TempUnknown, tt.unknown)
			}
		})
	}
}

func TestRainHint(t *testing.T) {
	tests := []struct {
		name string
		pop  *int
		want bool
	}{{"50", ip(50), true}, {"100", ip(100), true}, {"49", ip(49), false}, {"null", nil, false}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RulesFor(fc(fp(15), fp(12), fp(18), tt.pop)).RainHint; got != tt.want {
				t.Errorf("RainHint = %v, want %v", got, tt.want)
			}
		})
	}
	// Hint only: same tiers and outerwear with or without rain.
	a, b := RulesFor(fc(fp(5), nil, nil, ip(90))), RulesFor(fc(fp(5), nil, nil, nil))
	if !reflect.DeepEqual(a.Tiers, b.Tiers) || a.Outerwear != b.Outerwear {
		t.Errorf("rain changed the rules: %+v vs %+v", a, b)
	}
}

func it(id, cat, warmth, formality string) store.Item {
	return store.Item{ID: id, Category: cat, WarmthTier: warmth, Formality: formality}
}

func ids(items []store.Item) []string {
	out := []string{}
	for _, i := range items {
		out = append(out, i.ID)
	}
	return out
}

func TestFilter(t *testing.T) {
	catalog := []store.Item{
		it("top-l", "top", "light", "casual"),
		it("top-h", "top", "heavy", "formal"),
		it("bot-m", "bottom", "medium", "casual"),
		it("out-h", "outerwear", "heavy", "formal"),
		it("out-l", "outerwear", "light", "casual"),
		it("shoe-l", "footwear", "light", "casual"),
		it("shoe-h", "footwear", "heavy", "formal"),
		it("hat-h", "headwear", "heavy", "casual"),
		it("acc-l", "accessory", "light", "formal"),
	}
	cold := RulesFor(fc(fp(5), nil, nil, nil))
	hot := RulesFor(fc(fp(25), nil, nil, nil))
	mild := RulesFor(fc(fp(15), nil, nil, nil))
	unknown := RulesFor(fc(nil, nil, nil, nil))
	tests := []struct {
		name      string
		r         Rules
		formality string
		want      []string
	}{
		{"cold keeps medium and heavy, exempt categories stay", cold, "",
			[]string{"top-h", "bot-m", "out-h", "shoe-h", "hat-h", "acc-l"}},
		{"hot drops outerwear entirely and heavy items", hot, "",
			[]string{"top-l", "shoe-l", "hat-h", "acc-l"}},
		{"mild keeps light and medium", mild, "",
			[]string{"top-l", "bot-m", "out-l", "shoe-l", "hat-h", "acc-l"}},
		{"unknown temperature keeps everything", unknown, "",
			ids(catalog)},
		{"formal applies to every category", unknown, "formal",
			[]string{"top-h", "out-h", "shoe-h", "acc-l"}},
		{"formality combines with warmth", mild, "formal",
			[]string{"acc-l"}},
		{"no match gives empty", hot, "smart-casual", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ids(Filter(catalog, tt.r, tt.formality)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingSlots(t *testing.T) {
	all := []store.Item{
		it("t", "top", "light", "casual"), it("b", "bottom", "light", "casual"),
		it("f", "footwear", "light", "casual"), it("o", "outerwear", "heavy", "casual"),
	}
	required := Rules{Outerwear: OuterwearRequired}
	optional := Rules{Outerwear: OuterwearOptional}
	tests := []struct {
		name  string
		cands []store.Item
		r     Rules
		want  []string
	}{
		{"all present, outerwear required", all, required, nil},
		{"no footwear", all[:2], optional, []string{"footwear"}},
		{"nothing but hats", []store.Item{it("h", "headwear", "light", "casual")}, optional, []string{"top", "bottom", "footwear"}},
		{"required outerwear missing", all[:3], required, []string{"outerwear"}},
		{"optional outerwear not reported", all[:3], optional, nil},
		{"everything missing when required", nil, required, []string{"top", "bottom", "footwear", "outerwear"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MissingSlots(tt.cands, tt.r); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
