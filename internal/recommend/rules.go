// Package recommend holds the Phase 3 recommender. This file is the rule
// filter: weather to warmth rules and the candidate filter (06-decisions.md
// "Weather → warmth thresholds", 07-architecture.md "Recommender (Phase 3)"
// flow steps 2 and 3). Pure functions, no I/O.
package recommend

import (
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

// Starting thresholds from 06-decisions.md. Tune them there and here together.
const (
	// coldBelowC: feels-like below this is the cold band.
	coldBelowC = 10.0
	// hotAboveC: feels-like above this is the hot band. 10 and 20 themselves
	// belong to the middle band.
	hotAboveC = 20.0
	// rainHintPct: precipitation_probability_max at or above this sets RainHint.
	rainHintPct = 50
)

// Outerwear is the outerwear rule for the day.
type Outerwear int

const (
	OuterwearOptional Outerwear = iota
	OuterwearRequired
	OuterwearExcluded
)

// Rules is the outcome of the weather rules for one forecast.
//
// Tiers is the set of allowed warmth tiers. It is nil exactly when
// TempUnknown or WeatherIgnored is true, and then Filter applies no warmth
// filter.
type Rules struct {
	Tiers       map[string]bool
	Outerwear   Outerwear
	TempUnknown bool
	RainHint    bool
	// WeatherIgnored: the request turned the weather rules off
	// (06-decisions.md "Ignoring weather").
	WeatherIgnored bool
}

// WithoutWeather returns r with the weather rules off: no warmth filter and
// outerwear optional. The rain hint is kept; it is guidance, not a rule.
func (r Rules) WithoutWeather() Rules {
	return Rules{RainHint: r.RainHint, WeatherIgnored: true}
}

const (
	bandCold = iota
	bandMild
	bandHot
)

// bandTiers maps a band to its allowed warmth tiers (taxonomy values).
var bandTiers = [3][]string{
	bandCold: {"medium", "heavy"},
	bandMild: {"light", "medium"},
	bandHot:  {"light"},
}

func band(c float64) int {
	switch {
	case c < coldBelowC:
		return bandCold
	case c > hotAboveC:
		return bandHot
	}
	return bandMild
}

// RulesFor computes the warmth rules for a forecast. The feels-like value
// picks the base band and the outerwear rule. If it is nil, the midpoint of
// today's min and max is used, or the single present one. If all three are
// nil, no warmth filter applies and outerwear is optional.
//
// The min..max range adds the tiers of every band it touches, using the same
// edges as the band table (a range ending exactly at 10 touches the middle
// band, one ending exactly at 20 does not touch the hot band).
func RulesFor(f weather.Forecast) Rules {
	r := Rules{RainHint: f.Today.PrecipitationProbabilityMax != nil &&
		*f.Today.PrecipitationProbabilityMax >= rainHintPct}

	mn, mx := f.Today.TemperatureMinC, f.Today.TemperatureMaxC
	base := f.Current.ApparentTemperatureC
	switch {
	case base != nil:
	case mn != nil && mx != nil:
		mid := (*mn + *mx) / 2
		base = &mid
	case mn != nil:
		base = mn
	case mx != nil:
		base = mx
	default:
		r.TempUnknown = true
		return r
	}

	lo, hi := band(*base), band(*base)
	for _, v := range []*float64{mn, mx} {
		if v != nil {
			lo, hi = min(lo, band(*v)), max(hi, band(*v))
		}
	}
	r.Tiers = map[string]bool{}
	for b := lo; b <= hi; b++ {
		for _, t := range bandTiers[b] {
			r.Tiers[t] = true
		}
	}

	switch band(*base) {
	case bandCold:
		r.Outerwear = OuterwearRequired
	case bandHot:
		r.Outerwear = OuterwearExcluded
	}
	return r
}

// warmthFiltered are the categories the warmth rules apply to. Headwear and
// accessory are exempt.
var warmthFiltered = map[string]bool{"top": true, "bottom": true, "outerwear": true, "footwear": true}

// warmthExcludes reports whether the warmth filter drops it.
func warmthExcludes(r Rules, it store.Item) bool {
	return r.Tiers != nil && warmthFiltered[it.Category] && !r.Tiers[it.WarmthTier]
}

// Filter returns the candidates for the rules and formality, in input order.
// An empty formality means no formality filter (otherwise exact match, every
// category). Outerwear is dropped when excluded.
func Filter(items []store.Item, r Rules, formality string) []store.Item {
	out := []store.Item{}
	for _, it := range items {
		if formality != "" && it.Formality != formality {
			continue
		}
		if it.Category == "outerwear" && r.Outerwear == OuterwearExcluded {
			continue
		}
		if warmthExcludes(r, it) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// MissingSlots names the required slots that have no candidate, in the fixed
// order top, bottom, footwear, outerwear (07-architecture.md step 3).
// Outerwear is required only when r.Outerwear is OuterwearRequired.
func MissingSlots(cands []store.Item, r Rules) []string {
	have := map[string]bool{}
	for _, it := range cands {
		have[it.Category] = true
	}
	need := []string{"top", "bottom", "footwear"}
	if r.Outerwear == OuterwearRequired {
		need = append(need, "outerwear")
	}
	var missing []string
	for _, s := range need {
		if !have[s] {
			missing = append(missing, s)
		}
	}
	return missing
}
