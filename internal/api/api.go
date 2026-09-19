// Package api exposes the read-only catalog and photo HTTP surface over
// Gin (07-architecture.md "Backend", "Full project structure").
package api

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"wardrobe/internal/store"
)

// DefaultPhotosDir is where stored garment photos live
// (07-architecture.md "Backend": image storage under data/photos/).
const DefaultPhotosDir = "data/photos"

// dateFormat is the added_date rendering the read-only contract fixes:
// YYYY-MM-DD.
const dateFormat = "2006-01-02"

// New builds the Gin engine with only the read-only catalog/photo
// routes. photosDir is the directory photo requests are served from;
// cmd/server passes DefaultPhotosDir, and tests may point it at a temp
// directory so they never touch the real data/photos/.
func New(st *store.Store, photosDir string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/api/items", listItems(st))
	r.GET("/api/photos/:filename", servePhoto(photosDir))
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
