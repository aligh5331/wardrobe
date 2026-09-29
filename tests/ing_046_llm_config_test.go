// Package tests holds black-box acceptance tests for backlog tickets.
// ING-046: LLM_TEMPERATURE and LLM_MODEL follow the same env var contract
// as their VLM_* counterparts (07-architecture.md "Env var contract";
// 06-decisions.md "LLM config", "VLM_TEMPERATURE acceptable range"), and
// LLM_URL becomes a startup requirement in cmd/server only, via
// RequireLLMURL — config.Load() itself must keep accepting an empty
// LLM_URL (TestING001_AC5_LLMURLNotValidated pins that).
//
// These tests exercise the exported config.Load/RequireLLMURL API only;
// implementation code is never touched from here.
package tests

import (
	"os"
	"strings"
	"testing"

	"wardrobe/internal/config"
)

// llmTemperatureEnv is the minimal valid environment plus the LLM
// temperature under test.
func llmTemperatureEnv(value string) map[string]string {
	return map[string]string{
		"VLM_URL":         "http://vlm.local",
		"LLM_TEMPERATURE": value,
	}
}

// assertLLMTemperatureRejected asserts config.Load() fails and names
// LLM_TEMPERATURE as the offending variable.
func assertLLMTemperatureRejected(t *testing.T, value string) {
	t.Helper()

	setContractEnv(t, llmTemperatureEnv(value))

	cfg, err := config.Load()
	if err == nil {
		t.Fatalf("config.Load() = nil error for LLM_TEMPERATURE=%q, want error naming LLM_TEMPERATURE", value)
	}
	if cfg != nil {
		t.Fatalf("config.Load() returned a config (%+v) alongside the error for LLM_TEMPERATURE=%q", cfg, value)
	}
	if !strings.Contains(err.Error(), "LLM_TEMPERATURE") {
		t.Fatalf("config.Load() error = %q for LLM_TEMPERATURE=%q, want it to name LLM_TEMPERATURE", err, value)
	}
}

// AC1: Given LLM_TEMPERATURE is unset or empty / When config loads /
// Then LLMTemperature is 0.4.
func TestING046_AC1_DefaultTemperature(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local"})
		os.Unsetenv("LLM_TEMPERATURE")

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.LLMTemperature != 0.4 {
			t.Errorf("LLMTemperature = %v, want 0.4", cfg.LLMTemperature)
		}
	})

	t.Run("empty", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_TEMPERATURE": ""})

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.LLMTemperature != 0.4 {
			t.Errorf("LLMTemperature = %v, want 0.4", cfg.LLMTemperature)
		}
	})
}

// AC2: Given LLM_TEMPERATURE is "0", "0.7", or "1.0" / When config
// loads / Then it is accepted with that value.
func TestING046_AC2_ValidTemperatureAccepted(t *testing.T) {
	tests := []struct {
		value string
		want  float64
	}{
		{value: "0", want: 0},
		{value: "0.7", want: 0.7},
		{value: "1.0", want: 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			setContractEnv(t, llmTemperatureEnv(tt.value))

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v for LLM_TEMPERATURE=%q, want nil", err, tt.value)
			}
			if cfg.LLMTemperature != tt.want {
				t.Errorf("LLMTemperature = %v, want %v", cfg.LLMTemperature, tt.want)
			}
		})
	}
}

// AC3: Given LLM_TEMPERATURE is non-numeric, NaN, Inf, -Inf, negative,
// or > 1.0 / When config loads / Then it returns an error naming
// LLM_TEMPERATURE (same rules as VLM_TEMPERATURE, ING-008).
func TestING046_AC3_InvalidTemperatureErrors(t *testing.T) {
	values := []string{
		"abc",
		"NaN", "nan", "+NaN",
		"Inf", "-Inf", "+Inf", "inf", "-inf", "Infinity", "-Infinity",
		"-1", "-0.001", "-100",
		"1.5", "100", "1.0001",
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			assertLLMTemperatureRejected(t, value)
		})
	}
}

// AC4: Given LLM_MODEL is unset/empty or set to "qwen3:8b" / When
// config loads / Then LLMModel is "" or "qwen3:8b" respectively
// (trimmed), never an error.
func TestING046_AC4_LLMModelNeverErrors(t *testing.T) {
	tests := []struct {
		name  string
		unset bool
		value string
		want  string
	}{
		{name: "unset", unset: true, want: ""},
		{name: "empty", value: "", want: ""},
		{name: "qwen3:8b", value: "qwen3:8b", want: "qwen3:8b"},
		{name: "surrounding whitespace trimmed", value: "  qwen3:8b  ", want: "qwen3:8b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_MODEL": tt.value})
			if tt.unset {
				os.Unsetenv("LLM_MODEL")
			}

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v for LLM_MODEL=%q, want nil", err, tt.value)
			}
			if cfg.LLMModel != tt.want {
				t.Errorf("LLMModel = %q, want %q", cfg.LLMModel, tt.want)
			}
		})
	}
}

// AC5 (unit level): Given LLM_URL is empty or whitespace-only / Then
// config.Load() still succeeds (LLM_URL is not validated by Load, per
// TestING001_AC5_LLMURLNotValidated) but RequireLLMURL() returns an
// error naming LLM_URL — the check cmd/server runs at startup.
func TestING046_AC5_RequireLLMURLNamesVariable(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "whitespace only", url: "   "},
		{name: "tabs and newlines", url: "\t\n  \r"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_URL": tt.url})

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v, want nil (LLM_URL is not validated by Load)", err)
			}

			err = cfg.RequireLLMURL()
			if err == nil {
				t.Fatalf("RequireLLMURL() = nil for LLM_URL=%q, want an error naming LLM_URL", tt.url)
			}
			if !strings.Contains(err.Error(), "LLM_URL") {
				t.Errorf("RequireLLMURL() error = %q, want it to name LLM_URL", err)
			}
		})
	}
}

// AC5 (unit level, positive case): a set LLM_URL makes RequireLLMURL()
// proceed with no error.
func TestING046_AC5_RequireLLMURLProceedsWhenSet(t *testing.T) {
	setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_URL": "http://llm.local"})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil", err)
	}
	if err := cfg.RequireLLMURL(); err != nil {
		t.Errorf("RequireLLMURL() error = %v, want nil when LLM_URL is set", err)
	}
}

// AC6 (unit level): Given LLM_URL is empty and VLM_URL is set / Then
// config.Load() still succeeds — the shared step every entry point
// (cmd/server and cmd/ingest) takes before either does its own
// server-only RequireLLMURL check. Process-level coverage that
// cmd/ingest never calls RequireLLMURL lives in
// tests/ing_046_server_integration_test.go.
func TestING046_AC6_LoadSucceedsWithEmptyLLMURL(t *testing.T) {
	setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_URL": ""})

	if _, err := config.Load(); err != nil {
		t.Fatalf("config.Load() error = %v, want nil (LLM_URL empty must not block config loading)", err)
	}
}

// AC7 (unit level): Given LLM_URL is set to a malformed value / Then
// RequireLLMURL() still proceeds — no URL-format validation, only a
// non-empty check.
func TestING046_AC7_RequireLLMURLIgnoresMalformedValue(t *testing.T) {
	tests := []string{"not a url", "vlm.local", "http://", "ftp://example.com"}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_URL": raw})

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v, want nil", err)
			}
			if cfg.LLMURL != raw {
				t.Errorf("LLMURL = %q, want %q carried through unchanged", cfg.LLMURL, raw)
			}
			if err := cfg.RequireLLMURL(); err != nil {
				t.Errorf("RequireLLMURL() error = %v, want nil for a malformed but non-empty LLM_URL %q (no URL-format validation)", err, raw)
			}
		})
	}
}
