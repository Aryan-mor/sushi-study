package tg

import (
	"context"
	"fmt"
)

// InlineQuery is a typed @bot query (catalog-bot search).
type InlineQuery struct {
	ID       string `json:"id"`
	From     User   `json:"from"`
	Query    string `json:"query"`
	Offset   string `json:"offset"`
	ChatType string `json:"chat_type"`
}

// ChosenInlineResult is the pick from an inline result list.
type ChosenInlineResult struct {
	ResultID string `json:"result_id"`
	From     User   `json:"from"`
	Query    string `json:"query"`
}

// InlineQueryResultArticle is one searchable row. ThumbnailURL is image.tmdb.org (R-SEC-12).
type InlineQueryResultArticle struct {
	Type                string            `json:"type"`
	ID                  string            `json:"id"`
	Title               string            `json:"title"`
	Description         string            `json:"description,omitempty"`
	ThumbnailURL        string            `json:"thumbnail_url,omitempty"`
	ThumbnailWidth      int               `json:"thumbnail_width,omitempty"`
	ThumbnailHeight     int               `json:"thumbnail_height,omitempty"`
	InputMessageContent InlineTextMessage `json:"input_message_content"`
	ReplyMarkup         *InlineKeyboard   `json:"reply_markup,omitempty"`
}

// InlineTextMessage is the message Telegram inserts when the row is picked.
type InlineTextMessage struct {
	MessageText string `json:"message_text"`
	ParseMode   string `json:"parse_mode,omitempty"`
}

// AnswerInlineQuery replies to one typed query. CacheTime of 1 keeps a debounce from serving stale
// keystrokes; IsPersonal stops Telegram sharing one person's results with another.
func (c *Client) AnswerInlineQuery(ctx context.Context, queryID string, results []InlineQueryResultArticle, cacheTime int) error {
	payload := struct {
		InlineQueryID string                     `json:"inline_query_id"`
		Results       []InlineQueryResultArticle `json:"results"`
		CacheTime     int                        `json:"cache_time"`
		IsPersonal    bool                       `json:"is_personal"`
	}{
		InlineQueryID: queryID,
		Results:       results,
		CacheTime:     cacheTime,
		IsPersonal:    true,
	}
	if payload.Results == nil {
		payload.Results = []InlineQueryResultArticle{}
	}
	if _, err := callJSON[bool](ctx, c, "answerInlineQuery", payload); err != nil {
		return fmt.Errorf("telegram answerInlineQuery: %w", err)
	}
	return nil
}
