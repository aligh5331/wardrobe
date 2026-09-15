// Package tests holds black-box acceptance tests for backlog tickets.
// ING-011: a malformed VLM_URL must be accepted at startup (no URL-format
// validation) and surface on use as the ING-002 ErrVLMUnreachable, while
// ING-001's empty-URL startup failure and ING-002's well-formed-but-
// unreachable behavior stay unchanged. Black-box: exercises the exported
// tagging/config API only; implementation code is never touched.
package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"wardrobe/internal/config"
	"wardrobe/internal/tagging"
)

// AC1: Given VLM_URL is malformed or not a usable URL / When Tag is
// called / Then the returned error satisfies
// errors.Is(err, tagging.ErrVLMUnreachable).
//
// A malformed base URL can fail either in http.NewRequestWithContext
// (parse error) or in the transport (unsupported scheme / missing host);
// both paths must be the same distinct error the caller already handles.
func TestING011_AC1_MalformedURLIsUnreachable(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "not a url", url: "not a url"},
		{name: "bare hostname", url: "vlm.local"},
		{name: "scheme only", url: "http://"},
		{name: "wrong scheme", url: "ftp://example.com"},
		{name: "bare host and port", url: "vlm.local:2525"},
		{name: "unterminated ipv6 host", url: "http://[::1"},
		{name: "relative path", url: "/var/run"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tagging.NewClient(tt.url, "")
			_, err := client.Tag(context.Background(), writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING011")))
			if err == nil {
				t.Fatalf("Tag() with VLM_URL=%q error = nil, want ErrVLMUnreachable", tt.url)
			}
			if !errors.Is(err, tagging.ErrVLMUnreachable) {
				t.Errorf("errors.Is(err, tagging.ErrVLMUnreachable) = false for VLM_URL=%q; err = %v", tt.url, err)
			}
		})
	}
}

// AC1 (startup half, per 07-architecture.md "VLM_URL" row: "No URL-format
// validation at startup; a malformed value is accepted and surfaces when a
// request is attempted"): a malformed but non-empty VLM_URL must not fail
// Load(); it is carried through for the client to fail on use. This is the
// "no new startup validation" clause of the ticket.
func TestING011_AC1_MalformedURLAcceptedAtStartup(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "not a url", url: "not a url"},
		{name: "bare hostname", url: "vlm.local"},
		{name: "scheme only", url: "http://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": tt.url})

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v, want nil (no URL-format validation at startup)", err)
			}
			if cfg.VLMURL != tt.url {
				t.Errorf("VLMURL = %q, want %q carried through unchanged", cfg.VLMURL, tt.url)
			}
		})
	}
}

// AC2: Given VLM_URL is well-formed but the host is unreachable / When Tag
// is called / Then the error is still ErrVLMUnreachable (ING-002 behavior
// unchanged).
func TestING011_AC2_WellFormedUnreachableStillUnreachable(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	refused.Close()

	tests := []struct {
		name string
		url  string
	}{
		{name: "connection refused", url: refused.URL},
		{name: "DNS failure", url: "http://ing011-does-not-resolve.invalid:2525"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tagging.NewClient(tt.url, "")
			_, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x")))
			if err == nil {
				t.Fatal("Tag() error = nil, want ErrVLMUnreachable")
			}
			if !errors.Is(err, tagging.ErrVLMUnreachable) {
				t.Errorf("errors.Is(err, ErrVLMUnreachable) = false, err = %v", err)
			}
		})
	}
}

// AC3: Given VLM_URL is empty or unset / When the server starts / Then it
// still exits with the startup error naming VLM_URL (ING-001 behavior
// unchanged — this ticket adds no startup URL validation).
func TestING011_AC3_EmptyOrUnsetURLStillFailsAtStartup(t *testing.T) {
	tests := []struct {
		name  string
		unset bool
	}{
		{name: "empty", unset: false},
		{name: "unset", unset: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": ""})
			if tt.unset {
				// t.Setenv above registered restoration; now truly unset.
				os.Unsetenv("VLM_URL")
			}

			_, err := config.Load()
			if err == nil {
				t.Fatalf("config.Load() = nil error, want startup error naming VLM_URL")
			}
			if !strings.Contains(err.Error(), "VLM_URL") {
				t.Errorf("config.Load() error = %q, want it to name VLM_URL", err)
			}
		})
	}
}

// Spec-implied edge, distinctness (ING-002): the malformed-URL wrap must
// not swallow the bad-output distinction. A reachable VLM that returns
// unusable output is still NOT ErrVLMUnreachable after this change.
func TestING011_Edge_BadOutputIsStillNotUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := tagging.NewClient(srv.URL, "")
	_, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x")))
	if err == nil {
		t.Fatal("Tag() error = nil, want a status error")
	}
	if errors.Is(err, tagging.ErrVLMUnreachable) {
		t.Errorf("a reachable VLM returning bad output must not be ErrVLMUnreachable, got %v", err)
	}
}
