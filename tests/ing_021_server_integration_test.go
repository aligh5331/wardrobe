//go:build integration

// Process-level acceptance tests for ING-021: the real cmd/server binary
// serves the embedded SPA entry and its assets, keeps /api/... routed to the
// API, and never lets the SPA fallback shadow an unknown /api path. The
// binary is run with its working directory set to an isolated temp root (via
// the ING-019 harness) so it never writes the repo's real data/.
package tests

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"

	"wardrobe/frontend"
)

// AC1/AC2/AC3: one running binary serves the embedded UI, the API, the SPA
// fallback for client routes, and 404s unknown /api paths.
func TestING021_Integration_ServesEmbeddedSPAAndAPI(t *testing.T) {
	port := ing019FreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	h := newING019Harness(t)
	cmd, buf := h.start(t, map[string]string{"VLM_URL": "http://127.0.0.1:1"}, "-addr", addr)
	defer killServer(cmd)

	if !waitForOutput(buf, "listening on "+addr, 30*time.Second) {
		t.Fatalf("server did not report the override address %s; output:\n%s", addr, buf.String())
	}
	base := "http://" + addr

	index, err := fs.ReadFile(frontend.Dist, "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}

	// AC1/AC2: the root URL serves the SPA entry byte-for-byte.
	body, status, ok := ing019Get(base+"/", 10*time.Second)
	if !ok {
		t.Fatalf("server did not answer GET /; output:\n%s", buf.String())
	}
	if status != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", status)
	}
	if body != string(index) {
		t.Errorf("GET / body is not the embedded index.html:\n got %q\nwant %q", body, index)
	}

	// AC2: a real embedded Vite asset (the grid bundle) is served by the same
	// binary. Skipped only if this is a fresh checkout with no assets.
	if js := firstING021Asset(t, ".js"); js != "" {
		body, status, ok = ing019Get(base+"/"+js, 10*time.Second)
		if !ok || status != http.StatusOK {
			t.Errorf("GET /%s status = %d, ok=%v, want 200", js, status, ok)
		} else if !strings.Contains(body, "No items cataloged yet") {
			t.Errorf("GET /%s did not return the grid UI bundle", js)
		}
	}

	// AC2: /api/... is still routed to the API, not the HTML fallback.
	body, status, ok = ing019Get(base+"/api/items", 10*time.Second)
	if !ok || status != http.StatusOK {
		t.Errorf("GET /api/items status = %d, ok=%v, want 200", status, ok)
	} else if strings.TrimSpace(body) != "[]" {
		t.Errorf("GET /api/items body = %q, want [] (empty catalog)", body)
	}

	// AC3: an unknown non-/api path serves the SPA entry.
	body, status, ok = ing019Get(base+"/some/client/route", 10*time.Second)
	if !ok || status != http.StatusOK {
		t.Errorf("GET /some/client/route status = %d, ok=%v, want 200", status, ok)
	} else if body != string(index) {
		t.Errorf("GET /some/client/route did not serve the SPA entry:\n got %q\nwant %q", body, index)
	}

	// AC3: an unknown /api path is 404, not HTML.
	body, status, ok = ing019Get(base+"/api/unknown", 10*time.Second)
	if !ok || status != http.StatusNotFound {
		t.Errorf("GET /api/unknown status = %d, ok=%v, want 404", status, ok)
	} else if strings.Contains(strings.ToLower(body), "<html") {
		t.Errorf("GET /api/unknown returned HTML:\n%s", body)
	}
}

// firstING021Asset returns "assets/<name>" for the first embedded asset with
// the given suffix, or "" when the embedded build has no assets directory
// (fresh checkout with only the placeholder).
func firstING021Asset(t *testing.T, suffix string) string {
	t.Helper()
	entries, err := fs.ReadDir(frontend.Dist, "assets")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			return "assets/" + e.Name()
		}
	}
	return ""
}
