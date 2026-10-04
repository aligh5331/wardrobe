package tests

// Shared pieces of the ING-059 recommender eval: the trap wardrobe, the
// scenarios, the auto-flag checker, and the Markdown form writer and parser
// (docs/adr/0002-recommender-eval-is-human-rated.md). Untagged so the unit
// tests in ing_059_eval_test.go can run them without an LLM; the live run
// and the summary live in recommend_eval_test.go (integration tag).

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

// evalFormsDir is where forms are written, relative to the module root.
const evalFormsDir = "tests/evals/recommender"

// evalFormName matches <YYYY-MM-DD-HHMM>-<label>.md; anything else in the
// forms directory (README.md) is ignored by the summary.
var evalFormName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{4}-[a-z0-9-]+\.md$`)

// evalFlags are the auto-flag names in table order (ING-059 "Auto-flags").
var evalFlags = []string{"pattern", "accent", "weather-acc", "rain-shoes", "formality", "layering", "belt-shoes", "repeat"}

type evalScenario struct {
	Name     string
	Forecast weather.Forecast
	Note     string
}

func f64(v float64) *float64 { return &v }
func i32(v int) *int         { return &v }

// evalScenarios returns the 4 scenarios in table order. Formality is always
// "" (any).
func evalScenarios() []evalScenario {
	fc := func(feels, mn, mx *float64, pop, code *int) weather.Forecast {
		return weather.Forecast{
			Current: weather.Current{ApparentTemperatureC: feels},
			Today: weather.Today{TemperatureMinC: mn, TemperatureMaxC: mx,
				PrecipitationProbabilityMax: pop, WeatherCode: code},
		}
	}
	return []evalScenario{
		{"cold-rain", fc(f64(8), f64(6), f64(14), i32(80), i32(61)), ""},
		{"mild-dry", fc(f64(16), f64(12), f64(19), i32(10), i32(2)), "dinner with friends, walking there"},
		{"hot-sunny", fc(f64(31), f64(23), f64(34), i32(0), i32(0)), ""},
		{"temp-unknown", fc(nil, nil, nil, i32(20), i32(3)), ""},
	}
}

// evalID is the fixed, UUID-shaped id of trap wardrobe row n.
func evalID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

// trapWardrobe returns the 35 synthetic items from the ING-059 table, row n
// having id evalID(n). PhotoPath, Notes and AddedDate stay empty.
func trapWardrobe() []store.Item {
	rows := []struct{ cat, sub, dom, sec, pat, warmth, form string }{
		{"top", "t-shirt", "white", "", "solid", "light", "casual"},
		{"top", "t-shirt", "black", "", "solid", "light", "casual"},
		{"top", "polo", "navy", "", "solid", "light", "smart-casual"},
		{"top", "shirt", "white", "", "solid", "medium", "formal"},
		{"top", "shirt", "blue", "white", "striped", "light", "smart-casual"},
		{"top", "shirt", "red", "black", "plaid", "medium", "casual"},
		{"top", "sweater", "gray", "white", "striped", "medium", "smart-casual"},
		{"top", "sweater", "burgundy", "", "solid", "heavy", "smart-casual"},
		{"top", "hoodie", "orange", "", "solid", "medium", "casual"},
		{"top", "tank-top", "purple", "", "solid", "light", "casual"},
		{"bottom", "jeans", "blue", "", "solid", "medium", "casual"},
		{"bottom", "chinos", "tan", "", "solid", "light", "smart-casual"},
		{"bottom", "dress-pants", "gray", "", "solid", "medium", "formal"},
		{"bottom", "shorts", "olive", "", "solid", "light", "casual"},
		{"bottom", "sweatpants", "gray", "", "solid", "medium", "casual"},
		{"bottom", "shorts", "yellow", "white", "print", "light", "casual"},
		{"outerwear", "blazer", "navy", "", "solid", "medium", "formal"},
		{"outerwear", "coat", "black", "", "solid", "heavy", "formal"},
		{"outerwear", "jacket", "olive", "", "solid", "medium", "casual"},
		{"outerwear", "vest", "gray", "black", "plaid", "medium", "smart-casual"},
		{"footwear", "sneakers", "white", "", "solid", "light", "casual"},
		{"footwear", "boots", "brown", "", "solid", "heavy", "casual"},
		{"footwear", "dress-shoes", "black", "", "solid", "medium", "formal"},
		{"footwear", "sandals", "brown", "", "solid", "light", "casual"},
		{"footwear", "loafers", "tan", "", "solid", "light", "smart-casual"},
		{"footwear", "sneakers", "red", "white", "solid", "light", "casual"},
		{"headwear", "beanie", "black", "", "solid", "heavy", "casual"},
		{"headwear", "cap", "green", "", "solid", "light", "casual"},
		{"headwear", "hat", "tan", "", "solid", "light", "smart-casual"},
		{"accessory", "belt", "brown", "", "solid", "medium", "smart-casual"},
		{"accessory", "belt", "black", "", "solid", "medium", "formal"},
		{"accessory", "scarf", "burgundy", "gray", "plaid", "heavy", "casual"},
		{"accessory", "gloves", "black", "", "solid", "heavy", "casual"},
		{"accessory", "tie", "navy", "red", "striped", "light", "formal"},
		{"accessory", "sunglasses", "black", "", "solid", "light", "casual"},
	}
	items := make([]store.Item, len(rows))
	for i, r := range rows {
		var sec []string
		if r.sec != "" {
			sec = []string{r.sec}
		}
		items[i] = store.Item{ID: evalID(i + 1), Category: r.cat, Subcategory: r.sub,
			DominantColor: r.dom, SecondaryColors: sec, Pattern: r.pat,
			WarmthTier: r.warmth, Formality: r.form}
	}
	return items
}

// accentFamily maps a color to its accent family, or "" for neutrals,
// near-neutral olive, and blue on jeans.
func accentFamily(color, subcategory string) string {
	switch color {
	case "red", "burgundy", "pink":
		return "red"
	case "blue":
		if subcategory == "jeans" {
			return ""
		}
		return "blue"
	case "green", "purple", "yellow", "orange":
		return color
	}
	return ""
}

var garmentCategory = map[string]bool{"top": true, "bottom": true, "outerwear": true, "footwear": true}

// outfitFlags returns the per-outfit auto-flags (all but "repeat") in table
// order. feels is the scenario's feels-like value (nil = unknown).
func outfitFlags(items []store.Item, r recommend.Rules, feels *float64) []string {
	has := map[string]bool{}
	var top, belt, shoes *store.Item
	patternedGarments, patternedSmall := 0, 0
	families := map[string]bool{}
	for i := range items {
		it := &items[i]
		has[it.Subcategory] = true
		if it.Pattern != "solid" {
			if garmentCategory[it.Category] {
				patternedGarments++
			} else {
				patternedSmall++
			}
		}
		for _, c := range append([]string{it.DominantColor}, it.SecondaryColors...) {
			if f := accentFamily(c, it.Subcategory); f != "" {
				families[f] = true
			}
		}
		switch {
		case it.Category == "top":
			top = it
		case it.Subcategory == "belt":
			belt = it
		case it.Subcategory == "dress-shoes" || it.Subcategory == "loafers" || it.Subcategory == "boots":
			shoes = it
		}
	}

	var flags []string
	if patternedGarments >= 2 || (patternedGarments >= 1 && patternedSmall >= 1) {
		flags = append(flags, "pattern")
	}
	if len(families) >= 2 {
		flags = append(flags, "accent")
	}
	if feels != nil && *feels > 20 && (has["beanie"] || has["scarf"] || has["gloves"]) {
		flags = append(flags, "weather-acc")
	}
	if r.RainHint && has["sandals"] {
		flags = append(flags, "rain-shoes")
	}
	if (has["dress-shoes"] && (has["shorts"] || has["sweatpants"])) ||
		(has["tie"] && (top == nil || top.Subcategory != "shirt")) {
		flags = append(flags, "formality")
	}
	if has["blazer"] && (has["hoodie"] || has["sweatshirt"] || has["tank-top"]) {
		flags = append(flags, "layering")
	}
	brownish := func(c string) bool { return c == "brown" || c == "tan" }
	if belt != nil && shoes != nil &&
		((belt.DominantColor == "black" && brownish(shoes.DominantColor)) ||
			(shoes.DominantColor == "black" && brownish(belt.DominantColor))) {
		flags = append(flags, "belt-shoes")
	}
	return flags
}

// repeatFlag reports whether all outfits of a run share the same top or the
// same bottom.
func repeatFlag(outfits [][]store.Item) bool {
	if len(outfits) < 2 {
		return false
	}
	same := func(cat string) bool {
		first := ""
		for n, o := range outfits {
			id := ""
			for _, it := range o {
				if it.Category == cat {
					id = it.ID
				}
			}
			if id == "" || (n > 0 && id != first) {
				return false
			}
			first = id
		}
		return true
	}
	return same("top") || same("bottom")
}

// apiOrder is the item order within an outfit (07-architecture.md response).
var apiOrder = map[string]int{"outerwear": 0, "top": 1, "bottom": 2, "footwear": 3, "headwear": 4, "accessory": 5}

func sortItems(items []store.Item) []store.Item {
	out := append([]store.Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return apiOrder[out[i].Category] < apiOrder[out[j].Category] })
	return out
}

// describeItems renders an outfit as "<color> <subcategory> (<pattern>) + ...".
func describeItems(items []store.Item) string {
	parts := make([]string, 0, len(items))
	for _, it := range sortItems(items) {
		s := it.DominantColor + " " + it.Subcategory
		if it.Pattern != "solid" {
			s += " (" + it.Pattern + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " + ")
}

// wmoWord labels the WMO codes the scenarios use, as the frontend does.
func wmoWord(code *int) string {
	if code == nil {
		return "condition unknown"
	}
	words := map[int]string{0: "clear", 1: "mainly clear", 2: "partly cloudy", 3: "cloudy", 61: "rain"}
	if w, ok := words[*code]; ok {
		return fmt.Sprintf("%s (WMO %d)", w, *code)
	}
	return fmt.Sprintf("WMO %d", *code)
}

func tempWord(v *float64) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64) + " °C"
}

// describeWeather renders a scenario's weather and resulting rules in words.
func describeWeather(f weather.Forecast, r recommend.Rules) string {
	rain := "unknown"
	if p := f.Today.PrecipitationProbabilityMax; p != nil {
		rain = fmt.Sprintf("%d%%", *p)
		if r.RainHint {
			rain += " (rain likely)"
		}
	}
	outer := map[recommend.Outerwear]string{recommend.OuterwearRequired: "required",
		recommend.OuterwearOptional: "optional", recommend.OuterwearExcluded: "excluded"}[r.Outerwear]
	return fmt.Sprintf("feels-like %s, min %s / max %s, rain chance %s, %s; outerwear %s",
		tempWord(f.Current.ApparentTemperatureC), tempWord(f.Today.TemperatureMinC),
		tempWord(f.Today.TemperatureMaxC), rain, wmoWord(f.Today.WeatherCode), outer)
}

// evalLabel sanitizes EVAL_LABEL: lowercase, anything outside [a-z0-9-]
// becomes "-".
func evalLabel(s string) string {
	b := []byte(strings.ToLower(s))
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			b[i] = '-'
		}
	}
	return string(b)
}

type evalOutfit struct {
	Items  []store.Item
	Reason string
	Flags  []string
}

type evalRun struct {
	Outfits  []evalOutfit
	Err      error
	FirstTry bool // valid on the first LLM request
	Repeat   bool
}

func (r evalRun) valid() bool { return r.Err == nil }

type evalResult struct {
	Scenario evalScenario
	Rules    recommend.Rules
	Runs     []evalRun
}

type evalHeader struct {
	When        time.Time
	Label       string
	Commit      string
	Fingerprint string
	Model       string
	Temperature float64
}

// newEvalRun builds a run from Pick's outfits, computing auto-flags.
func newEvalRun(outfits [][]store.Item, reasons []string, r recommend.Rules, feels *float64, firstTry bool) evalRun {
	run := evalRun{FirstTry: firstTry, Repeat: repeatFlag(outfits)}
	for i, items := range outfits {
		run.Outfits = append(run.Outfits, evalOutfit{Items: items, Reason: reasons[i],
			Flags: outfitFlags(items, r, feels)})
	}
	return run
}

// flagCounts counts each auto-flag over every outfit of every valid run;
// "repeat" counts runs.
func flagCounts(res evalResult) map[string]int {
	c := map[string]int{}
	for _, run := range res.Runs {
		if !run.valid() {
			continue
		}
		for _, o := range run.Outfits {
			for _, f := range o.Flags {
				c[f]++
			}
		}
		if run.Repeat {
			c["repeat"]++
		}
	}
	return c
}

// flagTable renders the auto-flag table as Markdown.
func flagTable(results []evalResult) string {
	var b strings.Builder
	b.WriteString("| Scenario | Valid runs | First-try valid |")
	for _, f := range evalFlags {
		b.WriteString(" " + f + " |")
	}
	b.WriteString("\n|---|---|---|" + strings.Repeat("---|", len(evalFlags)) + "\n")
	for _, res := range results {
		valid, first := 0, 0
		for _, run := range res.Runs {
			if run.valid() {
				valid++
				if run.FirstTry {
					first++
				}
			}
		}
		c := flagCounts(res)
		fmt.Fprintf(&b, "| %s | %d/%d | %d/%d |", res.Scenario.Name, valid, len(res.Runs), first, len(res.Runs))
		for _, f := range evalFlags {
			fmt.Fprintf(&b, " %d |", c[f])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeEvalForm renders the Markdown form for one eval.
func writeEvalForm(h evalHeader, results []evalResult) string {
	valid, total, flags := 0, 0, 0
	for _, res := range results {
		for _, run := range res.Runs {
			total++
			if run.valid() {
				valid++
			}
		}
		for _, n := range flagCounts(res) {
			flags += n
		}
	}

	var b strings.Builder
	b.WriteString("# Recommender eval form\n\n")
	fmt.Fprintf(&b, "- Date: %s\n", h.When.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "- Label: %s\n", h.Label)
	fmt.Fprintf(&b, "- Commit: %s\n", h.Commit)
	fmt.Fprintf(&b, "- Fingerprint: %s\n", h.Fingerprint)
	fmt.Fprintf(&b, "- Model: %s\n", h.Model)
	fmt.Fprintf(&b, "- Temperature: %s\n", strconv.FormatFloat(h.Temperature, 'f', -1, 64))
	fmt.Fprintf(&b, "- Valid runs: %d/%d\n", valid, total)
	fmt.Fprintf(&b, "- Total auto-flags: %d\n\n", flags)
	b.WriteString("How to fill: for each outfit, replace `_` after Score with 1-5 (5 = I'd wear it as is) " +
		"and after Would wear with y or n; the note is optional. Rate each scenario's Variety 1-5. " +
		"Then run the summary (see README.md).\n")

	for _, res := range results {
		note := res.Scenario.Note
		if note == "" {
			note = "none"
		}
		fmt.Fprintf(&b, "\n## Scenario: %s\n\n", res.Scenario.Name)
		fmt.Fprintf(&b, "Weather: %s\n\n", describeWeather(res.Scenario.Forecast, res.Rules))
		fmt.Fprintf(&b, "Request note: %s\n", note)

		rated := -1
		for i, run := range res.Runs {
			if run.valid() {
				rated = i
				break
			}
		}
		if rated < 0 {
			b.WriteString("\nNo run of this scenario was valid, so there is nothing to rate.\n")
			continue
		}
		fmt.Fprintf(&b, "Rated run: %d of %d\n", rated+1, len(res.Runs))
		for i, o := range res.Runs[rated].Outfits {
			flags := "none"
			if len(o.Flags) > 0 {
				flags = strings.Join(o.Flags, ", ")
			}
			fmt.Fprintf(&b, "\n### Outfit %d\n\n", i+1)
			fmt.Fprintf(&b, "- Items: %s\n", describeItems(o.Items))
			fmt.Fprintf(&b, "- Reason: %q\n", o.Reason)
			fmt.Fprintf(&b, "- Auto-flags: %s\n", flags)
			b.WriteString("- Score (1-5): _\n- Would wear (y/n): _\n- Note:\n")
		}
		if res.Runs[rated].Repeat {
			b.WriteString("\nRun auto-flag: repeat\n")
		}
		b.WriteString("\n- Variety (1-5): _\n")
	}

	b.WriteString("\n## Auto-flags (all runs)\n\n")
	b.WriteString(flagTable(results))
	return b.String()
}

// parsedForm is what the summary needs from one form.
type parsedForm struct {
	Label, Fingerprint, Model, ValidRuns string
	TotalFlags                           int
	Outfits                              int // Score fields, rated or not
	Scores                               []int
	WouldWear                            []bool
	Variety                              []int
}

func (p parsedForm) rated() bool { return len(p.Scores)+len(p.WouldWear)+len(p.Variety) > 0 }

// parseEvalForm reads a form. Blank fields ("_" or empty) are unrated; a
// value outside the allowed range is an error naming the line.
func parseEvalForm(text string) (parsedForm, error) {
	var p parsedForm
	score := func(v string, n int, field string) (int, bool, error) {
		if v == "" || v == "_" {
			return 0, false, nil
		}
		s, err := strconv.Atoi(v)
		if err != nil || s < 1 || s > 5 {
			return 0, false, fmt.Errorf("line %d: %s must be 1-5 or _, got %q", n, field, v)
		}
		return s, true, nil
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		field, value, ok := strings.Cut(line, ":")
		if !ok || !strings.HasPrefix(field, "- ") {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimPrefix(field, "- ") {
		case "Label":
			p.Label = value
		case "Fingerprint":
			p.Fingerprint = value
		case "Model":
			p.Model = value
		case "Valid runs":
			p.ValidRuns = value
		case "Total auto-flags":
			p.TotalFlags, _ = strconv.Atoi(value)
		case "Score (1-5)":
			p.Outfits++
			s, ok, err := score(value, n, "Score")
			if err != nil {
				return p, err
			}
			if ok {
				p.Scores = append(p.Scores, s)
			}
		case "Variety (1-5)":
			s, ok, err := score(value, n, "Variety")
			if err != nil {
				return p, err
			}
			if ok {
				p.Variety = append(p.Variety, s)
			}
		case "Would wear (y/n)":
			switch strings.ToLower(value) {
			case "", "_":
			case "y", "yes":
				p.WouldWear = append(p.WouldWear, true)
			case "n", "no":
				p.WouldWear = append(p.WouldWear, false)
			default:
				return p, fmt.Errorf("line %d: Would wear must be y, n or _, got %q", n, value)
			}
		}
	}
	return p, sc.Err()
}

// summaryRow renders one summary line for a parsed form.
func summaryRow(file string, p parsedForm) string {
	avg := func(xs []int) string {
		if len(xs) == 0 {
			return "-"
		}
		sum := 0
		for _, x := range xs {
			sum += x
		}
		return strconv.FormatFloat(float64(sum)/float64(len(xs)), 'f', 2, 64)
	}
	wear := "-"
	if len(p.WouldWear) > 0 {
		yes := 0
		for _, w := range p.WouldWear {
			if w {
				yes++
			}
		}
		wear = fmt.Sprintf("%d%%", yes*100/len(p.WouldWear))
	}
	return fmt.Sprintf("%s | label=%s | fingerprint=%s | model=%s | rated=%d/%d | avg score=%s | would wear=%s | avg variety=%s | valid runs=%s | auto-flags=%d",
		file, p.Label, p.Fingerprint, p.Model, len(p.Scores), p.Outfits, avg(p.Scores), wear, avg(p.Variety), p.ValidRuns, p.TotalFlags)
}
