// Package tests holds black-box acceptance tests for backlog tickets.
// ING-004: the raw VLM response is parsed as JSON and every field is
// validated against 03-taxonomy.md's enums and 04-data-schema.md's
// required fields, so only taxonomy-compliant tagging results ever reach
// the catalog.
//
// These tests exercise the exported API of wardrobe/internal/tagging
// only; implementation code is never touched from here. The enum
// vocabulary is parsed out of 03-taxonomy.md at runtime, so the tests
// check the spec rather than the ticket wording or a hand-copied list.
package tests

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wardrobe/internal/tagging"
)

// taxonomy is the closed enum vocabulary parsed from 03-taxonomy.md.
type taxonomy struct {
	categories    []string
	subcategories map[string][]string
	colors        []string
	patterns      []string
	warmthTiers   []string
	formalities   []string
}

// loadTaxonomy reads 03-taxonomy.md and extracts every enum table.
func loadTaxonomy(t *testing.T) taxonomy {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "03-taxonomy.md"))
	if err != nil {
		t.Fatalf("read 03-taxonomy.md: %v", err)
	}
	spec := string(raw)

	tax := taxonomy{
		categories:    csvValues(specBlock(t, spec, "## Categories")),
		colors:        csvValues(specBlock(t, spec, "## Color palette")),
		patterns:      csvValues(specBlock(t, spec, "## Pattern")),
		warmthTiers:   csvValues(specBlock(t, spec, "## Warmth tiers")),
		formalities:   csvValues(specBlock(t, spec, "## Formality")),
		subcategories: map[string][]string{},
	}
	for _, line := range strings.Split(specBlock(t, spec, "### Subcategories"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		category, list, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("subcategory line %q has no ':'", line)
		}
		tax.subcategories[strings.TrimSpace(category)] = csvValues(list)
	}

	if len(tax.categories) == 0 || len(tax.colors) == 0 || len(tax.patterns) == 0 ||
		len(tax.warmthTiers) == 0 || len(tax.formalities) == 0 || len(tax.subcategories) == 0 {
		t.Fatal("taxonomy parse produced an empty table; 03-taxonomy.md format changed?")
	}
	return tax
}

// specBlock returns the first fenced code block following header.
func specBlock(t *testing.T, spec, header string) string {
	t.Helper()

	i := strings.Index(spec, header)
	if i < 0 {
		t.Fatalf("03-taxonomy.md has no %q header", header)
	}
	rest := spec[i:]
	open := strings.Index(rest, "```")
	if open < 0 {
		t.Fatalf("no fenced code block after %q", header)
	}
	rest = rest[open+3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatalf("unterminated fenced code block after %q", header)
	}
	return rest[:end]
}

// csvValues splits a comma-separated fenced block into trimmed values,
// dropping the empty entries produced by wrapped lines.
func csvValues(block string) []string {
	var out []string
	for _, v := range strings.Split(block, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// omitted marks a field to be deleted from a test payload, as opposed to
// setting it to JSON null. json.Marshal cannot express "omit this key"
// from a map, so this sentinel is filtered out before marshalling.
type omittedField struct{}

var omitted = omittedField{}

// taggingJSON builds a valid tagging record from the spec vocabulary,
// applying overrides. A value of `omitted` removes the key; nil encodes
// JSON null.
func taggingJSON(t *testing.T, tax taxonomy, overrides map[string]any) string {
	t.Helper()

	category := tax.categories[0]
	base := map[string]any{
		"category":         category,
		"subcategory":      tax.subcategories[category][0],
		"dominant_color":   tax.colors[0],
		"secondary_colors": []string{tax.colors[1]},
		"pattern":          tax.patterns[0],
		"warmth_tier":      tax.warmthTiers[0],
		"formality":        tax.formalities[0],
	}
	for k, v := range overrides {
		if _, ok := v.(omittedField); ok {
			delete(base, k)
			continue
		}
		base[k] = v
	}

	b, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal test record: %v", err)
	}
	return string(b)
}

// foreignSubcategory returns a spec subcategory that is valid under some
// category other than the given one.
func foreignSubcategory(tax taxonomy, category string) string {
	for _, c := range tax.categories {
		if c == category {
			continue
		}
		if subs := tax.subcategories[c]; len(subs) > 0 {
			return subs[0]
		}
	}
	panic("taxonomy has only one category")
}

// assertInvalid checks the failure shape common to every validation
// error: it wraps ErrInvalidTaggingResult, is not a connectivity error,
// mentions the given fragments, and returns no partial result.
func assertInvalid(t *testing.T, got tagging.TaggingResult, err error, fragments ...string) {
	t.Helper()

	if err == nil {
		t.Fatal("ParseTaggingResult() error = nil, want validation failure")
	}
	if !errors.Is(err, tagging.ErrInvalidTaggingResult) {
		t.Errorf("errors.Is(err, ErrInvalidTaggingResult) = false, err = %v", err)
	}
	if errors.Is(err, tagging.ErrVLMUnreachable) {
		t.Errorf("validation failure must not be ErrVLMUnreachable, err = %v", err)
	}
	for _, want := range fragments {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err.Error(), want)
		}
	}
	if !reflect.DeepEqual(got, tagging.TaggingResult{}) {
		t.Errorf("ParseTaggingResult() = %+v, want zero result on failure", got)
	}
}

// AC1: Given a raw VLM response that is valid JSON matching the expected
// schema / When it is validated / Then a typed tagging result is produced
// with category, subcategory, dominant_color, secondary_colors, pattern,
// warmth_tier, and formality all populated.
//
// Exercised for every category/subcategory pair in 03-taxonomy.md.
func TestING004_AC1_ValidRecordProducesPopulatedResult(t *testing.T) {
	tax := loadTaxonomy(t)

	for _, category := range tax.categories {
		for _, subcategory := range tax.subcategories[category] {
			t.Run(category+"/"+subcategory, func(t *testing.T) {
				want := tagging.TaggingResult{
					Category:        category,
					Subcategory:     subcategory,
					DominantColor:   tax.colors[0],
					SecondaryColors: []string{tax.colors[1]},
					Pattern:         tax.patterns[0],
					WarmthTier:      tax.warmthTiers[0],
					Formality:       tax.formalities[0],
				}
				raw := taggingJSON(t, tax, map[string]any{
					"category":         category,
					"subcategory":      subcategory,
					"dominant_color":   want.DominantColor,
					"secondary_colors": want.SecondaryColors,
					"pattern":          want.Pattern,
					"warmth_tier":      want.WarmthTier,
					"formality":        want.Formality,
				})

				got, err := tagging.ParseTaggingResult(raw)
				if err != nil {
					t.Fatalf("ParseTaggingResult() error = %v, want success", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("ParseTaggingResult() = %+v, want %+v", got, want)
				}
				// Belt and braces: every field is non-empty.
				for name, v := range map[string]string{
					"category":       got.Category,
					"subcategory":    got.Subcategory,
					"dominant_color": got.DominantColor,
					"pattern":        got.Pattern,
					"warmth_tier":    got.WarmthTier,
					"formality":      got.Formality,
				} {
					if v == "" {
						t.Errorf("%s is empty, want populated", name)
					}
				}
				if got.SecondaryColors == nil {
					t.Error("secondary_colors is nil, want populated (may be empty)")
				}
			})
		}
	}
}

// AC2: Given a response where subcategory is not one of the values listed
// under its category in 03-taxonomy.md (e.g. category=top,
// subcategory=jeans) / When it is validated / Then validation fails with
// a specific error naming the invalid category/subcategory pair.
func TestING004_AC2_SubcategoryMustBelongToCategory(t *testing.T) {
	tax := loadTaxonomy(t)

	for _, category := range tax.categories {
		foreign := foreignSubcategory(tax, category)
		t.Run(category+"/"+foreign, func(t *testing.T) {
			raw := taggingJSON(t, tax, map[string]any{
				"category":    category,
				"subcategory": foreign,
			})
			got, err := tagging.ParseTaggingResult(raw)
			// Message must name the pair; "pair" distinguishes it from the
			// generic invalid-enum wording.
			assertInvalid(t, got, err, category, foreign, "pair")
		})
	}

	t.Run("ticket-example-top-jeans", func(t *testing.T) {
		raw := taggingJSON(t, tax, map[string]any{"category": "top", "subcategory": "jeans"})
		got, err := tagging.ParseTaggingResult(raw)
		assertInvalid(t, got, err, `"top"`, `"jeans"`, "pair")
	})
}

// AC3: Given a response missing subcategory, or with subcategory: null /
// When it is validated / Then validation fails — subcategory is required
// and must not be null.
func TestING004_AC3_SubcategoryRequired(t *testing.T) {
	tax := loadTaxonomy(t)

	cases := []struct {
		name      string
		overrides map[string]any
	}{
		{name: "missing", overrides: map[string]any{"subcategory": omitted}},
		{name: "null", overrides: map[string]any{"subcategory": nil}},
		{name: "empty-string", overrides: map[string]any{"subcategory": ""}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tagging.ParseTaggingResult(taggingJSON(t, tax, tt.overrides))
			assertInvalid(t, got, err, "subcategory", "required")
		})
	}
}

// AC4: Given a response missing pattern, or with pattern: null / When it
// is validated / Then validation fails — pattern is required and must not
// be null.
func TestING004_AC4_PatternRequired(t *testing.T) {
	tax := loadTaxonomy(t)

	cases := []struct {
		name      string
		overrides map[string]any
	}{
		{name: "missing", overrides: map[string]any{"pattern": omitted}},
		{name: "null", overrides: map[string]any{"pattern": nil}},
		{name: "empty-string", overrides: map[string]any{"pattern": ""}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tagging.ParseTaggingResult(taggingJSON(t, tax, tt.overrides))
			assertInvalid(t, got, err, "pattern", "required")
		})
	}
}

// AC5: Given a response with a dominant_color or secondary_colors value
// outside the 16-color palette (e.g. "gold") / When it is validated /
// Then validation fails with a specific error naming the out-of-palette
// value — do not silently coerce or drop it.
func TestING004_AC5_OutOfPaletteColorRejectedNotCoerced(t *testing.T) {
	tax := loadTaxonomy(t)

	// Assert the parsed palette really has 16 colors, so "outside the
	// 16-color palette" is checked against the spec, not assumed.
	if len(tax.colors) != 16 {
		t.Fatalf("parsed color palette has %d values, want 16: %v", len(tax.colors), tax.colors)
	}

	t.Run("dominant_color", func(t *testing.T) {
		for _, bad := range []string{"gold", "silver", "teal"} {
			t.Run(bad, func(t *testing.T) {
				got, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"dominant_color": bad}))
				assertInvalid(t, got, err, "dominant_color", bad)
			})
		}
	})

	t.Run("secondary_colors", func(t *testing.T) {
		for _, bad := range []string{"gold", "silver"} {
			t.Run(bad, func(t *testing.T) {
				got, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"secondary_colors": []string{bad}}))
				assertInvalid(t, got, err, "secondary_colors", bad)
			})
		}
	})

	// The value must not be silently dropped while the rest of the record
	// is accepted: a mixed list must fail and return no partial result.
	t.Run("mixed-list-not-dropped", func(t *testing.T) {
		raw := taggingJSON(t, tax, map[string]any{"secondary_colors": []string{tax.colors[0], "gold"}})
		got, err := tagging.ParseTaggingResult(raw)
		assertInvalid(t, got, err, "gold")
		if got.SecondaryColors != nil {
			t.Errorf("SecondaryColors = %v, want zero result (out-of-palette value must not be dropped)", got.SecondaryColors)
		}
	})
}

// Spec-implied edge: every enum value in 03-taxonomy.md must be accepted
// (the validator must not be stricter than the spec). AC1 covers
// subcategories; this covers the flat enums and callers' color fields.
func TestING004_EverySpecEnumValueAccepted(t *testing.T) {
	tax := loadTaxonomy(t)

	t.Run("dominant_color", func(t *testing.T) {
		for _, c := range tax.colors {
			t.Run(c, func(t *testing.T) {
				_, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"dominant_color": c}))
				if err != nil {
					t.Errorf("dominant_color %q from the spec rejected: %v", c, err)
				}
			})
		}
	})
	t.Run("secondary_colors", func(t *testing.T) {
		for _, c := range tax.colors {
			t.Run(c, func(t *testing.T) {
				_, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"secondary_colors": []string{c}}))
				if err != nil {
					t.Errorf("secondary_colors value %q from the spec rejected: %v", c, err)
				}
			})
		}
	})
	t.Run("pattern", func(t *testing.T) {
		for _, p := range tax.patterns {
			t.Run(p, func(t *testing.T) {
				_, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"pattern": p}))
				if err != nil {
					t.Errorf("pattern %q from the spec rejected: %v", p, err)
				}
			})
		}
	})
	t.Run("warmth_tier", func(t *testing.T) {
		for _, w := range tax.warmthTiers {
			t.Run(w, func(t *testing.T) {
				_, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"warmth_tier": w}))
				if err != nil {
					t.Errorf("warmth_tier %q from the spec rejected: %v", w, err)
				}
			})
		}
	})
	t.Run("formality", func(t *testing.T) {
		for _, f := range tax.formalities {
			t.Run(f, func(t *testing.T) {
				_, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"formality": f}))
				if err != nil {
					t.Errorf("formality %q from the spec rejected: %v", f, err)
				}
			})
		}
	})
}

// Spec-implied edge: 05-vlm-tagging-spec.md "Output validation" requires
// rejecting invalid enum values and missing required fields. Every
// tagging field is required for a persisted record (04-data-schema.md
// marks only `notes` optional); secondary_colors may be empty but not
// null/omitted.
func TestING004_InvalidEnumAndMissingFieldRejected(t *testing.T) {
	tax := loadTaxonomy(t)

	enumCases := []struct {
		name      string
		overrides map[string]any
		field     string
		value     string
	}{
		{"category", map[string]any{"category": "dress"}, "category", "dress"},
		{"pattern", map[string]any{"pattern": "floral"}, "pattern", "floral"},
		{"warmth_tier", map[string]any{"warmth_tier": "warm"}, "warmth_tier", "warm"},
		{"formality", map[string]any{"formality": "business"}, "formality", "business"},
		{"dominant_color", map[string]any{"dominant_color": "gold"}, "dominant_color", "gold"},
	}
	for _, tt := range enumCases {
		t.Run("enum/"+tt.name, func(t *testing.T) {
			got, err := tagging.ParseTaggingResult(taggingJSON(t, tax, tt.overrides))
			assertInvalid(t, got, err, tt.field, tt.value)
		})
	}

	requiredCases := []struct {
		name      string
		overrides map[string]any
		field     string
	}{
		{"category", map[string]any{"category": omitted}, "category"},
		{"category_null", map[string]any{"category": nil}, "category"},
		{"subcategory", map[string]any{"subcategory": omitted}, "subcategory"},
		{"dominant_color", map[string]any{"dominant_color": omitted}, "dominant_color"},
		{"secondary_colors_missing", map[string]any{"secondary_colors": omitted}, "secondary_colors"},
		{"secondary_colors_null", map[string]any{"secondary_colors": nil}, "secondary_colors"},
		{"pattern", map[string]any{"pattern": omitted}, "pattern"},
		{"warmth_tier", map[string]any{"warmth_tier": omitted}, "warmth_tier"},
		{"formality", map[string]any{"formality": omitted}, "formality"},
	}
	for _, tt := range requiredCases {
		t.Run("required/"+tt.name, func(t *testing.T) {
			got, err := tagging.ParseTaggingResult(taggingJSON(t, tax, tt.overrides))
			assertInvalid(t, got, err, tt.field, "required")
		})
	}
}

// Spec-implied edge: malformed JSON is rejected (05-vlm-tagging-spec.md
// "Output validation"), including empty input, a truncated object, a JSON
// array/scalar, and markdown-fenced JSON the model may emit.
func TestING004_MalformedJSONRejected(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"prose":          "here is your tagging result",
		"truncated":      `{"category": "top", "subcategory": "t-shirt"`,
		"array":          `[{"category":"top"}]`,
		"scalar":         `"top"`,
		"markdown_fence": "```json\n{\"category\":\"top\"}\n```",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tagging.ParseTaggingResult(raw)
			assertInvalid(t, got, err, "malformed json")
		})
	}
}

// Spec-implied edge: secondary_colors "0 or more" means an empty list is
// valid (the prompt instructs the model to send [] when there are none).
// Unknown extra keys are not in the spec's rejection list and are ignored.
func TestING004_EmptySecondaryColorsAndUnknownFieldsAccepted(t *testing.T) {
	tax := loadTaxonomy(t)

	t.Run("empty_secondary_colors", func(t *testing.T) {
		got, err := tagging.ParseTaggingResult(taggingJSON(t, tax, map[string]any{"secondary_colors": []string{}}))
		if err != nil {
			t.Fatalf("ParseTaggingResult() error = %v, want success", err)
		}
		if got.SecondaryColors == nil || len(got.SecondaryColors) != 0 {
			t.Errorf("SecondaryColors = %#v, want empty non-nil slice", got.SecondaryColors)
		}
	})

	t.Run("unknown_fields", func(t *testing.T) {
		raw := `{"category":"top","subcategory":"t-shirt","dominant_color":"white",
			"secondary_colors":[],"pattern":"solid","warmth_tier":"light",
			"formality":"casual","confidence":0.9,"note":"extra chatter"}`
		if _, err := tagging.ParseTaggingResult(raw); err != nil {
			t.Errorf("ParseTaggingResult() error = %v, want unknown fields ignored", err)
		}
	})
}
