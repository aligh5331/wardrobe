package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"wardrobe/internal/logging"
	"wardrobe/internal/tagging"
)

// accessLog emits one record per request (07-architecture.md "Logging":
// HTTP access log, request-scoped logger, correlation). It generates the
// request_id server-side and ignores any inbound X-Request-ID, returns it in
// the X-Request-ID response header, and stores a logger carrying it in the
// request context.
//
// It is registered before gin.Recovery(), so a handler panic is recovered
// (500 written) by Recovery, which returns normally to c.Next() here. The
// record therefore shows status 500 at error level without a defer/recover.
//
// base nil means slog.Default(), looked up per request so a later
// slog.SetDefault is honored.
func accessLog(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		logger := base
		if logger == nil {
			logger = slog.Default()
		}
		// The UUIDv4 generator never fails in practice (crypto/rand); an
		// empty id on failure is not worth a second code path.
		id, _ := tagging.NewItemID()
		l := logger.With("request_id", id)
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(logging.WithLogger(c.Request.Context(), l))

		c.Next()

		status := c.Writer.Status()
		size := max(c.Writer.Size(), 0) // -1 when nothing was written
		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("route", c.FullPath()), // empty on the NoRoute path
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
			slog.Int("size", size),
			slog.String("client_ip", c.ClientIP()),
		}
		level := slog.LevelInfo
		switch {
		case status >= http.StatusInternalServerError:
			level = slog.LevelError
			itemID := c.Param("id")
			if itemID == "" {
				itemID = c.GetString("item_id")
			}
			if itemID != "" {
				attrs = append(attrs, slog.String("item_id", itemID))
			}
		case status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}
		l.LogAttrs(c.Request.Context(), level, "http request", attrs...)
	}
}
