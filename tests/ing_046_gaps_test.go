package tests

import (
	"strings"
	"testing"

	"wardrobe/internal/config"
)

// ING-046 gaps the Coder tests leave open: whitespace-only LLM_MODEL,
// LLM_URL trimming in the stored value, and LLM_TEMPERATURE not leaking
// into VLM_TEMPERATURE (and the reverse).
func TestING046_Gaps(t *testing.T) {
	t.Run("whitespace-only LLM_MODEL becomes empty", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_MODEL": " \t "})

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.LLMModel != "" {
			t.Errorf("LLMModel = %q, want empty", cfg.LLMModel)
		}
	})

	t.Run("LLM_URL is trimmed in the stored value", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "LLM_URL": "  http://llm.local  "})

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.LLMURL != "http://llm.local" {
			t.Errorf("LLMURL = %q, want %q", cfg.LLMURL, "http://llm.local")
		}
	})

	t.Run("LLM and VLM temperatures are independent", func(t *testing.T) {
		setContractEnv(t, map[string]string{
			"VLM_URL":         "http://vlm.local",
			"VLM_TEMPERATURE": "0.9",
			"LLM_TEMPERATURE": "0.1",
		})

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() error = %v, want nil", err)
		}
		if cfg.VLMTemperature != 0.9 || cfg.LLMTemperature != 0.1 {
			t.Errorf("VLMTemperature, LLMTemperature = %v, %v, want 0.9, 0.1", cfg.VLMTemperature, cfg.LLMTemperature)
		}
	})

	t.Run("invalid VLM_TEMPERATURE still names VLM_TEMPERATURE", func(t *testing.T) {
		setContractEnv(t, map[string]string{"VLM_URL": "http://vlm.local", "VLM_TEMPERATURE": "1.5", "LLM_TEMPERATURE": "0.5"})

		_, err := config.Load()
		if err == nil {
			t.Fatal("config.Load() = nil error for VLM_TEMPERATURE=1.5, want error")
		}
		if !strings.Contains(err.Error(), "VLM_TEMPERATURE") {
			t.Errorf("error = %q, want it to name VLM_TEMPERATURE", err)
		}
	})
}
