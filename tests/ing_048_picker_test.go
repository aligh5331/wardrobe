package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

// g48Items has real taxonomy values. Every item carries photo path, notes and
// added date markers that must never reach the LLM.
func g48Items() []store.Item {
	rows := [][3]string{
		{"t1", "top", "t-shirt"}, {"t2", "top", "sweater"},
		{"b1", "bottom", "jeans"}, {"b2", "bottom", "chinos"},
		{"f1", "footwear", "sneakers"}, {"f2", "footwear", "boots"},
		{"o1", "outerwear", "jacket"}, {"o2", "outerwear", "coat"},
		{"h1", "headwear", "cap"}, {"h2", "headwear", "beanie"},
		{"a1", "accessory", "belt"},
	}
	var out []store.Item
	for _, r := range rows {
		out = append(out, store.Item{ID: r[0], Category: r[1], Subcategory: r[2],
			DominantColor: "navy", SecondaryColors: []string{"white"}, Pattern: "solid",
			WarmthTier: "medium", Formality: "casual",
			PhotoPath: "PHOTOPATH-" + r[0] + ".jpg", Notes: "NOTESTEXT-" + r[0],
			AddedDate: time.Date(2031, 7, 7, 0, 0, 0, 0, time.UTC)})
	}
	return out
}

type g48Outfit struct {
	ItemIDs []string `json:"item_ids"`
	Reason  string   `json:"reason"`
}

func g48O(reason string, ids ...string) g48Outfit { return g48Outfit{ids, reason} }

func g48Raw(o ...g48Outfit) string {
	b, _ := json.Marshal(map[string]any{"outfits": o})
	return string(b)
}

func g48Reply(w http.ResponseWriter, content string) {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
	w.Write(b)
}

// g48Serve starts a fake LLM. The handler gets the 1-based call number.
func g48Serve(t *testing.T, h func(n int, w http.ResponseWriter, r *http.Request)) (string, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h(int(calls.Add(1)), w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

func g48In(outer recommend.Outerwear) recommend.Input {
	return recommend.Input{Candidates: g48Items(), Rules: recommend.Rules{Outerwear: outer}}
}

// A valid answer under optional outerwear. Ids are deliberately not sorted.
var g48Good = g48Raw(
	g48O("first", "f2", "t2", "b1", "o1"),
	g48O("second", "t1", "b2", "f1"),
	g48O("third", "b1", "t2", "f1", "h2", "a1"))

func TestING048_PromptEndToEnd(t *testing.T) {
	var raw string
	url, _ := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		g48Reply(w, g48Raw(g48O("a", "t1", "b1", "f1", "o1"), g48O("b", "t2", "b1", "f2", "o1"), g48O("c", "t1", "b2", "f2", "o2")))
	})
	feels, mn, mx, pop, code := 3.5, 1.0, 8.0, 80, 63
	in := g48In(recommend.OuterwearRequired)
	in.Rules.TempUnknown = true
	in.Forecast = weather.Forecast{
		Current: weather.Current{ApparentTemperatureC: &feels},
		Today: weather.Today{TemperatureMinC: &mn, TemperatureMaxC: &mx,
			PrecipitationProbabilityMax: &pop, WeatherCode: &code}}
	in.Formality, in.Note = "smart-casual", "dinner with friends"
	p := recommend.Picker{URL: url, Timeout: 5 * time.Second}
	if _, err := p.Pick(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"PHOTOPATH", "NOTESTEXT", ".jpg", "2031-07-07"} {
		if strings.Contains(raw, s) {
			t.Errorf("request body contains %q", s)
		}
	}
	var body struct{ Messages []struct{ Content string } }
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	prompt := body.Messages[0].Content + "\n" + body.Messages[1].Content
	for _, s := range []string{"3.5", "1.0", "8.0", "80", "63", "smart-casual", "dinner with friends",
		"required", "temperature unknown", `{"outfits":[{"item_ids":`, `"reason"`, "id=t1 ", "id=a1 "} {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
}

func TestING048_TrailingSlash(t *testing.T) {
	var path string
	url, _ := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		g48Reply(w, g48Good)
	})
	p := recommend.Picker{URL: url + "/", Timeout: 5 * time.Second}
	if _, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional)); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path = %q", path)
	}
}

func TestING048_ValidOrder(t *testing.T) {
	url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) { g48Reply(w, g48Good) })
	p := recommend.Picker{URL: url, Timeout: 5 * time.Second}
	got, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
	if err != nil || calls.Load() != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls.Load())
	}
	want := []recommend.Outfit{
		{ItemIDs: []string{"f2", "t2", "b1", "o1"}, Reason: "first"},
		{ItemIDs: []string{"t1", "b2", "f1"}, Reason: "second"},
		{ItemIDs: []string{"b1", "t2", "f1", "h2", "a1"}, Reason: "third"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Extra JSON fields are not in the contract. The spec does not forbid them.
func TestING048_ExtraFieldsAccepted(t *testing.T) {
	content := `{"note":"hi","outfits":[` +
		`{"item_ids":["t1","b1","f1"],"reason":"a","score":9},` +
		`{"item_ids":["t2","b1","f1"],"reason":"b"},` +
		`{"item_ids":["t1","b2","f2"],"reason":"c"}]}`
	url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) { g48Reply(w, content) })
	p := recommend.Picker{URL: url, Timeout: 5 * time.Second}
	got, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
	if err != nil || len(got) != 3 || calls.Load() != 1 {
		t.Errorf("got %v, %v, calls %d", got, err, calls.Load())
	}
}

func TestING048_BadAnswerTwice(t *testing.T) {
	a, b, c := g48O("a", "t1", "b1", "f1"), g48O("b", "t2", "b2", "f2"), g48O("c", "t1", "b2", "f1")
	withOuter := g48Raw(g48O("a", "t1", "b1", "f1", "o1"), g48O("b", "t2", "b1", "f2", "o1"), g48O("c", "t1", "b2", "f2", "o2"))
	opt, req, exc := recommend.OuterwearOptional, recommend.OuterwearRequired, recommend.OuterwearExcluded
	tests := []struct {
		name    string
		content string
		outer   recommend.Outerwear
	}{
		{"id outside the candidates", g48Raw(a, b, g48O("c", "t1", "b2", "nope")), opt},
		{"missing footwear", g48Raw(a, b, g48O("c", "t1", "b2")), opt},
		{"missing top", g48Raw(a, b, g48O("c", "b2", "f1")), opt},
		{"missing bottom", g48Raw(a, b, g48O("c", "t1", "f1")), opt},
		{"two footwear", g48Raw(a, b, g48O("c", "t1", "b2", "f1", "f2")), opt},
		{"outerwear present when excluded", withOuter, exc},
		{"outerwear absent when required", g48Raw(a, b, c), req},
		{"one outfit lacks required outerwear", g48Raw(g48O("a", "t1", "b1", "f1", "o1"), g48O("b", "t2", "b1", "f2", "o1"), c), req},
		{"two outerwear", g48Raw(a, b, g48O("c", "t1", "b2", "f1", "o1", "o2")), opt},
		{"two headwear", g48Raw(a, b, g48O("c", "t1", "b2", "f1", "h1", "h2")), opt},
		{"duplicate id in outfit", g48Raw(a, b, g48O("c", "t1", "b2", "f1", "f1")), opt},
		{"all three the same set", g48Raw(a, a, a), opt},
		{"same set in other orders", g48Raw(a, g48O("b", "f1", "b1", "t1"), g48O("c", "b1", "t1", "f1")), opt},
		{"empty reason", g48Raw(a, b, g48O("", "t1", "b2", "f1")), opt},
		{"whitespace reason", g48Raw(a, b, g48O("  \n", "t1", "b2", "f1")), opt},
		{"two outfits", g48Raw(a, b), opt},
		{"four outfits", g48Raw(a, b, c, g48O("d", "t2", "b1", "f2")), opt},
		{"no outfits", g48Raw(), opt},
		{"not JSON", "Here are three outfits for you.", opt},
		{"empty content", "", opt},
		{"JSON array", "[]", opt},
		{"outfits is a string", `{"outfits":"x"}`, opt},
		{"prose before JSON", "Sure! " + g48Good, opt},
		{"null item_ids", `{"outfits":[{"item_ids":null,"reason":"a"},{"item_ids":null,"reason":"b"},{"item_ids":null,"reason":"c"}]}`, opt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) { g48Reply(w, tt.content) })
			p := recommend.Picker{URL: url, Timeout: 5 * time.Second}
			got, err := p.Pick(context.Background(), g48In(tt.outer))
			if !errors.Is(err, recommend.ErrLLM) {
				t.Fatalf("got %v, %v, want ErrLLM", got, err)
			}
			if calls.Load() != 2 {
				t.Errorf("calls = %d, want 2", calls.Load())
			}
		})
	}
}

func TestING048_BadThenGood(t *testing.T) {
	url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			g48Reply(w, "nope")
			return
		}
		g48Reply(w, g48Good)
	})
	p := recommend.Picker{URL: url, Timeout: 5 * time.Second}
	got, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
	if err != nil || calls.Load() != 2 || len(got) != 3 || got[0].Reason != "first" {
		t.Errorf("got %v, %v, calls %d", got, err, calls.Load())
	}
}

// A non-2xx on the retry is a hard failure: 2 calls in total, ErrLLM.
func TestING048_BadThenNon2xx(t *testing.T) {
	url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			g48Reply(w, "nope")
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	p := recommend.Picker{URL: url, Model: "m", Timeout: 5 * time.Second}
	_, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
	if !errors.Is(err, recommend.ErrLLM) || calls.Load() != 2 {
		t.Errorf("err = %v, calls %d, want ErrLLM and 2", err, calls.Load())
	}
}

// Counts TCP connections, and hangs up on each one. A retry would make 2.
func TestING048_UnreachableOneAttempt(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var conns atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			c.Close()
		}
	}()
	p := recommend.Picker{URL: "http://" + ln.Addr().String(), Timeout: 5 * time.Second}
	_, err = p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
	if !errors.Is(err, recommend.ErrLLM) {
		t.Fatalf("err = %v, want ErrLLM", err)
	}
	if strings.Contains(err.Error(), "invalid output") {
		t.Errorf("hang-up took the retry path: %v", err)
	}
	if conns.Load() != 1 {
		t.Errorf("connections = %d, want 1", conns.Load())
	}
}

func TestING048_ClosedServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	p := recommend.Picker{URL: url, Timeout: 5 * time.Second}
	_, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
	if !errors.Is(err, recommend.ErrLLM) || strings.Contains(err.Error(), "invalid output") {
		t.Errorf("err = %v, want a plain ErrLLM", err)
	}
}

func TestING048_Timeout(t *testing.T) {
	url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	p := recommend.Picker{URL: url, Timeout: 100 * time.Millisecond}
	start := time.Now()
	_, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
	if !errors.Is(err, recommend.ErrLLM) {
		t.Errorf("err = %v, want ErrLLM", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v, the timeout is 100ms", d)
	}
}

func TestING048_Non2xxHint(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		for _, model := range []string{"", "qwen"} {
			url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
				http.Error(w, "rejected", status)
			})
			p := recommend.Picker{URL: url, Model: model, Timeout: 5 * time.Second}
			_, err := p.Pick(context.Background(), g48In(recommend.OuterwearOptional))
			if !errors.Is(err, recommend.ErrLLM) || calls.Load() != 1 {
				t.Fatalf("status %d model %q: err = %v, calls %d", status, model, err, calls.Load())
			}
			if has := strings.Contains(err.Error(), "LLM_MODEL"); has != (model == "") {
				t.Errorf("status %d model %q: hint = %v, error: %v", status, model, has, err)
			}
		}
	}
}
