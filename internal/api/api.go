// Package api exposes the catalog read surface plus the single-item edit
// write routes (GET/PUT /api/items/:id), photo, and taxonomy HTTP surface
// over Gin (07-architecture.md "Backend", "Catalog write API", "Full
// project structure").
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
)

// DefaultPhotosDir is where stored garment photos live
// (07-architecture.md "Backend": image storage under data/photos/).
const DefaultPhotosDir = "data/photos"

// dateFormat is the added_date rendering the read-only contract fixes:
// YYYY-MM-DD.
const dateFormat = "2006-01-02"

// New builds the Gin engine with the approved catalog routes: the read
// routes (GET /api/items, /api/items/:id, /api/photos/:filename,
// /api/taxonomy) and the single-item edit write route
// (PUT /api/items/:id). No delete route exists in Phase 1
// (07-architecture.md "Catalog write API"). photosDir is the directory
// photo requests are served from; cmd/server passes DefaultPhotosDir, and
// tests may point it at a temp directory so they never touch the real
// data/photos/.
func New(st *store.Store, photosDir string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/api/items", listItems(st))
	r.GET("/api/items/:id", getItem(st))
	r.PUT("/api/items/:id", updateItem(st))
	r.GET("/api/photos/:filename", servePhoto(photosDir))
	r.GET("/api/taxonomy", taxonomy)
	return r
}

// ServeFrontend registers the embedded-SPA fallback on r, which must already
// have its /api/... routes registered (New does that). Gin calls the fallback
// only when no route matched: unknown /api paths get 404 so they never return
// HTML, and any other unknown path is served from dist, falling back to
// index.html so client-side navigation works (07-architecture.md "Runtime
// model": one executable serves both the API and the UI).
func ServeFrontend(r *gin.Engine, dist fs.FS) {
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if p == "/api" || strings.HasPrefix(p, "/api/") {
			c.Status(http.StatusNotFound)
			return
		}

		name := strings.TrimPrefix(p, "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(dist, name)
		if err != nil {
			// Unknown client-side route (or a directory): serve the SPA entry.
			name = "index.html"
			data, err = fs.ReadFile(dist, "index.html")
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
		}
		c.Data(http.StatusOK, contentType(name, data), data)
	})
}

// contentType picks the response type from the file extension, falling back
// to content sniffing when the extension is unknown.
func contentType(name string, data []byte) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return http.DetectContentType(data)
}

// taxonomy serves the closed enum vocabulary the create/edit forms load
// (07-architecture.md "Taxonomy read route"): every category with its
// valid subcategories, the color palette, patterns, warmth tiers, and
// formality — derived from the same internal/tagging tables
// ParseTaggingResult validates against, never a second hard-coded copy
// (06-decisions.md "Taxonomy exported to the browser via
// GET /api/taxonomy, not a bundled copy"). It reads no store and runs no
// model: a pure read of package tables.
func taxonomy(c *gin.Context) {
	c.JSON(http.StatusOK, tagging.TaxonomyTables())
}

// itemResponse is the JSON shape of one catalog row: the
// 04-data-schema.md fields plus the photo URL the grid fetches.
type itemResponse struct {
	ID              string   `json:"id"`
	Category        string   `json:"category"`
	Subcategory     string   `json:"subcategory"`
	DominantColor   string   `json:"dominant_color"`
	SecondaryColors []string `json:"secondary_colors"`
	Pattern         string   `json:"pattern"`
	WarmthTier      string   `json:"warmth_tier"`
	Formality       string   `json:"formality"`
	PhotoPath       string   `json:"photo_path"`
	AddedDate       string   `json:"added_date"`
	Notes           string   `json:"notes"`
	PhotoURL        string   `json:"photo_url"`
}

func listItems(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := st.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list items"})
			return
		}
		// make(..., 0, ...) so an empty catalog serializes as [] not null.
		out := make([]itemResponse, 0, len(items))
		for _, item := range items {
			out = append(out, toResponse(item))
		}
		c.JSON(http.StatusOK, out)
	}
}

// getItem returns one catalog row in the same JSON shape GET /api/items
// returns per element — the 04-data-schema.md fields plus photo_url — or
// 404 for an unknown id (07-architecture.md "Catalog write API": "an
// unknown id is 404").
func getItem(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		item, err := st.Get(c.Param("id"))
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load item"})
			return
		}
		c.JSON(http.StatusOK, toResponse(item))
	}
}

// updateItemRequest is the PUT /api/items/:id body: the seven tagging
// fields of 04-data-schema.md plus notes. id, added_date, and photo_path
// are not decoded, so a body carrying them can never reach the store —
// they are immutable via PUT (07-architecture.md "Catalog write API").
type updateItemRequest struct {
	tagging.TaggingResult
	Notes string `json:"notes"`
}

// updateItem validates the body against the taxonomy and persists the
// mutable fields. Validation reuses tagging.ParseTaggingResult — the same
// tables a model output goes through — so the API keeps no second enum
// copy: an invalid enum value, an invalid category/subcategory pair, or a
// missing required tagging field is 400 with the offending field named in
// the message, and the item is left unchanged. An unknown id is 404 (the
// id lookup happens before the body is read, so it wins over any body
// problem). The photo is never touched: store.Update writes only the
// seven tagging fields and notes.
func updateItem(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		existing, err := st.Get(c.Param("id"))
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load item"})
			return
		}

		var body updateItemRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
			return
		}

		raw, err := json.Marshal(body.TaggingResult)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate update"})
			return
		}
		if _, err := tagging.ParseTaggingResult(string(raw)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		updated := existing
		updated.Category = body.Category
		updated.Subcategory = body.Subcategory
		updated.DominantColor = body.DominantColor
		updated.SecondaryColors = body.SecondaryColors
		updated.Pattern = body.Pattern
		updated.WarmthTier = body.WarmthTier
		updated.Formality = body.Formality
		updated.Notes = body.Notes
		if err := st.Update(updated); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save item"})
			return
		}
		c.JSON(http.StatusOK, toResponse(updated))
	}
}

func toResponse(item store.Item) itemResponse {
	colors := item.SecondaryColors
	if colors == nil {
		colors = []string{}
	}
	return itemResponse{
		ID:              item.ID,
		Category:        item.Category,
		Subcategory:     item.Subcategory,
		DominantColor:   item.DominantColor,
		SecondaryColors: colors,
		Pattern:         item.Pattern,
		WarmthTier:      item.WarmthTier,
		Formality:       item.Formality,
		PhotoPath:       item.PhotoPath,
		AddedDate:       item.AddedDate.Format(dateFormat),
		Notes:           item.Notes,
		PhotoURL:        photoURL(item.PhotoPath),
	}
}

// photoURL is the URL the grid fetches a stored photo from; it is built
// from the basename so the client never receives a path.
func photoURL(photoPath string) string {
	if photoPath == "" {
		return ""
	}
	return "/api/photos/" + filepath.Base(photoPath)
}

// servePhoto serves data/photos/<filename> bytes with the Content-Type
// inferred from the extension, and 404 when the file is absent.
func servePhoto(dir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("filename")
		if !validPhotoName(name) {
			c.Status(http.StatusBadRequest)
			return
		}
		c.File(filepath.Join(dir, name))
	}
}

// validPhotoName accepts only a plain basename inside the photos
// directory. Rejecting "..", separators, and "."/"" is trust-boundary
// validation: it guarantees the resolved path cannot escape photosDir.
func validPhotoName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return !strings.Contains(name, "..")
}
