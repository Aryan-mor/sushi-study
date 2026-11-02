// Package llm is the indexer LLM transport (ADR 0025). Callers pick a model with Named; the rest
// of indexerd does not know whether the bytes go to Vertex or OpenRouter.
package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/openrouter"
	corellm "github.com/Aryan-mor/sushi-study/be/internal/core/llm"
)

const (
	providerGoogle     = "google"
	providerOpenRouter = "openrouter"
	httpTimeout        = 120 * time.Second
)

// Config is the boot-time route. Provider is "google" or "openrouter". Model fields are provider
// ids (Vertex model id, or OpenRouter "org/model").
type Config struct {
	Provider string

	ClassifyModel string
	RefereeModel  string
	VerifierModel string
	FallbackModel string

	VertexProject  string
	VertexLocation string
	VertexSAJSON   string

	OpenRouterAPIKey   string
	OpenRouterProvider string
}

// Client talks to one backend. Safe for concurrent use. Named returns a view bound to one model.
type Client struct {
	backend  backend
	model    string
	fallback string
}

type backend interface {
	complete(ctx context.Context, model, systemPrompt, userContent string, schema map[string]any) (string, error)
}

// New builds a client for cfg.Provider. A missing secret is a boot error (R-GO-9).
func New(ctx context.Context, cfg Config) (*Client, error) {
	p := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch p {
	case providerGoogle:
		b, err := newGoogle(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return &Client{backend: b, fallback: vertexFallback(cfg.FallbackModel)}, nil
	case providerOpenRouter:
		if strings.TrimSpace(cfg.OpenRouterAPIKey) == "" {
			return nil, fmt.Errorf("llm: openrouter: API key missing")
		}
		return &Client{
			backend: &openRouterBackend{cfg: openrouter.Config{
				APIKey:   cfg.OpenRouterAPIKey,
				Provider: cfg.OpenRouterProvider,
			}},
			fallback: strings.TrimSpace(cfg.FallbackModel),
		}, nil
	default:
		return nil, fmt.Errorf("llm: unknown provider %q (want google or openrouter)", cfg.Provider)
	}
}

// Named binds this client to model. Empty model is a complete-time error.
func (c *Client) Named(model string) *Client {
	if c == nil {
		return nil
	}
	out := *c
	out.model = strings.TrimSpace(model)
	return &out
}

// Complete implements classify.LLM.
func (c *Client) Complete(ctx context.Context, systemPrompt, userContent string, schema map[string]any) (string, error) {
	if c == nil || c.backend == nil {
		return "", fmt.Errorf("llm: not configured")
	}
	if c.model == "" {
		return "", fmt.Errorf("llm: model empty")
	}
	out, err := c.backend.complete(ctx, c.model, systemPrompt, userContent, schema)
	if err == nil {
		return out, nil
	}
	err = wrapHalt(err)
	fb := c.usableFallback()
	if corellm.IsHalt(err) || fb == "" {
		return "", err
	}
	out, ferr := c.backend.complete(ctx, fb, systemPrompt, userContent, schema)
	if ferr != nil {
		ferr = wrapHalt(ferr)
		if corellm.IsHalt(ferr) {
			return "", ferr
		}
		return "", err
	}
	return out, nil
}

func (c *Client) usableFallback() string {
	fb := strings.TrimSpace(c.fallback)
	if fb == "" || fb == c.model {
		return ""
	}
	if _, ok := c.backend.(*googleBackend); ok {
		return vertexFallback(fb)
	}
	return fb
}

// vertexFallback drops OpenRouter org/model ids. Sending those to Vertex generateContent 404s and
// used to replace a retryable primary error with a review-channel flood.
func vertexFallback(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "/") {
		return ""
	}
	return s
}

type openRouterBackend struct {
	cfg openrouter.Config
}

func (b *openRouterBackend) complete(ctx context.Context, model, systemPrompt, userContent string, schema map[string]any) (string, error) {
	c := openrouter.New(openrouter.Config{
		APIKey:   b.cfg.APIKey,
		Model:    model,
		Provider: b.cfg.Provider,
	})
	return c.Complete(ctx, systemPrompt, userContent, schema)
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: httpTimeout}
}
