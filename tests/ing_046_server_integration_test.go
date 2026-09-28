//go:build integration

// Process-level acceptance tests for ING-046: LLM_URL becomes a startup
// requirement in the real cmd/server process only; cmd/ingest starts
// fine with LLM_URL empty since it never calls RequireLLMURL. Run with
// `go test -tags=integration -run TestING046 ./tests/`.
//
// Reuses the shared process helpers (envWith, moduleRoot, startServer,
// killServer, assertReachedListenStage) from the ING-001 integration
// file, and the cmd/ingest harness (newING012Harness, ing012Env) from
// the ING-012 integration file, all in this package.
package tests

import (
	"os/exec"
	"strings"
	"testing"
)

// AC5 process-level: LLM_URL empty or whitespace-only makes the real
// server process exit nonzero with a startup error naming LLM_URL.
func TestING046_Integration_AC5_MissingLLMURLExits(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "whitespace only", url: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", "run", "./cmd/server")
			cmd.Dir = moduleRoot(t)
			cmd.Env = envWith(map[string]string{
				"VLM_URL": "http://127.0.0.1:1",
				"LLM_URL": tt.url,
			})

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("server exited 0 for LLM_URL=%q, want nonzero; output:\n%s", tt.url, out)
			}
			if code := cmd.ProcessState.ExitCode(); code != 1 {
				t.Errorf("exit code = %d for LLM_URL=%q, want 1; output:\n%s", code, tt.url, out)
			}
			for _, want := range []string{"startup", "LLM_URL"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("startup error for LLM_URL=%q missing %q; output:\n%s", tt.url, want, out)
				}
			}
		})
	}
}

// AC7 process-level: a malformed but non-empty LLM_URL does not fail
// startup — no URL-format validation, only the server-only
// RequireLLMURL non-empty check.
func TestING046_Integration_AC7_MalformedLLMURLProceeds(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL": "http://127.0.0.1:1",
		"LLM_URL": "not a url",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}

// AC6 process-level: LLM_URL empty and VLM_URL set — cmd/ingest is
// unaffected, since it never calls RequireLLMURL. Reuses the ING-012
// harness so this exercises the real compiled ingest binary.
func TestING046_Integration_AC6_IngestUnaffectedByEmptyLLMURL(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	photo := writePhoto(t, "garment.jpg", []byte("ING046-AC6-JPEG-BYTES"))

	h := newING012Harness(t)
	srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })

	_, stderr, code := h.run(t, ing012Env(map[string]string{
		"VLM_URL": srv.URL,
		"LLM_URL": "",
	}), photo)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (ingest must not require LLM_URL); stderr:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "LLM_URL") {
		t.Errorf("stderr mentions LLM_URL, want ingest unaffected by it:\n%s", stderr)
	}
}
