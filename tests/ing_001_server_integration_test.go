//go:build integration

// Process-level acceptance tests: they compile and run the real server
// binary to verify startup/exit behavior. Build-tagged per the Go
// testing skill; run with `go test -tags=integration ./tests/...`.
package tests

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// envWith returns the current environment with every contract var
// removed, then the overrides appended. Prevents a locally-exported
// VLM_URL etc. from leaking into a process test.
func envWith(overrides map[string]string) []string {
	drop := make(map[string]bool, len(contractVars))
	for _, name := range contractVars {
		drop[name] = true
	}

	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if ok && drop[name] {
			continue
		}
		env = append(env, kv)
	}
	for name, value := range overrides {
		env = append(env, name+"="+value)
	}
	return env
}

// syncBuffer is a concurrency-safe io.Writer for the child process's
// combined output, polled while the process is still running.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startServer launches `go run ./cmd/server` in its own process group so
// the compiled child (and any subprocess) can be killed reliably.
func startServer(t *testing.T, env map[string]string) (*exec.Cmd, *syncBuffer) {
	t.Helper()

	buf := &syncBuffer{}
	cmd := exec.Command("go", "run", "./cmd/server")
	cmd.Dir = moduleRoot(t)
	cmd.Env = envWith(env)
	cmd.Stdout = buf
	cmd.Stderr = buf
	setProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	return cmd, buf
}

func killServer(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	killTree(cmd)
	_ = cmd.Wait()
}

func waitForOutput(buf *syncBuffer, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return strings.Contains(buf.String(), want)
}

// AC1 process-level: missing VLM_URL exits nonzero with a startup error
// naming VLM_URL.
func TestING001_Integration_AC1_MissingURLExits(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/server")
	cmd.Dir = moduleRoot(t)
	cmd.Env = envWith(map[string]string{"VLM_URL": ""})

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("server exited 0, want nonzero; output:\n%s", out)
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1; output:\n%s", code, out)
	}
	for _, want := range []string{"startup", "VLM_URL"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("startup error missing %q; output:\n%s", want, out)
		}
	}
}

// AC3 process-level: nonzero delay + serialization off logs a startup
// warning and proceeds (no hard config error).
func TestING001_Integration_AC3_WarnsAndContinues(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL":                "http://127.0.0.1:1",
		"VLM_REQUEST_DELAY_MS":   "15",
		"VLM_SERIALIZE_REQUESTS": "false",
	})
	defer killServer(cmd)

	if !waitForOutput(buf, "will not be applied", 30*time.Second) {
		t.Fatalf("startup warning not logged; output:\n%s", buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "startup warning") {
		t.Errorf("warning line missing 'startup warning'; output:\n%s", out)
	}
	if strings.Contains(out, "startup:") {
		t.Errorf("got a hard startup error; output:\n%s", out)
	}
}

// AC2 process-level: VLM_API_KEY unset — startup proceeds normally.
func TestING001_Integration_AC2_ProceedsWithoutKey(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL":     "http://127.0.0.1:1",
		"VLM_API_KEY": "",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}

// AC5 process-level: LLM_URL unset — startup proceeds normally.
func TestING001_Integration_AC5_ProceedsWithoutLLMURL(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL": "http://127.0.0.1:1",
		"LLM_URL": "",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}

// ING-006 process-level tests: VLM_TEMPERATURE env var contract.

// AC1 process-level: VLM_TEMPERATURE unset — server starts normally
// with the default temperature of 0.4.
func TestING006_Integration_AC1_DefaultTemperature(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL": "http://127.0.0.1:1",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}

// AC2 process-level: VLM_TEMPERATURE set to a non-numeric value —
// server exits with a startup error naming VLM_TEMPERATURE.
func TestING006_Integration_AC2_InvalidTemperatureExits(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/server")
	cmd.Dir = moduleRoot(t)
	cmd.Env = envWith(map[string]string{
		"VLM_URL":         "http://127.0.0.1:1",
		"VLM_TEMPERATURE": "abc",
	})

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("server exited 0, want nonzero; output:\n%s", out)
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1; output:\n%s", code, out)
	}
	for _, want := range []string{"startup", "VLM_TEMPERATURE"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("startup error missing %q; output:\n%s", want, out)
		}
	}
}

// AC3 process-level: VLM_TEMPERATURE set to a valid float —
// server starts normally.
func TestING006_Integration_AC3_ValidTemperatureProceeds(t *testing.T) {
	cmd, buf := startServer(t, map[string]string{
		"VLM_URL":         "http://127.0.0.1:1",
		"VLM_TEMPERATURE": "0.7",
	})
	defer killServer(cmd)

	assertReachedListenStage(t, buf)
}

// assertReachedListenStage waits for the server to either bind or hit a
// bind error, and asserts no config-validation error occurred. A bind
// error still proves config validation passed; the port may be occupied
// by another process in the environment.
func assertReachedListenStage(t *testing.T, buf *syncBuffer) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out := buf.String()
		if strings.Contains(out, "listening on") ||
			strings.Contains(out, "listen tcp") ||
			strings.Contains(out, "startup:") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	out := buf.String()
	if strings.Contains(out, "startup:") {
		t.Fatalf("startup failed on config validation; output:\n%s", out)
	}
	if !strings.Contains(out, "listening on") && !strings.Contains(out, "listen tcp") {
		t.Fatalf("server did not reach the listen stage; output:\n%s", out)
	}
}
