//go:build integration

// Tester additions for ING-054. The Coder's process tests never set LOG_LEVEL
// to a valid value, so nothing proved the configured logger honors cfg.LogLevel.
package tests

import (
	"net"
	"strings"
	"testing"
)

// LOG_LEVEL=error must filter the info "listening on" record and the warn
// startup warning, and still emit the error-level bind failure. The process
// exits, so the assertion is race-free.
func TestING054_Tester_LogLevelAppliesToConfiguredLogger(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	h := newING019Harness(t)
	stderr, code := ing054Fail(t, h, map[string]string{
		"VLM_URL":                "http://127.0.0.1:1",
		"LLM_URL":                "http://127.0.0.1:1",
		"LOG_LEVEL":              "error",
		"VLM_REQUEST_DELAY_MS":   "15",
		"VLM_SERIALIZE_REQUESTS": "false",
	}, "-addr", ln.Addr().String())

	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "server stopped") || !strings.Contains(stderr, "level=ERROR") {
		t.Errorf("error-level bind failure missing:\n%s", stderr)
	}
	for _, hidden := range []string{"listening on", "startup warning", "level=INFO", "level=WARN"} {
		if strings.Contains(stderr, hidden) {
			t.Errorf("LOG_LEVEL=error should hide %q:\n%s", hidden, stderr)
		}
	}
}
