// ING-049: POST /api/recommendations. Weather and the LLM are httptest fakes,
// the catalog is a real temp SQLite store. Fixtures come from ing_048 (g48*)
// and ing_043 (ing043*).
package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"wardrobe/internal/api"
	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

// Feels-like 15 C with a 12..18 range: mild band, outerwear optional.
const e49Mild = `{"current":{"temperature_2m":15.0,"apparent_temperature":15.2,"weather_code":3,"precipitation":0.0},
 "daily":{"temperature_2m_min":[12.0],"temperature_2m_max":[18.0],"precipitation_probability_max":[10],"weather_code":[3]}}`

// Feels-like 3 C: cold band, outerwear required.
const e49Cold = `{"current":{"temperature_2m":4.0,"apparent_temperature":3.0,"weather_code":3,"precipitation":0.0},
 "daily":{"temperature_2m_min":[1.0],"temperature_2m_max":[6.0],"precipitation_probability_max":[10],"weather_code":[3]}}`

// e49Answer lists item ids out of category order on purpose.
var e49Answer = g48Raw(
	g48O("a", "f2", "a1", "t2", "o1", "b1", "h1"),
	g48O("b", "f1", "b2", "t1"),
	g48O("c", "f1", "b1", "o1", "t1"))

// e49Items is the g48 catalog with the given ids marked formal and the drop ids removed.
func e49Items(formal []string, drop ...string) []store.Item {
	var out []store.Item
	for _, it := range g48Items() {
		if slices.Contains(drop, it.ID) {
			continue
		}
		if slices.Contains(formal, it.ID) {
			it.Formality = "formal"
		}
		out = append(out, it)
	}
	return out
}

func e49Engine(t *testing.T, items []store.Item, weatherURL string, p *recommend.Picker) (http.Handler, *store.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, it := range items {
		if err := st.Insert(it); err != nil {
			t.Fatal(err)
		}
	}
	return api.New(st, t.TempDir(), api.WithWeather(weather.New(weatherURL, "", time.Second)), api.WithRecommender(p)), st
}

// e49Fake is an LLM that always answers content and keeps the last request body.
func e49Fake(t *testing.T, content string) (string, *atomic.Int32, *atomic.Value) {
	var body atomic.Value
	url, calls := g48Serve(t, func(n int, w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body.Store(string(b))
		g48Reply(w, content)
	})
	return url, calls, &body
}

// e49Prompt returns the user message of the last captured LLM request.
func e49Prompt(t *testing.T, body *atomic.Value) string {
	t.Helper()
	raw, _ := body.Load().(string)
	var req struct{ Messages []struct{ Content string } }
	if err := json.Unmarshal([]byte(raw), &req); err != nil || len(req.Messages) < 2 {
		t.Fatalf("no LLM request captured: %v", err)
	}
	return req.Messages[1].Content
}

func e49Post(h http.Handler, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/recommendations", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// e49Err checks the status and that the JSON error message contains sub.
func e49Err(t *testing.T, rr *httptest.ResponseRecorder, code int, sub string) string {
	t.Helper()
	msg, _ := ing043JSON(t, rr)["error"].(string)
	if rr.Code != code || msg == "" || !strings.Contains(msg, sub) {
		t.Errorf("status %d error %q, want %d and an error containing %q", rr.Code, msg, code, sub)
	}
	return msg
}

func e49Calls(t *testing.T, calls *atomic.Int32, want int32) {
	t.Helper()
	if n := calls.Load(); n != want {
		t.Errorf("LLM calls = %d, want %d", n, want)
	}
}

// AC1: 200 with {weather, outfits} for body {} and for no body.
func TestING049_AC1_Success(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	wantWeather := ing043JSON(t, ing043Do(t, e, "GET", "/api/weather", ""))
	var list []map[string]any
	if err := json.Unmarshal(ing043Do(t, e, "GET", "/api/items", "").Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, m := range list {
		byID[m["id"].(string)] = m
	}
	wantOrder := [][]string{{"o1", "t2", "b1", "f2", "h1", "a1"}, {"t1", "b2", "f1"}, {"o1", "t1", "b1", "f1"}}

	for name, body := range map[string]io.Reader{"empty object": strings.NewReader("{}"), "nil body": nil} {
		rr := e49Post(e, body)
		if rr.Code != 200 {
			t.Errorf("%s: status = %d, body %s", name, rr.Code, rr.Body)
			continue
		}
		var got struct {
			Weather map[string]any
			Outfits []struct {
				Items  []map[string]any
				Reason string
			}
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Weather, wantWeather) {
			t.Errorf("%s: weather = %v, want %v", name, got.Weather, wantWeather)
		}
		if len(got.Outfits) != 3 {
			t.Fatalf("%s: outfits = %d, want 3", name, len(got.Outfits))
		}
		for i, o := range got.Outfits {
			if o.Reason == "" {
				t.Errorf("%s: outfit %d has no reason", name, i)
			}
			var ids []string
			for _, it := range o.Items {
				id, _ := it["id"].(string)
				ids = append(ids, id)
				if !reflect.DeepEqual(it, byID[id]) {
					t.Errorf("%s: item %s = %v, want %v", name, id, it, byID[id])
				}
			}
			if !slices.Equal(ids, wantOrder[i]) {
				t.Errorf("%s: outfit %d order = %v, want %v", name, i, ids, wantOrder[i])
			}
		}
	}
	e49Calls(t, calls, 2)
}

// AC2: the formal filter and the trimmed note reach the prompt.
func TestING049_AC2_FormalityAndNoteInPrompt(t *testing.T) {
	answer := g48Raw(g48O("a", "t2", "b2", "f2"), g48O("b", "f2", "o2", "b2", "t2"), g48O("c", "h2", "f2", "b2", "t2"))
	url, calls, body := e49Fake(t, answer)
	items := e49Items([]string{"t2", "b2", "f2", "o2", "h2"})
	e, _ := e49Engine(t, items, ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	rr := e49Post(e, strings.NewReader(`{"formality":"formal","note":"  wedding  "}`))
	if rr.Code != 200 {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	e49Calls(t, calls, 1)
	prompt := e49Prompt(t, body)
	for _, s := range []string{"Note: wedding\n", "Formality: formal\n", "id=t2 ", "id=b2 ", "id=f2 ", "id=o2 ", "id=h2 "} {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
	for _, s := range []string{"id=t1 ", "id=b1 ", "id=f1 ", "id=o1 ", "id=h1 ", "id=a1 ", "Note:  "} {
		if strings.Contains(prompt, s) {
			t.Errorf("prompt has %q", s)
		}
	}
}

// AC3: invalid input is 400 and never reaches the LLM. The 500-rune limit is inclusive.
func TestING049_AC3_BadRequest400(t *testing.T) {
	note500, note501 := strings.Repeat("é", 500), strings.Repeat("é", 501)
	url, calls, body := e49Fake(t, e49Answer)
	e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	for _, tc := range []struct{ name, body, field string }{
		{"formality outside the taxonomy", `{"formality":"business"}`, "formality"},
		{"note of 501 runes", `{"note":"` + note501 + `"}`, "note"},
		{"note of 501 runes with padding", `{"note":"  ` + note501 + `  "}`, "note"},
		{"malformed JSON", `{bad`, "body"},
	} {
		e49Err(t, e49Post(e, strings.NewReader(tc.body)), 400, tc.field)
	}
	e49Calls(t, calls, 0)
	for name, note := range map[string]string{"500 runes": note500, "500 runes with padding": "  " + note500 + "  "} {
		if rr := e49Post(e, strings.NewReader(`{"note":"`+note+`"}`)); rr.Code != 200 {
			t.Errorf("%s: status = %d, want 200, body %.200s", name, rr.Code, rr.Body)
		} else if p := e49Prompt(t, body); !strings.Contains(p, "Note: "+note500+"\n") {
			t.Errorf("%s: prompt does not hold the trimmed note", name)
		}
	}
}

// AC4: an upstream weather failure is 502 and the LLM is not called.
func TestING049_AC4_WeatherUpstreamFails502(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	e, _ := e49Engine(t, g48Items(), ing043Fake(t, 500, `{}`).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	e49Err(t, e49Post(e, strings.NewReader("{}")), 502, "weather")
	e49Calls(t, calls, 0)
}

// AC5: a missing required slot is 422 naming the slot, and the LLM is not called.
func TestING049_AC5_MissingSlot422(t *testing.T) {
	someFormal := []string{"t2", "b2", "o2"}
	for _, tc := range []struct {
		name     string
		items    []store.Item
		forecast string
		body     string
		want     []string
	}{
		{"no footwear", e49Items(nil, "f1", "f2"), e49Mild, `{}`, []string{"footwear"}},
		{"no top and no bottom", e49Items(nil, "t1", "t2", "b1", "b2"), e49Mild, `{}`, []string{"top", "bottom"}},
		{"formal filter removes footwear", e49Items(someFormal), e49Mild, `{"formality":"formal"}`, []string{"footwear"}},
		{"no outerwear when the cold forecast requires it", e49Items(nil, "o1", "o2"), e49Cold, `{}`, []string{"outerwear"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, calls, _ := e49Fake(t, e49Answer)
			e, _ := e49Engine(t, tc.items, ing043Fake(t, 200, tc.forecast).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
			rr := e49Post(e, strings.NewReader(tc.body))
			for _, w := range tc.want {
				e49Err(t, rr, 422, w)
			}
			e49Calls(t, calls, 0)
		})
	}
}

// AC6: every LLM failure is 502 with a JSON error. LLM_MODEL is named only when Model is empty.
func TestING049_AC6_LLMFailures502(t *testing.T) {
	hang := func(n int, w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}
	non2xx := func(n int, w http.ResponseWriter, r *http.Request) { http.Error(w, "rejected", 500) }
	junk := func(n int, w http.ResponseWriter, r *http.Request) { g48Reply(w, "nope") }
	for _, tc := range []struct {
		name    string
		h       func(int, http.ResponseWriter, *http.Request)
		model   string
		timeout time.Duration
		calls   int32 // -1: not counted
		hint    bool
	}{
		{"unreachable", nil, "m", 5 * time.Second, -1, false},
		{"timeout", hang, "m", 100 * time.Millisecond, 1, false},
		{"non-2xx with model", non2xx, "m", 5 * time.Second, 1, false},
		{"non-2xx without model", non2xx, "", 5 * time.Second, 1, true},
		{"invalid twice", junk, "m", 5 * time.Second, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var url string
			calls := new(atomic.Int32)
			if tc.h == nil {
				srv := httptest.NewServer(http.NotFoundHandler())
				url = srv.URL
				srv.Close()
			} else {
				url, calls = g48Serve(t, tc.h)
			}
			e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Model: tc.model, Timeout: tc.timeout})
			rr := e49Post(e, strings.NewReader("{}"))
			msg := e49Err(t, rr, 502, "")
			if has := strings.Contains(msg, "LLM_MODEL"); has != tc.hint {
				t.Errorf("error %q: mentions LLM_MODEL = %v, want %v", msg, has, tc.hint)
			}
			if tc.calls >= 0 {
				e49Calls(t, calls, tc.calls)
			}
		})
	}
}

// AC7: a broken local database is 500 and the LLM is not called.
// The test closes the store's connection, as ING-043 does for the weather routes.
func TestING049_AC7_DBFailure500(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	e, st := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	_ = st.Close()
	e49Err(t, e49Post(e, strings.NewReader("{}")), 500, "")
	e49Calls(t, calls, 0)
}

// AC8: the existing routes are unchanged once the recommender route exists.
func TestING049_AC8_ExistingRoutesUnchanged(t *testing.T) {
	url, calls, _ := e49Fake(t, e49Answer)
	e, _ := e49Engine(t, g48Items(), ing043Fake(t, 200, e49Mild).url, &recommend.Picker{URL: url, Timeout: 5 * time.Second})
	var list []map[string]any
	rr := ing043Do(t, e, "GET", "/api/items", "")
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil || rr.Code != 200 || len(list) != 11 || len(list[0]) != len(ing019ItemKeys) {
		t.Errorf("GET /api/items: status %d, %d items, err %v", rr.Code, len(list), err)
	}
	rr = ing043Do(t, e, "GET", "/api/weather", "")
	if m := ing043JSON(t, rr); rr.Code != 200 || len(m) != 3 {
		t.Errorf("GET /api/weather: status %d, keys %v", rr.Code, ing019Keys(m))
	}
	body := ing030Marshal(t, ing030ValidBody(t))
	if rr = ing030Put(t, e, "/api/items/t1", body); rr.Code != 200 {
		t.Errorf("PUT /api/items/t1: status %d, body %s", rr.Code, rr.Body)
	}
	e49Calls(t, calls, 0)
}

// AC10: without WithRecommender the route is 503 with a JSON error.
func TestING049_AC10_NotWired503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := api.New(st, t.TempDir(), api.WithWeather(weather.New(ing043Fake(t, 200, e49Mild).url, "", time.Second)))
	e49Err(t, e49Post(e, strings.NewReader("{}")), 503, "recommender")
}

// AC11: cmd/server has no test harness, so this reads the source like the ING-043 startup check.
func TestING049_AC11_ServerWiresRecommender(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{"api.WithRecommender(&recommend.Picker{", "cfg.LLMURL", "cfg.LLMAPIKey", "cfg.LLMModel", "cfg.LLMTemperature"} {
		if !strings.Contains(s, want) {
			t.Errorf("cmd/server/main.go lacks %q", want)
		}
	}
	if strings.Contains(s, ".Pick(") {
		t.Error("cmd/server/main.go calls the picker at startup")
	}
}
