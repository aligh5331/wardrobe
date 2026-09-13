// Package config loads the VLM/LLM env var contract described in
// 07-architecture.md and 06-decisions.md.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// Config is the resolved VLM/LLM endpoint configuration.
type Config struct {
	// VLMURL is the base URL of the VLM endpoint. Required.
	VLMURL string
	// VLMAPIKey is optional; an empty value means no Authorization
	// header is sent on VLM requests.
	VLMAPIKey string
	// VLMSerializeRequests forces a global one-at-a-time queue around
	// VLM calls. Defaults to false (concurrent requests).
	VLMSerializeRequests bool
	// VLMRequestDelayMS is the wait after each VLM response before the
	// next request. Only meaningful when VLMSerializeRequests is true.
	VLMRequestDelayMS int
	// VLMTemperature is the sampling temperature passed to the VLM.
	// Defaults to 0.4 when unset.
	VLMTemperature float64

	// LLMURL and LLMAPIKey are provisioned ahead of Phase 3; no Phase 1
	// consumer, and neither is validated at startup yet.
	LLMURL    string
	LLMAPIKey string
}

// Load reads the env var contract from the process environment and
// validates it. It returns an error only for misconfiguration that must
// fail startup: a missing VLM_URL, or an unparseable optional value.
// LLM_URL is deliberately not validated in Phase 1 (no consumer yet).
func Load() (*Config, error) {
	cfg := &Config{
		VLMURL:    os.Getenv("VLM_URL"),
		VLMAPIKey: os.Getenv("VLM_API_KEY"),
		LLMURL:    os.Getenv("LLM_URL"),
		LLMAPIKey: os.Getenv("LLM_API_KEY"),
	}

	if cfg.VLMURL == "" {
		return nil, errors.New("missing required environment variable: VLM_URL")
	}

	serialize, err := boolEnv("VLM_SERIALIZE_REQUESTS", false)
	if err != nil {
		return nil, err
	}
	cfg.VLMSerializeRequests = serialize

	delayMS, err := intEnv("VLM_REQUEST_DELAY_MS", 0)
	if err != nil {
		return nil, err
	}
	cfg.VLMRequestDelayMS = delayMS

	temperature, err := floatEnv("VLM_TEMPERATURE", 0.4)
	if err != nil {
		return nil, err
	}
	cfg.VLMTemperature = temperature

	return cfg, nil
}

// Warnings returns startup warnings for valid-but-misconfigured
// combinations the spec says to proceed on rather than hard-fail.
func (c *Config) Warnings() []string {
	if c.VLMRequestDelayMS > 0 && !c.VLMSerializeRequests {
		return []string{fmt.Sprintf(
			"VLM_REQUEST_DELAY_MS=%d is set but VLM_SERIALIZE_REQUESTS is false; the delay will not be applied",
			c.VLMRequestDelayMS,
		)}
	}
	return nil
}

func boolEnv(name string, fallback bool) (bool, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: must be true or false", name, raw)
	}
	return v, nil
}

func intEnv(name string, fallback int) (int, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be an integer", name, raw)
	}
	return v, nil
}

func floatEnv(name string, fallback float64) (float64, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be a number", name, raw)
	}
	return v, nil
}
