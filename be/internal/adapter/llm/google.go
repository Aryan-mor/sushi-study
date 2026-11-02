package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	vertexScope    = "https://www.googleapis.com/auth/cloud-platform"
	maxLLMResponse = 1 << 20
	vertexTemp     = 0.2
)

type googleBackend struct {
	project  string
	location string
	http     *http.Client
	tokens   *tokenSource
}

func newGoogle(_ context.Context, cfg Config) (*googleBackend, error) {
	raw := strings.TrimSpace(cfg.VertexSAJSON)
	if raw == "" || strings.TrimSpace(cfg.VertexProject) == "" {
		return nil, fmt.Errorf("llm: google: GOOGLE_VERTEX_SA_JSON and GOOGLE_VERTEX_PROJECT required")
	}
	loc := strings.TrimSpace(cfg.VertexLocation)
	if loc == "" {
		loc = "us-central1"
	}
	httpClient := newHTTPClient()
	tokens, err := newTokenSource(raw, httpClient)
	if err != nil {
		return nil, err
	}
	return &googleBackend{
		project:  strings.TrimSpace(cfg.VertexProject),
		location: loc,
		http:     httpClient,
		tokens:   tokens,
	}, nil
}

func (g *googleBackend) complete(ctx context.Context, model, systemPrompt, userContent string, schema map[string]any) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("google: model empty")
	}
	if strings.Contains(model, "/") {
		return "", fmt.Errorf("google: %s: vertex model id cannot contain '/'", model)
	}
	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		g.location, g.project, g.location, model,
	)
	body, err := json.Marshal(vertexRequest(systemPrompt, userContent, schema))
	if err != nil {
		return "", fmt.Errorf("google: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("google: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	tok, err := g.tokens.accessToken(ctx)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	res, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("google: %s: %w", model, err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxLLMResponse))
	if err != nil {
		return "", fmt.Errorf("google: read %s: %w", model, err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google: %s http %d: %s", model, res.StatusCode, truncate(raw, 240))
	}
	text, err := vertexText(raw)
	if err != nil {
		return "", fmt.Errorf("google: %s: %w", model, err)
	}
	return text, nil
}

type vertexReq struct {
	SystemInstruction *vertexContent  `json:"systemInstruction,omitempty"`
	Contents          []vertexContent `json:"contents"`
	GenerationConfig  vertexGen       `json:"generationConfig"`
}

type vertexContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []vertexPart `json:"parts"`
}

type vertexPart struct {
	Text string `json:"text"`
}

type vertexGen struct {
	Temperature      float64        `json:"temperature"`
	ResponseMIMEType string         `json:"responseMimeType"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
}

func vertexRequest(systemPrompt, userContent string, schema map[string]any) vertexReq {
	req := vertexReq{
		Contents: []vertexContent{{Role: "user", Parts: []vertexPart{{Text: userContent}}}},
		GenerationConfig: vertexGen{
			Temperature:      vertexTemp,
			ResponseMIMEType: "application/json",
			ResponseSchema:   vertexSchema(schema),
		},
	}
	if s := strings.TrimSpace(systemPrompt); s != "" {
		req.SystemInstruction = &vertexContent{Parts: []vertexPart{{Text: s}}}
	}
	return req
}

type vertexResp struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func vertexText(raw []byte) (string, error) {
	var parsed vertexResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("%s", parsed.Error.Message)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response")
	}
	var b strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("empty response")
	}
	return out, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
