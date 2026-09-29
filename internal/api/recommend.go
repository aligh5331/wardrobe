package api

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
	"wardrobe/internal/weather"
)

const (
	// maxNoteRunes is the note length limit (07-architecture.md "Recommender").
	maxNoteRunes = 500
	// maxRecommendBody caps the request body. The note is at most 500
	// characters, so 64 KiB is far above any valid request.
	maxRecommendBody = 64 << 10
)

// outfitCategoryOrder is the order of items inside an outfit
// (07-architecture.md "Recommender").
var outfitCategoryOrder = map[string]int{
	"outerwear": 0, "top": 1, "bottom": 2, "footwear": 3, "headwear": 4, "accessory": 5,
}

type recommendRequest struct {
	Formality string `json:"formality"`
	Note      string `json:"note"`
}

type outfitResponse struct {
	Items  []itemResponse `json:"items"`
	Reason string         `json:"reason"`
}

type recommendResponse struct {
	Weather weatherResponse  `json:"weather"`
	Outfits []outfitResponse `json:"outfits"`
}

// postRecommendations serves POST /api/recommendations
// (07-architecture.md "Recommender (Phase 3)"). Validation, weather, catalog
// and the missing-slot check all run before the LLM, so a failure in any of
// them never calls it.
func postRecommendations(st *store.Store, w *weather.Client, p *recommend.Picker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "recommender is not configured"})
			return
		}

		var body recommendRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRecommendBody)
		// io.EOF means an empty body, which is valid.
		// ponytail: data after the first JSON value is ignored.
		if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
			return
		}
		if body.Formality != "" && !slices.Contains(tagging.TaxonomyTables().Formality, body.Formality) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid formality: must be one of " +
				strings.Join(tagging.TaxonomyTables().Formality, ", ")})
			return
		}
		note := strings.TrimSpace(body.Note)
		if utf8.RuneCountInString(note) > maxNoteRunes {
			c.JSON(http.StatusBadRequest, gin.H{"error": "note must be at most 500 characters"})
			return
		}

		wr, status, msg := fetchWeather(c, st, w)
		if status != http.StatusOK {
			c.JSON(status, gin.H{"error": msg})
			return
		}

		items, err := st.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list items"})
			return
		}
		rules := recommend.RulesFor(wr.Forecast)
		cands := recommend.Filter(items, rules, body.Formality)
		if missing := recommend.MissingSlots(cands, rules); len(missing) > 0 {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "cannot build outfits, missing: " + strings.Join(missing, ", ")})
			return
		}

		outfits, err := p.Pick(c.Request.Context(), recommend.Input{
			Forecast: wr.Forecast, Rules: rules, Formality: body.Formality, Note: note, Candidates: cands,
		})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		byID := make(map[string]store.Item, len(cands))
		for _, it := range cands {
			byID[it.ID] = it
		}
		resp := recommendResponse{Weather: wr, Outfits: make([]outfitResponse, 0, len(outfits))}
		for _, o := range outfits {
			out := outfitResponse{Items: make([]itemResponse, 0, len(o.ItemIDs)), Reason: o.Reason}
			for _, id := range o.ItemIDs {
				out.Items = append(out.Items, toResponse(byID[id]))
			}
			slices.SortStableFunc(out.Items, func(a, b itemResponse) int {
				return cmp.Compare(outfitCategoryOrder[a.Category], outfitCategoryOrder[b.Category])
			})
			resp.Outfits = append(resp.Outfits, out)
		}
		c.JSON(http.StatusOK, resp)
	}
}
