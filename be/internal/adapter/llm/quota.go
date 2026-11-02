package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corellm "github.com/Aryan-mor/sushi-study/be/internal/core/llm"
)

// wrapHalt tags billing exhaustion as ErrQuota, missing/wrong model ids as ErrConfig, and HTTP
// 429s plus client timeouts as ErrRateLimit. Those stay retryable and must not pause the indexer
// (ADR 0025).
func wrapHalt(err error) error {
	err = wrapQuota(err)
	if err == nil || corellm.IsHalt(err) || corellm.IsRateLimit(err) {
		return err
	}
	if configMessage(err.Error()) {
		return fmt.Errorf("%w: %w", corellm.ErrConfig, err)
	}
	if rateLimitMessage(err.Error()) || timeoutErr(err) {
		return fmt.Errorf("%w: %w", corellm.ErrRateLimit, err)
	}
	return err
}

// wrapQuota returns ErrQuota when the provider's error is a billing/credit exhaustion, and the
// original error otherwise. Rate-limit 429s without billing language stay retryable.
func wrapQuota(err error) error {
	if err == nil || corellm.IsQuota(err) {
		return err
	}
	if !quotaMessage(err.Error()) {
		return err
	}
	return fmt.Errorf("%w: %w", corellm.ErrQuota, err)
}

func configMessage(msg string) bool {
	s := strings.ToLower(msg)
	if strings.Contains(s, "http 404") {
		return true
	}
	if strings.Contains(s, "cannot contain '/'") {
		return true
	}
	if strings.Contains(s, "not_found") {
		return true
	}
	if strings.Contains(s, "not found") && strings.Contains(s, "model") {
		return true
	}
	return false
}

func quotaMessage(msg string) bool {
	s := strings.ToLower(msg)
	if strings.Contains(s, "rate limit") || strings.Contains(s, "rate-limit") || strings.Contains(s, "ratelimit") {
		return false
	}
	for _, n := range []string{
		"credits are depleted",
		"prepayment",
		"please enable billing",
		"billing account",
		"free tier has expired",
		"exceeded your current quota",
		"quota exceeded",
		"insufficient credits",
		"insufficient quota",
		"credit balance",
		"billing hard limit",
	} {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func rateLimitMessage(msg string) bool {
	s := strings.ToLower(msg)
	if strings.Contains(s, "http 429") {
		return true
	}
	if strings.Contains(s, "resource exhausted") && strings.Contains(s, "try again later") {
		return true
	}
	return false
}

func timeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "deadline exceeded") || strings.Contains(s, "client.timeout")
}
