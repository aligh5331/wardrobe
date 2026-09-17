package tagging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultAttemptLogPath is where ING-005 appends one JSON object per VLM
// attempt. It lives under logs/, the gitignored runtime-log directory in
// 07-architecture.md.
const DefaultAttemptLogPath = "logs/vlm-attempts.jsonl"

// AttemptLog is one VLM tagging attempt, as specified by ING-005's log
// format. Every attempt made for a photo is recorded — failed and successful
// alike — so a later analysis can join attempt 1 and attempt 2 by ItemID and
// compare their answers (the calibration signal in later-ideas.md). On a
// successful attempt FailureType and FailureDetail are empty, and ParsedJSON
// holds the (valid) parsed object; on malformed JSON ParsedJSON is null.
type AttemptLog struct {
	ItemID        string          `json:"item_id"`
	Timestamp     string          `json:"timestamp"`
	PhotoPath     string          `json:"photo_path"`
	Attempt       int             `json:"attempt"`
	Temperature   float64         `json:"temperature"`
	RawResponse   string          `json:"raw_response"`
	ParsedJSON    json.RawMessage `json:"parsed_json"`
	FailureType   string          `json:"failure_type"`
	FailureDetail string          `json:"failure_detail"`
}

// AttemptLogger appends one AttemptLog record. The concrete logger writes to
// logs/vlm-attempts.jsonl; tests supply an in-memory implementation.
type AttemptLogger interface {
	Log(AttemptLog) error
}

// JSONLAttemptLogger appends each AttemptLog as one JSON object per line to a
// file, creating the parent directory if needed. Safe for concurrent use.
type JSONLAttemptLogger struct {
	path string
	mu   sync.Mutex
}

// NewJSONLAttemptLogger returns a logger writing to path (which may be
// relative to the process working directory).
func NewJSONLAttemptLogger(path string) *JSONLAttemptLogger {
	return &JSONLAttemptLogger{path: path}
}

// Log appends a as one JSON line.
func (l *JSONLAttemptLogger) Log(a AttemptLog) error {
	line, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("encode attempt log: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if dir := filepath.Dir(l.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create attempt log dir %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open attempt log %s: %w", l.path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write attempt log %s: %w", l.path, err)
	}
	return nil
}
