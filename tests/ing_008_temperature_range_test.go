// ING-008: floatEnv() must reject non-finite (NaN/Inf/-Inf) and
// out-of-range temperature values. The acceptable range is 0.0-1.0
// inclusive, default 0.4 (07-architecture.md "Env var contract";
// 06-decisions.md "VLM_TEMPERATURE acceptable range").
//
// Black-box acceptance tests: package tests, no build tag, no
// implementation code touched.
package tests

import (
	"strings"
	"testing"

	"wardrobe/internal/config"
)

// temperatureEnv is the minimal valid environment plus the temperature
// under test, isolating VLM_TEMPERATURE from the rest of the contract.
func temperatureEnv(value string) map[string]string {
	return map[string]string{
		"VLM_URL":         "http://vlm.local",
		"VLM_TEMPERATURE": value,
	}
}

// assertTemperatureRejected asserts config.Load() fails and names
// VLM_TEMPERATURE as the offending variable.
func assertTemperatureRejected(t *testing.T, value string) {
	t.Helper()

	setContractEnv(t, temperatureEnv(value))

	cfg, err := config.Load()
	if err == nil {
		t.Fatalf("config.Load() = nil error for VLM_TEMPERATURE=%q, want error naming VLM_TEMPERATURE", value)
	}
	if cfg != nil {
		t.Fatalf("config.Load() returned a config (%+v) alongside the error for VLM_TEMPERATURE=%q", cfg, value)
	}
	if !strings.Contains(err.Error(), "VLM_TEMPERATURE") {
		t.Fatalf("config.Load() error = %q for VLM_TEMPERATURE=%q, want it to name VLM_TEMPERATURE", err, value)
	}
	t.Logf("VLM_TEMPERATURE=%q -> %v", value, err)
}

// AC1: Given VLM_TEMPERATURE is set to "NaN" / When the server starts /
// Then it exits with a startup error naming VLM_TEMPERATURE as invalid.
//
// Spec-implied variants: strconv.ParseFloat accepts NaN case-insensitively
// ("nan"), so every spelling must be rejected, not just the ticket's
// literal "NaN".
func TestING008_AC1_NaNRejected(t *testing.T) {
	for _, value := range []string{"NaN", "nan", "+NaN"} {
		t.Run(value, func(t *testing.T) {
			assertTemperatureRejected(t, value)
		})
	}
}

// AC2: Given VLM_TEMPERATURE is set to "Inf" or "-Inf" / When the server
// starts / Then it exits with a startup error naming VLM_TEMPERATURE as
// invalid.
//
// Spec-implied variants: ParseFloat also accepts "Infinity"/"+Inf" and
// lowercase forms, all non-finite and therefore all invalid.
func TestING008_AC2_InfRejected(t *testing.T) {
	for _, value := range []string{"Inf", "-Inf", "+Inf", "inf", "-inf", "Infinity", "-Infinity"} {
		t.Run(value, func(t *testing.T) {
			assertTemperatureRejected(t, value)
		})
	}
}

// AC3: Given VLM_TEMPERATURE is set to a negative value (e.g. "-1") /
// When the server starts / Then it exits with a startup error naming
// VLM_TEMPERATURE as invalid.
func TestING008_AC3_NegativeRejected(t *testing.T) {
	for _, value := range []string{"-1", "-0.001", "-100"} {
		t.Run(value, func(t *testing.T) {
			assertTemperatureRejected(t, value)
		})
	}
}

// AC4: Given VLM_TEMPERATURE is set to a value exceeding 1.0 (e.g. "1.5"
// or "100") / When the server starts / Then it exits with a startup error
// naming VLM_TEMPERATURE as invalid.
func TestING008_AC4_AboveRangeRejected(t *testing.T) {
	for _, value := range []string{"1.5", "100", "1.0001"} {
		t.Run(value, func(t *testing.T) {
			assertTemperatureRejected(t, value)
		})
	}
}

// AC5: Given VLM_TEMPERATURE is set to "0" / When the server starts /
// Then startup proceeds normally — 0 is a valid value (deliberate
// deterministic runs are allowed), it is simply not the default.
func TestING008_AC5_ZeroAccepted(t *testing.T) {
	setContractEnv(t, temperatureEnv("0"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v for VLM_TEMPERATURE=\"0\", want nil", err)
	}
	if cfg.VLMTemperature != 0 {
		t.Fatalf("VLMTemperature = %v, want 0", cfg.VLMTemperature)
	}
}

// AC6: Given VLM_TEMPERATURE is set to a valid finite value within
// 0.0-1.0 inclusive (e.g. "0", "0.4", "0.7", "1.0") / When the server
// starts / Then startup proceeds normally and the value is available to
// the VLM client.
//
// The inclusive endpoints are the boundary cases: 0.0 and 1.0 must be
// accepted, not treated as "just outside".
func TestING008_AC6_ValidRangeAccepted(t *testing.T) {
	tests := []struct {
		value   string
		wantVal float64
	}{
		{value: "0", wantVal: 0},
		{value: "0.0", wantVal: 0},
		{value: "0.4", wantVal: 0.4},
		{value: "0.7", wantVal: 0.7},
		{value: "1.0", wantVal: 1.0},
		{value: "1", wantVal: 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			setContractEnv(t, temperatureEnv(tt.value))

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error = %v for VLM_TEMPERATURE=%q, want nil", err, tt.value)
			}
			if cfg.VLMTemperature != tt.wantVal {
				t.Fatalf("VLMTemperature = %v, want %v for VLM_TEMPERATURE=%q", cfg.VLMTemperature, tt.wantVal, tt.value)
			}
		})
	}
}

// Spec-implied edge case (not a literal AC): the range is inclusive, so
// the exact endpoints are the cases most likely to be broken by an
// off-by-one `<`/`>` bound. Covered above in AC6; this asserts it
// explicitly on its own so a regression names the boundary, not a
// generic value.
func TestING008_Edge_InclusiveBounds(t *testing.T) {
	t.Run("0.0 floor accepted", func(t *testing.T) {
		setContractEnv(t, temperatureEnv("0.0"))
		if _, err := config.Load(); err != nil {
			t.Fatalf("config.Load() error = %v, want nil at the inclusive floor", err)
		}
	})
	t.Run("1.0 ceiling accepted", func(t *testing.T) {
		setContractEnv(t, temperatureEnv("1.0"))
		if _, err := config.Load(); err != nil {
			t.Fatalf("config.Load() error = %v, want nil at the inclusive ceiling", err)
		}
	})
}
