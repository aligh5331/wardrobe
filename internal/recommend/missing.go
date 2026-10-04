package recommend

// Missing-slot explanation for the 422 (07-architecture.md "Recommender
// (Phase 3)" step 3; CONTEXT.md "Missing slot"). Pure, no I/O.

import (
	"fmt"
	"strings"

	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
	"wardrobe/internal/weather"
)

// MissingMessage returns the 422 error for the required slots that items
// cannot fill under r and formality, or "" when every slot has a candidate.
// Each missing slot says how many items of that category are owned and how
// many the warmth and formality filters each excluded; an item failing both
// counts in both.
func MissingMessage(items []store.Item, r Rules, formality string, f weather.Forecast) string {
	missing := MissingSlots(Filter(items, r, formality), r)
	if len(missing) == 0 {
		return ""
	}

	clauses := make([]string, 0, len(missing))
	anyWarmth := false
	for _, slot := range missing {
		owned, byWarmth, byFormality := 0, 0, 0
		for _, it := range items {
			if it.Category != slot {
				continue
			}
			owned++
			if warmthExcludes(r, it) {
				byWarmth++
			}
			if formality != "" && it.Formality != formality {
				byFormality++
			}
		}
		name := slot
		if slot == "outerwear" {
			// "cold days", not a feels-like number: the band may come from
			// the min/max fallback when feels-like is null.
			name = "outerwear (required on cold days)"
		}
		if owned == 0 {
			clauses = append(clauses, name+": none in catalog")
			continue
		}
		c := fmt.Sprintf("%s: %d owned", name, owned)
		if byWarmth > 0 {
			c += fmt.Sprintf(", %d excluded by warmth", byWarmth)
			anyWarmth = true
		}
		if byFormality > 0 {
			c += fmt.Sprintf(", %d excluded by formality (%s)", byFormality, formality)
		}
		clauses = append(clauses, c)
	}

	msg := "cannot build outfits. " + strings.Join(clauses, "; ") + "."
	if anyWarmth {
		var allowed []string
		for _, t := range tagging.TaxonomyTables().WarmthTiers {
			if r.Tiers[t] {
				allowed = append(allowed, t)
			}
		}
		msg += fmt.Sprintf(" Today allows warmth: %s (feels-like %s °C, range %s–%s °C).",
			strings.Join(allowed, ", "),
			orUnknown(f.Current.ApparentTemperatureC, "%.1f"),
			orUnknown(f.Today.TemperatureMinC, "%.1f"),
			orUnknown(f.Today.TemperatureMaxC, "%.1f"))
		msg += ` Tick "Ignore weather" to skip weather rules.`
	}
	return msg
}
