// ING-049 Tester gap tests for POST /api/recommendations. They reuse the
// e49* helpers from the Coder's file and the g48* fixtures from ING-048.
package tests

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"wardrobe/internal/api"
	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

// Feels-like 28 C: hot band, outerwear excluded, only light tiers.
const t49Hot = `{"current":{"temperature_2m":28.0,"apparent_temperature":28.0,"weather_code":0,"precipitation":0.0},
 "daily":{"temperature_2m_min":[24.0],"temperature_2m_max":[31.0],"precipitation_probability_max":[0],"weather_code":[0]}}`

// Feels-like, min and max are all null: no temperature, no warmth filter.
const t49Unknown = `{"current":{"temperature_2m":null,"apparent_temperature":null,"weather_code":3,"precipitation":0.0},
 "daily":{"temperature_2m_min":[null],"temperature_2m_max":[null],"precipitation_probability_max":[10],"weather_code":[3]}}`

// t49Items is the g48 catalog after mut ran on every item.
func t49Items(mut func(*store.Item)) []store.Item {
	items := g48Items()
	for i := range items {
		mut(&items[i])
	}
	return items
}

func t49Keys(m map[string]json.RawMessage) []string { return slices.Sorted(maps.Keys(m)) }

// The response is checked as raw JSON: exact key sets and no null arrays.
func TestT49_RawJSONShape(t *testing.T) {
	url, _, _ := e49Fake(t, e49Answer)
	items := t49Items(func(it *store.Item) {
		if it.ID == "t1" {
			it.SecondaryColors = nil
		}
	})
	e, _ := e49Engine(t, items, ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(ing043Do(t, e, "GET", "/api/items", "").Body.Bytes(), &list); err != nil || len(list) == 0 {
		t.Fatalf("GET /api/items: %v", err)
	}
	itemKeys := t49Keys(list[0])

	rr := e49Post(e, strings.NewReader("{}"))
	if rr.Code != 200 {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), `"secondary_colors":null`) {
		t.Error("an item has secondary_colors null, want []")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil || !slices.Equal(t49Keys(top), []string{"outfits", "weather"}) {
		t.Fatalf("top-level keys = %v, err %v", t49Keys(top), err)
	}
	var w map[string]json.RawMessage
	if err := json.Unmarshal(top["weather"], &w); err != nil || !slices.Equal(t49Keys(w), []string{"current", "location", "today"}) {
		t.Errorf("weather keys = %v, err %v", t49Keys(w), err)
	}
	var outfits []map[string]json.RawMessage
	if err := json.Unmarshal(top["outfits"], &outfits); err != nil || len(outfits) != 3 {
		t.Fatalf("outfits = %s, err %v", top["outfits"], err)
	}
	for i, o := range outfits {
		if !slices.Equal(t49Keys(o), []string{"items", "reason"}) {
			t.Errorf("outfit %d keys = %v", i, t49Keys(o))
		}
		var its []map[string]json.RawMessage
		if err := json.Unmarshal(o["items"], &its); err != nil || len(its) == 0 {
			t.Errorf("outfit %d items = %s, err %v", i, o["items"], err)
		}
		for _, it := range its {
			if got := t49Keys(it); !slices.Equal(got, itemKeys) {
				t.Errorf("outfit %d item keys = %v, want %v", i, got, itemKeys)
			}
		}
	}
}

// A failure of st.List after the weather step is 500 and never reaches the LLM.
// A second SQLite connection to the same file drops the items table.
func TestT49_ListFailure500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := newStorePath(t)
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, it := range g48Items() {
		if err := st.Insert(it); err != nil {
			t.Fatal(err)
		}
	}
	url, calls, _ := e49Fake(t, e49Answer)
	e := api.New(st, t.TempDir(), api.WithWeather(weather.New(ing043Fake(t, 200, e49Mild).url, "", time.Second)),
		api.WithRecommender(&recommend.Picker{URL: url, Timeout: 5 * time.Second}))
	raw, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Exec("DROP TABLE items").Error; err != nil {
		t.Fatalf("drop: %v", err)
	}
	if sqlDB, err := raw.DB(); err == nil {
		sqlDB.Close()
	}
	if rr := ing043Do(t, e, "GET", "/api/weather", ""); rr.Code != 200 {
		t.Fatalf("GET /api/weather = %d, the weather step must still work", rr.Code)
	}
	e49Err(t, e49Post(e, strings.NewReader("{}")), 500, "list")
	e49Calls(t, calls, 0)
}

// With no temperature the route answers 200, filters nothing by warmth and says so in the prompt.
func TestT49_TemperatureUnknown(t *testing.T) {
	url, calls, body := e49Fake(t, e49Answer)
	items := t49Items(func(it *store.Item) {
		if it.ID == "t1" {
			it.WarmthTier = "heavy"
		}
	})
	e, _ := e49Engine(t, items, ing043Fake(t, 200, t49Unknown).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	rr := e49Post(e, strings.NewReader("{}"))
	if rr.Code != 200 {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	e49Calls(t, calls, 1)
	p := e49Prompt(t, body)
	for _, s := range []string{"temperature unknown", "id=t1 ", "id=o1 "} {
		if !strings.Contains(p, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
}

// Cold: outerwear is required. An answer without it is rejected, one with it passes.
func TestT49_ColdRequiresOuterwear(t *testing.T) {
	noOuter := g48Raw(g48O("a", "t1", "b1", "f1"), g48O("b", "t2", "b2", "f2"), g48O("c", "t1", "b2", "f1"))
	withOuter := g48Raw(g48O("a", "t1", "b1", "f1", "o1"), g48O("b", "t2", "b2", "f2", "o1"), g48O("c", "t1", "b2", "f2", "o2"))
	for _, tc := range []struct {
		name, answer string
		code         int
		calls        int32
	}{{"without outerwear", noOuter, 502, 2}, {"with outerwear", withOuter, 200, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			url, calls, body := e49Fake(t, tc.answer)
			e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Cold).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
			if rr := e49Post(e, strings.NewReader("{}")); rr.Code != tc.code {
				t.Errorf("status = %d, want %d, body %s", rr.Code, tc.code, rr.Body)
			}
			e49Calls(t, calls, tc.calls)
			if p := e49Prompt(t, body); !strings.Contains(p, "Outerwear: required") {
				t.Errorf("prompt does not say outerwear is required")
			}
		})
	}
}

// Hot: outerwear items are not in the prompt, and an answer with outerwear is rejected twice.
func TestT49_HotExcludesOuterwear(t *testing.T) {
	light := func(it *store.Item) { it.WarmthTier = "light" }
	withOuter := g48Raw(g48O("a", "t1", "b1", "f1", "o1"), g48O("b", "t2", "b2", "f2"), g48O("c", "t1", "b2", "f1"))
	noOuter := g48Raw(g48O("a", "t1", "b1", "f1"), g48O("b", "t2", "b2", "f2"), g48O("c", "t1", "b2", "f1"))
	for _, tc := range []struct {
		name, answer string
		code         int
		calls        int32
	}{{"answer with outerwear", withOuter, 502, 2}, {"answer without outerwear", noOuter, 200, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			url, calls, body := e49Fake(t, tc.answer)
			e, _ := e49Engine(t, t49Items(light), ing043Fake(t, 200, t49Hot).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
			if rr := e49Post(e, strings.NewReader("{}")); rr.Code != tc.code {
				t.Errorf("status = %d, want %d, body %s", rr.Code, tc.code, rr.Body)
			}
			e49Calls(t, calls, tc.calls)
			p := e49Prompt(t, body)
			for _, s := range []string{"id=o1 ", "id=o2 "} {
				if strings.Contains(p, s) {
					t.Errorf("prompt has %q", s)
				}
			}
			if !strings.Contains(p, "id=t1 ") {
				t.Error("prompt lacks id=t1")
			}
		})
	}
}

// Mild forecast: heavy top, footwear and outerwear are out. Heavy headwear stays (exempt).
func TestT49_WarmthFilterEndToEnd(t *testing.T) {
	heavy := []string{"t2", "f2", "o2", "h2"}
	items := t49Items(func(it *store.Item) {
		if slices.Contains(heavy, it.ID) {
			it.WarmthTier = "heavy"
		}
	})
	answer := g48Raw(g48O("a", "t1", "b1", "f1"), g48O("b", "t1", "b2", "f1", "o1"), g48O("c", "t1", "b1", "f1", "h2"))
	url, calls, body := e49Fake(t, answer)
	e, _ := e49Engine(t, items, ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	if rr := e49Post(e, strings.NewReader("{}")); rr.Code != 200 {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	e49Calls(t, calls, 1)
	p := e49Prompt(t, body)
	for _, s := range []string{"id=t2 ", "id=f2 ", "id=o2 "} {
		if strings.Contains(p, s) {
			t.Errorf("prompt has %q", s)
		}
	}
	for _, s := range []string{"id=t1 ", "id=o1 ", "id=h2 ", "id=a1 "} {
		if !strings.Contains(p, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
}

func TestT49_MethodAndBodyChecks(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})

	rr := ing043Do(t, e, "GET", "/api/recommendations", "")
	t.Logf("GET /api/recommendations gives %d", rr.Code)
	if rr.Code != 404 && rr.Code != 405 {
		t.Errorf("GET status = %d, want 404 or 405", rr.Code)
	}
	e49Calls(t, calls, 0)

	good := map[string]string{
		"whitespace only": " \n\t ", "null": "null", "empty formality": `{"formality":""}`,
		"unknown fields": `{"extra":[1],"note":"x"}`,
	}
	for name, b := range good {
		if rr := e49Post(e, strings.NewReader(b)); rr.Code != 200 {
			t.Errorf("%s: status = %d, want 200, body %.200s", name, rr.Code, rr.Body)
		}
	}
	e49Calls(t, calls, int32(len(good)))

	for name, b := range map[string]string{
		"note is a number":      `{"note":5}`,
		"formality is a number": `{"formality":5}`,
		"body over 64 KiB":      `{"note":"` + strings.Repeat("a", 70<<10) + `"}`,
	} {
		t.Run(name, func(t *testing.T) { e49Err(t, e49Post(e, strings.NewReader(b)), 400, "body") })
	}
	e49Calls(t, calls, int32(len(good)))
}

// The API key goes to the LLM as a Bearer token and never comes back in a response, including 502s.
func TestT49_APIKeyNotInResponse(t *testing.T) {
	const key = "sk-t49-DISTINCT-key-91c4"
	for name, tc := range map[string]struct {
		h    func(w http.ResponseWriter)
		code int
	}{
		"401":            {func(w http.ResponseWriter) { http.Error(w, "unauthorized", 401) }, 502},
		"invalid output": {func(w http.ResponseWriter) { g48Reply(w, "nope") }, 502},
		"success":        {func(w http.ResponseWriter) { g48Reply(w, e49Answer) }, 200},
	} {
		t.Run(name, func(t *testing.T) {
			var auth atomic.Value
			url, _ := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
				auth.Store(r.Header.Get("Authorization"))
				tc.h(w)
			})
			e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url,
				&recommend.Picker{URL: url, APIKey: key, Model: "m", Timeout: 5 * time.Second})
			rr := e49Post(e, strings.NewReader("{}"))
			if rr.Code != tc.code {
				t.Errorf("status = %d, want %d", rr.Code, tc.code)
			}
			if got, _ := auth.Load().(string); got != "Bearer "+key {
				t.Errorf("Authorization = %q, the key was not sent", got)
			}
			if strings.Contains(rr.Body.String(), key) {
				t.Errorf("response body holds the API key: %s", rr.Body)
			}
		})
	}
}

// Open finding: the 502 text carries the LLM server's reply, cut to 300 bytes.
// This test only records it and checks the cut.
func TestT49_LLMReplyReachesBrowser(t *testing.T) {
	url, _ := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		http.Error(w, "T49MARK"+strings.Repeat("Z", 400), 400)
	})
	e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Model: "m", Timeout: 5 * time.Second})
	rr := e49Post(e, strings.NewReader("{}"))
	msg := e49Err(t, rr, 502, "T49MARK")
	t.Logf("502 error carries the LLM reply, %d bytes: %.120s...", len(msg), msg)
	if strings.Count(msg, "Z") > 300 {
		t.Errorf("reply is not cut to 300 bytes: %d Z", strings.Count(msg, "Z"))
	}
}
