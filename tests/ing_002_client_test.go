// Package tests holds black-box acceptance tests for backlog tickets.
// ING-002: the VLM client sends one garment photo + the tagging prompt
// to VLM_URL and returns the raw model response. These tests exercise
// the exported API of wardrobe/internal/tagging only; implementation
// code is never touched from here.
package tests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wardrobe/internal/tagging"
)

// moduleRoot locates the module root (dir containing go.mod) from the
// tests/ package working directory, so spec files can be read and
// compared against instead of hand-copied into the test. It lives in
// this non-build-tagged file so both the integration and
// non-integration test files share one copy.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above test directory")
		}
		dir = parent
	}
}

// specTaggingPrompt extracts the fenced prompt out of
// 05-vlm-tagging-spec.md's "## Prompt" section, so AC1 asserts the
// client sends the spec's prompt rather than a literal copy that can
// drift from the spec.
func specTaggingPrompt(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "05-vlm-tagging-spec.md"))
	if err != nil {
		t.Fatalf("read 05-vlm-tagging-spec.md: %v", err)
	}
	section := string(raw)
	i := strings.Index(section, "## Prompt")
	if i < 0 {
		t.Fatal(`05-vlm-tagging-spec.md has no "## Prompt" section`)
	}
	rest := section[i:]
	open := strings.Index(rest, "```")
	if open < 0 {
		t.Fatal("no fenced code block in the Prompt section")
	}
	rest = rest[open+3:]
	rest = rest[strings.IndexByte(rest, '\n')+1:]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatal("unterminated fenced code block in the Prompt section")
	}
	return strings.TrimRight(rest[:end], "\n")
}

// writePhoto writes a known-bytes photo file into the test's temp dir
// (never the OS temp dir, per 06-decisions.md) and returns its path.
func writePhoto(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write photo: %v", err)
	}
	return path
}

// rawEnvelope wraps arbitrary model text in a valid chat-completions
// response envelope.
func rawEnvelope(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
	})
	return string(b)
}

// decodeRequest decodes a captured chat request body.
func decodeRequest(t *testing.T, r *http.Request) map[string]any {
	t.Helper()

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	return body
}

// AC1: Given a valid image file path and a reachable VLM at VLM_URL /
// When the client sends a tagging request / Then it returns the raw
// text response from the model, sending an auth header only if
// VLM_API_KEY is set.
func TestING002_AC1_ReturnsRawResponse(t *testing.T) {
	const raw = "{\"category\":\"top\",\"subcategory\":\"t-shirt\"}\n trailing text kept raw "
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-AC1"))

	var (
		gotMethod, gotPath, gotAuth, gotContentType string
		gotBody                                     map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBody = decodeRequest(t, r)
		_, _ = w.Write([]byte(rawEnvelope(raw)))
	}))
	defer srv.Close()

	client := tagging.NewClient(srv.URL, "")
	got, err := client.Tag(context.Background(), photo)
	if err != nil {
		t.Fatalf("Tag() error = %v, want nil", err)
	}
	if got != raw {
		t.Errorf("Tag() = %q, want the raw model content untouched", got)
	}

	if gotMethod != http.MethodPost || gotPath != "/v1/chat/completions" {
		t.Errorf("request = %s %s, want POST /v1/chat/completions (llama.cpp OpenAI-compatible endpoint)", gotMethod, gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want no header when VLM_API_KEY is empty", gotAuth)
	}

	messages, ok := gotBody["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %v, want exactly a system + user message", gotBody["messages"])
	}
	system, _ := messages[0].(map[string]any)
	if system["role"] != "system" {
		t.Errorf("messages[0].role = %v, want system", system["role"])
	}
	if want := specTaggingPrompt(t); system["content"] != want {
		t.Errorf("system message is not the spec's tagging prompt verbatim\n got: %q\nwant: %q", system["content"], want)
	}

	user, ok := messages[1].(map[string]any)
	if !ok || user["role"] != "user" {
		t.Fatalf("messages[1] = %v, want the user message", messages[1])
	}
	parts, ok := user["content"].([]any)
	if !ok || len(parts) == 0 {
		t.Fatalf("user content = %v, want content parts", user["content"])
	}
	part0, _ := parts[0].(map[string]any)
	imgURL, _ := part0["image_url"].(map[string]any)
	wantURI := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("JPEG-BYTES-AC1"))
	if part0["type"] != "image_url" || imgURL["url"] != wantURI {
		t.Errorf("first user content part = %v, want image_url with data URI %s", parts[0], wantURI)
	}
}

// AC1 (auth clause): the Authorization header is present with the key
// when set, and absent when empty. Table-driven to pin both paths.
func TestING002_AC1_AuthHeaderGatedOnKey(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantHdr string
	}{
		{name: "empty key sends no header", apiKey: "", wantHdr: ""},
		{name: "set key sends bearer header", apiKey: "secret-token", wantHdr: "Bearer secret-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
			}))
			defer srv.Close()

			client := tagging.NewClient(srv.URL, tt.apiKey)
			if _, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x"))); err != nil {
				t.Fatalf("Tag() error = %v", err)
			}
			if gotAuth != tt.wantHdr {
				t.Errorf("Authorization = %q, want %q", gotAuth, tt.wantHdr)
			}
		})
	}
}

// Spec-implied edge (05-vlm-tagging-spec.md "Input": photo of a
// garment; client accepts a valid file path): the MIME type on the data
// URI follows the image extension.
func TestING002_Edge_MIMETypeFromExtension(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{name: "png", file: "photo.png", want: "image/png"},
		{name: "webp", file: "photo.webp", want: "image/webp"},
		{name: "unknown extension falls back to jpeg", file: "photo.xyz", want: "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotURI string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := decodeRequest(t, r)
				messages := body["messages"].([]any)
				user := messages[1].(map[string]any)
				parts := user["content"].([]any)
				part0 := parts[0].(map[string]any)
				gotURI, _ = part0["image_url"].(map[string]any)["url"].(string)
				_, _ = w.Write([]byte(rawEnvelope("{}")))
			}))
			defer srv.Close()

			client := tagging.NewClient(srv.URL, "")
			if _, err := client.Tag(context.Background(), writePhoto(t, tt.file, []byte("x"))); err != nil {
				t.Fatalf("Tag() error = %v", err)
			}
			if !strings.HasPrefix(gotURI, "data:"+tt.want+";base64,") {
				t.Errorf("data URI = %q, want prefix data:%s;base64,", gotURI, tt.want)
			}
		})
	}
}

// Spec-implied edge: a trailing slash on VLM_URL must not produce a
// double-slashed endpoint path. 07-architecture.md defines VLM_URL as a
// bare URL; callers may reasonably include a trailing slash.
func TestING002_Edge_TrailingSlashURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(rawEnvelope("{}")))
	}))
	defer srv.Close()

	client := tagging.NewClient(srv.URL+"/", "")
	if _, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x"))); err != nil {
		t.Fatalf("Tag() error = %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions", gotPath)
	}
}

// Spec-implied edge: an unreadable image path is a local input error
// (ticket says "a valid image file path"), not a VLM connectivity
// failure, and must not be reported as ErrVLMUnreachable.
func TestING002_Edge_MissingImageIsLocalError(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		_, _ = w.Write([]byte(rawEnvelope("{}")))
	}))
	defer srv.Close()

	client := tagging.NewClient(srv.URL, "")
	_, err := client.Tag(context.Background(), filepath.Join(t.TempDir(), "does-not-exist.jpg"))
	if err == nil {
		t.Fatal("Tag() error = nil, want an error for a missing image file")
	}
	if errors.Is(err, tagging.ErrVLMUnreachable) {
		t.Errorf("missing image must not be ErrVLMUnreachable, got %v", err)
	}
	if hit.Load() {
		t.Error("client contacted the VLM despite an unreadable image; should fail before dialing")
	}
}

// AC2: Given VLM_URL is unreachable (connection refused, timeout, DNS
// failure) / When the client attempts a tagging request / Then it
// returns a distinct "VLM unreachable" error, separate from the
// malformed-output case.
func TestING002_AC2_Unreachable(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	refused.Close()

	// Slow (not dead) server: the client's 150ms deadline expires while
	// the handler is still working, exercising the transport timeout
	// without leaving a handler blocked forever at Close().
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(750 * time.Millisecond)
		_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
	}))
	defer blocked.Close()

	tests := []struct {
		name string
		url  string
		ctx  func() (context.Context, context.CancelFunc)
		// wantCause is an additional cause the caller must be able to
		// recover with errors.Is (spec-implied: a deadline error is
		// actionable, a bare sentinel is not).
		wantCause error
	}{
		{
			name: "connection refused",
			url:  refused.URL,
			ctx:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
		},
		{
			name: "timeout",
			url:  blocked.URL,
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 150*time.Millisecond)
			},
			wantCause: context.DeadlineExceeded,
		},
		{
			name: "DNS failure",
			url:  "http://ing002-does-not-resolve.invalid:2525",
			ctx:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.ctx()
			defer cancel()

			client := tagging.NewClient(tt.url, "")
			_, err := client.Tag(ctx, writePhoto(t, "p.jpg", []byte("x")))
			if err == nil {
				t.Fatal("Tag() error = nil, want ErrVLMUnreachable")
			}
			if !errors.Is(err, tagging.ErrVLMUnreachable) {
				t.Errorf("errors.Is(err, ErrVLMUnreachable) = false, err = %v", err)
			}
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Errorf("errors.Is(err, %v) = false; the underlying cause must stay recoverable, err = %v", tt.wantCause, err)
			}
		})
	}
}

// AC2 (distinctness): connectivity failures and bad model output are
// different failure modes and must not be conflated. A reachable VLM
// that returns malformed/empty output is an error, but NOT
// ErrVLMUnreachable — that distinction is what ING-005 builds on.
func TestING002_AC2_BadOutputIsNotUnreachable(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "non-2xx status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "model exploded", http.StatusInternalServerError)
			},
		},
		{
			name: "200 with invalid JSON envelope",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not json at all"))
			},
		},
		{
			name: "200 with empty choices",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"choices":[]}`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			client := tagging.NewClient(srv.URL, "")
			_, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x")))
			if err == nil {
				t.Fatal("Tag() error = nil, want an error for unusable model output")
			}
			if errors.Is(err, tagging.ErrVLMUnreachable) {
				t.Errorf("unusable model output must NOT be ErrVLMUnreachable, got %v", err)
			}
		})
	}
}

// Spec-implied edge: empty model content is returned as-is (no error),
// leaving it to ING-005's validation to reject. Returning it here is the
// whole point of "returns the raw model response".
func TestING002_Edge_EmptyContentReturnedRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer srv.Close()

	client := tagging.NewClient(srv.URL, "")
	got, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x")))
	if err != nil {
		t.Fatalf("Tag() error = %v, want nil (empty content is ING-005's concern)", err)
	}
	if got != "" {
		t.Errorf("Tag() = %q, want empty raw content", got)
	}
}

// AC3: Given multiple tagging requests are in flight at once and
// VLM_SERIALIZE_REQUESTS=false (default) / When they are sent / Then
// they execute concurrently with no artificial single-request queue.
//
// The client is expected to have no queue or lock at all, so this
// detects overlap at the server rather than reading config.
func TestING002_AC3_ConcurrentByDefault(t *testing.T) {
	var cur, max atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		defer cur.Add(-1)
		for {
			m := max.Load()
			if n <= m || max.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
	}))
	defer srv.Close()

	const n = 8
	client := tagging.NewClient(srv.URL, "")
	photo := writePhoto(t, "p.jpg", []byte("x"))

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			if _, err := client.Tag(context.Background(), photo); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Tag() error = %v", err)
	}
	if got := max.Load(); got <= 1 {
		t.Errorf("max concurrent requests = %d, want > 1 (no serialization queue by default)", got)
	}
}
