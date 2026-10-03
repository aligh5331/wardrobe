// Package config loads the VLM/LLM env var contract described in
// 07-architecture.md and 06-decisions.md.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
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
	// delayWasNegative records that VLM_REQUEST_DELAY_MS was set to a
	// negative value and clamped to 0, so Warnings can surface it.
	delayWasNegative bool
	// VLMTemperature is the sampling temperature passed to the VLM.
	// Defaults to 0.4 when unset.
	VLMTemperature float64

	// LLMURL and LLMAPIKey are provisioned ahead of Phase 3. LLMURL is
	// required by cmd/server only (see RequireLLMURL); cmd/ingest never
	// calls the LLM and does not need it validated.
	LLMURL    string
	LLMAPIKey string
	// LLMModel is sent as the request "model" field when set; empty
	// means the field is omitted. Never validated — any trimmed value
	// is accepted, since server-specific model names aren't enumerable.
	LLMModel string
	// LLMTemperature is the sampling temperature passed to the LLM.
	// Defaults to 0.4 when unset; same validation as VLMTemperature.
	LLMTemperature float64

	// LogLevel is one of debug, info, warn, error, stored lower-case.
	// Defaults to info when unset.
	LogLevel string
	// LogFormat is one of text, json, stored lower-case. Defaults to
	// text when unset.
	LogFormat string
}

// Load reads the env var contract from the process environment and
// validates it. It returns an error only for misconfiguration that must
// fail startup: a missing VLM_URL, out-of-range VLM_TEMPERATURE or an unparseable optional value.
// LLM_URL is deliberately not validated in Phase 1 (no consumer yet).
func Load() (*Config, error) {
	cfg := &Config{
		VLMURL:    strings.TrimSpace(os.Getenv("VLM_URL")),
		VLMAPIKey: os.Getenv("VLM_API_KEY"),
		LLMURL:    strings.TrimSpace(os.Getenv("LLM_URL")),
		LLMAPIKey: os.Getenv("LLM_API_KEY"),
		LLMModel:  strings.TrimSpace(os.Getenv("LLM_MODEL")),
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
	if delayMS < 0 {
		cfg.delayWasNegative = true
		delayMS = 0
	}
	cfg.VLMRequestDelayMS = delayMS

	minTemp, maxTemp := 0.0, 1.0
	temperature, err := floatEnv("VLM_TEMPERATURE", 0.4, &minTemp, &maxTemp)
	if err != nil {
		return nil, err
	}
	cfg.VLMTemperature = temperature

	llmTemperature, err := floatEnv("LLM_TEMPERATURE", 0.4, &minTemp, &maxTemp)
	if err != nil {
		return nil, err
	}
	cfg.LLMTemperature = llmTemperature

	logLevel, err := enumEnv("LOG_LEVEL", "info", "debug", "info", "warn", "error")
	if err != nil {
		return nil, err
	}
	cfg.LogLevel = logLevel

	logFormat, err := enumEnv("LOG_FORMAT", "text", "text", "json")
	if err != nil {
		return nil, err
	}
	cfg.LogFormat = logFormat

	return cfg, nil
}

// RequireLLMURL enforces the server-only LLM_URL requirement
// (06-decisions.md "LLM config"). cmd/ingest never calls this — it never
// calls the LLM and starts fine with LLM_URL empty.
func (c *Config) RequireLLMURL() error {
	if c.LLMURL == "" {
		return errors.New("missing required environment variable: LLM_URL")
	}
	return nil
}

// Warnings returns startup warnings for valid-but-misconfigured
// combinations the spec says to proceed on rather than hard-fail.
func (c *Config) Warnings() []string {
	warnings := []string{}
	if c.delayWasNegative {
		warnings = append(warnings, "VLM_REQUEST_DELAY_MS is negative; treated as 0")
	}
	if c.VLMRequestDelayMS > 0 && !c.VLMSerializeRequests {
		warnings = append(warnings, fmt.Sprintf(
			"VLM_REQUEST_DELAY_MS=%d is set but VLM_SERIALIZE_REQUESTS is false; the delay will not be applied",
			c.VLMRequestDelayMS,
		))
	}
	return warnings
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

// enumEnv reads an optional case-insensitive enum env var and returns the
// lower-case value. Surrounding whitespace is trimmed; unset or blank
// gives the fallback. Any value outside allowed is an error.
func enumEnv(name, fallback string, allowed ...string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback, nil
	}
	for _, a := range allowed {
		if raw == a {
			return raw, nil
		}
	}
	return "", fmt.Errorf("invalid %s %q: must be one of %s", name, os.Getenv(name), strings.Join(allowed, ", "))
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

// floatEnv reads an optional float env var. Non-finite values (NaN, Inf,
// -Inf) are always rejected — a non-finite float is never a usable
// setting. If min and/or max are non-nil the parsed value must fall
// within those bounds; pass nil for a variable with no natural range, so
// one variable's range never silently constrains another's.
func floatEnv(name string, fallback float64, min, max *float64) (float64, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be a number", name, raw)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("invalid %s %q: must be a finite number", name, raw)
	}
	lo, hi := math.Inf(-1), math.Inf(1)
	if min != nil {
		lo = *min
	}
	if max != nil {
		hi = *max
	}
	if v < lo || v > hi {
		return 0, fmt.Errorf("invalid %s %q: must be between %v and %v", name, raw, lo, hi)
	}
	return v, nil
}
