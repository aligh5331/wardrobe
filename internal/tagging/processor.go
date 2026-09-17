package tagging

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Outcome is the result of processing one photo through ING-005's settled
// retry policy. Flagged is true only when both attempts failed validation;
// in that case Result is zero and Attempts carries both attempts' full detail
// for manual review. When Flagged is false, Result holds the validated
// tagging result and Attempts holds every attempt made (one on success, two
// when attempt 1 failed and attempt 2 succeeded).
type Outcome struct {
	ItemID    string
	PhotoPath string
	Result    TaggingResult
	Flagged   bool
	Attempts  []AttemptLog
}

// Processor drives the settled malformed-output policy from 06-decisions.md:
// send the tagging request, validate the response, and if validation fails
// retry exactly once at the configured (nonzero) temperature. Both attempts
// are logged keyed by a shared item id; only a second failure flags the photo
// for manual review. In keeping with that decision, a VLM-unreachable error
// (ING-002) is not a malformed-output case: it surfaces immediately and is
// never retried or turned into a flag.
type Processor struct {
	client *Client
	log    AttemptLogger
}

// ProcessorOption configures a Processor at construction time.
type ProcessorOption func(*Processor)

// WithAttemptLogger overrides where per-attempt records are appended. A nil
// logger is ignored, leaving the default logs/vlm-attempts.jsonl in place.
func WithAttemptLogger(l AttemptLogger) ProcessorOption {
	return func(p *Processor) {
		if l != nil {
			p.log = l
		}
	}
}

// NewProcessor returns a Processor using client and appending attempt logs to
// logs/vlm-attempts.jsonl unless WithAttemptLogger is supplied.
func NewProcessor(client *Client, opts ...ProcessorOption) *Processor {
	p := &Processor{
		client: client,
		log:    NewJSONLAttemptLogger(DefaultAttemptLogPath),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Process sends the photo at photoPath to the VLM, validating attempt 1 and —
// only if it fails validation — retrying once with a fresh request. It
// returns a nil error when the photo is either tagged successfully or flagged
// for manual review, so a batch caller can keep processing other photos
// rather than being aborted by one bad result. A nil error therefore does not
// by itself mean success: check Outcome.Flagged.
//
// A non-nil error is a hard failure that should stop or surface to the
// caller: VLM unreachable (errors.Is(err, ErrVLMUnreachable)), a transport or
// response-envelope failure, or a failure to write the attempt log. Per
// 06-decisions.md this case is not retried and not flagged.
func (p *Processor) Process(ctx context.Context, photoPath string) (Outcome, error) {
	itemID, err := newItemID()
	if err != nil {
		return Outcome{}, fmt.Errorf("generate item id: %w", err)
	}
	out := Outcome{ItemID: itemID, PhotoPath: photoPath}

	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Read the temperature the client is about to send so it can be
		// logged with the attempt. Tag reads its source afresh too, matching
		// ING-007's "independent sample" requirement.
		temperature := p.client.Temperature()
		raw, err := p.client.Tag(ctx, photoPath)
		if err != nil {
			return Outcome{}, err
		}

		result, validationErr := ParseTaggingResult(raw)
		entry := newAttemptLog(itemID, photoPath, attempt, temperature, raw, validationErr)
		if err := p.log.Log(entry); err != nil {
			return Outcome{}, fmt.Errorf("log vlm attempt %d for %s: %w", attempt, photoPath, err)
		}
		out.Attempts = append(out.Attempts, entry)

		if validationErr == nil {
			out.Result = result
			return out, nil
		}
	}

	out.Flagged = true
	return out, nil
}

// newAttemptLog builds the log record for one attempt. ParsedJSON is the raw
// response when it is valid JSON and null otherwise; failure fields are empty
// on a successful attempt.
func newAttemptLog(itemID, photoPath string, attempt int, temperature float64, raw string, validationErr error) AttemptLog {
	entry := AttemptLog{
		ItemID:      itemID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		PhotoPath:   photoPath,
		Attempt:     attempt,
		Temperature: temperature,
		RawResponse: raw,
	}
	if json.Valid([]byte(raw)) {
		entry.ParsedJSON = json.RawMessage(raw)
	}
	if validationErr != nil {
		entry.FailureType, entry.FailureDetail = failureInfo(validationErr)
	}
	return entry
}

// failureInfo classifies a validation error for the attempt log. Every error
// ParseTaggingResult returns is a *ValidationError, so the fallback is only a
// safety net.
func failureInfo(err error) (failureType, detail string) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.FailureType, ve.FailureDetail
	}
	return "unknown", err.Error()
}

// newItemID returns a random UUIDv4 to link a photo's attempts in the log.
// 04-data-schema.md types the wardrobe item's id as a uuid; generating it at
// ingestion means the eventual store row can reuse the same id.
func newItemID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
