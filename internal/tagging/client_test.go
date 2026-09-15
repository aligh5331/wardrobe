package tagging

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testImage = "not really an image"

const cannedResponse = `{"choices":[{"message":{"role":"assistant","content":"{\"category\":\"top\"}"}}]}`

func writeTestImage(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/photo.jpg"
	if err := os.WriteFile(path, []byte(testImage), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTag(t *testing.T) {
	var (
		gotMethod, gotPath, gotAuth, gotContentType string
		gotBody                                     map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBody = map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("request body not valid JSON: %v", err)
		}
		w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	msg, err := c.Tag(context.Background(), writeTestImage(t))
	if err != nil {
		t.Fatalf("Tag() error = %v", err)
	}

	// The client must send to the OpenAI-compatible chat endpoint.
	if gotMethod != http.MethodPost || gotPath != "/v1/chat/completions" {
		t.Errorf("request = %s %s, want POST /v1/chat/completions", gotMethod, gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	// No API key configured: no Authorization header.
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none when apiKey empty", gotAuth)
	}

	// The spec's tagging prompt must be sent verbatim as the system message.
	messages, ok := gotBody["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %v, want system + user", gotBody["messages"])
	}
	system, ok := messages[0].(map[string]any)
	if !ok || system["role"] != "system" || system["content"] != taggingPrompt {
		t.Errorf("system message = %v, want the spec taggingPrompt verbatim", messages[0])
	}

	// The user message must carry the photo as a base64 data URI.
	user, ok := messages[1].(map[string]any)
	if !ok || user["role"] != "user" {
		t.Fatalf("user message = %v, want role user", messages[1])
	}
	parts, ok := user["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("user content parts = %v, want image_url + text", user["content"])
	}
	wantURI := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte(testImage))
	part0, _ := parts[0].(map[string]any)
	imgURL, _ := part0["image_url"].(map[string]any)
	if part0["type"] != "image_url" || imgURL["url"] != wantURI {
		t.Errorf("image part = %v, want image_url with data URI %q", parts[0], wantURI)
	}

	// The raw model text is returned untouched.
	if msg != `{"category":"top"}` {
		t.Errorf("Tag() = %q, want the canned raw content", msg)
	}
}

func TestTag_SendsTemperature(t *testing.T) {
	// ING-007: every request carries "temperature"; WithTemperature's
	// source is read afresh per call so ING-005's retry is an independent
	// second sample rather than a cached repeat.
	var got []float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("request body not valid JSON: %v", err)
		}
		got = append(got, body["temperature"].(float64))
		w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	// No option: the config default is sent.
	if _, err := NewClient(srv.URL, "").Tag(context.Background(), writeTestImage(t)); err != nil {
		t.Fatalf("Tag() error = %v", err)
	}
	if len(got) != 1 || got[0] != defaultTemperature {
		t.Fatalf("temperatures = %v, want [%v] (config default)", got, defaultTemperature)
	}

	// With a source: changing it between calls changes the payload.
	temp := 0.2
	c := NewClient(srv.URL, "", WithTemperature(func() float64 { return temp }))
	if _, err := c.Tag(context.Background(), writeTestImage(t)); err != nil {
		t.Fatalf("Tag() error = %v", err)
	}
	temp = 0.9
	if _, err := c.Tag(context.Background(), writeTestImage(t)); err != nil {
		t.Fatalf("Tag() error = %v", err)
	}
	if len(got) != 3 || got[1] != 0.2 || got[2] != 0.9 {
		t.Errorf("temperatures = %v, want [0.2 0.9] read fresh each call", got[1:])
	}
}

func TestTag_SendsAuthHeaderWhenKeySet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret-token")
	if _, err := c.Tag(context.Background(), writeTestImage(t)); err != nil {
		t.Fatalf("Tag() error = %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", gotAuth)
	}
}

func TestTag_EmptyContentReturnedAsIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	msg, err := c.Tag(context.Background(), writeTestImage(t))
	if err != nil {
		t.Fatalf("Tag() error = %v", err)
	}
	if msg != "" {
		t.Errorf("Tag() = %q, want empty raw content", msg)
	}
}

func TestTag_Unreachable(t *testing.T) {
	// A server that is closed again: connections are refused, which is a
	// distinct failure mode from a reachable model returning bad output.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.Tag(context.Background(), writeTestImage(t))
	if err == nil {
		t.Fatal("Tag() error = nil, want ErrVLMUnreachable")
	}
	if !errors.Is(err, ErrVLMUnreachable) {
		t.Errorf("errors.Is(err, ErrVLMUnreachable) = false, err = %v", err)
	}
}

func TestTag_Non2xxIsNotUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal model error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.Tag(context.Background(), writeTestImage(t))
	if err == nil {
		t.Fatal("Tag() error = nil, want status error")
	}
	if errors.Is(err, ErrVLMUnreachable) {
		t.Errorf("non-2xx response must not be ErrVLMUnreachable, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to name the status", err)
	}
}

func TestTag_MalformedEnvelopeIsNotUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.Tag(context.Background(), writeTestImage(t))
	if err == nil {
		t.Fatal("Tag() error = nil, want envelope error")
	}
	if errors.Is(err, ErrVLMUnreachable) {
		t.Errorf("malformed model output must not be ErrVLMUnreachable, got %v", err)
	}
}

// ING-011: a malformed VLM_URL (not a valid URL) must surface as
// ErrVLMUnreachable when Tag is called, not as a generic request-build
// error. This matches the behavior for connection-refused/timeout/DNS
// failures so callers have one distinct error path to handle.
func TestTag_MalformedURLIsUnreachable(t *testing.T) {
	malformedURLs := []string{
		"not a url",
		"vlm.local",
		"http://",
		"ftp://example.com", // wrong scheme for our endpoint but still a URL parse error in context
	}
	for _, u := range malformedURLs {
		t.Run(u, func(t *testing.T) {
			c := NewClient(u, "")
			_, err := c.Tag(context.Background(), writeTestImage(t))
			if err == nil {
				t.Fatalf("Tag() error = nil for %q, want ErrVLMUnreachable", u)
			}
			if !errors.Is(err, ErrVLMUnreachable) {
				t.Errorf("errors.Is(err, ErrVLMUnreachable) = false for %q, err = %v", u, err)
			}
		})
	}
}

func TestTag_Concurrent(t *testing.T) {
	// An overlapping-request detector: if the client had a serialization
	// queue, the handler would never see more than one request in flight.
	var cur, max atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		defer cur.Add(-1)
		for {
			m := max.Load()
			if n <= m || max.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	const n = 10
	c := NewClient(srv.URL, "")
	img := writeTestImage(t)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			if _, err := c.Tag(context.Background(), img); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Tag() error = %v", err)
	}
	if got := max.Load(); got <= 1 {
		t.Errorf("max concurrent requests = %d, want > 1 (requests must not be serialized)", got)
	}
}

func TestTag_Serialized(t *testing.T) {
	// With serialization on, the overlap detector must never see more
	// than one request in flight.
	var cur, max atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		defer cur.Add(-1)
		for {
			m := max.Load()
			if n <= m || max.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	const n = 8
	c := NewClient(srv.URL, "", WithSerialization(0))
	img := writeTestImage(t)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			if _, err := c.Tag(context.Background(), img); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("serialized Tag() error = %v", err)
	}
	if got := max.Load(); got != 1 {
		t.Errorf("max concurrent requests = %d, want exactly 1 under serialization", got)
	}
}

func TestTag_SerializedDelayBetweenRequests(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	const delay = 40 * time.Millisecond
	const n = 3
	c := NewClient(srv.URL, "", WithSerialization(int(delay/time.Millisecond)))
	img := writeTestImage(t)
	for range n {
		if _, err := c.Tag(context.Background(), img); err != nil {
			t.Fatalf("Tag() error = %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(starts) != n {
		t.Fatalf("requests = %d, want %d", len(starts), n)
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < delay {
			t.Errorf("gap between request %d and %d = %v, want >= %v", i-1, i, gap, delay)
		}
	}
}
