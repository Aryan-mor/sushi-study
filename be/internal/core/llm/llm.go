// Package llm is the provider-agnostic contract the indexer uses for caption classify, referee,
// and verifier calls (ADR 0025). Adapters implement Completer; this package owns the quota
// sentinel so indexerd can pause without knowing Google from OpenRouter.
package llm

import (
	"context"
	"errors"
)

// Completer is one system+user exchange constrained to a JSON schema, returning the model's JSON
// string (R-GO-5). internal/adapter/llm satisfies it; so does a test fake.
type Completer interface {
	Complete(ctx context.Context, systemPrompt, userContent string, schema map[string]any) (string, error)
}

// ErrQuota means the provider refused the call because credits, billing, or a hard quota ran out.
// Transient rate limits are not this error — those stay retryable and must not pause the indexer.
var ErrQuota = errors.New("llm quota exhausted")

// ErrConfig means the model id is missing on the provider or is the wrong shape for this backend
// (Vertex 404, OpenRouter org/model id on Google). Intake must pause; posting one error card per
// row would flood the review channel.
var ErrConfig = errors.New("llm model missing or misconfigured")

// ErrRateLimit is a transient 429 / RESOURCE_EXHAUSTED "try again later". The row stays `new`,
// the pass backs off, and no error card is posted. It must not trip indexer_enabled.
var ErrRateLimit = errors.New("llm rate limited")

// IsQuota reports whether err is (or wraps) ErrQuota.
func IsQuota(err error) bool {
	return errors.Is(err, ErrQuota)
}

// IsConfig reports whether err is (or wraps) ErrConfig.
func IsConfig(err error) bool {
	return errors.Is(err, ErrConfig)
}

// IsRateLimit reports whether err is (or wraps) ErrRateLimit.
func IsRateLimit(err error) bool {
	return errors.Is(err, ErrRateLimit)
}

// IsHalt reports whether intake should pause: billing exhaustion or a broken model id.
func IsHalt(err error) bool {
	return IsQuota(err) || IsConfig(err)
}
