package recommend

// LLM outfit picker (07-architecture.md "Recommender (Phase 3)" steps 4 and 5,
// "LLM request behavior"; 06-decisions.md "Recommender output validation",
// "LLM config"). One prompt, one OpenAI-compatible call, untrusted-JSON
// validation, one retry on invalid output.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"wardrobe/internal/store"
	"wardrobe/internal/weather"
)

// ErrLLM marks every picker failure: unreachable, timeout, non-2xx, or
// invalid output twice. ING-049 maps it to 502.
var ErrLLM = errors.New("llm failure")

const (
	// llmTimeout is the fixed request timeout (06-decisions.md "LLM config").
	llmTimeout = 120 * time.Second
	maxTokens  = 1024
)

// errBadOutput marks a 200 response whose body is unusable. Unlike transport
// and non-2xx failures, Pick retries it once.
var errBadOutput = errors.New("bad llm output")

// Picker asks the local text LLM for outfits. Timeout 0 means the 120 s
// production value; tests set a shorter one.
type Picker struct {
	URL         string
	APIKey      string
	Model       string
	Temperature float64
	Timeout     time.Duration
}

// Input is everything one Pick call needs. Candidates are already filtered.
type Input struct {
	Forecast   weather.Forecast
	Rules      Rules
	Formality  string
	Note       string
	Candidates []store.Item
}

// Outfit is one validated suggestion.
type Outfit struct {
	ItemIDs []string
	Reason  string
}

const systemPrompt = `You pick outfits for one person from a list of clothing items.
Respond with ONLY a JSON object, no other text, in exactly this shape:

{"outfits":[{"item_ids":["<id>","..."],"reason":"<one sentence>"}]}

Rules:
- Return exactly 3 outfits, and the 3 outfits must be different from each other.
- Every outfit has exactly one top, one bottom and one footwear item.
- Follow the outerwear rule in the request: required means exactly one outerwear item, excluded means none, optional means at most one.
- An outfit has at most one outerwear item and at most one headwear item. Accessories are optional.
- Use only ids from the item list. Never repeat an id inside an outfit.
- Coordinate colors: pair neutrals with one accent, avoid clashing colors and too many patterns.
- Read the note and let it guide the choice.
- Each reason is one short sentence.`

// Pick asks the LLM for 3 outfits. Every error satisfies errors.Is(err, ErrLLM).
//
// Retry policy, per 07-architecture.md "LLM request behavior":
//   - Invalid content, or a 200 whose body is not a chat envelope with a
//     choice: one retry with the same prompt, then fail.
//   - Transport failure (unreachable, timeout, DNS): one call, no retry.
//   - Non-2xx status: one call, no retry. The ticket only names invalid
//     output as retryable, and a rejecting server will reject again.
//
// NOTE: the non-2xx and bad-envelope readings above are judgment calls, not
// spelled out in the ticket.
func (p *Picker) Pick(ctx context.Context, in Input) ([]Outfit, error) {
	system, user := buildPrompt(in)
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = llmTimeout
	}
	client := &http.Client{Timeout: timeout}

	var last error
	for range 2 {
		content, err := p.call(ctx, client, system, user)
		if err == nil {
			var outfits []Outfit
			if outfits, err = validate(content, in); err == nil {
				return outfits, nil
			}
		} else if !errors.Is(err, errBadOutput) {
			return nil, err
		}
		last = err
	}
	return nil, fmt.Errorf("%w: invalid output after retry: %v", ErrLLM, last)
}

type chatRequest struct {
	Model       string    `json:"model,omitempty"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// call makes one request and returns the first choice's content. Failures
// other than errBadOutput already wrap ErrLLM.
func (p *Picker) call(ctx context.Context, client *http.Client, system, user string) (string, error) {
	payload, err := json.Marshal(chatRequest{
		Model:       p.Model,
		Messages:    []message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature: p.Temperature,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("%w: encode request: %v", ErrLLM, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.URL, "/")+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrLLM, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrLLM, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%w: read response: %w", ErrLLM, err)
	}

	if resp.StatusCode/100 != 2 {
		msg := fmt.Sprintf("llm returned %s: %s", resp.Status, snippet(body))
		if p.Model == "" {
			msg += ". Set LLM_MODEL if your LLM server requires a model name"
		}
		return "", fmt.Errorf("%w: %s", ErrLLM, msg)
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("%w: response is not valid JSON: %v", errBadOutput, err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("%w: response had no choices", errBadOutput)
	}
	return cr.Choices[0].Message.Content, nil
}

// snippet returns a single-line prefix of b, at most 300 bytes.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func orUnknown[T any](v *T, format string) string {
	if v == nil {
		return "unknown"
	}
	return fmt.Sprintf(format, *v)
}

// buildPrompt returns the system and user prompts. The user prompt has the
// weather summary, formality, note, and one line per candidate. It never
// includes photo paths, photo data, notes or added dates.
//
// ponytail: warmth tiers are not printed. Candidates are already filtered by
// them. Print Rules.Tiers (sorted) if the model starts ignoring the weather.
func buildPrompt(in Input) (system, user string) {
	var b strings.Builder
	f := in.Forecast
	b.WriteString("Weather today:\n")
	if in.Rules.TempUnknown {
		b.WriteString("- temperature unknown (no forecast temperature is available)\n")
	}
	fmt.Fprintf(&b, "- feels-like: %s C\n", orUnknown(f.Current.ApparentTemperatureC, "%.1f"))
	fmt.Fprintf(&b, "- min: %s C, max: %s C\n",
		orUnknown(f.Today.TemperatureMinC, "%.1f"), orUnknown(f.Today.TemperatureMaxC, "%.1f"))
	fmt.Fprintf(&b, "- rain chance: %s%%\n", orUnknown(f.Today.PrecipitationProbabilityMax, "%d"))
	fmt.Fprintf(&b, "- weather code (WMO): %s\n", orUnknown(f.Today.WeatherCode, "%d"))

	switch in.Rules.Outerwear {
	case OuterwearRequired:
		b.WriteString("Outerwear: required. Every outfit has exactly one outerwear item.\n")
	case OuterwearExcluded:
		b.WriteString("Outerwear: excluded. No outfit has an outerwear item.\n")
	default:
		b.WriteString("Outerwear: optional. Each outfit has at most one outerwear item.\n")
	}

	formality := in.Formality
	if formality == "" {
		formality = "any"
	}
	note := strings.TrimSpace(in.Note)
	if note == "" {
		note = "none"
	}
	fmt.Fprintf(&b, "Formality: %s\nNote: %s\n\nItems:\n", formality, note)

	for _, it := range in.Candidates {
		fmt.Fprintf(&b, "id=%s category=%s subcategory=%s dominant_color=%s secondary_colors=[%s] pattern=%s warmth_tier=%s formality=%s\n",
			it.ID, it.Category, it.Subcategory, it.DominantColor,
			strings.Join(it.SecondaryColors, ","), it.Pattern, it.WarmthTier, it.Formality)
	}
	b.WriteString("\nPick 3 outfits and answer with the JSON object only.")
	return systemPrompt, b.String()
}

// stripFence removes surrounding whitespace and an optional ```json fence.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	} else {
		s = ""
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// validate parses the model's content and checks every rule from
// 06-decisions.md "Recommender output validation". It returns the outfits in
// the model's order. Error text never lists the candidates.
func validate(content string, in Input) ([]Outfit, error) {
	var parsed struct {
		Outfits []struct {
			ItemIDs []string `json:"item_ids"`
			Reason  string   `json:"reason"`
		} `json:"outfits"`
	}
	if err := json.Unmarshal([]byte(stripFence(content)), &parsed); err != nil {
		return nil, fmt.Errorf("unparseable JSON: %v", err)
	}
	if len(parsed.Outfits) != 3 {
		return nil, fmt.Errorf("got %d outfits, want 3", len(parsed.Outfits))
	}

	category := make(map[string]string, len(in.Candidates))
	for _, it := range in.Candidates {
		category[it.ID] = it.Category
	}

	out := make([]Outfit, 0, 3)
	sets := map[string]bool{}
	for n, o := range parsed.Outfits {
		if strings.TrimSpace(o.Reason) == "" {
			return nil, fmt.Errorf("outfit %d has an empty reason", n+1)
		}
		seen := map[string]bool{}
		count := map[string]int{}
		for _, id := range o.ItemIDs {
			cat, ok := category[id]
			if !ok {
				return nil, fmt.Errorf("outfit %d has an id that is not a candidate", n+1)
			}
			if seen[id] {
				return nil, fmt.Errorf("outfit %d repeats an id", n+1)
			}
			seen[id] = true
			count[cat]++
		}
		for _, slot := range []string{"top", "bottom", "footwear"} {
			if count[slot] != 1 {
				return nil, fmt.Errorf("outfit %d has %d %s items, want 1", n+1, count[slot], slot)
			}
		}
		switch {
		case count["outerwear"] > 1:
			return nil, fmt.Errorf("outfit %d has more than one outerwear item", n+1)
		case count["outerwear"] == 0 && in.Rules.Outerwear == OuterwearRequired:
			return nil, fmt.Errorf("outfit %d lacks the required outerwear", n+1)
		case count["outerwear"] > 0 && in.Rules.Outerwear == OuterwearExcluded:
			return nil, fmt.Errorf("outfit %d has outerwear but it is excluded", n+1)
		case count["headwear"] > 1:
			return nil, fmt.Errorf("outfit %d has more than one headwear item", n+1)
		}
		ids := append([]string(nil), o.ItemIDs...)
		sort.Strings(ids)
		sets[strings.Join(ids, "\x00")] = true
		out = append(out, Outfit{ItemIDs: o.ItemIDs, Reason: strings.TrimSpace(o.Reason)})
	}
	if len(sets) == 1 {
		return nil, errors.New("all 3 outfits are the same set of items")
	}
	return out, nil
}
