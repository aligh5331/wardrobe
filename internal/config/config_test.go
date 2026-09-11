package config

import (
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
		"LLM_URL", "LLM_API_KEY",
	} {
		t.Setenv(name, env[name])
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		wantErr       string
		wantKey       string
		wantSerialize bool
		wantDelayMS   int
		wantWarnings  int
	}{
		{
			name:    "missing VLM_URL errors naming it",
			env:     map[string]string{"VLM_URL": ""},
			wantErr: "VLM_URL",
		},
		{
			name:        "empty VLM_API_KEY proceeds with no key",
			env:         map[string]string{"VLM_URL": "http://vlm", "VLM_API_KEY": ""},
			wantKey:     "",
			wantDelayMS: 0,
		},
		{
			name:          "serialize unset defaults to false",
			env:           map[string]string{"VLM_URL": "http://vlm"},
			wantSerialize: false,
		},
		{
			name:          "serialize true is honored",
			env:           map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "true"},
			wantSerialize: true,
		},
		{
			name:         "nonzero delay with serialize unset warns",
			env:          map[string]string{"VLM_URL": "http://vlm", "VLM_REQUEST_DELAY_MS": "15"},
			wantDelayMS:  15,
			wantWarnings: 1,
		},
		{
			name:         "nonzero delay with serialize false warns",
			env:          map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "false", "VLM_REQUEST_DELAY_MS": "15"},
			wantDelayMS:  15,
			wantWarnings: 1,
		},
		{
			name:          "nonzero delay with serialize true does not warn",
			env:           map[string]string{"VLM_URL": "http://vlm", "VLM_SERIALIZE_REQUESTS": "true", "VLM_REQUEST_DELAY_MS": "15"},
			wantDelayMS:   15,
			wantSerialize: true,
			wantWarnings:  0,
		},
		{
			name: "unset LLM_URL proceeds",
			env:  map[string]string{"VLM_URL": "http://vlm", "LLM_URL": ""},
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
			if got := len(cfg.Warnings()); got != tt.wantWarnings {
				t.Errorf("len(Warnings()) = %d, want %d", got, tt.wantWarnings)
			}
		})
	}
}
