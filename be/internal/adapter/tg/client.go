// Package tg is a minimal Telegram Bot API client: the three calls the protocol transport needs
// and nothing else.
//
// Hand-written rather than pulled in, for now. The surface is getMe, getUpdates and sendMessage;
// a library would bring a few hundred methods we do not use and would still need wrapping for the
// rate limiting in docs/02 §6. Revisit when deliveryd needs copyMessage and file downloads
// (R-REPO-8).
//
// Long polling, never webhooks: no inbound port, no TLS, no domain, nothing of ours reachable from
// the internet (docs/01, the invisibility rule).
package tg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one bot.
type Client struct {
	token    string
	username string
	http     *http.Client
	// baseURL overrides api.telegram.org in tests. Empty means production.
	baseURL string
}

// New builds a client for a bot token.
func New(token string) *Client {
	return &Client{
		token: token,
		// Long polling holds a request open for the poll timeout, so this must exceed it.
		http: &http.Client{Timeout: 90 * time.Second},
	}
}

// Username is the handle Telegram reports, once Me has been called.
func (c *Client) Username() string { return c.username }

// User is the identity behind a token.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"first_name"`
	// Language is Telegram's IETF tag for the user's client, e.g. "fa" or "en-GB". Advisory: it
	// says what their Telegram is set to, not what they want to read.
	Language string `json:"language_code"`
}

// Chat is where a message happened.
type Chat struct {
	ID int64 `json:"id"`
}

// Message is the subset of a Telegram message the protocol reads.
type Message struct {
	ID                   int64           `json:"message_id"`
	From                 *User           `json:"from"`
	Chat                 Chat            `json:"chat"`
	Text                 string          `json:"text"`
	Date                 int64           `json:"date"`
	Caption              string          `json:"caption"`
	Document             *Document       `json:"document"`
	Video                *Video          `json:"video"`
	ReplyTo              *Message        `json:"reply_to_message"`
	ForwardOrigin        json.RawMessage `json:"forward_origin"`
	ForwardFromChat      *Chat           `json:"forward_from_chat"`
	ForwardFromMessageID int64           `json:"forward_from_message_id"`
}

// Document is a file attached to a message (APK, installer, a video sent as a file, …).
type Document struct {
	FileName     string `json:"file_name"`
	FileUniqueID string `json:"file_unique_id"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
}

// Update is one polled event. ChannelPost is a message in a channel the bot administers -- a
// moderator's reply to a review card arrives here, not in Message.
type Update struct {
	ID           int64               `json:"update_id"`
	Message      *Message            `json:"message"`
	ChannelPost  *Message            `json:"channel_post"`
	Callback     *CallbackQuery      `json:"callback_query"`
	InlineQuery  *InlineQuery        `json:"inline_query"`
	ChosenInline *ChosenInlineResult `json:"chosen_inline_result"`
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// Error is a failure Telegram reported, as opposed to a transport failure.
type Error struct {
	Code        int
	Description string
	// RetryAfter is how long Telegram says to wait, for 429s. Honour it exactly rather than
	// guessing at a backoff (docs/02 §6).
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}

// IsRateLimit reports whether this is a 429.
func (e *Error) IsRateLimit() bool { return e.Code == http.StatusTooManyRequests }

// IsUnauthorized reports the 401 Telegram returns for every call once a token is revoked or the
// bot account itself is gone -- the signal a bot is actually banned/dead, not just having a bad
// moment (docs/02 §7).
func (e *Error) IsUnauthorized() bool { return e.Code == http.StatusUnauthorized }

// IsChatNotFound reports the specific 400 Telegram returns for a chat id it cannot resolve --
// exactly what happens the first time one bot ever addresses another by numeric id, before any
// message has established that peer (confirmed live 2026-08-26: a bare bot-to-bot chat id fails
// this way even with BotFather's Bot-to-Bot mode on both sides; addressing the same chat by
// @username succeeds immediately, on the very first message, in either direction). This is the
// case CopyMessageToUsername exists for.
func (e *Error) IsChatNotFound() bool {
	return e.Code == http.StatusBadRequest && strings.Contains(e.Description, "chat not found")
}

// IsNotModified reports the 400 Telegram returns when editMessageText is a no-op — same text and
// same buttons. Treating it as success keeps a second tap on Home from looking like a dead button.
func (e *Error) IsNotModified() bool {
	return e.Code == http.StatusBadRequest && strings.Contains(strings.ToLower(e.Description), "message is not modified")
}

// dropNotModified turns Telegram's no-op 400 into success. editMessageMedia / Caption / Text all
// return it when the card already has this content (a second Fix reply with the same id).
func dropNotModified(err error) error {
	var te *Error
	if errors.As(err, &te) && te.IsNotModified() {
		return nil
	}
	return err
}

// IsForbidden reports a bare 403 -- Telegram refused the call outright (bot blocked, kicked, not
// a member, messaging another bot without Bot-to-Bot mode). Narrower predicates below classify the
// common cases; use this only where the surrounding call makes one meaning the sole possibility.
func (e *Error) IsForbidden() bool { return e.Code == http.StatusForbidden }

// IsBotToBotDenied reports the error Telegram returns when a bot without Bot-to-Bot
// Communication Mode (BotFather) tries to message another bot -- the ADR 0013 enrolment probe.
//
// Observed live 2026-09-02: "400 Bad Request: USER_BOT_TO_BOT_DISABLED" -- a 400, not a 403, and
// the only reliable signal is that token, not the HTTP code. Older/other phrasings seen are
// 403s reading "bots can't send messages to bots" / "... to other bots" / "bot to bot ...".
func (e *Error) IsBotToBotDenied() bool {
	d := strings.ToLower(e.Description)
	if strings.Contains(d, "user_bot_to_bot_disabled") {
		return true
	}
	if e.Code != http.StatusForbidden {
		return false
	}
	return strings.Contains(d, "send messages to bot") ||
		strings.Contains(d, "other bots") ||
		strings.Contains(d, "bot to bot")
}

func call[T any](ctx context.Context, c *Client, method string, params url.Values) (T, error) {
	var zero T

	endpoint := "https://api.telegram.org/bot" + c.token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return zero, fmt.Errorf("telegram %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, fmt.Errorf("telegram %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

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

// Me identifies the bot behind the token and caches its handle.
//
// Every pool bot is verified this way at boot: the configured name is not evidence, getMe is
// (R-SEC-17). Dev has already produced a pool whose handles and configured names disagreed, and a
// token that was simply dead.
func (c *Client) Me(ctx context.Context) (User, error) {
	u, err := call[User](ctx, c, "getMe", nil)
	if err != nil {
		return User{}, err
	}
	c.username = u.Username
	return u, nil
}

// GetUpdates long-polls for events after offset.
//
// One consumer per token, ever. Two processes polling the same bot silently steal each other's
// updates, which is the worst failure mode of this design (R-OPS-3).
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	params := url.Values{}
	params.Set("offset", strconv.FormatInt(offset, 10))
	params.Set("timeout", strconv.Itoa(int(timeout.Seconds())))
	// DM messages, channel posts (a moderator's reply to a review card), button presses, and
	// catalog-bot's inline search. Bots that do not handle inline updates ignore them.
	params.Set("allowed_updates", `["message","channel_post","callback_query","inline_query","chosen_inline_result"]`)

	return call[[]Update](ctx, c, "getUpdates", params)
}

// SendMessage sends plain text.
//
// No parse mode: protocol answers are base64 and prose is not sent from here, so there is nothing
// to escape and nothing that can be mangled by Telegram's markdown (R-SEC-10).
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (Message, error) {
	return c.sendMessage(ctx, chatID, text, "")
}

// SendHTML sends text with parse_mode=HTML. Used for bot-login success copy (ADR 0013), never for
// protocol payloads.
func (c *Client) SendHTML(ctx context.Context, chatID int64, text string) (Message, error) {
	return c.sendMessage(ctx, chatID, text, "HTML")
}

// SendPhotoHTML sends a photo (by URL — Telegram fetches it) with an HTML caption. Used for the
// content-request admin alert (ADR 0014 §D4); the caller falls back to SendHTML when there is no
// poster or Telegram rejects the URL.
func (c *Client) SendPhotoHTML(ctx context.Context, chatID int64, photoURL, caption string) (Message, error) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("photo", photoURL)
	params.Set("caption", caption)
	params.Set("parse_mode", "HTML")
	params.Set("disable_notification", "true")
	return call[Message](ctx, c, "sendPhoto", params)
}

// SendReplyHTML posts a short HTML message as a reply to another. The review runner uses it to
// acknowledge a moderator's correction reply (a reaction is unreliable in a broadcast channel).
func (c *Client) SendReplyHTML(ctx context.Context, chatID, replyToID int64, text string) (Message, error) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)
	params.Set("parse_mode", "HTML")
	params.Set("disable_notification", "true")
	params.Set("disable_web_page_preview", "true")
	params.Set("reply_parameters", `{"message_id":`+strconv.FormatInt(replyToID, 10)+`}`)
	return call[Message](ctx, c, "sendMessage", params)
}

// SendForceReplyHTML asks the person to reply to this message (Telegram's input becomes a reply).
// Used for BotFather tokens so they are not typed into a glass-button chat (the prototype's walkthrough).
func (c *Client) SendForceReplyHTML(ctx context.Context, chatID int64, text, placeholder string) (Message, error) {
	markup, err := marshalForceReply(placeholder)
	if err != nil {
		return Message{}, fmt.Errorf("telegram: marshal force reply: %w", err)
	}

	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)
	params.Set("parse_mode", "HTML")
	params.Set("reply_markup", string(markup))
	params.Set("disable_notification", "true")

	return call[Message](ctx, c, "sendMessage", params)
}

// SendMessageToUsername sends plain text to a chat addressed by @handle. Same first-contact
// reason as CopyMessageToUsername: a numeric id fails until a message has established the peer.
func (c *Client) SendMessageToUsername(ctx context.Context, username, text string) (Message, error) {
	params := url.Values{}
	params.Set("chat_id", "@"+strings.TrimPrefix(username, "@"))
	params.Set("text", text)
	params.Set("disable_notification", "true")
	return call[Message](ctx, c, "sendMessage", params)
}

func (c *Client) sendMessage(ctx context.Context, chatID int64, text, parseMode string) (Message, error) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)
	params.Set("disable_notification", "true")
	if parseMode != "" {
		params.Set("parse_mode", parseMode)
	}

	return call[Message](ctx, c, "sendMessage", params)
}

// InlineButton is one glass button. Primary navigation is buttons, never "type this command"
// (doc 13 §5). CopyText is Telegram's clipboard button (Bot API CopyTextButton); omit Data
// when it is set — callback_data is 64 bytes, a bot-login code is not.
//
// Style tints the button. Telegram accepts exactly the three values below and falls back to the
// client's default grey when it is empty, so an unstyled row stays valid — which is why the field
// is a plain string with constants rather than an enum that would need a zero value spelling
// "unset". Old Telegram clients ignore it, so a styled keyboard is never a hard dependency.
type InlineButton struct {
	Text                         string    `json:"text"`
	Data                         string    `json:"callback_data,omitempty"`
	URL                          string    `json:"url,omitempty"`
	SwitchInlineQuery            string    `json:"switch_inline_query,omitempty"`
	SwitchInlineQueryCurrentChat string    `json:"switch_inline_query_current_chat,omitempty"`
	CopyText                     *CopyText `json:"copy_text,omitempty"`
	Style                        string    `json:"style,omitempty"`
}

// The three button tints Telegram renders (Bot API InlineKeyboardButton.style). Use them to say
// what a button *is*, not to decorate: one primary per screen (the thing we want pressed), success
// for a confirmed-good outcome, danger for anything destructive.
const (
	StylePrimary = "primary" // blue — the one action this screen exists for
	StyleSuccess = "success" // green — completes something, safely
	StyleDanger  = "danger"  // red — destroys data or revokes access
)

// CopyText is the payload for InlineButton.CopyText. Telegram caps text at 256 characters.
type CopyText struct {
	Text string `json:"text"`
}

// InlineKeyboard is rows of buttons, attached under a message.
type InlineKeyboard struct {
	Rows [][]InlineButton `json:"inline_keyboard"`
}

// CallbackQuery is a button press.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// SendKeyboard sends text with buttons under it.
func (c *Client) SendKeyboard(ctx context.Context, chatID int64, text string, kb InlineKeyboard) (Message, error) {
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
	params.Set("disable_web_page_preview", "true") // a menu never wants a link-preview card over its buttons

	return call[Message](ctx, c, "sendMessage", params)
}

func attachKeyboard(params url.Values, kb InlineKeyboard) error {
	if kb.Rows == nil {
		kb.Rows = [][]InlineButton{}
	}
	markup, err := json.Marshal(kb)
	if err != nil {
		return fmt.Errorf("telegram: marshal keyboard: %w", err)
	}
	params.Set("reply_markup", string(markup))
	return nil
}

// EditKeyboard replaces a message's text and buttons.
//
// Editing beats sending: an onboarding flow that answers each tap with a new message leaves the
// chat full of dead menus.
func (c *Client) EditKeyboard(ctx context.Context, chatID, messageID int64, text string, kb InlineKeyboard) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("message_id", strconv.FormatInt(messageID, 10))
	params.Set("text", text)
	params.Set("parse_mode", "HTML")
	params.Set("disable_web_page_preview", "true") // a menu never wants a link-preview card over its buttons
	if err := attachKeyboard(params, kb); err != nil {
		return err
	}

	_, err := call[json.RawMessage](ctx, c, "editMessageText", params)
	return dropNotModified(err)
}

// SendPhotoKeyboard posts a photo (by URL) with an HTML caption and buttons under it. The review
// channel uses it so a moderator sees the poster without opening anything.
func (c *Client) SendPhotoKeyboard(ctx context.Context, chatID int64, photoURL, caption string, kb InlineKeyboard) (Message, error) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("photo", photoURL)
	params.Set("caption", caption)
	params.Set("parse_mode", "HTML")
	params.Set("disable_notification", "true")
	if err := attachKeyboard(params, kb); err != nil {
		return Message{}, err
	}
	return call[Message](ctx, c, "sendPhoto", params)
}

// SetReaction puts a single emoji reaction on a message (empty emoji clears it). Best-effort
// acknowledgement -- the review runner thumbs-up a moderator's correction reply.
func (c *Client) SetReaction(ctx context.Context, chatID, messageID int64, emoji string) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("message_id", strconv.FormatInt(messageID, 10))
	if emoji != "" {
		params.Set("reaction", `[{"type":"emoji","emoji":"`+emoji+`"}]`)
	} else {
		params.Set("reaction", "[]")
	}
	_, err := call[json.RawMessage](ctx, c, "setMessageReaction", params)
	return err
}

// EditCaptionKeyboard replaces a photo message's caption and buttons in one call. The text-message
// twin is EditKeyboard; a photo message needs editMessageCaption, not editMessageText.
func (c *Client) EditCaptionKeyboard(ctx context.Context, chatID, messageID int64, caption string, kb InlineKeyboard) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("message_id", strconv.FormatInt(messageID, 10))
	params.Set("caption", caption)
	params.Set("parse_mode", "HTML")
	if err := attachKeyboard(params, kb); err != nil {
		return err
	}
	_, err := call[json.RawMessage](ctx, c, "editMessageCaption", params)
	return dropNotModified(err)
}

// EditMediaKeyboard replaces a photo (or turns a text card into one) and its caption + buttons.
// Used when a Fix reply points at a different TMDB title whose poster must change in place.
func (c *Client) EditMediaKeyboard(ctx context.Context, chatID, messageID int64, photoURL, caption string, kb InlineKeyboard) error {
	media, err := json.Marshal(map[string]string{
		"type":       "photo",
		"media":      photoURL,
		"caption":    caption,
		"parse_mode": "HTML",
	})
	if err != nil {
		return fmt.Errorf("telegram: marshal input media: %w", err)
	}
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("message_id", strconv.FormatInt(messageID, 10))
	params.Set("media", string(media))
	if err := attachKeyboard(params, kb); err != nil {
		return err
	}
	_, callErr := call[json.RawMessage](ctx, c, "editMessageMedia", params)
	return dropNotModified(callErr)
}

// DeleteMessage removes a message the bot sent or received. Used to scrub a bot-login token out of
// the chat the instant it has been verified and bound (docs/04 §3) -- a secret must not linger
// where a screenshot could leak it.
func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("message_id", strconv.FormatInt(messageID, 10))

	_, err := call[json.RawMessage](ctx, c, "deleteMessage", params)
	return err
}

// AnswerCallback clears the spinner on a tapped button. Telegram shows it for a few seconds if
// nobody answers, which reads as a hang.
func (c *Client) AnswerCallback(ctx context.Context, id string) error {
	return c.AnswerCallbackText(ctx, id, "")
}

// AnswerCallbackText answers a button press with a short toast (Telegram caps it at 200 chars).
// Empty text just dismisses the loading spinner.
func (c *Client) AnswerCallbackText(ctx context.Context, id, text string) error {
	params := url.Values{}
	params.Set("callback_query_id", id)
	if text != "" {
		params.Set("text", text)
	}

	_, err := call[json.RawMessage](ctx, c, "answerCallbackQuery", params)
	return err
}

// DeleteWebhook drops a leftover webhook so getUpdates can run. A webhook left on the token
// makes long-polling 409, which looks like a bot that never answers anyone.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	_, err := call[bool](ctx, c, "deleteWebhook", nil)
	return err
}

// BotCommand is one entry in Telegram's menu drawer (setMyCommands).
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands replaces the menu-drawer list. languageCode empty is the default list; "en" and
// "fa" are the per-client-language lists Telegram picks from the user's app language.
func (c *Client) SetMyCommands(ctx context.Context, cmds []BotCommand, languageCode string) error {
	body, err := json.Marshal(cmds)
	if err != nil {
		return fmt.Errorf("telegram: marshal commands: %w", err)
	}
	params := url.Values{}
	params.Set("commands", string(body))
	if languageCode != "" {
		params.Set("language_code", languageCode)
	}
	_, err = call[bool](ctx, c, "setMyCommands", params)
	return err
}

// SetMyCommandsForChat replaces this private chat's command drawer. Staff chats get extra
// entries the default list does not — /admin opens the staff hub; it is not /promote.
func (c *Client) SetMyCommandsForChat(ctx context.Context, chatID int64, cmds []BotCommand) error {
	body, err := json.Marshal(cmds)
	if err != nil {
		return fmt.Errorf("telegram: marshal commands: %w", err)
	}
	params := url.Values{}
	params.Set("commands", string(body))
	params.Set("scope", fmt.Sprintf(`{"type":"chat","chat_id":%d}`, chatID))
	_, err = call[bool](ctx, c, "setMyCommands", params)
	return err
}

// SetChatMenuButton puts the commands drawer on this private chat's menu button, rather than a
// web-app button Telegram sometimes leaves behind from an earlier BotFather edit.
func (c *Client) SetChatMenuButton(ctx context.Context, chatID int64) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("menu_button", `{"type":"commands"}`)
	_, err := call[bool](ctx, c, "setChatMenuButton", params)
	return err
}
