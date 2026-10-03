// Package logging builds the app's log/slog logger (stderr plus an
// append-only log file) and carries a logger in a context.Context.
// See 07-architecture.md "Logging".
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// New builds a logger that writes to stderr and to the file at path.
// level and format are the lower-case values config.Load produces
// (debug, info, warn, error; text, json). Any other value is an error.
//
// The file and its directory are created if missing, and records are
// appended. If the file cannot be opened, New still succeeds: the logger
// writes to stderr only and a warning naming the failure is written to
// stderr first. That warning ignores the level, so LOG_LEVEL=error does
// not hide it.
//
// The returned Closer closes the file. It is never nil, and closing it
// after a fallback is a no-op. A long-running process may skip closing.
func New(level, format, path string, stderr io.Writer) (*slog.Logger, io.Closer, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, nil, fmt.Errorf("logging: invalid level %q: %w", level, err)
	}
	if format != "text" && format != "json" {
		return nil, nil, fmt.Errorf("logging: invalid format %q: must be text or json", format)
	}

	w := stderr
	var closer io.Closer = io.NopCloser(nil)
	f, openErr := openAppend(path)
	if openErr == nil {
		w = io.MultiWriter(stderr, f)
		closer = f
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewTextHandler(w, opts)
	if format == "json" {
		h = slog.NewJSONHandler(w, opts)
	}

	if openErr != nil {
		r := slog.NewRecord(time.Now(), slog.LevelWarn, "cannot open log file, logging to stderr only", 0)
		r.AddAttrs(slog.String("path", path), slog.Any("error", openErr))
		_ = h.Handle(context.Background(), r)
	}
	return slog.New(h), closer, nil
}

func openAppend(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

type ctxKey struct{}

// WithLogger returns a copy of ctx that carries l.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger stored by WithLogger, or slog.Default()
// when ctx carries none.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// Outcome classifies a failed outbound HTTP call for an attempt record:
// "canceled" when the caller went away, "timeout" for a deadline or net
// timeout, otherwise fallback.
func Outcome(err error, fallback string) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	}
	return fallback
}

// Snippet returns a single-line prefix of b, at most 300 bytes, for the
// body-snippet attribute of an attempt record.
func Snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
