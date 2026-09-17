// Package tests holds black-box acceptance tests for backlog tickets.
// Kept under tests/ (never under internal/) per the Tester role: this
// agent does not modify implementation code.
package tests

import (
	"os"
	"strings"
	"testing"

	"wardrobe/internal/config"
)

// contractVars is every env var in the 07-architecture.md env contract.
var contractVars = []string{
	"VLM_URL",
	"VLM_API_KEY",
	"VLM_SERIALIZE_REQUESTS",
	"VLM_REQUEST_DELAY_MS",
	"VLM_TEMPERATURE",
	"LLM_URL",
	"LLM_API_KEY",
}

// setContractEnv sets all contract vars (missing map entry => empty) via
// t.Setenv, so each case is deterministic regardless of the surrounding
// process environment and is restored automatically.
func setContractEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, name := range contractVars {
		t.Setenv(name, env[name])
	}
}

// AC1: Given VLM_URL is unset or empty / When the server starts / Then
// it exits with a startup error naming VLM_URL as the missing required
// variable.
func TestING001_AC1_VLMURLRequired(t *testing.T) {
	tests := []struct {
		name  string
		unset bool
	}{
		{name: "unset", unset: true},
		{name: "empty", unset: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{})
			if tt.unset {
				// t.Setenv above registered restoration; now truly unset.
				os.Unsetenv("VLM_URL")
			}

			_, err := config.Load()
			if err == nil {
				t.Fatalf("config.Load() = nil error, want error naming VLM_URL")
			}
			if !strings.Contains(err.Error(), "VLM_URL") {
				t.Fatalf("config.Load() error = %q, want it to name VLM_URL", err)
			}
		})
	}
}

// AC2: Given VLM_API_KEY is unset / When the server starts / Then
// startup proceeds normally and no Authorization header is sent on VLM
// requests.
//
// The header clause is a VLM-client behavior; no client exists in this
// ticket. Covered here: startup proceeds and the empty key is carried
// so the client can gate the header on VLMAPIKey != "".
func TestING001_AC2_VLMAPIKeyOptional(t *testing.T) {
	t.Run("unset proceeds with empty key", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local"})

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.VLMAPIKey != "" {
			t.Errorf("VLMAPIKey = %q, want empty", cfg.VLMAPIKey)
		}
	})

	t.Run("set is carried for the VLM client", func(t *testing.T) {
		setContractEnv(t, map[string]string{
			"VLM_URL":     "http://vlm.local",
			"VLM_API_KEY": "secret-token",
		})

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.VLMAPIKey != "secret-token" {
			t.Errorf("VLMAPIKey = %q, want secret-token", cfg.VLMAPIKey)
		}
	})
}

// AC3: Given VLM_REQUEST_DELAY_MS is set to a nonzero value and
// VLM_SERIALIZE_REQUESTS is false or unset / When the server starts /
// Then it logs a startup warning that the delay will not be applied, and
// proceeds without a hard error.
func TestING001_AC3_DelayWithoutSerializeWarns(t *testing.T) {
	tests := []struct {
		name      string
		serialize string
	}{
		{name: "serialize unset", serialize: ""},
		{name: "serialize false", serialize: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{
				"VLM_URL":                "http://vlm.local",
				"VLM_REQUEST_DELAY_MS":   "15",
				"VLM_SERIALIZE_REQUESTS": tt.serialize,
			})

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v, want nil (no hard error)", err)
			}
			if cfg.VLMSerializeRequests {
				t.Errorf("VLMSerializeRequests = true, want false")
			}
			if cfg.VLMRequestDelayMS != 15 {
				t.Errorf("VLMRequestDelayMS = %d, want 15", cfg.VLMRequestDelayMS)
			}

			warnings := cfg.Warnings()
			if len(warnings) != 1 {
				t.Fatalf("Warnings() = %v, want exactly 1 warning", warnings)
			}
			w := warnings[0]
			for _, want := range []string{
				"VLM_REQUEST_DELAY_MS",
				"VLM_SERIALIZE_REQUESTS",
				"will not be applied",
			} {
				if !strings.Contains(w, want) {
					t.Errorf("warning %q does not mention %q", w, want)
				}
			}
		})
	}
}

// AC4: Given VLM_SERIALIZE_REQUESTS is unset / When the server starts /
// Then it defaults to false (concurrent VLM requests, no artificial
// bottleneck).
func TestING001_AC4_SerializeDefaultsFalse(t *testing.T) {
	setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local"})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil", err)
	}
	if cfg.VLMSerializeRequests {
		t.Errorf("VLMSerializeRequests default = true, want false")
	}
}

// AC5: Given LLM_URL is unset / When the server starts / Then startup
// proceeds normally — LLM_URL validation is NOT enforced in Phase 1,
// since there is no Phase 1 consumer of it yet.
func TestING001_AC5_LLMURLNotValidated(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local"})
		os.Unsetenv("LLM_URL")

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil (LLM_URL not required)", err)
		}
		if cfg.LLMURL != "" {
			t.Errorf("LLMURL = %q, want empty", cfg.LLMURL)
		}
	})

	t.Run("empty", func(t *testing.T) {
		setContractEnv(t, map[string]string{
			"VLM_URL": "http://vlm.local",
			"LLM_URL": "",
		})

		if _, err := config.Load(); err != nil {
			t.Fatalf("config.Load() error = %v, want nil (LLM_URL not required)", err)
		}
	})

	t.Run("set is carried but not validated", func(t *testing.T) {
		setContractEnv(t, map[string]string{
			"VLM_URL": "http://vlm.local",
			"LLM_URL": "http://llm.local",
		})

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.LLMURL != "http://llm.local" {
			t.Errorf("LLMURL = %q, want http://llm.local", cfg.LLMURL)
		}
	})
}

// Spec-implied edge cases (not literal ACs) -----------------------------

// Spec: delay only takes effect when serialization is on. With
// serialization on there is nothing to warn about.
func TestING001_Edge_DelayWithSerializeNoWarning(t *testing.T) {
	setContractEnv(t, map[string]string{
		"VLM_URL":                "http://vlm.local",
		"VLM_SERIALIZE_REQUESTS": "true",
		"VLM_REQUEST_DELAY_MS":   "15",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil", err)
	}
	if !cfg.VLMSerializeRequests {
		t.Errorf("VLMSerializeRequests = false, want true")
	}
	if got := cfg.Warnings(); len(got) != 0 {
		t.Errorf("Warnings() = %v, want none when serialize is on", got)
	}
}

// Spec: VLM_REQUEST_DELAY_MS defaults to 0; only >0 with serialization
// off warns. Zero must not warn.
func TestING001_Edge_ZeroDelayNoWarning(t *testing.T) {
	setContractEnv(t, map[string]string{
		"VLM_URL":              "http://vlm.local",
		"VLM_REQUEST_DELAY_MS": "0",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil", err)
	}
	if cfg.VLMRequestDelayMS != 0 {
		t.Errorf("VLMRequestDelayMS = %d, want 0", cfg.VLMRequestDelayMS)
	}
	if got := cfg.Warnings(); len(got) != 0 {
		t.Errorf("Warnings() = %v, want none for a zero delay", got)
	}
}

// Coder-added behavior (not in the ACs): unparseable optional values
// fail fast naming the offending variable rather than silently
// defaulting. Verify it holds.
func TestING001_Edge_InvalidValuesFailFast(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantVar string
	}{
		{
			name:    "non-integer delay",
			env:     map[string]string{"VLM_URL": "http://vlm.local", "VLM_REQUEST_DELAY_MS": "abc"},
			wantVar: "VLM_REQUEST_DELAY_MS",
		},
		{
			name:    "non-bool serialize",
			env:     map[string]string{"VLM_URL": "http://vlm.local", "VLM_SERIALIZE_REQUESTS": "maybe"},
			wantVar: "VLM_SERIALIZE_REQUESTS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, tt.env)

			_, err := config.Load()
			if err == nil {
				t.Fatalf("config.Load() = nil, want error naming %s", tt.wantVar)
			}
			if !strings.Contains(err.Error(), tt.wantVar) {
				t.Errorf("error = %q, want it to name %s", err, tt.wantVar)
			}
		})
	}
}

// ING-013: a whitespace-only VLM_URL is trimmed and then rejected, same
// as empty/unset. This supersedes the pre-c356101 expectation that a
// whitespace value passed the `!= ""` check. Consistent with
// 07-architecture.md's "startup error if empty" and ING-011 AC3.
func TestING001_Edge_WhitespaceURLIsRejected(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "spaces", url: "   "},
		{name: "tabs and newlines", url: "\t\n  \r"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": tt.url})

			_, err := config.Load()
			if err == nil {
				t.Fatalf("config.Load() = nil error, want rejection of whitespace-only VLM_URL %q", tt.url)
			}
			if !strings.Contains(err.Error(), "missing required environment variable: VLM_URL") {
				t.Errorf("config.Load() error = %q, want it to contain %q", err, "missing required environment variable: VLM_URL")
			}
		})
	}
}

// ING-013 spec-implied edge: trimming must not corrupt a real URL —
// surrounding whitespace is stripped and the trimmed value is carried.
func TestING001_Edge_WhitespacePaddedURLIsTrimmedAndAccepted(t *testing.T) {
	setContractEnv(t, map[string]string{"VLM_URL": "  http://vlm.local  "})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil for a padded valid URL", err)
	}
	if cfg.VLMURL != "http://vlm.local" {
		t.Errorf("VLMURL = %q, want %q", cfg.VLMURL, "http://vlm.local")
	}
}

// ING-010: negative VLM_REQUEST_DELAY_MS is clamped to 0 and a warning
// is emitted. This test documents the new expected behavior.
func TestING001_Edge_NegativeDelayClampsAndWarns(t *testing.T) {
	setContractEnv(t, map[string]string{
		"VLM_URL":              "http://vlm.local",
		"VLM_REQUEST_DELAY_MS": "-5",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil", err)
	}
	if cfg.VLMRequestDelayMS != 0 {
		t.Errorf("VLMRequestDelayMS = %d, want 0 (clamped from %s)", cfg.VLMRequestDelayMS, "-5")
	}
	if got := cfg.Warnings(); len(got) != 1 {
		t.Errorf("Warnings() = %v, want exactly 1 warning (clamp+warn)", got)
	} else {
		w := got[0]
		for _, want := range []string{"VLM_REQUEST_DELAY_MS", "negative", "0"} {
			if !strings.Contains(w, want) {
				t.Errorf("warning %q does not mention %q", w, want)
			}
		}
	}
}

// ING-006 acceptance criteria -------------------------------

// AC1: Given VLM_TEMPERATURE is unset / When the server starts /
// Then it defaults to 0.4.
func TestING006_AC1_DefaultTemperature(t *testing.T) {
	setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local"})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil", err)
	}
	if cfg.VLMTemperature != 0.4 {
		t.Errorf("VLMTemperature = %v, want 0.4", cfg.VLMTemperature)
	}
}

// AC2: Given VLM_TEMPERATURE is set to a non-numeric value /
// When the server starts / Then it exits with a startup error
// naming VLM_TEMPERATURE as invalid.
func TestING006_AC2_InvalidTemperatureErrors(t *testing.T) {
	setContractEnv(t, map[string]string{
		"VLM_URL":         "http://vlm.local",
		"VLM_TEMPERATURE": "abc",
	})

	_, err := config.Load()
	if err == nil {
		t.Fatalf("config.Load() = nil, want error naming VLM_TEMPERATURE")
	}
	if !strings.Contains(err.Error(), "VLM_TEMPERATURE") {
		t.Errorf("error = %q, want it to name VLM_TEMPERATURE", err)
	}
}

// AC3: Given VLM_TEMPERATURE is set to a valid float /
// When the server starts / Then startup proceeds normally and the
// value is carried in the config for the VLM client (ING-007).
func TestING006_AC3_ValidTemperatureProceeds(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantVal float64
	}{
		{name: "0.4", value: "0.4", wantVal: 0.4},
		{name: "0.7", value: "0.7", wantVal: 0.7},
		{name: "1.0", value: "1.0", wantVal: 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setContractEnv(t, map[string]string{
				"VLM_URL":         "http://vlm.local",
				"VLM_TEMPERATURE": tt.value,
			})

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v, want nil", err)
			}
			if cfg.VLMTemperature != tt.wantVal {
				t.Errorf("VLMTemperature = %v, want %v", cfg.VLMTemperature, tt.wantVal)
			}
		})
	}
}
