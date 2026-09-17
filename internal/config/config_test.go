package config

import (
	"os"
	"strings"
	"testing"
)

// setEnv applies the full contract to t.Setenv so each case is
// deterministic regardless of the surrounding process environment.
// Empty string means unset for the purposes of Load.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, name := range []string{
		"VLM_URL", "VLM_API_KEY",
		"VLM_SERIALIZE_REQUESTS", "VLM_REQUEST_DELAY_MS",
		"VLM_TEMPERATURE",
		"LLM_URL", "LLM_API_KEY",
	} {
		t.Setenv(name, env[name])
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name            string
		env             map[string]string
		wantErr         string
		wantKey         string
		wantSerialize   bool
		wantDelayMS     int
		wantTemperature float64
		wantWarnings    int
	}{
		{
			name:    "missing VLM_URL errors naming it",
			env:     map[string]string{"VLM_URL": ""},
			wantErr: "VLM_URL",
		},
		{
			name:            "empty VLM_API_KEY proceeds with no key",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_API_KEY": ""},
			wantKey:         "",
			wantDelayMS:     0,
			wantTemperature: 0.4,
		},
		{
			name:            "serialize unset defaults to false",
			env:             map[string]string{"VLM_URL": "http://vlm"},
			wantSerialize:   false,
			wantTemperature: 0.4,
		},
		{
			name:            "serialize true is honored",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "true"},
			wantSerialize:   true,
			wantTemperature: 0.4,
		},
		{
			name:            "nonzero delay with serialize unset warns",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_REQUEST_DELAY_MS": "15"},
			wantDelayMS:     15,
			wantWarnings:    1,
			wantTemperature: 0.4,
		},
		{
			name:            "nonzero delay with serialize false warns",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "false", "VLM_REQUEST_DELAY_MS": "15"},
			wantDelayMS:     15,
			wantWarnings:    1,
			wantTemperature: 0.4,
		},
		{
			name:            "nonzero delay with serialize true does not warn",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "true", "VLM_REQUEST_DELAY_MS": "15"},
			wantDelayMS:     15,
			wantSerialize:   true,
			wantWarnings:    0,
			wantTemperature: 0.4,
		},
		{
			name:            "unset LLM_URL proceeds",
			env:             map[string]string{"VLM_URL": "http://vlm", "LLM_URL": ""},
			wantTemperature: 0.4,
		},
		{
			name:    "invalid VLM_REQUEST_DELAY_MS errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_REQUEST_DELAY_MS": "abc"},
			wantErr: "VLM_REQUEST_DELAY_MS",
		},
		{
			name:    "invalid VLM_SERIALIZE_REQUESTS errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "maybe"},
			wantErr: "VLM_SERIALIZE_REQUESTS",
		},
		// ING-006: VLM_TEMPERATURE tests
		{
			name:            "unset VLM_TEMPERATURE defaults to 0.4",
			env:             map[string]string{"VLM_URL": "http://vlm"},
			wantTemperature: 0.4,
		},
		{
			name:            "valid VLM_TEMPERATURE is honored",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "0.7"},
			wantTemperature: 0.7,
		},
		{
			name:    "invalid VLM_TEMPERATURE errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "abc"},
			wantErr: "VLM_TEMPERATURE",
		},
		// ING-008: VLM_TEMPERATURE must be finite and within 0.0-1.0.
		{
			name:    "NaN VLM_TEMPERATURE errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "NaN"},
			wantErr: "VLM_TEMPERATURE",
		},
		{
			name:    "Inf VLM_TEMPERATURE errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "Inf"},
			wantErr: "VLM_TEMPERATURE",
		},
		{
			name:    "minus Inf VLM_TEMPERATURE errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "-Inf"},
			wantErr: "VLM_TEMPERATURE",
		},
		{
			name:    "negative VLM_TEMPERATURE errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "-1"},
			wantErr: "VLM_TEMPERATURE",
		},
		{
			name:    "out-of-range VLM_TEMPERATURE errors naming it",
			env:     map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "1.5"},
			wantErr: "VLM_TEMPERATURE",
		},
		{
			name:            "zero VLM_TEMPERATURE is accepted",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "0"},
			wantTemperature: 0,
		},
		{
			name:            "upper-bound VLM_TEMPERATURE is accepted",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_TEMPERATURE": "1.0"},
			wantTemperature: 1.0,
		},
		// ING-010: negative VLM_REQUEST_DELAY_MS clamps to 0 and warns.
		{
			name:            "negative delay clamps to zero and warns",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_REQUEST_DELAY_MS": "-100"},
			wantDelayMS:     0,
			wantWarnings:    1,
			wantTemperature: 0.4,
		},
		{
			name:            "negative delay with serialize true still clamps and warns",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "true", "VLM_REQUEST_DELAY_MS": "-5"},
			wantSerialize:   true,
			wantDelayMS:     0,
			wantWarnings:    1,
			wantTemperature: 0.4,
		},
		{
			name:            "zero delay does not warn",
			env:             map[string]string{"VLM_URL": "http://vlm", "VLM_REQUEST_DELAY_MS": "0"},
			wantDelayMS:     0,
			wantWarnings:    0,
			wantTemperature: 0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			cfg, err := Load()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want error naming %s", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %q, want it to name %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if cfg.VLMAPIKey != tt.wantKey {
				t.Errorf("VLMAPIKey = %q, want %q", cfg.VLMAPIKey, tt.wantKey)
			}
			if cfg.VLMSerializeRequests != tt.wantSerialize {
				t.Errorf("VLMSerializeRequests = %v, want %v", cfg.VLMSerializeRequests, tt.wantSerialize)
			}
			if cfg.VLMRequestDelayMS != tt.wantDelayMS {
				t.Errorf("VLMRequestDelayMS = %d, want %d", cfg.VLMRequestDelayMS, tt.wantDelayMS)
			}
			if cfg.VLMTemperature != tt.wantTemperature {
				t.Errorf("VLMTemperature = %v, want %v", cfg.VLMTemperature, tt.wantTemperature)
			}
			if got := len(cfg.Warnings()); got != tt.wantWarnings {
				t.Errorf("len(Warnings()) = %d, want %d", got, tt.wantWarnings)
			}
		})
	}
}

// ING-010: the negative-delay warning must name the variable and state
// it was treated as 0, not merely be present.
func TestWarnings_NegativeDelayContent(t *testing.T) {
	setEnv(t, map[string]string{"VLM_URL": "http://vlm", "VLM_REQUEST_DELAY_MS": "-100"})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	warnings := cfg.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("Warnings() = %v, want exactly 1 warning", warnings)
	}
	w := warnings[0]
	for _, want := range []string{"VLM_REQUEST_DELAY_MS", "negative", "0"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q does not mention %q", w, want)
		}
	}
}

// floatEnv is shared by every float env var. The finite-value (NaN/Inf)
// guard applies unconditionally, while the numeric range is opt-in: a
// caller that passes nil bounds is not silently constrained by
// VLM_TEMPERATURE's 0.0-1.0 range.
func TestFloatEnv_BoundsAreOptIn(t *testing.T) {
	t.Run("nil bounds accept a large finite value", func(t *testing.T) {
		t.Setenv("TEST_FLOAT_ENV", "100")
		got, err := floatEnv("TEST_FLOAT_ENV", 0.4, nil, nil)
		if err != nil {
			t.Fatalf("floatEnv() error = %v, want nil for an unbounded finite value", err)
		}
		if got != 100 {
			t.Errorf("floatEnv() = %v, want 100", got)
		}
	})

	t.Run("nil bounds still reject NaN", func(t *testing.T) {
		t.Setenv("TEST_FLOAT_ENV", "NaN")
		if _, err := floatEnv("TEST_FLOAT_ENV", 0.4, nil, nil); err == nil {
			t.Fatal("floatEnv() = nil, want error for NaN even without bounds")
		}
	})
}

// ING-009 decision 2: VLM_SERIALIZE_REQUESTS is parsed strictly with
// strconv.ParseBool. Only the documented spellings are accepted; unset or
// empty is false; any other value is a startup error naming the variable
// — it must never silently evaluate to false.
func TestSerializeRequests_StrictParseBool(t *testing.T) {
	trueValues := []string{"1", "t", "T", "true", "TRUE", "True"}
	for _, raw := range trueValues {
		t.Run("true-"+raw, func(t *testing.T) {
			setEnv(t, map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": raw})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v, want nil for %q", err, raw)
			}
			if !cfg.VLMSerializeRequests {
				t.Errorf("VLMSerializeRequests = false, want true for %q", raw)
			}
		})
	}

	falseValues := []string{"0", "f", "F", "false", "FALSE", "False"}
	for _, raw := range falseValues {
		t.Run("false-"+raw, func(t *testing.T) {
			setEnv(t, map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": raw})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v, want nil for %q", err, raw)
			}
			if cfg.VLMSerializeRequests {
				t.Errorf("VLMSerializeRequests = true, want false for %q", raw)
			}
		})
	}

	t.Run("empty is false", func(t *testing.T) {
		setEnv(t, map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": ""})
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.VLMSerializeRequests {
			t.Error("VLMSerializeRequests = true, want false for empty")
		}
	})

	t.Run("unset is false", func(t *testing.T) {
		setEnv(t, map[string]string{"VLM_URL": "http://vlm"})
		if err := os.Unsetenv("VLM_SERIALIZE_REQUESTS"); err != nil {
			t.Fatalf("os.Unsetenv: %v", err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.VLMSerializeRequests {
			t.Error("VLMSerializeRequests = true, want false for unset")
		}
	})

	for _, raw := range []string{"yes", "ture", "maybe", "2", "TRUE "} {
		t.Run("invalid-"+strings.ReplaceAll(raw, " ", "_"), func(t *testing.T) {
			setEnv(t, map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": raw})
			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want a startup error for %q", raw)
			}
			if !strings.Contains(err.Error(), "VLM_SERIALIZE_REQUESTS") {
				t.Errorf("error %q does not name VLM_SERIALIZE_REQUESTS", err)
			}
		})
	}
}

// ING-009 decision 4: VLM_API_KEY is opaque. Any non-empty string is
// accepted verbatim with no format validation — including surrounding
// whitespace, which must not be trimmed away — and an empty or unset key
// stays empty so the client sends no Authorization header.
func TestVLMAPIKey_Opaque(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "plain token", key: "secret-token"},
		{name: "contains spaces", key: "with spaces"},
		{name: "arbitrary punctuation", key: "sk-!@#$%^&*()"},
		{name: "surrounding whitespace preserved", key: "  padded  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, map[string]string{"VLM_URL": "http://vlm", "VLM_API_KEY": tt.key})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v, want nil for an opaque key", err)
			}
			if cfg.VLMAPIKey != tt.key {
				t.Errorf("VLMAPIKey = %q, want %q stored verbatim", cfg.VLMAPIKey, tt.key)
			}
		})
	}

	t.Run("empty stays empty", func(t *testing.T) {
		setEnv(t, map[string]string{"VLM_URL": "http://vlm", "VLM_API_KEY": ""})
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.VLMAPIKey != "" {
			t.Errorf("VLMAPIKey = %q, want empty", cfg.VLMAPIKey)
		}
	})

	t.Run("unset stays empty", func(t *testing.T) {
		setEnv(t, map[string]string{"VLM_URL": "http://vlm"})
		if err := os.Unsetenv("VLM_API_KEY"); err != nil {
			t.Fatalf("os.Unsetenv: %v", err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.VLMAPIKey != "" {
			t.Errorf("VLMAPIKey = %q, want empty", cfg.VLMAPIKey)
		}
	})
}
