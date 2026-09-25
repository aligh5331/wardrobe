// Package tests holds black-box acceptance tests for backlog tickets.
// ING-021: the Vite build output (frontend/dist) is embedded into the Go
// binary with go:embed and served by cmd/server alongside the read-only API:
// the SPA entry is served at / and at unknown non-/api paths, while unknown
// /api paths stay 404 and never return HTML. These tests exercise the exported
// wardrobe/internal/api and wardrobe/frontend surfaces; implementation code is
// never touched from here.
package tests

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"wardrobe/frontend"
	"wardrobe/internal/api"
	"wardrobe/internal/store"
)

// ing021IndexMarker is a string only the synthetic SPA entry contains, so a
// response body can be classified as the SPA entry vs. an API/404 body.
const ing021IndexMarker = "ING021-SPA-ENTRY"

// ing021Dist is a Vite-shaped build output used to drive the fallback handler
// deterministically: an SPA entry document plus hashed-looking assets.
func ing021Dist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":     {Data: []byte("<!doctype html><html><body>" + ing021IndexMarker + "</body></html>")},
		"assets/app.js":  {Data: []byte("console.log('" + ing021IndexMarker + "')")},
		"assets/app.css": {Data: []byte("body{color:red}")},
	}
}

// newING021Engine builds the real engine plus the SPA fallback over a
// throwaway store, mirroring cmd/server's wiring (api.New then
// api.ServeFrontend) so the fallback is exercised exactly as it ships.
func newING021Engine(t *testing.T, dist fs.FS) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatalf("store.Open() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	engine := api.New(st, filepath.Join(t.TempDir(), "photos"))
	api.ServeFrontend(engine, dist)
	return engine
}

// AC1: Given frontend/dist has been produced / When the Go binary is built /
// Then the built assets are embedded with go:embed. The embedded FS must
// contain every file on disk under frontend/dist with byte-identical content.
func TestING021_AC1_EmbedMirrorsBuiltDist(t *testing.T) {
	distDir := filepath.Join(moduleRoot(t), "frontend", "dist")

	if _, err := fs.Stat(frontend.Dist, "index.html"); err != nil {
		t.Fatalf("embedded frontend.Dist is missing index.html: %v", err)
	}

	onDisk := map[string][]byte{}
	err := filepath.WalkDir(distDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(distDir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		onDisk[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", distDir, err)
	}
	if len(onDisk) == 0 {
		t.Fatalf("frontend/dist contains no files to embed")
	}

	for name, want := range onDisk {
		got, err := fs.ReadFile(frontend.Dist, name)
		if err != nil {
			t.Errorf("embedded Dist is missing %q (present on disk): %v", name, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("embedded %q differs from disk: %d embedded bytes, %d on disk", name, len(got), len(want))
		}
	}
}

// AC1: cmd/server must serve the embedded FS through api.ServeFrontend.
func TestING021_AC1_MainServesEmbeddedFrontend(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "cmd", "server", "main.go"))
	if err != nil {
		t.Fatalf("read cmd/server/main.go: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "frontend.Dist") {
		t.Errorf("cmd/server/main.go does not reference frontend.Dist (the embedded asset FS)")
	}
	if !strings.Contains(s, "api.ServeFrontend(") {
		t.Errorf("cmd/server/main.go does not call api.ServeFrontend, so the embedded UI is not served")
	}
}

// AC1/AC2: the wired engine serves the embedded SPA entry at /.
func TestING021_AC1_RootServesEmbeddedIndex(t *testing.T) {
	engine := newING021Engine(t, frontend.Dist)

	want, err := fs.ReadFile(frontend.Dist, "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}

	rr := ing019Do(t, engine, http.MethodGet, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	if got := rr.Body.String(); got != string(want) {
		t.Errorf("GET / body is not the embedded index.html:\n got %q\nwant %q", got, want)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}
}

// AC2: a real previously-built Vite bundle is embedded and served from the
// single binary, proving the grid UI's assets ship inside the executable.
// Skipped on a fresh checkout where only the tracked placeholder exists.
func TestING021_AC2_RealGridBundleIsEmbeddedAndServed(t *testing.T) {
	entries, err := fs.ReadDir(frontend.Dist, "assets")
	if err != nil {
		t.Skipf("no embedded assets directory (fresh checkout): %v", err)
	}
	js := ""
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".js") {
			js = "assets/" + e.Name()
			break
		}
	}
	if js == "" {
		t.Skip("no embedded .js asset to check")
	}

	engine := newING021Engine(t, frontend.Dist)
	rr := ing019Do(t, engine, http.MethodGet, "/"+js)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /%s status = %d, want 200; body:\n%s", js, rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "No items cataloged yet") {
		t.Errorf("embedded %s does not contain the grid UI bundle's text", js)
	}
	if ct := strings.ToLower(rr.Header().Get("Content-Type")); !strings.Contains(ct, "javascript") {
		t.Errorf("GET /%s Content-Type = %q, want a javascript type", js, ct)
	}
}

// AC2: requests to /api/... are still routed to the API, not the HTML
// fallback, even with the SPA handler registered.
func TestING021_AC2_APINotShadowedBySPAFallback(t *testing.T) {
	engine := newING021Engine(t, ing021Dist())

	rr := ing019Do(t, engine, http.MethodGet, "/api/items")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/items status = %d, want 200; body:\n%s", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("GET /api/items Content-Type = %q, want application/json", ct)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "[]" {
		t.Errorf("GET /api/items body = %q, want [] (empty catalog)", body)
	}
}

// AC3: Given an unknown path that is not under /api/ / When it is requested /
// Then the SPA entry is served so client-side navigation works.
func TestING021_AC3_UnknownNonAPIPathServesSPAEntry(t *testing.T) {
	engine := newING021Engine(t, ing021Dist())

	for _, target := range []string{"/some/client/route", "/deeply/nested/view", "/assets/does-not-exist.js"} {
		rr := ing019Do(t, engine, http.MethodGet, target)
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200 with the SPA entry", target, rr.Code)
			continue
		}
		if !strings.Contains(rr.Body.String(), ing021IndexMarker) {
			t.Errorf("GET %s did not serve the SPA entry; body:\n%s", target, rr.Body)
		}
	}
}

// AC3: an unknown /api/... path returns 404 rather than HTML.
func TestING021_AC3_UnknownAPIPathIs404NotHTML(t *testing.T) {
	engine := newING021Engine(t, ing021Dist())

	for _, target := range []string{"/api", "/api/", "/api/unknown", "/api/items/extra", "/api/photos"} {
		rr := ing019Do(t, engine, http.MethodGet, target)
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", target, rr.Code)
		}
		body := rr.Body.String()
		t.Logf("GET %s -> %d, body %q", target, rr.Code, body)
		if strings.Contains(body, ing021IndexMarker) || strings.Contains(strings.ToLower(body), "<html") {
			t.Errorf("GET %s returned the SPA entry instead of 404:\n%s", target, body)
		}
	}
}

// AC3 edge: a non-GET request into the /api namespace is still the API
// namespace, so the SPA fallback must not answer it with HTML.
func TestING021_AC3_WrongMethodOnAPIPathIsNotSPA(t *testing.T) {
	engine := newING021Engine(t, ing021Dist())

	rr := ing019Do(t, engine, http.MethodPost, "/api/items")
	if rr.Code == http.StatusOK {
		t.Errorf("POST /api/items status = 200, want a rejection (no write route)")
	}
	if strings.Contains(rr.Body.String(), ing021IndexMarker) {
		t.Errorf("POST /api/items returned the SPA entry:\n%s", rr.Body)
	}
}

// AC4: Given a fresh checkout where frontend/dist has not been generated /
// When go build ./... runs / Then the build still succeeds because a small
// tracked placeholder keeps the embed pattern valid. The placeholder must
// exist, be embedded, and be tracked (not gitignored).
func TestING021_AC4_TrackedPlaceholderKeepsEmbedValid(t *testing.T) {
	root := moduleRoot(t)

	placeholder := filepath.Join(root, "frontend", "dist", "index.html")
	info, err := os.Stat(placeholder)
	if err != nil {
		t.Fatalf("embed placeholder %s missing: %v", placeholder, err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory, want a regular file", placeholder)
	}
	if _, err := fs.ReadFile(frontend.Dist, "index.html"); err != nil {
		t.Fatalf("embedded FS does not contain the placeholder index.html: %v", err)
	}

	// The embed must be the whole directory pattern, not a file list: that is
	// what keeps the build valid when only index.html exists on a fresh
	// checkout.
	embedSrc, err := os.ReadFile(filepath.Join(root, "frontend", "embed.go"))
	if err != nil {
		t.Fatalf("read frontend/embed.go: %v", err)
	}
	if !strings.Contains(string(embedSrc), "go:embed all:dist") {
		t.Errorf("frontend/embed.go does not use `//go:embed all:dist`; a file-list embed would break on a fresh checkout")
	}

	// git check-ignore -q exits 1 when the path is NOT ignored, i.e. a fresh
	// checkout would contain it.
	cmd := exec.Command("git", "check-ignore", "-q", "frontend/dist/index.html")
	cmd.Dir = root
	err = cmd.Run()
	if err == nil {
		t.Fatalf("frontend/dist/index.html is gitignored; a fresh checkout would not contain the embed placeholder")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("git check-ignore frontend/dist/index.html: %v", err)
	}
}
