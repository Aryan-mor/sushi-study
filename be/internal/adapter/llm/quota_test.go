package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	corellm "github.com/Aryan-mor/sushi-study/be/internal/core/llm"
)

func TestWrapQuota(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"already sentinel", corellm.ErrQuota, true},
		{"google prepaid", errors.New("Your prepayment credits are depleted. Please go to AI Studio"), true},
		{"openrouter credits", errors.New("openrouter: http 402: Insufficient credits"), true},
		{"quota exceeded", errors.New("RESOURCE_EXHAUSTED: Quota exceeded for"), true},
		{"rate limit stays retryable", errors.New("openrouter: http 429: rate limit"), false},
		{"vertex rate limit", errors.New("429 RESOURCE_EXHAUSTED: rate-limit"), false},
		{"method blocked is not billing", errors.New("GenerateContent are blocked"), false},
		{"ordinary 500", errors.New("google: gemini-2.5-pro http 500: boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := wrapQuota(tc.err)
			if corellm.IsQuota(got) != tc.want {
				t.Fatalf("wrapQuota(%v) quota=%v want %v", tc.err, corellm.IsQuota(got), tc.want)
			}
		})
	}
}

func TestWrapQuotaPreservesMessage(t *testing.T) {
	t.Parallel()
	got := wrapQuota(errors.New("credits are depleted"))
	if !strings.Contains(fmt.Sprint(got), "credits are depleted") {
		t.Fatalf("message lost: %v", got)
	}
}

func TestWrapHalt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		quota  bool
		config bool
		rate   bool
	}{
		{"nil", nil, false, false, false},
		{"404 is config", errors.New("google: google/gemini-3.1-flash-lite http 404: not found"), false, true, false},
		{"slash id is config", errors.New("google: google/gemini-3.1-flash-lite: vertex model id cannot contain '/'"), false, true, false},
		{"model NOT_FOUND", errors.New("google: gemini-3.1-flash-lite http 404: NOT_FOUND Publisher Model"), false, true, false},
		{"rate limit stays retryable", errors.New("openrouter: http 429: rate limit"), false, false, true},
		{"vertex rate limit", errors.New("google: gemini-2.5-pro http 429: Resource exhausted. Please try again later."), false, false, true},
		{"billing still quota", errors.New("openrouter: http 402: Insufficient credits"), true, false, false},
		{"ordinary 500", errors.New("google: gemini-2.5-pro http 500: boom"), false, false, false},
		{"vertex header timeout", errors.New(`google: gemini-2.5-pro: Post "https://example": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), false, false, true},
		{"deadline sentinel", context.DeadlineExceeded, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := wrapHalt(tc.err)
			if corellm.IsQuota(got) != tc.quota {
				t.Fatalf("wrapHalt(%v) quota=%v want %v", tc.err, corellm.IsQuota(got), tc.quota)
			}
			if corellm.IsConfig(got) != tc.config {
				t.Fatalf("wrapHalt(%v) config=%v want %v", tc.err, corellm.IsConfig(got), tc.config)
			}
			if corellm.IsRateLimit(got) != tc.rate {
				t.Fatalf("wrapHalt(%v) rate=%v want %v", tc.err, corellm.IsRateLimit(got), tc.rate)
			}
			if tc.quota || tc.config {
				if !corellm.IsHalt(got) {
					t.Fatalf("wrapHalt(%v) should halt", tc.err)
				}
			} else if corellm.IsHalt(got) {
				t.Fatalf("wrapHalt(%v) should not halt", tc.err)
			}
		})
	}
}
