package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	corellm "github.com/Aryan-mor/sushi-study/be/internal/core/llm"
)

type stubBackend struct {
	calls []string
	fail  map[string]error
	ok    map[string]string
}

func (s *stubBackend) complete(_ context.Context, model, _, _ string, _ map[string]any) (string, error) {
	s.calls = append(s.calls, model)
	if err, ok := s.fail[model]; ok {
		return "", err
	}
	if out, ok := s.ok[model]; ok {
		return out, nil
	}
	return "ok", nil
}

func TestVertexFallbackDropsOpenRouterIDs(t *testing.T) {
	t.Parallel()
	if got := vertexFallback("google/gemini-3.1-flash-lite"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := vertexFallback("gemini-2.0-flash"); got != "gemini-2.0-flash" {
		t.Fatalf("got %q", got)
	}
}

func TestComplete404DoesNotCallFallback(t *testing.T) {
	t.Parallel()
	b := &stubBackend{fail: map[string]error{
		"gemini-2.5-flash-lite": errors.New("google: gemini-2.5-flash-lite http 404: NOT_FOUND"),
	}}
	c := &Client{backend: b, model: "gemini-2.5-flash-lite", fallback: "gemini-2.0-flash"}
	_, err := c.Complete(context.Background(), "", "hi", nil)
	if !corellm.IsConfig(err) {
		t.Fatalf("err=%v, want ErrConfig", err)
	}
	if len(b.calls) != 1 {
		t.Fatalf("calls=%v, want primary only (halt, no fallback)", b.calls)
	}
}

func TestCompleteKeepsPrimaryWhenFallbackFailsRetryable(t *testing.T) {
	t.Parallel()
	primary := errors.New("google: gemini-2.5-flash-lite http 429: rate-limit")
	b := &stubBackend{fail: map[string]error{
		"gemini-2.5-flash-lite": primary,
		"gemini-2.0-flash":      errors.New("google: gemini-2.0-flash http 500: boom"),
	}}
	c := &Client{backend: b, model: "gemini-2.5-flash-lite", fallback: "gemini-2.0-flash"}
	_, err := c.Complete(context.Background(), "", "hi", nil)
	if err == nil || corellm.IsHalt(err) {
		t.Fatalf("err=%v, want retryable primary", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("err=%v, want primary 429 kept", err)
	}
	if len(b.calls) != 2 {
		t.Fatalf("calls=%v", b.calls)
	}
}

func TestComplete429IsRateLimit(t *testing.T) {
	t.Parallel()
	b := &stubBackend{fail: map[string]error{
		"gemini-2.5-pro": errors.New("google: gemini-2.5-pro http 429: Resource exhausted. Please try again later."),
	}}
	c := &Client{backend: b, model: "gemini-2.5-pro"}
	_, err := c.Complete(context.Background(), "", "hi", nil)
	if !corellm.IsRateLimit(err) || corellm.IsHalt(err) {
		t.Fatalf("err=%v, want ErrRateLimit not halt", err)
	}
}

func TestCompleteFallback404Halts(t *testing.T) {
	t.Parallel()
	b := &stubBackend{fail: map[string]error{
		"gemini-2.5-flash-lite": errors.New("google: gemini-2.5-flash-lite http 500: boom"),
		"gemini-3.1-flash-lite": errors.New("google: gemini-3.1-flash-lite http 404: NOT_FOUND"),
	}}
	c := &Client{backend: b, model: "gemini-2.5-flash-lite", fallback: "gemini-3.1-flash-lite"}
	_, err := c.Complete(context.Background(), "", "hi", nil)
	if !corellm.IsConfig(err) {
		t.Fatalf("err=%v, want ErrConfig from fallback 404", err)
	}
}
