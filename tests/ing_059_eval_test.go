package tests

// ING-059 unit tests for the recommender eval's helpers: trap wardrobe,
// scenarios, auto-flags, and the form writer and parser. No LLM, no network.

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

func TestING059_TrapWardrobeIsTaxonomyValid(t *testing.T) {
	tax := tagging.TaxonomyTables()
	ids := map[string]bool{}
	items := trapWardrobe()
	if len(items) != 35 {
		t.Fatalf("got %d items, want 35", len(items))
	}
	for i, it := range items {
		if it.ID != evalID(i+1) || ids[it.ID] {
			t.Errorf("row %d: id %q not the fixed unique id", i+1, it.ID)
		}
		ids[it.ID] = true
		if !slices.Contains(tax.Categories[it.Category], it.Subcategory) {
			t.Errorf("row %d: subcategory %q not valid for %q", i+1, it.Subcategory, it.Category)
		}
		for _, c := range append([]string{it.DominantColor}, it.SecondaryColors...) {
			if !slices.Contains(tax.Colors, c) {
				t.Errorf("row %d: color %q not in palette", i+1, c)
			}
		}
		if !slices.Contains(tax.Patterns, it.Pattern) || !slices.Contains(tax.WarmthTiers, it.WarmthTier) ||
			!slices.Contains(tax.Formality, it.Formality) {
			t.Errorf("row %d: pattern/warmth/formality not in taxonomy: %+v", i+1, it)
		}
		if it.PhotoPath != "" || it.Notes != "" || !it.AddedDate.IsZero() {
			t.Errorf("row %d: photo path, notes and added date must stay empty", i+1)
		}
	}
}

func TestING059_ScenariosFillSlots(t *testing.T) {
	want := map[string]struct {
		tiers     []string
		outerwear recommend.Outerwear
		rain      bool
	}{
		"cold-rain":    {[]string{"heavy", "light", "medium"}, recommend.OuterwearRequired, true},
		"mild-dry":     {[]string{"light", "medium"}, recommend.OuterwearOptional, false},
		"hot-sunny":    {[]string{"light"}, recommend.OuterwearExcluded, false},
		"temp-unknown": {nil, recommend.OuterwearOptional, false},
	}
	var names []string
	for _, sc := range evalScenarios() {
		names = append(names, sc.Name)
		r := recommend.RulesFor(sc.Forecast)
		var tiers []string
		for tier := range r.Tiers {
			tiers = append(tiers, tier)
		}
		slices.Sort(tiers)
		w := want[sc.Name]
		if !reflect.DeepEqual(tiers, w.tiers) || r.Outerwear != w.outerwear || r.RainHint != w.rain {
			t.Errorf("%s: tiers %v outerwear %v rain %v, want %v %v %v", sc.Name, tiers, r.Outerwear, r.RainHint, w.tiers, w.outerwear, w.rain)
		}
		if missing := recommend.MissingSlots(recommend.Filter(trapWardrobe(), r, ""), r); len(missing) > 0 {
			t.Errorf("%s: missing slots %v", sc.Name, missing)
		}
	}
	if !reflect.DeepEqual(names, []string{"cold-rain", "mild-dry", "hot-sunny", "temp-unknown"}) {
		t.Errorf("scenario order %v", names)
	}
}

// pick returns trap wardrobe rows by number.
func pick(rows ...int) []store.Item {
	all := trapWardrobe()
	var out []store.Item
	for _, n := range rows {
		out = append(out, all[n-1])
	}
	return out
}

func TestING059_OutfitFlags(t *testing.T) {
	hot, mild := f64(31), f64(16)
	rain := recommend.Rules{RainHint: true}
	dry := recommend.Rules{}
	cases := []struct {
		name  string
		rows  []int
		rules recommend.Rules
		feels *float64
		flag  string
		want  bool
	}{
		// 1 white tee, 12 tan chinos, 21 white sneakers: the clean baseline.
		{"clean", []int{1, 12, 21}, dry, mild, "", false},
		{"pattern two garments", []int{5, 16, 21}, dry, mild, "pattern", true},
		{"pattern one garment", []int{5, 12, 21}, dry, mild, "pattern", false},
		{"pattern garment plus accessory", []int{5, 12, 21, 32}, dry, mild, "pattern", true},
		{"pattern accessory alone", []int{1, 12, 21, 32}, dry, mild, "pattern", false},
		{"accent two families", []int{9, 12, 21, 28}, dry, mild, "accent", true},
		{"accent red shades one family", []int{8, 12, 26}, dry, mild, "accent", false},
		{"accent blue jeans neutral", []int{9, 11, 21}, dry, mild, "accent", false},
		{"accent olive neutral", []int{10, 14, 21}, dry, mild, "accent", false},
		{"weather-acc beanie hot", []int{1, 12, 21, 27}, dry, hot, "weather-acc", true},
		{"weather-acc beanie mild", []int{1, 12, 21, 27}, dry, mild, "weather-acc", false},
		{"weather-acc unknown temp", []int{1, 12, 21, 33}, dry, nil, "weather-acc", false},
		{"rain-shoes", []int{1, 12, 24}, rain, mild, "rain-shoes", true},
		{"rain-shoes dry", []int{1, 12, 24}, dry, mild, "rain-shoes", false},
		{"formality dress shoes shorts", []int{1, 14, 23}, dry, mild, "formality", true},
		{"formality dress shoes chinos", []int{1, 12, 23}, dry, mild, "formality", false},
		{"formality tie with tee", []int{1, 13, 23, 34}, dry, mild, "formality", true},
		{"formality tie with shirt", []int{4, 13, 23, 34}, dry, mild, "formality", false},
		{"layering blazer hoodie", []int{17, 9, 11, 21}, dry, mild, "layering", true},
		{"layering blazer shirt", []int{17, 4, 11, 21}, dry, mild, "layering", false},
		{"belt-shoes black belt brown boots", []int{1, 11, 22, 31}, dry, mild, "belt-shoes", true},
		{"belt-shoes brown belt tan loafers", []int{1, 12, 25, 30}, dry, mild, "belt-shoes", false},
		{"belt-shoes sneakers exempt", []int{1, 12, 21, 31}, dry, mild, "belt-shoes", false},
	}
	for _, c := range cases {
		got := outfitFlags(pick(c.rows...), c.rules, c.feels)
		if c.flag == "" {
			if len(got) != 0 {
				t.Errorf("%s: flags %v, want none", c.name, got)
			}
			continue
		}
		if slices.Contains(got, c.flag) != c.want {
			t.Errorf("%s: flags %v, want %s=%v", c.name, got, c.flag, c.want)
		}
	}
}

func TestING059_RepeatFlag(t *testing.T) {
	sameTop := [][]store.Item{pick(1, 11, 21), pick(1, 12, 22), pick(1, 13, 23)}
	sameBottom := [][]store.Item{pick(1, 12, 21), pick(2, 12, 22), pick(3, 12, 23)}
	varied := [][]store.Item{pick(1, 11, 21), pick(1, 12, 22), pick(2, 12, 23)}
	if !repeatFlag(sameTop) || !repeatFlag(sameBottom) || repeatFlag(varied) {
		t.Errorf("repeat: sameTop=%v sameBottom=%v varied=%v", repeatFlag(sameTop), repeatFlag(sameBottom), repeatFlag(varied))
	}
}

func TestING059_EvalLabel(t *testing.T) {
	for in, want := range map[string]string{"baseline": "baseline", "Advisor V1": "advisor-v1", "a_b/c.d": "a-b-c-d", "": ""} {
		if got := evalLabel(in); got != want {
			t.Errorf("evalLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// sampleResults builds results without an LLM: cold-rain has a valid run
// with flags, temp-unknown has only failed runs.
func sampleResults() []evalResult {
	scs := evalScenarios()
	cold, unknown := scs[0], scs[3]
	coldRules, unkRules := recommend.RulesFor(cold.Forecast), recommend.RulesFor(unknown.Forecast)
	outfits := [][]store.Item{pick(18, 4, 13, 23, 31), pick(19, 5, 16, 24), pick(17, 9, 11, 22, 31)}
	reasons := []string{"Black and gray stay formal.", "Olive with a print.", "Navy over orange."}
	failed := evalRun{Err: errors.New("llm failure")}
	return []evalResult{
		{Scenario: cold, Rules: coldRules, Runs: []evalRun{
			failed,
			newEvalRun(outfits, reasons, coldRules, cold.Forecast.Current.ApparentTemperatureC, true),
			newEvalRun(outfits, reasons, coldRules, cold.Forecast.Current.ApparentTemperatureC, false),
		}},
		{Scenario: unknown, Rules: unkRules, Runs: []evalRun{failed, failed, failed}},
	}
}

func TestING059_FormWriteAndParse(t *testing.T) {
	h := evalHeader{When: time.Date(2026, 10, 4, 14, 32, 0, 0, time.UTC), Label: "baseline",
		Commit: "9df55b4-dirty", Fingerprint: "abcdef012345", Model: "(server default)", Temperature: 0.4}
	form := writeEvalForm(h, sampleResults())

	for _, s := range []string{
		"- Date: 2026-10-04 14:32", "- Label: baseline", "- Commit: 9df55b4-dirty",
		"- Fingerprint: abcdef012345", "- Model: (server default)", "- Temperature: 0.4",
		"- Valid runs: 2/6", "How to fill:",
		"## Scenario: cold-rain", "rain chance 80% (rain likely)", "rain (WMO 61)", "outerwear required",
		"Request note: none", "Rated run: 2 of 3",
		// API order: outerwear, top, bottom, footwear, accessory.
		"- Items: black coat + white shirt + gray dress-pants + black dress-shoes + black belt\n",
		"- Items: olive jacket + blue shirt (striped) + yellow shorts (print) + brown sandals\n",
		`- Reason: "Navy over orange."`,
		"- Auto-flags: none", "- Auto-flags: pattern, accent, rain-shoes", "layering, belt-shoes",
		"## Scenario: temp-unknown", "No run of this scenario was valid",
		"| cold-rain | 2/3 | 1/3 |", "| temp-unknown | 0/3 | 0/3 |",
	} {
		if !strings.Contains(form, s) {
			t.Errorf("form lacks %q", s)
		}
	}
	if n := strings.Count(form, "- Score (1-5): _"); n != 3 {
		t.Errorf("got %d score fields, want 3 (one rated run)", n)
	}
	if n := strings.Count(form, "- Variety (1-5): _"); n != 1 {
		t.Errorf("got %d variety fields, want 1", n)
	}

	p, err := parseEvalForm(form)
	if err != nil {
		t.Fatal(err)
	}
	if p.rated() || p.Outfits != 3 || p.Label != "baseline" || p.Fingerprint != "abcdef012345" || p.ValidRuns != "2/6" {
		t.Errorf("blank form parsed as %+v", p)
	}
	// Flags: outfit 1 none; outfit 2 pattern, accent, rain-shoes; outfit 3
	// layering, belt-shoes (orange is its only accent); two valid runs.
	if p.TotalFlags != 2*(3+2) {
		t.Errorf("total auto-flags %d, want 10", p.TotalFlags)
	}

	filled := strings.Replace(form, "- Score (1-5): _", "- Score (1-5): 4", 1)
	filled = strings.Replace(filled, "- Score (1-5): _", "- Score (1-5): 2", 1)
	filled = strings.Replace(filled, "- Would wear (y/n): _", "- Would wear (y/n): y", 1)
	filled = strings.Replace(filled, "- Would wear (y/n): _", "- Would wear (y/n): n", 1)
	filled = strings.Replace(filled, "- Variety (1-5): _", "- Variety (1-5): 3", 1)
	p, err = parseEvalForm(filled)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Scores, []int{4, 2}) || !reflect.DeepEqual(p.WouldWear, []bool{true, false}) ||
		!reflect.DeepEqual(p.Variety, []int{3}) {
		t.Errorf("filled form parsed as %+v", p)
	}
	row := summaryRow("2026-10-04-1432-baseline.md", p)
	for _, s := range []string{"rated=2/3", "avg score=3.00", "would wear=50%", "avg variety=3.00", "valid runs=2/6", "auto-flags=10"} {
		if !strings.Contains(row, s) {
			t.Errorf("summary row %q lacks %q", row, s)
		}
	}
}

func TestING059_ParseRejectsMalformed(t *testing.T) {
	for _, line := range []string{"- Score (1-5): 7", "- Would wear (y/n): maybe", "- Variety (1-5): x"} {
		_, err := parseEvalForm("# form\n\n" + line + "\n")
		if err == nil || !strings.Contains(err.Error(), "line 3") {
			t.Errorf("%q: err %v, want an error naming line 3", line, err)
		}
	}
}

func TestING059_FormName(t *testing.T) {
	for name, want := range map[string]bool{
		"2026-10-04-1432-baseline.md": true, "2026-10-04-1432-advisor-v1.md": true,
		"README.md": false, "2026-10-04-baseline.md": false, "2026-10-04-1432-Baseline.md": false,
	} {
		if evalFormName.MatchString(name) != want {
			t.Errorf("%s: match = %v, want %v", name, !want, want)
		}
	}
}
