package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func build(t *testing.T, level, format, path string) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	l, c, err := New(level, format, path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return l, &buf
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLevelFilteringBothSinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	l, buf := build(t, "warn", "text", path)
	l.Debug("dbg-msg")
	l.Info("info-msg")
	l.Warn("warn-msg")
	for name, got := range map[string]string{"stderr": buf.String(), "file": readFile(t, path)} {
		if !strings.Contains(got, "warn-msg") || strings.Contains(got, "dbg-msg") || strings.Contains(got, "info-msg") {
			t.Errorf("%s sink = %q, want only warn-msg", name, got)
		}
	}
}

func TestJSONFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	l, buf := build(t, "info", "json", path)
	l.Info("hello", "item_id", 7)
	for name, got := range map[string]string{"stderr": buf.String(), "file": readFile(t, path)} {
		var m map[string]any
		if err := json.Unmarshal([]byte(got), &m); err != nil {
			t.Fatalf("%s sink not JSON: %v: %q", name, err, got)
		}
		if m["level"] != "INFO" || m["msg"] != "hello" || m["item_id"] != float64(7) {
			t.Errorf("%s sink = %v", name, m)
		}
	}
}

func TestTextFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	l, buf := build(t, "info", "text", path)
	l.Info("hello", "item_id", 7)
	for name, got := range map[string]string{"stderr": buf.String(), "file": readFile(t, path)} {
		if json.Valid([]byte(got)) || !strings.Contains(got, "level=INFO") ||
			!strings.Contains(got, "msg=hello") || !strings.Contains(got, "item_id=7") {
			t.Errorf("%s sink = %q, want text key=value", name, got)
		}
	}
}

func TestCreatesDirAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	l, _ := build(t, "info", "text", path)
	l.Info("first-build")
	l2, _ := build(t, "info", "text", path)
	l2.Info("second-build")
	got := readFile(t, path)
	if !strings.Contains(got, "first-build") || !strings.Contains(got, "second-build") {
		t.Errorf("file = %q, want both builds appended", got)
	}
}

func TestOpenFailureFallsBackToStderr(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory as the path, and a path under a regular file.
	for _, path := range []string{dir, filepath.Join(blocker, "app.log")} {
		// level=error must not hide the warning.
		l, buf := build(t, "error", "text", path)
		got := buf.String()
		if !strings.Contains(got, "level=WARN") || !strings.Contains(got, "stderr only") || !strings.Contains(got, "error=") {
			t.Errorf("path %q: warning = %q", path, got)
		}
		buf.Reset()
		l.Error("still-logging")
		if !strings.Contains(buf.String(), "still-logging") {
			t.Errorf("path %q: record after fallback missing: %q", path, buf.String())
		}
	}
}

func TestInvalidLevelOrFormat(t *testing.T) {
	var buf bytes.Buffer
	path := filepath.Join(t.TempDir(), "app.log")
	if _, _, err := New("verbose", "text", path, &buf); err == nil {
		t.Error("invalid level: want error")
	}
	if _, _, err := New("info", "yaml", path, &buf); err == nil {
		t.Error("invalid format: want error")
	}
}

func TestRepoLogsDirUntouched(t *testing.T) {
	repoLogs := filepath.Join("..", "..", "logs")
	names := func() string {
		es, _ := os.ReadDir(repoLogs)
		var s []string
		for _, e := range es {
			fi, _ := e.Info()
			s = append(s, fmt.Sprint(e.Name(), fi.ModTime(), fi.Size()))
		}
		return strings.Join(s, "|")
	}
	before := names()
	l, _ := build(t, "info", "json", filepath.Join(t.TempDir(), "app.log"))
	l.Info("x")
	if after := names(); after != before {
		t.Errorf("repo logs/ changed: %q -> %q", before, after)
	}
}

func TestContextHelper(t *testing.T) {
	ctx := context.Background()
	if FromContext(ctx) != slog.Default() {
		t.Error("empty ctx: want slog.Default()")
	}
	l := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	ctx2 := WithLogger(ctx, l)
	child, cancel := context.WithCancel(ctx2)
	defer cancel()
	if FromContext(ctx2) != l || FromContext(child) != l {
		t.Error("want l from ctx2 and derived ctx")
	}
	if FromContext(ctx) != slog.Default() {
		t.Error("original ctx must still return slog.Default()")
	}
}
