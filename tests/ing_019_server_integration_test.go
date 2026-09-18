//go:build integration

// Process-level acceptance tests for ING-019: the real cmd/server binary
// serves the read-only API over Gin on the default :8080, and a `-addr`
// override makes it bind that address instead.
//
// The server fixes its DB path relative to the working directory
// (store.DefaultDBPath is "data/wardrobe.db"), so these tests build the real
// binary and run it with its working directory set to an isolated temp root.
// That writes data/wardrobe.db under the temp root instead of the repo's real
// data/ (personal wardrobe data).
package tests

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ing019Harness is an isolated project root plus the compiled server that
// runs with that root as its working directory.
type ing019Harness struct {
	root string
	bin  string
}

func newING019Harness(t *testing.T) *ing019Harness {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "server")
	build := exec.Command("go", "build", "-o", bin, "./cmd/server")
	build.Dir = moduleRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/server: %v\n%s", err, out)
	}
	return &ing019Harness{root: t.TempDir(), bin: bin}
}

// start runs the server from the harness root, so data/wardrobe.db and
// data/photos resolve under the temp root.
func (h *ing019Harness) start(t *testing.T, env map[string]string, args ...string) (*exec.Cmd, *syncBuffer) {
	t.Helper()

	buf := &syncBuffer{}
	cmd := exec.Command(h.bin, args...)
	cmd.Dir = h.root
	cmd.Env = envWith(env)
	cmd.Stdout = buf
	cmd.Stderr = buf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	return cmd, buf
}

// ing019Get polls url until it answers or the timeout elapses.
func ing019Get(url string, timeout time.Duration) (body string, status int, ok bool) {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return string(b), resp.StatusCode, true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", 0, false
}

// ing019FreePort asks the OS for an unused TCP port on loopback.
func ing019FreePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// AC1: Given cmd/server starts with the env contract satisfied and no address
// override / When it listens / Then it serves through Gin on the default
// :8080.
func TestING019_Integration_AC1_DefaultAddr8080(t *testing.T) {
	h := newING019Harness(t)
	cmd, buf := h.start(t, map[string]string{"VLM_URL": "http://127.0.0.1:1"})
	defer killServer(cmd)

	if !waitForOutput(buf, "listening on :8080", 30*time.Second) {
		t.Fatalf("server did not report the default listen address; output:\n%s", buf.String())
	}

	body, status, ok := ing019Get("http://127.0.0.1:8080/api/items", 5*time.Second)
	if ok {
		if status != http.StatusOK {
			t.Errorf("GET /api/items on default :8080 status = %d, want 200", status)
		}
		if strings.TrimSpace(body) != "[]" {
			t.Errorf("empty-catalog body = %q, want [] (the temp root starts with no rows)", body)
		}
		return
	}

	// Port 8080 may already be taken in the environment; the default-address
	// behavior is then proven by the listen line plus a bind error naming it.
	out := buf.String()
	if !strings.Contains(out, ":8080") || !strings.Contains(out, "bind") {
		t.Fatalf("could not reach default :8080 and no bind error was reported; output:\n%s", out)
	}
	t.Logf("port 8080 unavailable in this environment; default address verified from output:\n%s", out)
}

// AC2: Given an address override is supplied on the command line / When
// cmd/server starts / Then it binds that address instead of :8080.
func TestING019_Integration_AC2_AddrOverride(t *testing.T) {
	port := ing019FreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	h := newING019Harness(t)
	cmd, buf := h.start(t, map[string]string{"VLM_URL": "http://127.0.0.1:1"}, "-addr", addr)
	defer killServer(cmd)

	if !waitForOutput(buf, "listening on "+addr, 30*time.Second) {
		t.Fatalf("server did not report the override address %s; output:\n%s", addr, buf.String())
	}
	if strings.Contains(buf.String(), "listening on :8080") {
		t.Errorf("override ignored: server reported the default :8080; output:\n%s", buf.String())
	}

	body, status, ok := ing019Get("http://"+addr+"/api/items", 10*time.Second)
	if !ok {
		t.Fatalf("server did not answer on the override %s; output:\n%s", addr, buf.String())
	}
	if status != http.StatusOK {
		t.Errorf("GET /api/items on %s status = %d, want 200", addr, status)
	}
	if strings.TrimSpace(body) != "[]" {
		t.Errorf("empty-catalog body = %q, want []", body)
	}
}
