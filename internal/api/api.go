// Package api exposes the catalog read surface plus the single-item edit
// write routes (GET/PUT /api/items/:id), photo, and taxonomy HTTP surface
// over Gin (07-architecture.md "Backend", "Catalog write API", "Full
// project structure").
package api

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
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

// DefaultStagingDir is where interactive uploads are staged until a human
// confirms the draft (07-architecture.md "Full project structure",
// "Catalog write API").
const DefaultStagingDir = "data/ingest-staging"

// maxPhotoBytes caps one uploaded photo at 20 MiB (06-decisions.md "Photo
// upload trust boundary").
const maxPhotoBytes = 20 << 20

// multipartSlack is headroom above maxPhotoBytes for multipart boundaries
// and part headers, so a file of exactly maxPhotoBytes is not rejected for
// the envelope's overhead. The file itself is size-checked separately.
const multipartSlack = 1 << 20

// acceptedPhotoExts is the fixed upload type set (06-decisions.md "Photo
// upload trust boundary"), matching cmd/ingest's accepted extensions.
var acceptedPhotoExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
}

// config carries the optional tagging wiring New needs to serve the upload
// route. It is built from Option values so existing read-only callers can
// keep calling New(st, photosDir) unchanged.
type config struct {
	processor  *tagging.Processor
	stagingDir string
}

// Option customises the engine New builds.
type Option func(*config)

// WithTagging wires the local VLM processor and staging directory used by
// POST /api/items/photo. cmd/server builds the processor from config the
// same way cmd/ingest does. Without it the route still exists but returns
// 503, so the registered route surface is fixed regardless of wiring.
func WithTagging(p *tagging.Processor, stagingDir string) Option {
	return func(c *config) {
		c.processor = p
		c.stagingDir = stagingDir
	}
}

// dateFormat is the added_date rendering the read-only contract fixes:
// YYYY-MM-DD.
const dateFormat = "2006-01-02"

// New builds the Gin engine with the approved catalog routes: the read
// routes (GET /api/items, /api/items/:id, /api/photos/:filename,
// /api/taxonomy), the single-item edit write route (PUT /api/items/:id),
// and the create draft route (POST /api/items/photo). No delete route
// exists in Phase 1 (07-architecture.md "Catalog write API"). photosDir is
// the directory photo requests are served from; cmd/server passes
// DefaultPhotosDir, and tests may point it at a temp directory so they
// never touch the real data/photos/.
func New(st *store.Store, photosDir string, opts ...Option) *gin.Engine {
	cfg := &config{stagingDir: DefaultStagingDir}
	for _, opt := range opts {
		opt(cfg)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/api/items", listItems(st))
	r.GET("/api/items/:id", getItem(st))
	r.PUT("/api/items/:id", updateItem(st))
	r.POST("/api/items/photo", uploadPhoto(cfg.processor, cfg.stagingDir))
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

// draftResponse is POST /api/items/photo's success body: the seven
// validated tagging fields of 04-data-schema.md (flattened), the generated
// item id, and photo_ref — the basename of the staged upload under the
// staging directory, which the client posts back to persist the draft
// (07-architecture.md "Catalog write API": "returns a non-persisted draft
// (tagging fields + a photo reference)"). It is not a catalog row: no row
// is written and no file reaches data/photos/.
type draftResponse struct {
	ItemID   string `json:"item_id"`
	PhotoRef string `json:"photo_ref"`
	tagging.TaggingResult
}

// uploadPhoto accepts one multipart garment photo on field "photo", stages
// it server-named <item_id>.<ext> under stagingDir, and runs the same local
// tagging pipeline (retry-once policy included) cmd/ingest uses, returning
// a non-persisted draft. The client filename is never used as a path — only
// its extension selects the stored form and the name is always the
// server-generated id — so a traversal filename cannot escape stagingDir
// (06-decisions.md "Photo upload trust boundary"). Rejections before
// tagging (bad extension, oversize) stage nothing; any tagging failure
// removes the staged upload so no residue accumulates.
func uploadPhoto(p *tagging.Processor, stagingDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tagging is not configured"})
			return
		}

		// Bound the request before parsing so an oversized upload is
		// rejected without an unbounded read. The file is size-checked
		// below; the slack covers multipart overhead for exactly 20 MiB.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPhotoBytes+multipartSlack)

		fileHeader, err := c.FormFile("photo")
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "photo exceeds the 20 MiB limit"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": `missing or malformed multipart photo in field "photo"`})
			return
		}
		if fileHeader.Size > maxPhotoBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "photo exceeds the 20 MiB limit"})
			return
		}

		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		if !acceptedPhotoExts[ext] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported photo extension; accepted: .jpg, .jpeg, .png, .webp"})
			return
		}

		itemID, err := tagging.NewItemID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate item id"})
			return
		}
		if err := os.MkdirAll(stagingDir, 0o755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare staging directory"})
			return
		}
		stagedName := itemID + ext
		stagedPath := filepath.Join(stagingDir, stagedName)
		if err := saveUpload(fileHeader, stagedPath); err != nil {
			_ = os.Remove(stagedPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stage upload"})
			return
		}

		outcome, err := p.ProcessWithID(c.Request.Context(), itemID, stagedPath)
		if err != nil {
			_ = os.Remove(stagedPath)
			if errors.Is(err, tagging.ErrVLMUnreachable) {
				c.JSON(http.StatusBadGateway, gin.H{"error": "VLM unreachable"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tagging failed"})
			return
		}
		if outcome.Flagged {
			_ = os.Remove(stagedPath)
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "tagging failed validation after retry; retry or choose another photo"})
			return
		}

		c.JSON(http.StatusOK, draftResponse{
			ItemID:        itemID,
			PhotoRef:      stagedName,
			TaggingResult: outcome.Result,
		})
	}
}

// saveUpload writes one uploaded part to dest. dest is always a
// server-built path, never the client-supplied filename.
func saveUpload(fh *multipart.FileHeader, dest string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
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
