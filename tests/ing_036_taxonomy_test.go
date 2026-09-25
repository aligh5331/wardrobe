// Package tests holds black-box acceptance tests for backlog tickets.
// ING-036: GET /api/taxonomy serves the closed enum vocabulary the
// create/edit forms load — 200 JSON carrying every category with its
// valid subcategories, the color palette, patterns, warmth tiers, and
// formality — derived from the same internal/tagging tables
// ParseTaggingResult validates against (06-decisions.md "Taxonomy
// exported to the browser via GET /api/taxonomy"), so client and server
// cannot drift. Expected values come from wardrobe/internal/tagging's
// exported tables, never a hand-copied enum list (the spirit of
// internal/tagging's "tables match the spec" test); the wire key names
// are the API contract and are asserted as such, like ing019ItemKeys.
//
// The fourth AC (route registered, existing routes unchanged) is covered
// by TestING019_AC7_OnlyReadOnlyRoutes in ing_019_api_test.go: that
// ticket's route-set assertion was updated by ING-036 to the exact
// approved set {GET /api/items, GET /api/photos/:filename,
// GET /api/taxonomy} and later extended by ING-030 to the then-approved
// set including GET/PUT /api/items/:id, which proves both halves in one
// exact-set check.
package tests

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"wardrobe/internal/tagging"
)

// ing036Keys is the exact top-level key set GET /api/taxonomy returns:
// the five vocabulary lists 07-architecture.md "Taxonomy read route"
// names. Anything extra or missing is drift in the wire contract.
// These key names are the contract under test; the enum values
// themselves are never spelled out here — they are compared against
// tagging.TaxonomyTables().
var ing036Keys = []string{"categories", "colors", "patterns", "warmth_tiers", "formality"}

// AC1: Given the server is running / When a client GETs /api/taxonomy /
// Then it receives 200 with a JSON body carrying the closed vocabulary
// (every category with its valid subcategories, the color palette, the
// pattern values, the warmth tiers, and the formality values). The plain
// unauthenticated request succeeding also covers the "read-only local
// route, no auth" clause of the last AC.
func TestING036_AC1_TaxonomyRouteServes200JSON(t *testing.T) {
	engine, _, _ := newING019API(t)

	rr := ing019Do(t, engine, http.MethodGet, "/api/taxonomy")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/taxonomy status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /api/taxonomy body is not a JSON object: %v\n%s", err, rr.Body)
	}
	if len(body) != len(ing036Keys) {
		t.Errorf("response keys = %v, want exactly %v", ing019Keys(body), ing036Keys)
	}
	for _, k := range ing036Keys {
		if _, ok := body[k]; !ok {
			t.Errorf("response missing key %q; keys = %v", k, ing019Keys(body))
		}
	}
}

// AC2 (and AC3's drift guard): the response matches internal/tagging's
// tables exactly — same categories, same per-category subcategories,
// same color palette, same patterns / warmth tiers / formality. The
// expected side is tagging.TaxonomyTables(), the very tables
// ParseTaggingResult validates against, not a hand-copied list: if the
// route ever served a second hard-coded copy, this test fails as soon as
// the tables change. Combined with TestTaxonomyTablesMatchSpec
// (internal/tagging: tables == 03-taxonomy.md, re-checked against the
// spec file), the response is pinned to the approved vocabulary without
// re-parsing the spec here.
func TestING036_AC2_ResponseMatchesTaggingTables(t *testing.T) {
	engine, _, _ := newING019API(t)

	rr := ing019Do(t, engine, http.MethodGet, "/api/taxonomy")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/taxonomy status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}

	var got tagging.Taxonomy
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode taxonomy response: %v\n%s", err, rr.Body)
	}
	want := tagging.TaxonomyTables()

	if !slices.Equal(got.Colors, want.Colors) {
		t.Errorf("colors = %v, want the tagging tables' %v", got.Colors, want.Colors)
	}
	if !slices.Equal(got.Patterns, want.Patterns) {
		t.Errorf("patterns = %v, want the tagging tables' %v", got.Patterns, want.Patterns)
	}
	if !slices.Equal(got.WarmthTiers, want.WarmthTiers) {
		t.Errorf("warmth_tiers = %v, want the tagging tables' %v", got.WarmthTiers, want.WarmthTiers)
	}
	if !slices.Equal(got.Formality, want.Formality) {
		t.Errorf("formality = %v, want the tagging tables' %v", got.Formality, want.Formality)
	}

	if len(got.Categories) == 0 {
		t.Fatal("response carries no categories, want every approved category")
	}
	for category, wantSubs := range want.Categories {
		gotSubs, ok := got.Categories[category]
		if !ok {
			t.Errorf("category %q missing from response; got categories %v",
				category, got.Categories)
			continue
		}
		if !slices.Equal(gotSubs, wantSubs) {
			t.Errorf("subcategories for %q = %v, want the tagging tables' %v",
				category, gotSubs, wantSubs)
		}
	}
	for category := range got.Categories {
		if _, ok := want.Categories[category]; !ok {
			t.Errorf("response carries category %q, which is not in the tagging tables",
				category)
		}
	}
}
