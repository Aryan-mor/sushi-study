package tg

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

func marshalForceReply(placeholder string) ([]byte, error) {
	return json.Marshal(struct {
		ForceReply  bool   `json:"force_reply"`
		Placeholder string `json:"input_field_placeholder,omitempty"`
		Selective   bool   `json:"selective"`
	}{
		ForceReply:  true,
		Placeholder: placeholder,
		Selective:   true,
	})
}

// SendHTMLDismissReply sends HTML and clears ForceReply / reply keyboards so the composer is not
// stuck "replying" to the BotFather walkthrough.
func (c *Client) SendHTMLDismissReply(ctx context.Context, chatID int64, text string) (Message, error) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)
	params.Set("parse_mode", "HTML")
	params.Set("reply_markup", `{"remove_keyboard":true,"selective":true}`)
	params.Set("disable_notification", "true")

	return call[Message](ctx, c, "sendMessage", params)
}
