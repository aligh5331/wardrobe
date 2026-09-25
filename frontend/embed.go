// Package frontend exposes the Vite build output (frontend/dist) embedded
// into the Go binary, so cmd/server can serve the UI from the single
// executable (07-architecture.md "Runtime model" / "Frontend").
package frontend

import (
	"embed"
	"io/fs"
)

// distFS is rooted at frontend/, where the Vite build writes dist/. A
// tracked placeholder (frontend/dist/index.html) keeps this pattern valid
// on a fresh checkout before a real `npm run build`.
//go:embed all:dist
var distFS embed.FS

// Dist is the built frontend rooted at frontend/dist, so paths are
// "index.html", "assets/...", not "dist/index.html".
var Dist = mustSub(distFS, "dist")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		// dist is embedded via the tracked placeholder, so it always exists.
		panic(err)
	}
	return sub
}
