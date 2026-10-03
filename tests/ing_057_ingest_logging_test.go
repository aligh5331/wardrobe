//go:build integration

// Process-level tests for ING-057: cmd/ingest sends every diagnostic through
// the configured slog logger (stderr + logs/app.log) and keeps stdout a strict
// one-JSON-object-per-line channel.
package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ing057Exists(h *ing012Harness, rel string) bool {
	_, err := os.Stat(filepath.Join(h.root, rel))
	return err == nil
}

// Config errors: text on stderr via slog.Default(), exit 1, empty stdout, no
// log file, even with LOG_FORMAT=json.
func TestING057_ConfigErrors(t *testing.T) {
	photo := writePhoto(t, "garment.jpg", []byte("ING057-CFG"))
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing VLM_URL", map[string]string{"VLM_URL": "", "LOG_FORMAT": "json"}, "VLM_URL"},
		{"bad LOG_LEVEL", map[string]string{"LOG_LEVEL": "verbose", "LOG_FORMAT": "json"}, "LOG_LEVEL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newING012Harness(t)
			stdout, stderr, code := h.run(t, ing012Env(tc.env), photo)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "ERROR") {
				t.Errorf("stderr is not a text error naming %s:\n%s", tc.want, stderr)
			}
			if strings.HasPrefix(strings.TrimSpace(stderr), "{") {
				t.Errorf("config error was JSON:\n%s", stderr)
			}
			if ing057Exists(h, "logs/app.log") {
				t.Error("logs/app.log was created for a config error")
			}
		})
	}
}

// No arguments: error-level usage record, exit 1, empty stdout. Config errors
// still win over the usage error.
func TestING057_Usage(t *testing.T) {
	h := newING012Harness(t)
	stdout, stderr, code := h.run(t, ing012Env(nil))
	if code != 1 || stdout != "" {
		t.Errorf("exit %d, stdout %q; want 1 and empty", code, stdout)
	}
	if !strings.Contains(stderr, "usage: ingest <photo-path> [photo-path...]") || !strings.Contains(stderr, "level=ERROR") {
		t.Errorf("stderr lacks the usage error record:\n%s", stderr)
	}

	_, stderr, code = h.run(t, ing012Env(map[string]string{"VLM_URL": ""}))
	if code != 1 || !strings.Contains(stderr, "VLM_URL") || strings.Contains(stderr, "usage:") {
		t.Errorf("config error should win over usage (exit %d):\n%s", code, stderr)
	}
}

// The delay warning is one warn record on stderr, not stdout.
func TestING057_StartupWarning(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	photo := writePhoto(t, "garment.jpg", []byte("ING057-WARN"))
	srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })

	h := newING012Harness(t)
	stdout, stderr, code := h.run(t, ing012Env(map[string]string{
		"VLM_URL": srv.URL, "VLM_REQUEST_DELAY_MS": "15", "VLM_SERIALIZE_REQUESTS": "false",
	}), photo)
	if code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr)
	}
	var lines []string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(line, "startup warning") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "will not be applied") || !strings.Contains(lines[0], "level=WARN") {
		t.Errorf("want one warn record with the warning text on stderr, got %q in:\n%s", lines, stderr)
	}
	if strings.Contains(stdout, "startup warning") {
		t.Errorf("warning leaked to stdout:\n%s", stdout)
	}
}

// At debug level, in both formats, stdout holds only tagged records and the
// diagnostics land on stderr and in logs/app.log.
func TestING057_StdoutStaysJSONOnly(t *testing.T) {
	tax := loadTaxonomy(t)
	valid, _ := validTaggingPayload(t, tax)
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			writeIng012Photo(t, dir, "one.jpg", []byte("ING057-1"))
			writeIng012Photo(t, dir, "two.png", []byte("ING057-2"))
			srv, _ := newING012VLM(t, func(int, ing012Request) string { return valid })

			h := newING012Harness(t)
			stdout, stderr, code := h.run(t, ing012Env(map[string]string{
				"VLM_URL": srv.URL, "LOG_LEVEL": "debug", "LOG_FORMAT": format,
				"VLM_REQUEST_DELAY_MS": "15", // forces a warn record onto the log
			}), dir)
			if code != 0 {
				t.Fatalf("exit %d; stderr:\n%s", code, stderr)
			}
			out := ing012ParseOutput(t, stdout) // fails on any non-JSON line
			if len(out) != 2 {
				t.Errorf("records = %d, want 2:\n%s", len(out), stdout)
			}
			for _, rec := range out {
				if _, ok := rec["item_id"]; !ok {
					t.Errorf("stdout line is not a tagged record: %v", rec)
				}
			}
			if !strings.Contains(stderr, "startup warning") {
				t.Errorf("stderr lacks diagnostics:\n%s", stderr)
			}
			data, err := os.ReadFile(filepath.Join(h.root, "logs", "app.log"))
			if err != nil || !strings.Contains(string(data), "startup warning") {
				t.Errorf("logs/app.log missing the records (err %v)", err)
			}
			for _, bad := range []string{"registry", "wardrobe.db", "LLM_URL"} {
				if strings.Contains(stderr, bad) {
					t.Errorf("stderr mentions %q:\n%s", bad, stderr)
				}
			}
		})
	}
}

// A hard failure is one error record that names the photo path, then exit 1
// with no tagged JSON on stdout.
func TestING057_FailureRecords(t *testing.T) {
	t.Run("missing photo", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "gone.jpg")
		h := newING012Harness(t)
		stdout, stderr, code := h.run(t, ing012Env(nil), missing)
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q", code, stdout)
		}
		if !strings.Contains(stderr, "error expanding") || !strings.Contains(stderr, missing) || !strings.Contains(stderr, "level=ERROR") {
			t.Errorf("stderr:\n%s", stderr)
		}
	})
	t.Run("VLM unreachable", func(t *testing.T) {
		photo := writePhoto(t, "garment.jpg", []byte("ING057-DEAD"))
		h := newING012Harness(t)
		stdout, stderr, code := h.run(t, ing012Env(nil), photo)
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q", code, stdout)
		}
		if !strings.Contains(stderr, "VLM unreachable") || !strings.Contains(stderr, photo) {
			t.Errorf("stderr:\n%s", stderr)
		}
	})
}

// cmd/ingest no longer uses the stdlib log package.
func TestING057_NoStdlibLog(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "cmd", "ingest", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, bad := range []string{`"log"`, "log.Printf", "log.Fatalf", "log.Println"} {
		if strings.Contains(s, bad) {
			t.Errorf("cmd/ingest/main.go still contains %s", bad)
		}
	}
	if strings.Count(s, "fmt.Print") != 1 || !strings.Contains(s, "fmt.Println(string(jsonBytes))") {
		t.Error("stdout writes are not exactly the one tagged-record Println")
	}
}
