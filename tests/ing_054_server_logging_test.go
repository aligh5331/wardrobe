//go:build integration

// Process-level acceptance tests for ING-054: the real cmd/server binary
// reports config errors as text through slog.Default() and logs everything
// after config through the configured logger. Every test runs the compiled
// binary from the temp-root harness (newING019Harness), so logs/app.log and
// data/ land under the temp root and never in the repo. Listening tests pass
// `-addr 127.0.0.1:<free port>` instead of relying on :8080; the small window
// between reserving and binding the port is the only flakiness risk.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ing054Fail runs the server until it exits and returns stderr and the exit code.
func ing054Fail(t *testing.T, h *ing019Harness, env map[string]string, args ...string) (string, int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, h.bin, args...)
	cmd.Dir = h.root
	cmd.Env = envWith(env)
	cmd.Stderr = &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("server did not exit with a status (err = %v); stderr:\n%s", err, stderr.String())
	}
	return stderr.String(), exitErr.ExitCode()
}

// ing054Serve starts the server on a free loopback port and returns its stderr.
func ing054Serve(t *testing.T, h *ing019Harness, env map[string]string) (addr string, stderr *syncBuffer) {
	t.Helper()

	addr = fmt.Sprintf("127.0.0.1:%d", ing019FreePort(t))
	stderr = &syncBuffer{}
	cmd := exec.Command(h.bin, "-addr", addr)
	cmd.Dir = h.root
	cmd.Env = envWith(env)
	cmd.Stderr = stderr
	setProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { killServer(cmd) })

	if !waitForOutput(stderr, "listening on "+addr, 30*time.Second) {
		t.Fatalf("server did not log 'listening on %s'; stderr:\n%s", addr, stderr.String())
	}
	return addr, stderr
}

// ing054Records parses every non-empty line as a JSON object.
func ing054Records(t *testing.T, text string) []map[string]any {
	t.Helper()

	var recs []map[string]any
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("stderr line is not a JSON object: %q (%v)", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

// ing054Find returns the first record whose msg contains want.
func ing054Find(recs []map[string]any, want string) map[string]any {
	for _, rec := range recs {
		if msg, _ := rec["msg"].(string); strings.Contains(msg, want) {
			return rec
		}
	}
	return nil
}

func ing054NoAppLog(t *testing.T, h *ing019Harness) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(h.root, "logs", "app.log")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("logs/app.log exists in the temp root after a config error (stat err = %v)", err)
	}
}

// ing054AssertTextError checks stderr is a text error line, not a JSON object.
func ing054AssertTextError(t *testing.T, stderr string) {
	t.Helper()

	if !strings.Contains(stderr, "ERROR") {
		t.Errorf("stderr has no error-level line:\n%s", stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil {
			t.Errorf("stderr line is a JSON object, want text: %q", line)
		}
	}
}

// AC1 and AC2: an unrecognized LOG_LEVEL fails config.Load. The error is a
// text line naming LOG_LEVEL, even with LOG_FORMAT=json, and no log file is
// created.
func TestING054_Process_InvalidLogLevel(t *testing.T) {
	for _, format := range []string{"", "json"} {
		t.Run("LOG_FORMAT="+format, func(t *testing.T) {
			h := newING019Harness(t)
			env := map[string]string{"VLM_URL": "http://127.0.0.1:1", "LOG_LEVEL": "verbose"}
			if format != "" {
				env["LOG_FORMAT"] = format
			}

			stderr, code := ing054Fail(t, h, env)
			if code != 1 {
				t.Errorf("exit code = %d, want 1; stderr:\n%s", code, stderr)
			}
			for _, want := range []string{"LOG_LEVEL", "startup"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q:\n%s", want, stderr)
				}
			}
			ing054AssertTextError(t, stderr)
			ing054NoAppLog(t, h)
		})
	}
}

// AC3: an empty LLM_URL is a config error like any other: text, exit 1,
// names LLM_URL, no log file (even with LOG_FORMAT=json).
func TestING054_Process_EmptyLLMURL(t *testing.T) {
	h := newING019Harness(t)
	stderr, code := ing054Fail(t, h, map[string]string{
		"VLM_URL": "http://127.0.0.1:1", "LLM_URL": "", "LOG_FORMAT": "json",
	})
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"LLM_URL", "startup"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
	ing054AssertTextError(t, stderr)
	ing054NoAppLog(t, h)
}

// AC4: with LOG_FORMAT=json the "listening on" record and a startup warning
// are JSON objects on stderr, and the same lines are in logs/app.log.
func TestING054_Process_JSONRecordsOnStderrAndFile(t *testing.T) {
	h := newING019Harness(t)
	addr, stderr := ing054Serve(t, h, map[string]string{
		"VLM_URL":                "http://127.0.0.1:1",
		"LLM_URL":                "http://127.0.0.1:1",
		"LOG_FORMAT":             "json",
		"VLM_REQUEST_DELAY_MS":   "15",
		"VLM_SERIALIZE_REQUESTS": "false",
	})

	recs := ing054Records(t, stderr.String())
	for _, rec := range recs {
		for _, key := range []string{"time", "level", "msg"} {
			if _, ok := rec[key]; !ok {
				t.Errorf("record missing %q: %v", key, rec)
			}
		}
	}
	warn := ing054Find(recs, "startup warning")
	if warn == nil {
		t.Fatalf("no startup warning record; stderr:\n%s", stderr.String())
	}
	if warn["level"] != "WARN" || !strings.Contains(warn["msg"].(string), "will not be applied") {
		t.Errorf("warning record = %v, want level WARN and msg containing 'will not be applied'", warn)
	}
	listen := ing054Find(recs, "listening on "+addr)
	if listen == nil || listen["level"] != "INFO" {
		t.Errorf("listening record = %v, want level INFO", listen)
	}

	// The file gets each record right after stderr, so poll for the last one.
	logPath := filepath.Join(h.root, "logs", "app.log")
	deadline := time.Now().Add(5 * time.Second)
	var file []byte
	for time.Now().Before(deadline) {
		file, _ = os.ReadFile(logPath)
		if bytes.Contains(file, []byte("listening on "+addr)) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if !strings.Contains(string(file), strings.TrimSpace(line)) {
			t.Errorf("logs/app.log is missing stderr line %q; file:\n%s", line, file)
		}
	}
}

// AC5: GET /api/taxonomy yields one JSON access-log record with a request_id,
// written by the configured logger.
func TestING054_Process_AccessLogRecord(t *testing.T) {
	h := newING019Harness(t)
	addr, stderr := ing054Serve(t, h, map[string]string{
		"VLM_URL": "http://127.0.0.1:1", "LLM_URL": "http://127.0.0.1:1", "LOG_FORMAT": "json",
	})

	if _, status, ok := ing019Get("http://"+addr+"/api/taxonomy", 10*time.Second); !ok || status != http.StatusOK {
		t.Fatalf("GET /api/taxonomy: ok=%v status=%d; stderr:\n%s", ok, status, stderr.String())
	}
	if !waitForOutput(stderr, `"msg":"http request"`, 5*time.Second) {
		t.Fatalf("no access-log record; stderr:\n%s", stderr.String())
	}

	var got []map[string]any
	for _, rec := range ing054Records(t, stderr.String()) {
		if rec["msg"] == "http request" && rec["route"] == "/api/taxonomy" {
			got = append(got, rec)
		}
	}
	if len(got) != 1 {
		t.Fatalf("access records for /api/taxonomy = %d, want 1; stderr:\n%s", len(got), stderr.String())
	}
	if id, _ := got[0]["request_id"].(string); id == "" {
		t.Errorf("access record has no request_id: %v", got[0])
	}
	if got[0]["status"] != float64(http.StatusOK) {
		t.Errorf("access record status = %v, want 200", got[0]["status"])
	}
}

// AC7a: store.Open fails (data is a file, so MkdirAll fails). The configured
// logger, which is text by default, logs an error-level record and the
// process exits 1.
func TestING054_Process_StoreOpenFailure(t *testing.T) {
	h := newING019Harness(t)
	if err := os.WriteFile(filepath.Join(h.root, "data"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr, code := ing054Fail(t, h, map[string]string{"VLM_URL": "http://127.0.0.1:1", "LLM_URL": "http://127.0.0.1:1"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"level=ERROR", "startup: open store"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

// AC7b: ListenAndServe fails (port taken). The configured logger logs an
// error-level record with the bind error and the process exits 1.
func TestING054_Process_BindFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	h := newING019Harness(t)
	stderr, code := ing054Fail(t, h, map[string]string{"VLM_URL": "http://127.0.0.1:1", "LLM_URL": "http://127.0.0.1:1"},
		"-addr", ln.Addr().String())
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"level=ERROR", "server stopped", "listen tcp"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}
