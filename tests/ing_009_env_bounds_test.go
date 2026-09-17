// Package tests holds black-box acceptance tests for backlog tickets.
// ING-009 audits the VLM_* env vars for parseable-but-semantically-invalid
// input: the same class of gap VLM_TEMPERATURE had (fixed in ING-008).
// These tests exercise the exported config.Load/Warnings and the tagging
// client only; implementation code is never touched from here.
//
// Per the ticket, decisions 1 (malformed VLM_URL) and 3 (negative delay)
// are mechanically implemented and covered by ING-011/ING-010; they are
// still asserted here so every AC in this ticket has an explicit result,
// but the primary new coverage is decisions 2 and 4.
package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"wardrobe/internal/config"
	"wardrobe/internal/tagging"
)

// AC1: Given VLM_URL is malformed ("not a url", a bare hostname, or
// "http://") / When the server starts / Then startup proceeds with no
// URL-format validation, and the malformed value surfaces on use as an
// "VLM unreachable" error per ING-002.
func TestING009_AC1_MalformedURLAcceptedAtStartup(t *testing.T) {
	for _, raw := range []string{"not a url", "vlm.local", "http://"} {
		t.Run(raw, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": raw})

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v, want nil (no URL-format validation at startup)", err)
			}
			if cfg.VLMURL != raw {
				t.Fatalf("VLMURL = %q, want %q carried through unchanged", cfg.VLMURL, raw)
			}

			client := tagging.NewClient(cfg.VLMURL, cfg.VLMAPIKey)
			_, err = client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x")))
			if err == nil {
				t.Fatal("Tag() error = nil, want ErrVLMUnreachable")
			}
			if !errors.Is(err, tagging.ErrVLMUnreachable) {
				t.Errorf("errors.Is(err, tagging.ErrVLMUnreachable) = false; err = %v", err)
			}
		})
	}
}

// AC2: Given VLM_SERIALIZE_REQUESTS is an unrecognized boolean string
// ("yes", "ture") / When the server starts / Then it exits with a startup
// error naming VLM_SERIALIZE_REQUESTS — never a silent false. The
// recognized spellings parse as documented; unset/empty is false.
func TestING009_AC2_SerializeRequestsStrictParseBool(t *testing.T) {
	t.Run("recognized true spellings", func(t *testing.T) {
		for _, raw := range []string{"1", "t", "T", "true", "TRUE", "True"} {
			t.Run(raw, func(t *testing.T) {
				setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_SERIALIZE_REQUESTS": raw})
				cfg, err := config.Load()
				if err != nil {
					t.Fatalf("config.Load() error = %v, want nil for %q", err, raw)
				}
				if !cfg.VLMSerializeRequests {
					t.Errorf("VLMSerializeRequests = false, want true for %q", raw)
				}
			})
		}
	})

	t.Run("recognized false spellings", func(t *testing.T) {
		for _, raw := range []string{"0", "f", "F", "false", "FALSE", "False"} {
			t.Run(raw, func(t *testing.T) {
				setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_SERIALIZE_REQUESTS": raw})
				cfg, err := config.Load()
				if err != nil {
					t.Fatalf("config.Load() error = %v, want nil for %q", err, raw)
				}
				if cfg.VLMSerializeRequests {
					t.Errorf("VLMSerializeRequests = true, want false for %q", raw)
				}
			})
		}
	})

	t.Run("unset is false", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local"})
		if err := os.Unsetenv("VLM_SERIALIZE_REQUESTS"); err != nil {
			t.Fatalf("os.Unsetenv: %v", err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.VLMSerializeRequests {
			t.Error("VLMSerializeRequests = true, want false when unset")
		}
	})

	t.Run("empty is false", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_SERIALIZE_REQUESTS": ""})
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.VLMSerializeRequests {
			t.Error("VLMSerializeRequests = true, want false when empty")
		}
	})

	// "yes"/"ture" are the ticket's examples. "on"/"off"/"2"/"TRUE "
	// are spec-implied: strconv.ParseBool does not accept them, so they
	// must error rather than fall through to false.
	for _, raw := range []string{"yes", "ture", "on", "off", "2", "TRUE "} {
		t.Run("invalid-"+strings.ReplaceAll(raw, " ", "_"), func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_SERIALIZE_REQUESTS": raw})
			_, err := config.Load()
			if err == nil {
				t.Fatalf("config.Load() = nil for VLM_SERIALIZE_REQUESTS=%q, want a startup error (must not silently mean false)", raw)
			}
			if !strings.Contains(err.Error(), "VLM_SERIALIZE_REQUESTS") {
				t.Errorf("error = %q, want it to name VLM_SERIALIZE_REQUESTS", err)
			}
		})
	}
}

// AC3: Given VLM_REQUEST_DELAY_MS is negative (e.g. "-100") / When the
// server starts / Then it is clamped to 0 and a startup warning is logged
// naming VLM_REQUEST_DELAY_MS as negative.
func TestING009_AC3_NegativeDelayClampsToZeroAndWarns(t *testing.T) {
	for _, raw := range []string{"-100", "-1", "-5"} {
		t.Run(raw, func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_REQUEST_DELAY_MS": raw})

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v, want nil (clamp, not a hard error)", err)
			}
			if cfg.VLMRequestDelayMS != 0 {
				t.Errorf("VLMRequestDelayMS = %d, want 0 (clamped from %q)", cfg.VLMRequestDelayMS, raw)
			}
			warnings := cfg.Warnings()
			if len(warnings) != 1 {
				t.Fatalf("Warnings() = %v, want exactly 1 warning naming the negative delay", warnings)
			}
			w := warnings[0]
			for _, want := range []string{"VLM_REQUEST_DELAY_MS", "negative", "0"} {
				if !strings.Contains(w, want) {
					t.Errorf("warning %q does not mention %q", w, want)
				}
			}
		})
	}
}

// AC4: Given VLM_REQUEST_DELAY_MS is a non-numeric value / When the
// server starts / Then it exits with a startup error naming
// VLM_REQUEST_DELAY_MS as invalid. Spec-implied: "1.5" parses as a float
// but is not an integer, so it is rejected too.
func TestING009_AC4_NonNumericDelayErrorsNamingIt(t *testing.T) {
	for _, raw := range []string{"abc", "ten", "1.5", "10ms", " "} {
		t.Run(strings.ReplaceAll(raw, " ", "_space"), func(t *testing.T) {
			setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_REQUEST_DELAY_MS": raw})

			_, err := config.Load()
			if err == nil {
				t.Fatalf("config.Load() = nil for VLM_REQUEST_DELAY_MS=%q, want a startup error", raw)
			}
			if !strings.Contains(err.Error(), "VLM_REQUEST_DELAY_MS") {
				t.Errorf("error = %q, want it to name VLM_REQUEST_DELAY_MS", err)
			}
		})
	}
}

// AC5: Given VLM_API_KEY is any non-empty string / When the server starts
// / Then it is accepted as-is with no format validation (opaque); when
// empty or unset, no Authorization header is sent.
func TestING009_AC5_APIKeyOpaque(t *testing.T) {
	t.Run("accepted as-is with no format validation", func(t *testing.T) {
		keys := []struct {
			name string
			key  string
		}{
			{name: "plain token", key: "secret-token"},
			{name: "contains spaces", key: "with spaces"},
			{name: "arbitrary punctuation", key: "sk-!@#$%^&*()"},
			{name: "surrounding whitespace preserved", key: "  padded  "},
			{name: "non-ascii", key: "clé-🔑"},
		}
		for _, tt := range keys {
			t.Run(tt.name, func(t *testing.T) {
				setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_API_KEY": tt.key})
				cfg, err := config.Load()
				if err != nil {
					t.Fatalf("config.Load() error = %v, want nil (opaque key, no format check)", err)
				}
				if cfg.VLMAPIKey != tt.key {
					t.Errorf("VLMAPIKey = %q, want %q stored verbatim", cfg.VLMAPIKey, tt.key)
				}
			})
		}
	})

	// Header gating is a client behavior: the key reaches the wire
	// verbatim when set; an empty key sends no header at all.
	headerCases := []struct {
		name    string
		key     string
		wantHdr string
	}{
		{name: "non-empty sends bearer verbatim", key: "sk-!@#$%^&*()", wantHdr: "Bearer sk-!@#$%^&*()"},
		{name: "non-ascii sent verbatim", key: "clé-🔑", wantHdr: "Bearer clé-🔑"},
		{name: "empty sends no header", key: "", wantHdr: ""},
	}
	for _, tt := range headerCases {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
			}))
			defer srv.Close()

			client := tagging.NewClient(srv.URL, tt.key)
			if _, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x"))); err != nil {
				t.Fatalf("Tag() error = %v", err)
			}
			if gotAuth != tt.wantHdr {
				t.Errorf("Authorization = %q, want %q", gotAuth, tt.wantHdr)
			}
		})
	}

	t.Run("unset sends no header", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local"})
		if err := os.Unsetenv("VLM_API_KEY"); err != nil {
			t.Fatalf("os.Unsetenv: %v", err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.VLMAPIKey != "" {
			t.Fatalf("VLMAPIKey = %q, want empty when unset", cfg.VLMAPIKey)
		}

		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
		}))
		defer srv.Close()

		client := tagging.NewClient(srv.URL, cfg.VLMAPIKey)
		if _, err := client.Tag(context.Background(), writePhoto(t, "p.jpg", []byte("x"))); err != nil {
			t.Fatalf("Tag() error = %v", err)
		}
		if gotAuth != "" {
			t.Errorf("Authorization = %q, want no header when VLM_API_KEY is unset", gotAuth)
		}
	})
}
