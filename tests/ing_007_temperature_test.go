// Package tests holds black-box acceptance tests for backlog tickets.
// ING-007: every tagging request carries "temperature": VLM_TEMPERATURE,
// and the value is read afresh per request, so ING-005's two attempts
// for the same photo are independent samples rather than a deterministic
// repeat.
//
// These tests exercise the exported API of wardrobe/internal/tagging
// (and wardrobe/internal/config for the config seam) only;
// implementation code is never touched from here.
package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"wardrobe/internal/config"
	"wardrobe/internal/tagging"
)

// temperatureServer is a stub VLM that records the decoded
// "temperature" value of every request. The returned func reports the
// recorded values in request order. A missing or non-numeric
// "temperature" is reported as a test failure at the point it is seen,
// so a request omitting the field cannot silently pass.
func temperatureServer(t *testing.T) (*httptest.Server, func() []float64) {
	t.Helper()

	var (
		mu  sync.Mutex
		got []float64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeRequest(t, r)
		raw, present := body["temperature"]
		if !present {
			t.Errorf("request payload has no \"temperature\" key; the spec requires it on every request")
			_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
			return
		}
		f, ok := raw.(float64)
		if !ok {
			t.Errorf("temperature = %v (%T), want a JSON number", raw, raw)
			_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
			return
		}
		mu.Lock()
		got = append(got, f)
		mu.Unlock()
		_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
	}))
	t.Cleanup(srv.Close)

	return srv, func() []float64 {
		mu.Lock()
		defer mu.Unlock()
		return append([]float64(nil), got...)
	}
}

// AC1: Given a tagging request is sent / When the request payload is
// built / Then it includes "temperature": VLM_TEMPERATURE.
//
// Covers both the client default (0.4, the VLM_TEMPERATURE config
// default per 07-architecture.md) and an explicit 0.0 — 0.0 is a valid
// configured value and the field must still be present, i.e. not
// dropped by omitempty.
func TestING007_AC1_TemperatureSentOnEveryRequest(t *testing.T) {
	tests := []struct {
		name string
		opts []tagging.Option
		want float64
	}{
		{name: "client default is VLM_TEMPERATURE default 0.4", want: 0.4},
		{
			name: "explicit zero is still sent",
			opts: []tagging.Option{tagging.WithTemperature(func() float64 { return 0 })},
			want: 0,
		},
		{
			name: "explicit 0.7 is sent",
			opts: []tagging.Option{tagging.WithTemperature(func() float64 { return 0.7 })},
			want: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, temps := temperatureServer(t)
			photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING007"))

			if _, err := tagging.NewClient(srv.URL, "", tt.opts...).Tag(context.Background(), photo); err != nil {
				t.Fatalf("Tag() error = %v, want nil", err)
			}

			got := temps()
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("temperatures sent = %v, want [%v]", got, tt.want)
			}
		})
	}
}

// AC2: Given the client is called twice in a row for the same photo /
// When each request is built / Then the temperature value is read from
// the source on each call, not cached or reused from the first request.
//
// The source's return value is changed between the two calls: if the
// client snapshotted the value at construction, the second request would
// still carry 0.2.
func TestING007_AC2_TemperatureReadFreshPerCall(t *testing.T) {
	srv, temps := temperatureServer(t)
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING007"))

	temp := 0.2
	client := tagging.NewClient(srv.URL, "", tagging.WithTemperature(func() float64 { return temp }))

	if _, err := client.Tag(context.Background(), photo); err != nil {
		t.Fatalf("first Tag() error = %v, want nil", err)
	}
	temp = 0.9
	if _, err := client.Tag(context.Background(), photo); err != nil {
		t.Fatalf("second Tag() error = %v, want nil", err)
	}

	got := temps()
	if len(got) != 2 {
		t.Fatalf("requests recorded = %d, want 2", len(got))
	}
	if got[0] != 0.2 || got[1] != 0.9 {
		t.Errorf("temperatures sent = %v, want [0.2 0.9] (read fresh each call, not cached)", got)
	}
}

// AC2 (config clause): the value is read from Config on each call, not
// snapshotted. Uses the wiring documented in the ticket's Implementation
// notes — func() float64 { return cfg.VLMTemperature } — and mutates the
// Config between calls to prove the payload tracks the current value.
func TestING007_AC2_ConfigBackedValueReadFresh(t *testing.T) {
	srv, temps := temperatureServer(t)
	photo := writePhoto(t, "photo.jpg", []byte("JPEG-BYTES-ING007"))

	setContractEnv(t, map[string]string{
		"VLM_URL":         srv.URL,
		"VLM_TEMPERATURE": "0.7",
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v, want nil", err)
	}
	if cfg.VLMTemperature != 0.7 {
		t.Fatalf("cfg.VLMTemperature = %v, want 0.7 from VLM_TEMPERATURE", cfg.VLMTemperature)
	}

	client := tagging.NewClient(cfg.VLMURL, cfg.VLMAPIKey,
		tagging.WithTemperature(func() float64 { return cfg.VLMTemperature }))

	if _, err := client.Tag(context.Background(), photo); err != nil {
		t.Fatalf("first Tag() error = %v, want nil", err)
	}
	cfg.VLMTemperature = 0.3
	if _, err := client.Tag(context.Background(), photo); err != nil {
		t.Fatalf("second Tag() error = %v, want nil", err)
	}

	got := temps()
	if len(got) != 2 {
		t.Fatalf("requests recorded = %d, want 2", len(got))
	}
	if got[0] != 0.7 || got[1] != 0.3 {
		t.Errorf("temperatures sent = %v, want [0.7 0.3] (read from Config each call, not cached)", got)
	}
}
