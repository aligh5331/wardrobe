// Package tests holds black-box acceptance tests for backlog tickets.
// ING-003: the opt-in serialization workaround. Given
// VLM_SERIALIZE_REQUESTS=true the client runs VLM requests one at a time
// through a single queue/mutex, optionally with a post-response delay;
// false (default) leaves ING-002's concurrent behavior untouched.
//
// These tests exercise the exported API of wardrobe/internal/tagging
// only; implementation code is never touched from here.
package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wardrobe/internal/tagging"
)

// overlapServer returns a stub VLM that records the peak number of
// in-flight requests (max) and the total call count, so a serialization
// queue can be observed from the server side rather than by reading
// client internals.
func overlapServer(t *testing.T, latency time.Duration) (srv *httptest.Server, max, calls *atomic.Int32) {
	t.Helper()

	var cur, peak, total atomic.Int32
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total.Add(1)
		n := cur.Add(1)
		defer cur.Add(-1)
		for {
			m := peak.Load()
			if n <= m || peak.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(latency)
		_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
	}))
	t.Cleanup(srv.Close)
	return srv, &peak, &total
}

// runConcurrent submits n Tag calls to client and fails on the first
// error / unexpected response.
func runConcurrent(t *testing.T, client *tagging.Client, photo string, n int) {
	t.Helper()

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			got, err := client.Tag(context.Background(), photo)
			if err != nil {
				errs <- err
				return
			}
			if want := `{"category":"top"}`; got != want {
				errs <- fmt.Errorf("Tag() = %q, want raw model content %q", got, want)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Tag() error = %v", err)
	}
}

// AC1: Given VLM_SERIALIZE_REQUESTS=true / When multiple tagging
// requests are submitted concurrently / Then they are processed one at a
// time through a single global queue/mutex — no two VLM requests are
// ever in flight simultaneously.
func TestING003_AC1_SerializedOneAtATime(t *testing.T) {
	const n = 12
	srv, max, calls := overlapServer(t, 25*time.Millisecond)

	client := tagging.NewClient(srv.URL, "", tagging.WithSerialization(0))
	photo := writePhoto(t, "p.jpg", []byte("x"))

	runConcurrent(t, client, photo, n)

	if got := calls.Load(); got != n {
		t.Errorf("VLM calls = %d, want %d (every request must still be sent)", got, n)
	}
	if got := max.Load(); got != 1 {
		t.Errorf("max concurrent VLM requests = %d, want exactly 1 under serialization", got)
	}
}

// AC2: Given VLM_SERIALIZE_REQUESTS=true and VLM_REQUEST_DELAY_MS=15 /
// When one VLM request completes / Then the client waits 15ms before
// sending the next queued request.
//
// Measured at the server: consecutive request start times must be
// separated by at least the configured delay. The handler latency is
// deliberately smaller than the delay so the gap can only come from the
// post-response wait.
func TestING003_AC2_DelayBetweenQueuedRequests(t *testing.T) {
	const (
		delayMS = 15
		n       = 4
	)

	var mu sync.Mutex
	var starts []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		_, _ = w.Write([]byte(rawEnvelope(`{"category":"top"}`)))
	}))
	t.Cleanup(srv.Close)

	client := tagging.NewClient(srv.URL, "", tagging.WithSerialization(delayMS))
	photo := writePhoto(t, "p.jpg", []byte("x"))
	runConcurrent(t, client, photo, n)

	mu.Lock()
	got := append([]time.Time(nil), starts...)
	mu.Unlock()
	if len(got) != n {
		t.Fatalf("VLM calls = %d, want %d", len(got), n)
	}

	sort.Slice(got, func(i, j int) bool { return got[i].Before(got[j]) })
	want := time.Duration(delayMS) * time.Millisecond
	for i := 1; i < len(got); i++ {
		if gap := got[i].Sub(got[i-1]); gap < want {
			t.Errorf("gap between queued request %d and %d = %v, want >= %v (post-response delay)", i-1, i, gap, want)
		}
	}
}

// AC3: Given VLM_SERIALIZE_REQUESTS=false (default) / When multiple
// tagging requests are submitted / Then the queue is not engaged at all
// — behavior falls through to ING-002's concurrent default.
//
// The client is built with no serialization option, exactly as a default
// config would build it; overlap at the server proves no queue.
func TestING003_AC3_DefaultStaysConcurrent(t *testing.T) {
	const n = 10
	srv, max, calls := overlapServer(t, 30*time.Millisecond)

	client := tagging.NewClient(srv.URL, "") // no WithSerialization
	photo := writePhoto(t, "p.jpg", []byte("x"))

	runConcurrent(t, client, photo, n)

	if got := calls.Load(); got != n {
		t.Errorf("VLM calls = %d, want %d", got, n)
	}
	if got := max.Load(); got <= 1 {
		t.Errorf("max concurrent VLM requests = %d, want > 1 (queue must not be engaged by default)", got)
	}
}

// Edge: VLM_REQUEST_DELAY_MS is optional and documented as only
// meaningful alongside serialization. A zero or negative value must not
// disable serialization nor panic time.Sleep (Coder assumption #4);
// negative values are accepted by ING-001's parser.
func TestING003_Edge_NonPositiveDelayStillSerializes(t *testing.T) {
	const n = 4
	for _, delayMS := range []int{0, -5} {
		t.Run(fmt.Sprintf("delay=%d", delayMS), func(t *testing.T) {
			srv, max, calls := overlapServer(t, 10*time.Millisecond)
			client := tagging.NewClient(srv.URL, "", tagging.WithSerialization(delayMS))
			photo := writePhoto(t, "p.jpg", []byte("x"))

			runConcurrent(t, client, photo, n)

			if got := calls.Load(); got != n {
				t.Errorf("VLM calls = %d, want %d", got, n)
			}
			if got := max.Load(); got != 1 {
				t.Errorf("max concurrent VLM requests = %d, want exactly 1 (non-positive delay must not disable serialization)", got)
			}
		})
	}
}

// Edge: the wait is tied to the configured delay, not to serialization
// itself. With a large delay, a sequential pair of calls must take at
// least that long; without any delay the same pair finishes well under
// it. Complements AC2's 15ms gap check with a wide, low-flake margin.
func TestING003_Edge_DelayIsConfigurable(t *testing.T) {
	srv, _, _ := overlapServer(t, 0)
	photo := writePhoto(t, "p.jpg", []byte("x"))

	const bigDelay = 150 * time.Millisecond
	start := time.Now()
	client := tagging.NewClient(srv.URL, "", tagging.WithSerialization(int(bigDelay/time.Millisecond)))
	for range 2 {
		if _, err := client.Tag(context.Background(), photo); err != nil {
			t.Fatalf("serialized Tag() error = %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed < bigDelay {
		t.Errorf("elapsed with delay=%v = %v, want >= %v", bigDelay, elapsed, bigDelay)
	}

	start = time.Now()
	client = tagging.NewClient(srv.URL, "")
	for range 2 {
		if _, err := client.Tag(context.Background(), photo); err != nil {
			t.Fatalf("default Tag() error = %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed >= bigDelay {
		t.Errorf("elapsed with no serialization delay = %v, want < %v", elapsed, bigDelay)
	}
}
