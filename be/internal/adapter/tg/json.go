package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (c *Client) apiURL(method string) string {
	base := strings.TrimRight(c.baseURL, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return base + "/bot" + c.token + "/" + method
}

func decodeJSONBody[T any](resp *http.Response, method string) (T, error) {
	var zero T
	var out apiResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("telegram %s: decode: %w", method, err)
	}
	if !out.OK {
		e := &Error{Code: out.ErrorCode, Description: out.Description}
		if out.Parameters != nil && out.Parameters.RetryAfter > 0 {
			e.RetryAfter = time.Duration(out.Parameters.RetryAfter) * time.Second
		}
		return zero, e
	}
	return out.Result, nil
}

// callJSON is call for nested bodies Telegram will not take as form fields — sendRichMessage's
// rich_message object is the one we have today.
func callJSON[T any](ctx context.Context, c *Client, method string, payload any) (T, error) {
	var zero T

	body, err := json.Marshal(payload)
	if err != nil {
		return zero, fmt.Errorf("telegram %s: marshal: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(method), bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("telegram %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, fmt.Errorf("telegram %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSONBody[T](resp, method)
}
