package tg

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// CopyMessage copies an existing message between chats the bot can see. Telegram holds the bytes;
// we only learn the new message id. Clone and backup copies stay unprotected so ops can save them.
//
// The caption is sent as HTML: this is the path fetcherd stamps the storage caption on
// (catalog.FormatStorageCaption), and that caption links the word "tmdb" and wraps ids in <code>.
// An empty caption (backupd) makes parse_mode a no-op, so the original caption rides along intact.
func (c *Client) CopyMessage(ctx context.Context, destChat, sourceChat, messageID int64, caption string) (int64, error) {
	return c.copyMessageTo(ctx, strconv.FormatInt(destChat, 10), sourceChat, messageID, caption, "HTML", false)
}

// CopyProtectedMessage is CopyMessage with protect_content. Reader DMs only (docs/05 §3): official
// clients cannot forward or save the file. Not DRM — screenshots and modified clients still work.
// The caption here is the plain locator, so no parse mode.
func (c *Client) CopyProtectedMessage(ctx context.Context, destChat, sourceChat, messageID int64, caption string) (int64, error) {
	return c.copyMessageTo(ctx, strconv.FormatInt(destChat, 10), sourceChat, messageID, caption, "", true)
}

// CopyMessageToUsername copies into a chat addressed by @handle rather than by numeric id.
//
// A numeric chat id only resolves once this bot has exchanged at least one message with that
// chat; a bare id fails with Error.IsChatNotFound for a peer it has never seen, even with
// Bot-to-Bot mode on both sides -- confirmed live 2026-08-26. @handle resolves on the very first
// contact, in either direction. This is deliveryd's fallback: unlike the API pool, which the
// client warms in full (ADR 0011), a delivery bot is picked from the pool per copy, so the
// client can never know in advance which one to warm.
func (c *Client) CopyMessageToUsername(ctx context.Context, username string, sourceChat, messageID int64, caption string) (int64, error) {
	return c.copyMessageTo(ctx, "@"+strings.TrimPrefix(username, "@"), sourceChat, messageID, caption, "", false)
}

// CopyProtectedMessageToUsername is the protected form of CopyMessageToUsername (delivery first-contact).
func (c *Client) CopyProtectedMessageToUsername(ctx context.Context, username string, sourceChat, messageID int64, caption string) (int64, error) {
	return c.copyMessageTo(ctx, "@"+strings.TrimPrefix(username, "@"), sourceChat, messageID, caption, "", true)
}

func copyMessageParams(destChat string, sourceChat, messageID int64, caption, parseMode string, protect bool) url.Values {
	params := url.Values{}
	params.Set("chat_id", destChat)
	params.Set("from_chat_id", strconv.FormatInt(sourceChat, 10))
	params.Set("message_id", strconv.FormatInt(messageID, 10))
	params.Set("disable_notification", "true")
	if caption != "" {
		params.Set("caption", caption)
		if parseMode != "" {
			params.Set("parse_mode", parseMode)
		}
	}
	if protect {
		params.Set("protect_content", "true")
	}
	return params
}

func (c *Client) copyMessageTo(ctx context.Context, destChat string, sourceChat, messageID int64, caption, parseMode string, protect bool) (int64, error) {
	msg, err := call[Message](ctx, c, "copyMessage", copyMessageParams(destChat, sourceChat, messageID, caption, parseMode, protect))
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

// EditCaption replaces a channel/DM caption. Used to stamp Sushi's storage caption on a copy that
// already exists.
func (c *Client) EditCaption(ctx context.Context, chatID, messageID int64, caption string) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("message_id", strconv.FormatInt(messageID, 10))
	params.Set("caption", caption)

	_, err := call[json.RawMessage](ctx, c, "editMessageCaption", params)
	return err
}
