package recommend

import (
	"regexp"
	"strings"
	"testing"

	"wardrobe/internal/tagging"
	"wardrobe/internal/weather"
)

// ING-060: every value in backticks in the advisor guidance is a taxonomy
// value or a candidate-line field name, so the guidance never invents an enum.
func TestAdvisorPromptUsesOnlyTaxonomyValues(t *testing.T) {
	tax := tagging.TaxonomyTables()
	allowed := map[string]bool{}
	for cat, subs := range tax.Categories {
		allowed[cat] = true
		for _, s := range subs {
			allowed[s] = true
		}
	}
	for _, list := range [][]string{tax.Colors, tax.Patterns, tax.WarmthTiers, tax.Formality,
		{"id", "category", "subcategory", "dominant_color", "secondary_colors", "pattern", "warmth_tier", "formality"}} {
		for _, v := range list {
			allowed[v] = true
		}
	}
	tokens := regexp.MustCompile("`([^`]*)`").FindAllStringSubmatch(advisorPrompt, -1)
	if len(tokens) < 50 {
		t.Fatalf("only %d backticked values; is the guidance embedded?", len(tokens))
	}
	for _, m := range tokens {
		if !allowed[m[1]] {
			t.Errorf("advisor_prompt.md uses %q, which is not a taxonomy value or field name", m[1])
		}
	}
}

func TestSystemPromptIsContractThenGuidance(t *testing.T) {
	system, _ := buildPrompt(Input{})
	if !strings.HasPrefix(system, contractPrompt) {
		t.Error("system prompt does not start with the output contract")
	}
	if !strings.Contains(system, strings.TrimSpace(advisorPrompt)) {
		t.Error("system prompt lacks the embedded advisor guidance")
	}
	for _, old := range []string{"Coordinate colors", "Read the note", "Each reason is one short sentence"} {
		if strings.Contains(contractPrompt, old) {
			t.Errorf("contract still carries style line %q", old)
		}
	}
	sections := []string{"Color:", "Pattern:", "Layering:", "Formality:", "Accessories and headwear:", "Weather:", "Variety across the 3 outfits:", "The reason:"}
	last := -1
	for _, s := range sections {
		i := strings.Index(advisorPrompt, "\n"+s+"\n")
		if i <= last {
			t.Errorf("section %q missing or out of order", s)
		}
		last = i
	}
}

func TestConditionAndRainLines(t *testing.T) {
	code := func(c int) *int { return &c }
	for _, c := range []struct {
		code *int
		want string
	}{
		{code(0), "- condition: clear (WMO 0)\n"},
		{code(48), "- condition: fog (WMO 48)\n"},
		{code(63), "- condition: rain (WMO 63)\n"},
		{code(86), "- condition: snow showers (WMO 86)\n"},
		{code(99), "- condition: thunderstorm (WMO 99)\n"},
		{code(4), "- condition: unknown (WMO 4)\n"},
		{nil, "- condition: unknown\n"},
	} {
		in := Input{Forecast: weather.Forecast{Today: weather.Today{WeatherCode: c.code}}}
		if _, u := buildPrompt(in); !strings.Contains(u, c.want) {
			t.Errorf("prompt lacks %q", c.want)
		}
	}
	pop := func(p int) *int { return &p }
	for _, c := range []struct {
		pop  *int
		want string
	}{{pop(80), "yes"}, {pop(30), "no"}, {nil, "unknown"}} {
		f := weather.Forecast{Today: weather.Today{PrecipitationProbabilityMax: c.pop}}
		in := Input{Forecast: f, Rules: RulesFor(f)}
		if _, u := buildPrompt(in); !strings.Contains(u, "- rain likely today: "+c.want+"\n") {
			t.Errorf("pop %v: prompt lacks rain line %q", c.pop, c.want)
		}
	}
	if _, u := buildPrompt(Input{}); strings.Contains(u, "weather code (WMO)") {
		t.Error("old weather code line still present")
	}
}
