//go:build integration

// ING-009 process-level acceptance tests: the config behavior the audit
// settled must hold in the real server process. A malformed VLM_URL and a
// non-empty VLM_API_KEY must still start; an unrecognized boolean or a
// non-numeric delay must exit 1 with a startup error naming the variable;
// a negative delay must warn and continue. Run with
// `go test -tags=integration -run TestING009 ./tests/`.
//
// Reuses the shared process helpers (envWith, moduleRoot, startServer,
// killServer, waitForOutput, assertReachedListenStage) from the ING-001
// integration file in this package.
package tests

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// AC1 process-level: a malformed VLM_URL is accepted at startup (no URL
// validation) and the server reaches the listen stage. The "surfaces as
// ErrVLMUnreachable on use" half is covered by the black-box Tag test.
func TestING009_Integration_MalformedURLProceeds(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{"VLM_URL": "not a url"})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}

// AC2 process-level: an unrecognized VLM_SERIALIZE_REQUESTS exits 1 with
// a startup error naming the variable.
func TestING009_Integration_InvalidSerializeExits(t *testing.T) {
	for _, raw := range []string{"yes", "ture"} {
		t.Run(raw, func(t *testing.T) {
			cmd := exec.Command("go", "run", "./cmd/server")
			cmd.Dir = moduleRoot(t)
			cmd.Env = envWith(map[string]string{
				"VLM_URL":                "http://127.0.0.1:1",
				"VLM_SERIALIZE_REQUESTS": raw,
			})

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("server exited 0 for VLM_SERIALIZE_REQUESTS=%q, want nonzero; output:\n%s", raw, out)
			}
			if code := cmd.ProcessState.ExitCode(); code != 1 {
				t.Errorf("exit code = %d for VLM_SERIALIZE_REQUESTS=%q, want 1; output:\n%s", code, raw, out)
			}
			for _, want := range []string{"startup", "VLM_SERIALIZE_REQUESTS"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("startup error for VLM_SERIALIZE_REQUESTS=%q missing %q; output:\n%s", raw, want, out)
				}
			}
		})
	}
}

// AC3 process-level: a negative VLM_REQUEST_DELAY_MS logs a startup
// warning naming the variable and proceeds — it must not be a hard error.
func TestING009_Integration_NegativeDelayWarns(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL":              "http://127.0.0.1:1",
		"VLM_REQUEST_DELAY_MS": "-100",
	})
	defer killServer(cmd)

	if !waitForOutput(buf, "VLM_REQUEST_DELAY_MS", 30*time.Second) {
		t.Fatalf("negative-delay startup warning not logged; output:\n%s", buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "startup warning") {
		t.Errorf("warning line missing 'startup warning'; output:\n%s", out)
	}
	if !strings.Contains(out, "negative") {
		t.Errorf("warning does not state the value was negative; output:\n%s", out)
	}
	if strings.Contains(out, "startup:") {
		t.Errorf("got a hard startup error for a negative delay; output:\n%s", out)
	}
}

// AC4 process-level: a non-numeric (or non-integer) VLM_REQUEST_DELAY_MS
// exits 1 with a startup error naming the variable.
func TestING009_Integration_NonNumericDelayExits(t *testing.T) {
	for _, raw := range []string{"abc", "1.5"} {
		t.Run(raw, func(t *testing.T) {
			cmd := exec.Command("go", "run", "./cmd/server")
			cmd.Dir = moduleRoot(t)
			cmd.Env = envWith(map[string]string{
				"VLM_URL":              "http://127.0.0.1:1",
				"VLM_REQUEST_DELAY_MS": raw,
			})

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("server exited 0 for VLM_REQUEST_DELAY_MS=%q, want nonzero; output:\n%s", raw, out)
			}
			if code := cmd.ProcessState.ExitCode(); code != 1 {
				t.Errorf("exit code = %d for VLM_REQUEST_DELAY_MS=%q, want 1; output:\n%s", code, raw, out)
			}
			for _, want := range []string{"startup", "VLM_REQUEST_DELAY_MS"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("startup error for VLM_REQUEST_DELAY_MS=%q missing %q; output:\n%s", raw, want, out)
				}
			}
		})
	}
}

// AC5 process-level: an arbitrary non-empty VLM_API_KEY is accepted at
// startup with no format validation — the server reaches the listen
// stage.
func TestING009_Integration_APIKeyNonEmptyProceeds(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL":     "http://127.0.0.1:1",
		"VLM_API_KEY": "sk-!@#$%^&*()",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}
