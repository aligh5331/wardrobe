//go:build integration

// ING-059 recommender eval (docs/adr/0002-recommender-eval-is-human-rated.md).
// Opt-in live run against the configured LLM; see
// tests/evals/recommender/README.md:
//
//	EVAL_LABEL=baseline go test -tags=integration -timeout 30m ./tests/ -run 'TestRecommendEval$' -v
//	go test -tags=integration ./tests/ -run 'TestRecommendEvalSummary$' -v
//
// TestRecommendEval skips when LLM_URL is unset, so CI never calls an LLM.
package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"wardrobe/internal/config"
	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
)

// recordingProxy forwards every request unchanged to the real LLM and keeps
// the bodies of the requests made since the last reset.
type recordingProxy struct {
	target string
	mu     sync.Mutex
	bodies [][]byte
}

func (p *recordingProxy) reset() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.bodies
	p.bodies = nil
	return b
}

func (p *recordingProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	p.mu.Lock()
	p.bodies = append(p.bodies, body)
	p.mu.Unlock()

	req, err := http.NewRequestWithContext(r.Context(), r.Method, p.target+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req.Header = r.Header.Clone()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// messageContents returns the message contents of one chat request body.
func messageContents(t *testing.T, body []byte) []string {
	t.Helper()
	var req struct{ Messages []struct{ Content string } }
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("captured request is not JSON: %v", err)
	}
	var out []string
	for _, m := range req.Messages {
		out = append(out, m.Content)
	}
	return out
}

// gitCommit is the short HEAD hash, "-dirty" when the tree has changes, or
// "unknown" when git is unavailable.
func gitCommit(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(out))
	if st, err := exec.Command("git", "-C", root, "status", "--porcelain").Output(); err == nil && len(bytes.TrimSpace(st)) > 0 {
		commit += "-dirty"
	}
	return commit
}

func TestRecommendEval(t *testing.T) {
	root := moduleRoot(t)
	_ = godotenv.Load(filepath.Join(root, ".env")) // existing env vars win
	if os.Getenv("LLM_URL") == "" {
		t.Skip("LLM_URL is unset; the recommender eval is opt-in")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	proxy := &recordingProxy{target: strings.TrimRight(cfg.LLMURL, "/")}
	srv := httptest.NewServer(proxy)
	defer srv.Close()
	picker := recommend.Picker{URL: srv.URL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, Temperature: cfg.LLMTemperature}

	wardrobe := trapWardrobe()
	byID := map[string]store.Item{}
	for _, it := range wardrobe {
		byID[it.ID] = it
	}

	fingerprint := sha256.New()
	var results []evalResult
	for _, sc := range evalScenarios() {
		rules := recommend.RulesFor(sc.Forecast)
		cands := recommend.Filter(wardrobe, rules, "")
		if missing := recommend.MissingSlots(cands, rules); len(missing) > 0 {
			t.Fatalf("fixture bug: scenario %s is missing slots %v", sc.Name, missing)
		}
		in := recommend.Input{Forecast: sc.Forecast, Rules: rules, Note: sc.Note, Candidates: cands}
		res := evalResult{Scenario: sc, Rules: rules}
		for run := 1; run <= 3; run++ {
			proxy.reset()
			outfits, err := picker.Pick(context.Background(), in)
			bodies := proxy.reset()
			if run == 1 && len(bodies) > 0 {
				for _, c := range messageContents(t, bodies[0]) {
					fingerprint.Write([]byte(c))
					fingerprint.Write([]byte{0})
				}
			}
			if err != nil {
				res.Runs = append(res.Runs, evalRun{Err: err})
				continue
			}
			var items [][]store.Item
			var reasons []string
			for _, o := range outfits {
				var its []store.Item
				for _, id := range o.ItemIDs {
					its = append(its, byID[id])
				}
				items = append(items, its)
				reasons = append(reasons, o.Reason)
			}
			res.Runs = append(res.Runs, newEvalRun(items, reasons, rules, sc.Forecast.Current.ApparentTemperatureC, len(bodies) == 1))
		}
		results = append(results, res)
	}

	commit := gitCommit(root)
	label := evalLabel(os.Getenv("EVAL_LABEL"))
	if label == "" {
		label = evalLabel(strings.TrimSuffix(commit, "-dirty"))
	}
	model := cfg.LLMModel
	if model == "" {
		model = "(server default)"
	}
	now := time.Now()
	form := writeEvalForm(evalHeader{When: now, Label: label, Commit: commit,
		Fingerprint: hex.EncodeToString(fingerprint.Sum(nil))[:12], Model: model,
		Temperature: cfg.LLMTemperature}, results)

	dir := filepath.Join(root, evalFormsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, now.Format("2006-01-02-1504")+"-"+label+".md")
	if err := os.WriteFile(path, []byte(form), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("form written: %s\n\n%s", path, flagTable(results))

	for _, res := range results {
		for i, run := range res.Runs {
			if !run.valid() {
				t.Errorf("scenario %s run %d: %v", res.Scenario.Name, i+1, run.Err)
			}
		}
	}
}

func TestRecommendEvalSummary(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), evalFormsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	rows := 0
	for _, e := range entries { // ReadDir sorts by name, so oldest first
		if e.IsDir() || !evalFormName.MatchString(e.Name()) {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		p, err := parseEvalForm(string(text))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if !p.rated() {
			continue
		}
		t.Log(summaryRow(e.Name(), p))
		rows++
	}
	if rows == 0 {
		t.Log("no rated forms yet")
	}
}
