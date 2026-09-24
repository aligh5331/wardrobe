package tagging

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrInvalidTaggingResult marks a VLM response that arrived but is unusable:
// malformed JSON, a missing required field, or an enum value outside
// 03-taxonomy.md. It is deliberately separate from ErrVLMUnreachable —
// connectivity failure and bad model output are different failure modes
// (05-vlm-tagging-spec.md "Output validation"). Callers can errors.Is on it
// to decide between retrying and flagging for manual review.
var ErrInvalidTaggingResult = errors.New("invalid tagging result")

// Failure types recorded in ING-005's attempt log. They are exactly the three
// rejection reasons in 05-vlm-tagging-spec.md "Output validation".
const (
	FailureTypeMalformedJSON        = "malformed_json"
	FailureTypeInvalidEnum          = "invalid_enum"
	FailureTypeMissingRequiredField = "missing_required_field"
)

// ValidationError is a taxonomy/schema validation failure. FailureType is one
// of the FailureType* constants and FailureDetail names the specific field or
// value, so ING-005 can log a failure without parsing its message. It wraps
// ErrInvalidTaggingResult, so errors.Is(err, ErrInvalidTaggingResult) still
// identifies bad model output.
type ValidationError struct {
	FailureType   string
	FailureDetail string
	err           error
}

func (e *ValidationError) Error() string { return e.err.Error() }
func (e *ValidationError) Unwrap() error { return e.err }

// newValidationError builds a ValidationError whose message preserves the
// pre-existing "invalid tagging result: <detail>" wording.
func newValidationError(failureType, detail string) *ValidationError {
	return &ValidationError{
		FailureType:   failureType,
		FailureDetail: detail,
		err:           fmt.Errorf("%w: %s", ErrInvalidTaggingResult, detail),
	}
}

// TaggingResult is the validated tagging output for one garment: exactly the
// tagging fields of 04-data-schema.md. Catalog-only fields (id, photo_path,
// added_date, notes) are added when the record is written to the store, not
// by the VLM.
type TaggingResult struct {
	Category        string   `json:"category"`
	Subcategory     string   `json:"subcategory"`
	DominantColor   string   `json:"dominant_color"`
	SecondaryColors []string `json:"secondary_colors"`
	Pattern         string   `json:"pattern"`
	WarmthTier      string   `json:"warmth_tier"`
	Formality       string   `json:"formality"`
}

// Taxonomy tables, transcribed from 03-taxonomy.md. They are the closed
// vocabulary every tagging result is validated against; keep them in sync
// with the spec (validate_test.go asserts they match the spec file).
var (
	validCategories = []string{"top", "bottom", "outerwear", "footwear", "headwear", "accessory"}

	validSubcategories = map[string][]string{
		"top":       {"t-shirt", "polo", "shirt", "sweater", "hoodie", "sweatshirt", "tank-top"},
		"bottom":    {"jeans", "chinos", "dress-pants", "shorts", "sweatpants"},
		"outerwear": {"jacket", "coat", "blazer", "vest"},
		"footwear":  {"sneakers", "boots", "dress-shoes", "sandals", "loafers"},
		"headwear":  {"cap", "beanie", "hat"},
		"accessory": {"belt", "scarf", "tie", "bag", "watch", "sunglasses", "gloves"},
	}

	colorPalette = []string{
		"black", "white", "gray", "navy", "blue", "red", "green", "olive",
		"brown", "tan", "beige", "burgundy", "pink", "purple", "yellow", "orange",
	}

	validPatterns    = []string{"solid", "striped", "plaid", "print"}
	validWarmthTiers = []string{"light", "medium", "heavy"}
	validFormalities = []string{"casual", "smart-casual", "formal"}
)

// Taxonomy is the closed enum vocabulary (03-taxonomy.md) in the JSON
// shape GET /api/taxonomy serves to the create/edit forms
// (07-architecture.md "Taxonomy read route"). Categories maps each
// category to its valid subcategories.
type Taxonomy struct {
	Categories  map[string][]string `json:"categories"`
	Colors      []string            `json:"colors"`
	Patterns    []string            `json:"patterns"`
	WarmthTiers []string            `json:"warmth_tiers"`
	Formality   []string            `json:"formality"`
}

// TaxonomyTables returns the tables ParseTaggingResult validates against,
// so GET /api/taxonomy serves the single source instead of a second
// hard-coded copy (06-decisions.md "Taxonomy exported to the browser via
// GET /api/taxonomy, not a bundled copy"). Categories is keyed from
// validCategories so every approved category appears with its
// subcategories.
func TaxonomyTables() Taxonomy {
	categories := make(map[string][]string, len(validCategories))
	for _, category := range validCategories {
		categories[category] = validSubcategories[category]
	}
	return Taxonomy{
		Categories:  categories,
		Colors:      colorPalette,
		Patterns:    validPatterns,
		WarmthTiers: validWarmthTiers,
		Formality:   validFormalities,
	}
}

// ParseTaggingResult parses raw model text and validates every field against
// 03-taxonomy.md's enums and 04-data-schema.md's required fields. It returns
// a typed TaggingResult only when the response is valid JSON and fully
// taxonomy-compliant; every failure wraps ErrInvalidTaggingResult.
//
// Required fields (05-vlm-tagging-spec.md "Output validation"): subcategory
// and pattern must not be null or omitted; the remaining tagging fields are
// likewise required because they are written to the catalog. Missing or null
// values are rejected, and out-of-palette colors are reported, never coerced
// or dropped.
func ParseTaggingResult(raw string) (TaggingResult, error) {
	var r TaggingResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return TaggingResult{}, &ValidationError{
			FailureType:   FailureTypeMalformedJSON,
			FailureDetail: "malformed json: " + err.Error(),
			err:           fmt.Errorf("%w: malformed json: %w", ErrInvalidTaggingResult, err),
		}
	}
	if err := r.validate(); err != nil {
		return TaggingResult{}, err
	}
	return r, nil
}

func (r TaggingResult) validate() error {
	if r.Category == "" {
		return requiredFieldError("category")
	}
	if !slices.Contains(validCategories, r.Category) {
		return enumFieldError("category", r.Category, validCategories)
	}

	if r.Subcategory == "" {
		return requiredFieldError("subcategory")
	}
	if !slices.Contains(validSubcategories[r.Category], r.Subcategory) {
		return newValidationError(FailureTypeInvalidEnum, fmt.Sprintf(
			"invalid category/subcategory pair %q/%q; valid subcategories for %q: %s",
			r.Category, r.Subcategory, r.Category,
			strings.Join(validSubcategories[r.Category], ", ")))
	}

	if r.DominantColor == "" {
		return requiredFieldError("dominant_color")
	}
	if !slices.Contains(colorPalette, r.DominantColor) {
		return enumFieldError("dominant_color", r.DominantColor, colorPalette)
	}

	// secondary_colors may be empty ([]), but must be present: null or an
	// omitted field is a missing required field.
	if r.SecondaryColors == nil {
		return requiredFieldError("secondary_colors")
	}
	for i, c := range r.SecondaryColors {
		if !slices.Contains(colorPalette, c) {
			return newValidationError(FailureTypeInvalidEnum, fmt.Sprintf(
				"secondary_colors[%d] %q is not in the palette; allowed: %s",
				i, c, strings.Join(colorPalette, ", ")))
		}
	}

	if r.Pattern == "" {
		return requiredFieldError("pattern")
	}
	if !slices.Contains(validPatterns, r.Pattern) {
		return enumFieldError("pattern", r.Pattern, validPatterns)
	}

	if r.WarmthTier == "" {
		return requiredFieldError("warmth_tier")
	}
	if !slices.Contains(validWarmthTiers, r.WarmthTier) {
		return enumFieldError("warmth_tier", r.WarmthTier, validWarmthTiers)
	}

	if r.Formality == "" {
		return requiredFieldError("formality")
	}
	if !slices.Contains(validFormalities, r.Formality) {
		return enumFieldError("formality", r.Formality, validFormalities)
	}

	return nil
}

func requiredFieldError(field string) error {
	return newValidationError(FailureTypeMissingRequiredField,
		fmt.Sprintf("missing required field %s (must not be null or omitted)", field))
}

func enumFieldError(field, value string, allowed []string) error {
	return newValidationError(FailureTypeInvalidEnum,
		fmt.Sprintf("%s %q is not valid; allowed: %s", field, value, strings.Join(allowed, ", ")))
}
