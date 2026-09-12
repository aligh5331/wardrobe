// Package tagging is the VLM client: it sends a garment photo plus the
// tagging prompt to VLM_URL and returns the model's raw text response
// (05-vlm-tagging-spec.md "Model"/"Prompt", 07-architecture.md "VLM
// request behavior").
package tagging

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrVLMUnreachable marks failures to reach the VLM at all — connection
// refused, timeout, DNS failure. It is deliberately separate from bad
// model output (malformed JSON, invalid enums), which is handled by
// ING-005: connectivity failure and bad output must not be conflated.
var ErrVLMUnreachable = errors.New("vlm unreachable")

// taggingPrompt is the tagging system prompt from 05-vlm-tagging-spec.md
// "Prompt" section, sent verbatim on every request.
const taggingPrompt = `You are tagging a single clothing item photo for a personal wardrobe
catalog. Respond with ONLY a JSON object matching this schema, no other
text:

{
  "category": one of [top, bottom, outerwear, footwear, headwear, accessory],
  "subcategory": one of the subcategories valid for the chosen category
    (see list below — required, must not be null),
  "dominant_color": one of [black, white, gray, navy, blue, red, green,
    olive, brown, tan, beige, burgundy, pink, purple, yellow, orange],
  "secondary_colors": array of the same color enum, [] if none,
  "pattern": one of [solid, striped, plaid, print] — required, must not
    be null,
  "warmth_tier": one of [light, medium, heavy],
  "formality": one of [casual, smart-casual, formal]
}

Valid subcategories per category:
top: t-shirt, polo, shirt, sweater, hoodie, sweatshirt, tank-top
bottom: jeans, chinos, dress-pants, shorts, sweatpants
outerwear: jacket, coat, blazer, vest
footwear: sneakers, boots, dress-shoes, sandals, loafers
headwear: cap, beanie, hat
accessory: belt, scarf, tie, bag, watch, sunglasses, gloves

If uncertain about a field, make your best guess rather than omitting it.
subcategory is required — always choose the closest match from the list
above for the chosen category. pattern is also required — if the item is
a single solid color with no visible stripe/plaid/print structure, use
"solid" rather than omitting the field.`

// Client sends tagging requests to one llama.cpp VLM via its
// OpenAI-compatible /v1/chat/completions endpoint. By default it is safe
// for concurrent use with no request queue or lock (07-architecture.md
// "VLM request behavior", default). WithSerialization opts into a global
// one-at-a-time queue as a workaround for VLMs with known parallel-request
// bugs.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client

	// serialize forces VLM calls through a single one-at-a-time queue
	// (mu). Default false: requests run concurrently.
	serialize bool
	// mu is the serialization queue; only locked when serialize is true.
	mu sync.Mutex
	// delay is the wait after each response before the next queued
	// request is sent. Only used when serialize is true.
	delay time.Duration
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithSerialization enables the opt-in global one-at-a-time request
// queue: no two VLM requests are in flight simultaneously. delayMS is
// the post-response wait before the next queued request is sent (0 or
// negative means no added delay). This is the VLM_SERIALIZE_REQUESTS /
// VLM_REQUEST_DELAY_MS workaround from 07-architecture.md; without this
// option the client keeps the default concurrent behavior.
func WithSerialization(delayMS int) Option {
	return func(c *Client) {
		c.serialize = true
		if delayMS > 0 {
			c.delay = time.Duration(delayMS) * time.Millisecond
		}
	}
}

// NewClient returns a Client for the VLM at vlmURL. apiKey may be empty;
// when empty, no Authorization header is sent on requests. Optional
// options (e.g. WithSerialization) tune request handling.
func NewClient(vlmURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(vlmURL, "/"),
		apiKey:  apiKey,
		http:    http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// acquire takes the serialization queue when enabled. It returns a
// release function that, when serialize is on, holds the queue through
// the post-response delay before allowing the next request.
func (c *Client) acquire() func() {
	if !c.serialize {
		return func() {}
	}
	c.mu.Lock()
	return func() {
		if c.delay > 0 {
			time.Sleep(c.delay)
		}
		c.mu.Unlock()
	}
}

// Tag sends the photo at imagePath plus the tagging prompt to the VLM
// and returns the model's raw text response (the JSON the prompt asks
// for, unvalidated — schema validation is ING-005's job).
//
// errors.Is(err, ErrVLMUnreachable) is true only when the VLM could not
// be reached at all (connection refused, timeout, DNS failure). Any
// other error is a VLM-side failure — non-2xx status or an unusable
// response envelope — and must not be conflated with unreachable.
func (c *Client) Tag(ctx context.Context, imagePath string) (string, error) {
	// No-op unless serialization is enabled; when enabled it makes the
	// whole request (and the post-response delay) one-at-a-time.
	release := c.acquire()
	defer release()

	image, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image %s: %w", imagePath, err)
	}

	mediaType := mime.TypeByExtension(filepath.Ext(imagePath))
	if mediaType == "" {
		mediaType = "image/jpeg"
	}

	payload, err := json.Marshal(chatRequest{
		Model:       "qwen3-vl",
		MaxTokens:   512,
		Temperature: 0,
		Messages: []message{
			{Role: "system", Content: taggingPrompt},
			{
				Role: "user",
				Content: []contentPart{
					{Type: "image_url", ImageURL: imageURL{URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(image)}},
					{Type: "text", Text: "Tag this garment in the image."},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("encode tagging request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build tagging request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// Transport-level failure: the VLM was never reached. Both %w
		// chains stay intact so callers can errors.Is on either the
		// sentinel or the underlying cause (e.g. context.DeadlineExceeded).
		return "", fmt.Errorf("%w: %w", ErrVLMUnreachable, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read vlm response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vlm returned %s: %s", resp.Status, snippet(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("vlm response is not valid JSON: %w (%s)", err, snippet(respBody))
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("vlm response had no choices: %s", snippet(respBody))
	}
	return chatResp.Choices[0].Message.Content, nil
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature int       `json:"temperature"`
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentPart struct {
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	ImageURL imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// snippet returns a single-line prefix of b for error messages.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 300
	if len(s) > max {
		s = s[:max]
	}
	return s
}
