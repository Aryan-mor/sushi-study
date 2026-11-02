// Package openrouter is a thin OpenRouter chat client for the classifier's caption pass
// (ADR 0015 D3). It knows nothing about classification -- it takes a system prompt, a user
// message, and a strict JSON schema, and returns the model's JSON string, with one fallback model
// on failure. The prompt and the schema live in internal/core/classify.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	endpoint    = "https://openrouter.ai/api/v1/chat/completions"
	maxResponse = 1 << 20
	temperature = 0.2
)

// Config mirrors internal/platform/config.OpenRouter; kept separate so the adapter does not import
// the config package.
type Config struct {
	APIKey        string
	Model         string
	FallbackModel string
	Provider      string
}

// Client talks to OpenRouter. Safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a client. A caller should check cfg.APIKey != "" first (config.OpenRouter.Configured).
func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 45 * time.Second}}
}

// Complete sends one system+user exchange constrained to schema and returns the model's JSON
// string. The primary model is tried first; any failure falls back to FallbackModel when set.
func (c *Client) Complete(ctx context.Context, systemPrompt, userContent string, schema map[string]any) (string, error) {
	if c == nil || c.cfg.APIKey == "" {
		return "", fmt.Errorf("openrouter: not configured")
	}
	out, err := c.call(ctx, c.cfg.Model, systemPrompt, userContent, schema)
	if err == nil {
		return out, nil
	}
	if strings.TrimSpace(c.cfg.FallbackModel) == "" || c.cfg.FallbackModel == c.cfg.Model {
		return "", err
	}
	return c.call(ctx, c.cfg.FallbackModel, systemPrompt, userContent, schema)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	Provider       *providerRoute  `json:"provider,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type providerRoute struct {
	Order []string `json:"order,omitempty"`
}

type responseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitempty"`
}

type jsonSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) call(ctx context.Context, model, systemPrompt, userContent string, schema map[string]any) (string, error) {
	reqBody := request{
		Model:       model,
		Temperature: temperature,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		ResponseFormat: &responseFormat{
			Type:       "json_schema",
			JSONSchema: &jsonSchema{Name: "caption_parse", Strict: true, Schema: schema},
		},
	}
	if p := strings.TrimSpace(c.cfg.Provider); p != "" {
		reqBody.Provider = &providerRoute{Order: []string{p}}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openrouter: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openrouter: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openrouter: %s: %w", model, err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return "", fmt.Errorf("openrouter: read %s: %w", model, err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openrouter: %s http %d: %s", model, res.StatusCode, truncate(raw, 200))
	}

	var parsed response
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("openrouter: decode %s: %w", model, err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("openrouter: %s: %s", model, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("openrouter: %s: empty response", model)
	}
	return parsed.Choices[0].Message.Content, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
