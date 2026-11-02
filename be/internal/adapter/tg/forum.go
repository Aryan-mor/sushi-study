package tg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ForumTopic is the slice of createForumTopic's result this process stores.
type ForumTopic struct {
	MessageThreadID int64  `json:"message_thread_id"`
	Name            string `json:"name"`
}

// CreateForumTopic opens a topic in a forum supergroup. The bot must be an admin with
// can_manage_topics. iconColor is one of Telegram's six topic colours; 0 leaves the default.
func (c *Client) CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int) (ForumTopic, error) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("name", name)
	if iconColor != 0 {
		params.Set("icon_color", strconv.Itoa(iconColor))
	}
	return call[ForumTopic](ctx, c, "createForumTopic", params)
}

// CopyMessageToThread copies a message into a forum topic of the same or another chat.
// An HTML caption replaces the original; an empty caption keeps it. Inline buttons are not copied.
func (c *Client) CopyMessageToThread(ctx context.Context, chatID, messageID, threadID int64, caption string) (int64, error) {
	params := copyMessageParams(strconv.FormatInt(chatID, 10), chatID, messageID, caption, "HTML", false)
	setMessageThread(params, threadID)
	msg, err := call[Message](ctx, c, "copyMessage", params)
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

// MoveToForumTopic copies a message into a topic and deletes the source. Telegram has no
// move-message method; this is the move. A source that is already gone counts as success so a
// retry after a partial move does not fail the close. A delete that fails after the copy
// succeeded is also success: returning that error would copy the card a second time.
func (c *Client) MoveToForumTopic(ctx context.Context, chatID, messageID, threadID int64, caption string) error {
	if _, err := c.CopyMessageToThread(ctx, chatID, messageID, threadID, caption); err != nil {
		if MessageMissing(err) {
			return nil
		}
		return err
	}
	c.forgetMessage(ctx, chatID, messageID)
	return nil
}

// forgetMessage deletes a message and drops the error. The copy already landed in the
// destination topic; a failed delete must not make the caller copy it again.
func (c *Client) forgetMessage(ctx context.Context, chatID, messageID int64) {
	err := c.DeleteMessage(ctx, chatID, messageID)
	if err == nil || MessageMissing(err) {
		return
	}
}

func setMessageThread(params url.Values, threadID int64) {
	if threadID > 0 {
		params.Set("message_thread_id", strconv.FormatInt(threadID, 10))
	}
}

// MessageMissing reports a 400 for a message Telegram can no longer see.
func MessageMissing(err error) bool {
	var te *Error
	return errors.As(err, &te) && te.IsMessageMissing()
}

// IsMessageMissing reports the 400 Telegram returns when the message id is already gone.
func (e *Error) IsMessageMissing() bool {
	if e.Code != 400 {
		return false
	}
	d := strings.ToLower(e.Description)
	return strings.Contains(d, "message to copy not found") ||
		strings.Contains(d, "message to delete not found") ||
		strings.Contains(d, "message to be copied not found") ||
		strings.Contains(d, "message_id_invalid") ||
		strings.Contains(d, "message can't be found")
}

// SendKeyboardThread is SendKeyboard into a forum topic. threadID 0 posts to the general topic.
func (c *Client) SendKeyboardThread(ctx context.Context, chatID, threadID int64, text string, kb InlineKeyboard) (Message, error) {
	markup, err := json.Marshal(kb)
	if err != nil {
		return Message{}, fmt.Errorf("telegram: marshal keyboard: %w", err)
	}
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)
	params.Set("parse_mode", "HTML")
	params.Set("reply_markup", string(markup))
	params.Set("disable_notification", "true")
	params.Set("disable_web_page_preview", "true")
	setMessageThread(params, threadID)
	return call[Message](ctx, c, "sendMessage", params)
}

// SendPhotoKeyboardThread is SendPhotoKeyboard into a forum topic.
func (c *Client) SendPhotoKeyboardThread(ctx context.Context, chatID, threadID int64, photoURL, caption string, kb InlineKeyboard) (Message, error) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("photo", photoURL)
	params.Set("caption", caption)
	params.Set("parse_mode", "HTML")
	params.Set("disable_notification", "true")
	setMessageThread(params, threadID)
	if err := attachKeyboard(params, kb); err != nil {
		return Message{}, err
	}
	return call[Message](ctx, c, "sendPhoto", params)
}
