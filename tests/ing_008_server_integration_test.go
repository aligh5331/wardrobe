//go:build integration

// ING-008 process-level acceptance tests: a non-finite or out-of-range
// VLM_TEMPERATURE must fail the real server process at startup (exit
// code 1, "startup" error naming VLM_TEMPERATURE), while 0 is valid.
// Run with `go test -tags=integration -run TestING008 ./tests/`.
//
// Reuses the shared process helpers (envWith, moduleRoot, startServer,
// killServer, assertReachedListenStage) from the ING-001 integration
// file in this package.
package tests

import (
	"os/exec"
	"strings"
	"testing"
)

// AC1-AC4 process-level: invalid (NaN, Inf, -Inf, negative, >1.0)
// VLM_TEMPERATURE exits 1 with a startup error naming VLM_TEMPERATURE.
func TestING008_Integration_InvalidTemperatureExits(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "NaN", value: "NaN"},
		{name: "Inf", value: "Inf"},
		{name: "minus Inf", value: "-Inf"},
		{name: "negative", value: "-1"},
		{name: "above one", value: "1.5"},
		{name: "far above one", value: "100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", "run", "./cmd/server")
			cmd.Dir = moduleRoot(t)
			cmd.Env = envWith(map[string]string{
				"VLM_URL":         "http://127.0.0.1:1",
				"VLM_TEMPERATURE": tt.value,
			})

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("server exited 0 for VLM_TEMPERATURE=%q, want nonzero; output:\n%s", tt.value, out)
			}
			if code := cmd.ProcessState.ExitCode(); code != 1 {
				t.Errorf("exit code = %d for VLM_TEMPERATURE=%q, want 1; output:\n%s", code, tt.value, out)
			}
			for _, want := range []string{"startup", "VLM_TEMPERATURE"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("startup error for VLM_TEMPERATURE=%q missing %q; output:\n%s", tt.value, want, out)
				}
			}
		})
	}
}

// AC5 process-level: VLM_TEMPERATURE=0 is valid — startup proceeds past
// config validation (reaches the listen stage) with no startup error.
func TestING008_Integration_ZeroTemperatureProceeds(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL":         "http://127.0.0.1:1",
		"VLM_TEMPERATURE": "0",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}

// AC6 process-level: the inclusive upper bound 1.0 is valid and startup
// proceeds normally.
func TestING008_Integration_UpperBoundTemperatureProceeds(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL":         "http://127.0.0.1:1",
		"VLM_TEMPERATURE": "1.0",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}
