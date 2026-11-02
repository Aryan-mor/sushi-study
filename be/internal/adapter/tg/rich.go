package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
)

type richMedia struct {
	Type  string `json:"type"`
	Media string `json:"media"`
}

type richMediaItem struct {
	ID    string    `json:"id"`
	Media richMedia `json:"media"`
}

type richMessage struct {
	HTML  string          `json:"html"`
	Media []richMediaItem `json:"media,omitempty"`
}

type sendRichBody struct {
	ChatID              int64           `json:"chat_id"`
	RichMessage         richMessage     `json:"rich_message"`
	DisableNotification bool            `json:"disable_notification"`
	ReplyMarkup         json.RawMessage `json:"reply_markup,omitempty"`
}

type editRichBody struct {
	ChatID      int64           `json:"chat_id"`
	MessageID   int64           `json:"message_id"`
	RichMessage richMessage     `json:"rich_message"`
	ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
}

const heroAttachName = "hero"

// SendRichKeyboard posts Telegram rich HTML (h3, tg-slideshow, details) with buttons under it.
// Used by the announce channel so a post can carry the prototype's slider + accordion without R2
// (ADR 0017 §6). heroJPEG is the optional slide-0 card, sent as attach://hero. Callers fall back
// to SendPhotoKeyboard when this is refused.
func (c *Client) SendRichKeyboard(ctx context.Context, chatID int64, html string, kb InlineKeyboard, heroJPEG []byte) (Message, error) {
	body := sendRichBody{
		ChatID:              chatID,
		RichMessage:         richMessage{HTML: html},
		DisableNotification: true,
	}
	if len(kb.Rows) > 0 {
		markup, err := json.Marshal(kb)
		if err != nil {
			return Message{}, fmt.Errorf("telegram: marshal keyboard: %w", err)
		}
		body.ReplyMarkup = markup
	}
	if len(heroJPEG) == 0 {
		return callJSON[Message](ctx, c, "sendRichMessage", body)
	}
	body.RichMessage.Media = []richMediaItem{{
		ID:    heroAttachName,
		Media: richMedia{Type: "photo", Media: "attach://" + heroAttachName},
	}}
	return c.sendRichMultipart(ctx, body, heroJPEG)
}

func (c *Client) sendRichMultipart(ctx context.Context, body sendRichBody, heroJPEG []byte) (Message, error) {
	var zero Message
	payload, err := json.Marshal(body.RichMessage)
	if err != nil {
		return zero, fmt.Errorf("telegram sendRichMessage: marshal: %w", err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err = writeRichMultipart(w, body, payload, heroJPEG); err != nil {
		return zero, err
	}
	if err = w.Close(); err != nil {
		return zero, fmt.Errorf("telegram sendRichMessage: close form: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL("sendRichMessage"), &buf)
	if err != nil {
		return zero, fmt.Errorf("telegram sendRichMessage: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, fmt.Errorf("telegram sendRichMessage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSONBody[Message](resp, "sendRichMessage")
}

func writeRichMultipart(w *multipart.Writer, body sendRichBody, payload, heroJPEG []byte) error {
	if err := w.WriteField("chat_id", strconv.FormatInt(body.ChatID, 10)); err != nil {
		return fmt.Errorf("telegram sendRichMessage: chat_id: %w", err)
	}
	if err := w.WriteField("rich_message", string(payload)); err != nil {
		return fmt.Errorf("telegram sendRichMessage: rich_message: %w", err)
	}
	if err := w.WriteField("disable_notification", "true"); err != nil {
		return fmt.Errorf("telegram sendRichMessage: disable_notification: %w", err)
	}
	if len(body.ReplyMarkup) > 0 {
		if err := w.WriteField("reply_markup", string(body.ReplyMarkup)); err != nil {
			return fmt.Errorf("telegram sendRichMessage: reply_markup: %w", err)
		}
	}
	part, err := w.CreateFormFile(heroAttachName, "hero.jpg")
	if err != nil {
		return fmt.Errorf("telegram sendRichMessage: form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(heroJPEG)); err != nil {
		return fmt.Errorf("telegram sendRichMessage: copy: %w", err)
	}
	return nil
}

// EditRichKeyboard replaces a rich message's HTML and buttons. The photo-caption twin is
// EditCaptionKeyboard; a rich post is editMessageText with rich_message, not a caption.
func (c *Client) EditRichKeyboard(ctx context.Context, chatID, messageID int64, html string, kb InlineKeyboard) error {
	body := editRichBody{
		ChatID:      chatID,
		MessageID:   messageID,
		RichMessage: richMessage{HTML: html},
	}
	if len(kb.Rows) > 0 {
		markup, err := json.Marshal(kb)
		if err != nil {
			return fmt.Errorf("telegram: marshal keyboard: %w", err)
		}
		body.ReplyMarkup = markup
	}
	_, err := callJSON[json.RawMessage](ctx, c, "editMessageText", body)
	return err
}
