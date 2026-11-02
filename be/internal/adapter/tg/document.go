package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sendDocumentTimeout = 30 * time.Minute

// SendDocument uploads a local file with sendDocument. Telegram caps one Bot API request at
// about 50 MiB, so callers split archives into volumes below that. Caption may be empty — later
// volumes of a multi-part backup carry no caption so the first message stays the human summary.
// Returns the new message's id (0 if the caller does not need it -- decodeSendDocument still
// validates the response either way).
func (c *Client) SendDocument(ctx context.Context, chatID int64, filePath, fileName, caption string) (int64, error) {
	return c.sendDocument(ctx, chatID, filePath, fileName, caption, "")
}

// SendDocumentHTML is SendDocument with parse_mode=HTML. Subtitle-channel archives use it so
// catalog.FormatSubtitleCaption's <a>/<code> links render the same way the reader act pass does.
func (c *Client) SendDocumentHTML(ctx context.Context, chatID int64, filePath, fileName, caption string) (int64, error) {
	return c.sendDocument(ctx, chatID, filePath, fileName, caption, "HTML")
}

func (c *Client) sendDocument(ctx context.Context, chatID int64, filePath, fileName, caption, parseMode string) (int64, error) {
	id, err := c.sendDocumentOnce(ctx, chatID, filePath, fileName, caption, parseMode)
	if err == nil {
		return id, nil
	}
	var te *Error
	if !errors.As(err, &te) || !te.IsRateLimit() || te.RetryAfter <= 0 {
		return 0, err
	}
	if sleepErr := sleepCtx(ctx, te.RetryAfter); sleepErr != nil {
		return 0, err
	}
	return c.sendDocumentOnce(ctx, chatID, filePath, fileName, caption, parseMode)
}

func (c *Client) sendDocumentOnce(ctx context.Context, chatID int64, filePath, fileName, caption, parseMode string) (int64, error) {
	body, contentType, err := documentBody(chatID, filePath, fileName, caption, parseMode)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sendDocumentURL(), body)
	if err != nil {
		return 0, fmt.Errorf("telegram sendDocument: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	client := &http.Client{Timeout: sendDocumentTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("telegram sendDocument: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("telegram sendDocument: read: %w", err)
	}
	return decodeSendDocument(raw, resp.StatusCode)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) sendDocumentURL() string {
	base := strings.TrimRight(c.baseURL, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return base + "/bot" + c.token + "/sendDocument"
}

func documentBody(chatID int64, filePath, fileName, caption, parseMode string) (*bytes.Buffer, string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("telegram sendDocument: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err = w.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return nil, "", fmt.Errorf("telegram sendDocument: chat_id: %w", err)
	}
	if caption != "" {
		if err = w.WriteField("caption", caption); err != nil {
			return nil, "", fmt.Errorf("telegram sendDocument: caption: %w", err)
		}
		if parseMode != "" {
			if err = w.WriteField("parse_mode", parseMode); err != nil {
				return nil, "", fmt.Errorf("telegram sendDocument: parse_mode: %w", err)
			}
		}
	}
	part, err := w.CreateFormFile("document", filepath.Base(fileName))
	if err != nil {
		return nil, "", fmt.Errorf("telegram sendDocument: form file: %w", err)
	}
	if _, copyErr := io.Copy(part, f); copyErr != nil {
		return nil, "", fmt.Errorf("telegram sendDocument: copy: %w", copyErr)
	}
	if closeErr := w.Close(); closeErr != nil {
		return nil, "", fmt.Errorf("telegram sendDocument: close form: %w", closeErr)
	}
	return body, w.FormDataContentType(), nil
}

func decodeSendDocument(raw []byte, status int) (int64, error) {
	var payload apiResponse[Message]
	if err := json.Unmarshal(raw, &payload); err != nil {
		if status < 200 || status >= 300 {
			return 0, fmt.Errorf("telegram sendDocument failed (%d): %s", status, string(raw))
		}
		return 0, fmt.Errorf("telegram sendDocument: decode: %w", err)
	}
	if payload.OK && status >= 200 && status < 300 {
		return payload.Result.ID, nil
	}
	e := &Error{Code: payload.ErrorCode, Description: payload.Description}
	if e.Code == 0 {
		e.Code = status
	}
	if payload.Parameters != nil && payload.Parameters.RetryAfter > 0 {
		e.RetryAfter = time.Duration(payload.Parameters.RetryAfter) * time.Second
	}
	if e.Description == "" {
		e.Description = "unknown error"
	}
	return 0, e
}
