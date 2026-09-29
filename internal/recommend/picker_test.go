package recommend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

func mk(id, cat, sub, color string) store.Item {
	return store.Item{ID: id, Category: cat, Subcategory: sub, DominantColor: color,
		Pattern: "solid", WarmthTier: "medium", Formality: "casual"}
}

// fixture has real taxonomy values. t1 carries a photo path and notes that
// must never reach the prompt.
func fixture() []store.Item {
	t1 := mk("t1", "top", "t-shirt", "navy")
	t1.PhotoPath, t1.Notes = "data/photos/SECRET-PATH.jpg", "SECRET-NOTE"
	t2 := mk("t2", "top", "sweater", "gray")
	t2.SecondaryColors, t2.Pattern, t2.Formality = []string{"white", "black"}, "striped", "smart-casual"
	return []store.Item{t1, t2,
		mk("b1", "bottom", "jeans", "blue"), mk("b2", "bottom", "chinos", "tan"),
		mk("f1", "footwear", "sneakers", "white"), mk("f2", "footwear", "boots", "brown"),
		mk("o1", "outerwear", "jacket", "black"), mk("o2", "outerwear", "coat", "olive"),
		mk("h1", "headwear", "cap", "red"), mk("h2", "headwear", "beanie", "black"),
		mk("a1", "accessory", "belt", "brown")}
}

func TestBuildPrompt(t *testing.T) {
	feels, mn, mx, pop, code := 4.5, 2.0, 9.0, 70, 61
	in := Input{
		Forecast: weather.Forecast{
			Current: weather.Current{ApparentTemperatureC: &feels},
			Today: weather.Today{TemperatureMinC: &mn, TemperatureMaxC: &mx,
				PrecipitationProbabilityMax: &pop, WeatherCode: &code},
		},
		Formality: "smart-casual", Note: "dinner with friends", Candidates: fixture(),
	}
	system, user := buildPrompt(in)

	lines := 0
	want := []string{"id", "category", "subcategory", "dominant_color", "secondary_colors", "pattern", "warmth_tier", "formality"}
	for _, l := range strings.Split(user, "\n") {
		if !strings.HasPrefix(l, "id=") {
			continue
		}
		lines++
		var keys []string
		for _, f := range strings.Fields(l) {
			keys = append(keys, f[:strings.IndexByte(f, '=')])
		}
		if !reflect.DeepEqual(keys, want) {
			t.Errorf("line %q has fields %v, want %v", l, keys, want)
		}
	}
	if lines != len(in.Candidates) {
		t.Errorf("got %d candidate lines, want %d", lines, len(in.Candidates))
	}
	t2 := "id=t2 category=top subcategory=sweater dominant_color=gray secondary_colors=[white,black] pattern=striped warmth_tier=medium formality=smart-casual"
	if !strings.Contains(user, t2+"\n") {
		t.Errorf("user prompt lacks the exact t2 line")
	}

	for _, s := range []string{"SECRET-PATH", "SECRET-NOTE"} {
		if strings.Contains(system, s) || strings.Contains(user, s) {
			t.Errorf("prompt contains %s", s)
		}
	}
	for _, s := range []string{"feels-like: 4.5", "min: 2.0", "max: 9.0", "rain chance: 70%",
		"weather code (WMO): 61", "Formality: smart-casual", "Note: dinner with friends"} {
		if !strings.Contains(user, s) {
			t.Errorf("user prompt lacks %q", s)
		}
	}
	if !strings.Contains(system, `{"outfits":[{"item_ids":`) {
		t.Error("system prompt lacks the JSON contract")
	}

	modes := map[Outerwear]string{OuterwearRequired: "required", OuterwearOptional: "optional", OuterwearExcluded: "excluded"}
	for o, word := range modes {
		in.Rules = Rules{Outerwear: o}
		_, u := buildPrompt(in)
		for _, w := range modes {
			if has := strings.Contains(u, "Outerwear: "+w+"."); has != (w == word) {
				t.Errorf("outerwear %s: prompt has %q = %v", word, w, has)
			}
		}
	}
	for _, unknown := range []bool{true, false} {
		in.Rules = Rules{TempUnknown: unknown}
		if _, u := buildPrompt(in); strings.Contains(u, "temperature unknown") != unknown {
			t.Errorf("TempUnknown=%v: prompt has hint = %v", unknown, !unknown)
		}
	}
}

type tOutfit struct {
	ItemIDs []string `json:"item_ids"`
	Reason  string   `json:"reason"`
}

func of(reason string, ids ...string) tOutfit { return tOutfit{ids, reason} }

func raw(outfits ...tOutfit) string {
	b, _ := json.Marshal(map[string]any{"outfits": outfits})
	return string(b)
}

// good is valid under optional outerwear: zero outerwear, then one.
var good = raw(of("a", "t1", "b1", "f1"), of("b", "t2", "b1", "f2", "o1"), of("c", "t1", "b2", "f2", "h1", "a1"))

func TestValidate(t *testing.T) {
	a, b, c := of("a", "t1", "b1", "f1"), of("b", "t2", "b2", "f2"), of("c", "t1", "b2", "f1", "h1")
	withOuter := raw(of("a", "t1", "b1", "f1", "o1"), of("b", "t2", "b1", "f2", "o2"), of("c", "t1", "b2", "f2", "o1"))
	tests := []struct {
		name    string
		content string
		outer   Outerwear
		want    []Outfit
	}{
		{"valid, optional outerwear allows zero or one", good, OuterwearOptional, []Outfit{
			{[]string{"t1", "b1", "f1"}, "a"}, {[]string{"t2", "b1", "f2", "o1"}, "b"},
			{[]string{"t1", "b2", "f2", "h1", "a1"}, "c"}}},
		{"valid, reason trimmed", raw(of("  a  ", "t1", "b1", "f1"), b, c), OuterwearOptional, []Outfit{
			{[]string{"t1", "b1", "f1"}, "a"}, {[]string{"t2", "b2", "f2"}, "b"}, {[]string{"t1", "b2", "f1", "h1"}, "c"}}},
		{"valid, json fence", "```json\n" + raw(a, b, c) + "\n```", OuterwearOptional, []Outfit{
			{[]string{"t1", "b1", "f1"}, "a"}, {[]string{"t2", "b2", "f2"}, "b"}, {[]string{"t1", "b2", "f1", "h1"}, "c"}}},
		{"valid, outerwear required and present", withOuter, OuterwearRequired, []Outfit{
			{[]string{"t1", "b1", "f1", "o1"}, "a"}, {[]string{"t2", "b1", "f2", "o2"}, "b"}, {[]string{"t1", "b2", "f2", "o1"}, "c"}}},

		{"two outfits", raw(a, b), OuterwearOptional, nil},
		{"four outfits", raw(a, b, c, a), OuterwearOptional, nil},
		{"id not in candidates", raw(a, b, of("c", "t1", "b2", "f1", "zz")), OuterwearOptional, nil},
		{"missing top", raw(a, b, of("c", "b1", "f1")), OuterwearOptional, nil},
		{"duplicate top", raw(a, b, of("c", "t1", "t2", "b1", "f1")), OuterwearOptional, nil},
		{"missing bottom", raw(a, b, of("c", "t1", "f1")), OuterwearOptional, nil},
		{"missing footwear", raw(a, b, of("c", "t1", "b1")), OuterwearOptional, nil},
		{"two footwear", raw(a, b, of("c", "t1", "b1", "f1", "f2")), OuterwearOptional, nil},
		{"outerwear missing when required", good, OuterwearRequired, nil},
		{"outerwear present when excluded", good, OuterwearExcluded, nil},
		{"two outerwear", raw(a, b, of("c", "t1", "b1", "f1", "o1", "o2")), OuterwearOptional, nil},
		{"two headwear", raw(a, b, of("c", "t1", "b1", "f1", "h1", "h2")), OuterwearOptional, nil},
		{"duplicate id in an outfit", raw(a, b, of("c", "t1", "b1", "f1", "f1")), OuterwearOptional, nil},
		{"same id set three times", raw(a, a, a), OuterwearOptional, nil},
		{"same id set, reordered", raw(a, of("b", "f1", "b1", "t1"), of("c", "b1", "t1", "f1")), OuterwearOptional, nil},
		{"empty reason", raw(a, b, of("", "t1", "b2", "f1")), OuterwearOptional, nil},
		{"whitespace reason", raw(a, b, of(" \t\n", "t1", "b2", "f1")), OuterwearOptional, nil},
		{"unparseable JSON", "here are your outfits", OuterwearOptional, nil},
		{"empty outfits array", `{"outfits":[]}`, OuterwearOptional, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validate(tt.content, Input{Candidates: fixture(), Rules: Rules{Outerwear: tt.outer}})
			if tt.want == nil {
				if err == nil {
					t.Fatalf("got %v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// serve starts a fake LLM. The handler gets the 1-based call number.
func serve(t *testing.T, h func(n int, w http.ResponseWriter, r *http.Request)) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h(int(calls.Add(1)), w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// reply writes a chat envelope with content as the first choice.
func reply(w http.ResponseWriter, content string) {
	body, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
	})
	w.Write(body)
}

func pickIn() Input { return Input{Candidates: fixture()} }

func TestPickRequestShape(t *testing.T) {
	for _, tt := range []struct{ key, model string }{{"secret", "qwen"}, {"", ""}} {
		var method, path, auth string
		var body map[string]any
		srv, calls := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
			method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
			json.NewDecoder(r.Body).Decode(&body)
			reply(w, good)
		})
		p := Picker{URL: srv.URL, APIKey: tt.key, Model: tt.model, Temperature: 0.3, Timeout: 5 * time.Second}
		if _, err := p.Pick(context.Background(), pickIn()); err != nil {
			t.Fatalf("key=%q model=%q: %v", tt.key, tt.model, err)
		}
		if calls.Load() != 1 {
			t.Errorf("got %d calls, want 1", calls.Load())
		}
		if method != http.MethodPost || path != "/v1/chat/completions" {
			t.Errorf("got %s %s", method, path)
		}
		if body["temperature"] != 0.3 {
			t.Errorf("temperature = %v, want 0.3", body["temperature"])
		}
		if s, has := body["stream"]; !has || s != false {
			t.Errorf("stream = %v (present %v), want explicit false", s, has)
		}
		msgs, _ := body["messages"].([]any)
		if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["role"] != "user" {
			t.Errorf("messages = %v, want system then user", body["messages"])
		}
		wantAuth := ""
		if tt.key != "" {
			wantAuth = "Bearer " + tt.key
		}
		if auth != wantAuth {
			t.Errorf("Authorization = %q, want %q", auth, wantAuth)
		}
		m, has := body["model"]
		if has != (tt.model != "") || (has && m != tt.model) {
			t.Errorf("model = %v (present %v), configured %q", m, has, tt.model)
		}
	}
}

func TestPickRetry(t *testing.T) {
	tests := []struct {
		name    string
		answers []string // content per call; "ENVELOPE" is a 200 that is not a chat envelope
		calls   int32
		wantErr bool
	}{
		{"first valid", []string{good, good}, 1, false},
		{"first invalid, second valid", []string{"nope", good}, 2, false},
		{"both invalid", []string{"nope", raw()}, 2, true},
		{"bad envelope, then valid", []string{"ENVELOPE", good}, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
				if tt.answers[n-1] == "ENVELOPE" {
					w.Write([]byte("not a chat envelope"))
					return
				}
				reply(w, tt.answers[n-1])
			})
			p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
			got, err := p.Pick(context.Background(), pickIn())
			if calls.Load() != tt.calls {
				t.Errorf("got %d calls, want %d", calls.Load(), tt.calls)
			}
			if tt.wantErr {
				if !errors.Is(err, ErrLLM) {
					t.Errorf("err = %v, want ErrLLM", err)
				}
				return
			}
			if err != nil || len(got) != 3 {
				t.Fatalf("got %v, %v, want 3 outfits", got, err)
			}
			if got[1].Reason != "b" || !reflect.DeepEqual(got[1].ItemIDs, []string{"t2", "b1", "f2", "o1"}) {
				t.Errorf("second outfit = %+v, not the valid answer", got[1])
			}
		})
	}
}

func TestPickUnreachable(t *testing.T) {
	srv, _ := serve(t, func(int, http.ResponseWriter, *http.Request) {})
	srv.Close()
	p := Picker{URL: srv.URL, Timeout: 5 * time.Second}
	_, err := p.Pick(context.Background(), pickIn())
	if !errors.Is(err, ErrLLM) {
		t.Fatalf("err = %v, want ErrLLM", err)
	}
	if strings.Contains(err.Error(), "invalid output after retry") {
		t.Errorf("unreachable server took the retry path: %v", err)
	}
}

func TestPickTimeout(t *testing.T) {
	srv, calls := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // lets the server see the client hang up
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	p := Picker{URL: srv.URL, Timeout: 50 * time.Millisecond}
	if _, err := p.Pick(context.Background(), pickIn()); !errors.Is(err, ErrLLM) {
		t.Errorf("err = %v, want ErrLLM", err)
	}
	if calls.Load() != 1 {
		t.Errorf("got %d calls, want 1", calls.Load())
	}
}

func TestPickNon2xxHint(t *testing.T) {
	for _, model := range []string{"", "qwen"} {
		srv, calls := serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
			http.Error(w, "model required", http.StatusBadRequest)
		})
		p := Picker{URL: srv.URL, Model: model, Timeout: 5 * time.Second}
		_, err := p.Pick(context.Background(), pickIn())
		if !errors.Is(err, ErrLLM) {
			t.Fatalf("model=%q: err = %v, want ErrLLM", model, err)
		}
		if calls.Load() != 1 {
			t.Errorf("model=%q: got %d calls, want 1", model, calls.Load())
		}
		if has := strings.Contains(err.Error(), "LLM_MODEL"); has != (model == "") {
			t.Errorf("model=%q: LLM_MODEL hint present = %v, error: %v", model, has, err)
		}
	}
}
