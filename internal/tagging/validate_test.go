package tagging

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// omit marks a field to be deleted from the test payload, as opposed to
// setting it to JSON null.
type omitKind struct{}

func validJSON(t *testing.T, overrides map[string]any) string {
	t.Helper()
	base := map[string]any{
		"category":         "bottom",
		"subcategory":      "jeans",
		"dominant_color":   "navy",
		"secondary_colors": []string{"white"},
		"pattern":          "solid",
		"warmth_tier":      "medium",
		"formality":        "casual",
	}
	for k, v := range overrides {
		if _, ok := v.(omitKind); ok {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal test payload: %v", err)
	}
	return string(b)
}

// AC1: valid JSON matching the schema produces a typed result with every
// tagging field populated.
func TestParseTaggingResult_Valid(t *testing.T) {
	got, err := ParseTaggingResult(validJSON(t, nil))
	if err != nil {
		t.Fatalf("ParseTaggingResult() error = %v", err)
	}
	want := TaggingResult{
		Category:        "bottom",
		Subcategory:     "jeans",
		DominantColor:   "navy",
		SecondaryColors: []string{"white"},
		Pattern:         "solid",
		WarmthTier:      "medium",
		Formality:       "casual",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTaggingResult() = %+v, want %+v", got, want)
	}
}

func TestParseTaggingResult_ValidEmptySecondaryColors(t *testing.T) {
	got, err := ParseTaggingResult(validJSON(t, map[string]any{"secondary_colors": []string{}}))
	if err != nil {
		t.Fatalf("ParseTaggingResult() error = %v", err)
	}
	if got.SecondaryColors == nil || len(got.SecondaryColors) != 0 {
		t.Errorf("SecondaryColors = %#v, want empty non-nil slice", got.SecondaryColors)
	}
}

// AC2: a subcategory that does not belong to its category fails with a
// specific error naming the invalid category/subcategory pair.
func TestParseTaggingResult_InvalidSubcategoryForCategory(t *testing.T) {
	_, err := ParseTaggingResult(validJSON(t, map[string]any{
		"category":    "top",
		"subcategory": "jeans",
	}))
	if err == nil {
		t.Fatal("ParseTaggingResult() error = nil, want invalid category/subcategory pair")
	}
	if !errors.Is(err, ErrInvalidTaggingResult) {
		t.Errorf("errors.Is(err, ErrInvalidTaggingResult) = false, err = %v", err)
	}
	for _, want := range []string{"category", "subcategory", "pair", `"top"`, `"jeans"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err.Error(), want)
		}
	}
}

// AC3: subcategory is required — missing, null, or empty all fail.
func TestParseTaggingResult_SubcategoryRequired(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"omitted", validJSON(t, map[string]any{"subcategory": omitKind{}})},
		{"null", validJSON(t, map[string]any{"subcategory": nil})},
		{"empty", validJSON(t, map[string]any{"subcategory": ""})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTaggingResult(tt.raw)
			if err == nil {
				t.Fatal("ParseTaggingResult() error = nil, want subcategory required")
			}
			if !errors.Is(err, ErrInvalidTaggingResult) {
				t.Errorf("errors.Is(err, ErrInvalidTaggingResult) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), "subcategory") || !strings.Contains(err.Error(), "required") {
				t.Errorf("error %q, want it to name subcategory as required", err.Error())
			}
			if !reflect.DeepEqual(got, TaggingResult{}) {
				t.Errorf("ParseTaggingResult() = %+v, want zero result on failure", got)
			}
		})
	}
}

// AC4: pattern is required — missing, null, or empty all fail.
func TestParseTaggingResult_PatternRequired(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"omitted", validJSON(t, map[string]any{"pattern": omitKind{}})},
		{"null", validJSON(t, map[string]any{"pattern": nil})},
		{"empty", validJSON(t, map[string]any{"pattern": ""})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTaggingResult(tt.raw)
			if err == nil {
				t.Fatal("ParseTaggingResult() error = nil, want pattern required")
			}
			if !errors.Is(err, ErrInvalidTaggingResult) {
				t.Errorf("errors.Is(err, ErrInvalidTaggingResult) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), "pattern") || !strings.Contains(err.Error(), "required") {
				t.Errorf("error %q, want it to name pattern as required", err.Error())
			}
			if !reflect.DeepEqual(got, TaggingResult{}) {
				t.Errorf("ParseTaggingResult() = %+v, want zero result on failure", got)
			}
		})
	}
}

// AC5: an out-of-palette color fails with a specific error naming the value;
// it is never coerced or dropped.
func TestParseTaggingResult_OutOfPaletteColor(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"dominant", validJSON(t, map[string]any{"dominant_color": "gold"})},
		{"secondary", validJSON(t, map[string]any{"secondary_colors": []string{"gold"}})},
		{"secondary after valid", validJSON(t, map[string]any{"secondary_colors": []string{"navy", "gold"}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTaggingResult(tt.raw)
			if err == nil {
				t.Fatal("ParseTaggingResult() error = nil, want out-of-palette failure")
			}
			if !errors.Is(err, ErrInvalidTaggingResult) {
				t.Errorf("errors.Is(err, ErrInvalidTaggingResult) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), "gold") {
				t.Errorf("error %q does not name the out-of-palette value", err.Error())
			}
			if !reflect.DeepEqual(got, TaggingResult{}) {
				t.Errorf("ParseTaggingResult() = %+v, want zero result on failure", got)
			}
		})
	}
}

func TestParseTaggingResult_MalformedJSON(t *testing.T) {
	for _, raw := range []string{"not json", `{"category": "top"`, ""} {
		_, err := ParseTaggingResult(raw)
		if err == nil {
			t.Fatalf("ParseTaggingResult(%q) error = nil, want malformed json", raw)
		}
		if !errors.Is(err, ErrInvalidTaggingResult) {
			t.Errorf("ParseTaggingResult(%q) not ErrInvalidTaggingResult: %v", raw, err)
		}
		if !strings.Contains(err.Error(), "malformed json") {
			t.Errorf("error %q, want malformed json", err.Error())
		}
	}
}

// ING-005: validation failures carry a machine-readable failure type and a
// detail naming the specific field/value, while still wrapping
// ErrInvalidTaggingResult.
func TestParseTaggingResult_FailureTypes(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantType string
	}{
		{"malformed json", "not json", FailureTypeMalformedJSON},
		{"missing required field", validJSON(t, map[string]any{"subcategory": nil}), FailureTypeMissingRequiredField},
		{"invalid enum", validJSON(t, map[string]any{"dominant_color": "gold"}), FailureTypeInvalidEnum},
		{"invalid subcategory pair", validJSON(t, map[string]any{"category": "top", "subcategory": "jeans"}), FailureTypeInvalidEnum},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTaggingResult(tt.raw)
			if err == nil {
				t.Fatal("ParseTaggingResult() error = nil, want validation failure")
			}
			if !errors.Is(err, ErrInvalidTaggingResult) {
				t.Errorf("errors.Is(err, ErrInvalidTaggingResult) = false, err = %v", err)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error %v is not a *ValidationError", err)
			}
			if ve.FailureType != tt.wantType {
				t.Errorf("FailureType = %q, want %q", ve.FailureType, tt.wantType)
			}
			if ve.FailureDetail == "" {
				t.Error("FailureDetail is empty, want the specific field/value")
			}
		})
	}
}

// Every enum field must be in the taxonomy, and every tagging field is
// required for the catalog row.
func TestParseTaggingResult_InvalidEnumsAndMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"missing category", validJSON(t, map[string]any{"category": omitKind{}}), "category"},
		{"invalid category", validJSON(t, map[string]any{"category": "dress"}), "category"},
		{"missing dominant_color", validJSON(t, map[string]any{"dominant_color": omitKind{}}), "dominant_color"},
		{"null dominant_color", validJSON(t, map[string]any{"dominant_color": nil}), "dominant_color"},
		{"missing secondary_colors", validJSON(t, map[string]any{"secondary_colors": omitKind{}}), "secondary_colors"},
		{"null secondary_colors", validJSON(t, map[string]any{"secondary_colors": nil}), "secondary_colors"},
		{"invalid pattern", validJSON(t, map[string]any{"pattern": "floral"}), "pattern"},
		{"invalid warmth_tier", validJSON(t, map[string]any{"warmth_tier": "warm"}), "warmth_tier"},
		{"missing warmth_tier", validJSON(t, map[string]any{"warmth_tier": omitKind{}}), "warmth_tier"},
		{"invalid formality", validJSON(t, map[string]any{"formality": "business"}), "formality"},
		{"missing formality", validJSON(t, map[string]any{"formality": omitKind{}}), "formality"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTaggingResult(tt.raw)
			if err == nil {
				t.Fatal("ParseTaggingResult() error = nil, want validation failure")
			}
			if !errors.Is(err, ErrInvalidTaggingResult) {
				t.Errorf("errors.Is(err, ErrInvalidTaggingResult) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q, want it to name %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParseTaggingResult_UnknownFieldsTolerated(t *testing.T) {
	raw := `{"category":"bottom","subcategory":"jeans","dominant_color":"navy",
		"secondary_colors":[],"pattern":"solid","warmth_tier":"medium",
		"formality":"casual","model_note":"extra chatter"}`
	if _, err := ParseTaggingResult(raw); err != nil {
		t.Errorf("ParseTaggingResult() error = %v, want unknown fields tolerated", err)
	}
}

// The Go enum tables must match 03-taxonomy.md exactly — no value invented
// and none of the spec's values missing (03-taxonomy.md "Change process").
func TestTaxonomyTablesMatchSpec(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "03-taxonomy.md"))
	if err != nil {
		t.Fatalf("read taxonomy spec: %v", err)
	}
	spec := string(raw)

	assertSameSet(t, "categories", validCategories, commaList(specCodeBlock(t, spec, "## Categories")))
	assertSameSet(t, "color palette", colorPalette, commaList(specCodeBlock(t, spec, "## Color palette")))
	assertSameSet(t, "patterns", validPatterns, commaList(specCodeBlock(t, spec, "## Pattern")))
	assertSameSet(t, "warmth tiers", validWarmthTiers, commaList(specCodeBlock(t, spec, "## Warmth tiers")))
	assertSameSet(t, "formalities", validFormalities, commaList(specCodeBlock(t, spec, "## Formality")))

	specSubs := map[string][]string{}
	for _, line := range strings.Split(specCodeBlock(t, spec, "### Subcategories"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		category, list, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("subcategory line %q missing ':'", line)
		}
		specSubs[strings.TrimSpace(category)] = commaList(list)
	}
	if len(specSubs) != len(validSubcategories) {
		t.Fatalf("spec has %d categories, code has %d", len(specSubs), len(validSubcategories))
	}
	for category, want := range specSubs {
		got, ok := validSubcategories[category]
		if !ok {
			t.Errorf("code is missing subcategories for category %q", category)
			continue
		}
		assertSameSet(t, "subcategories for "+category, got, want)
	}
}

// specCodeBlock returns the contents of the first fenced code block that
// follows the given markdown header in spec.
func specCodeBlock(t *testing.T, spec, header string) string {
	t.Helper()
	i := strings.Index(spec, header)
	if i < 0 {
		t.Fatalf("spec header %q not found", header)
	}
	rest := spec[i:]
	start := strings.Index(rest, "```")
	if start < 0 {
		t.Fatalf("no code block after %q", header)
	}
	rest = rest[start+len("```"):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatalf("unterminated code block after %q", header)
	}
	return rest[:end]
}

// commaList splits a comma-separated code block into trimmed values,
// dropping empty entries from line breaks.
func commaList(block string) []string {
	var out []string
	for _, part := range strings.Split(block, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func assertSameSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	g, w := slices.Clone(got), slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Errorf("%s mismatch:\n code = %v\n spec = %v", name, g, w)
	}
}
