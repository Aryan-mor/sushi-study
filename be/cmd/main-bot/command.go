package main

import (
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/app/enroll"
)

// inbound is one human message, already split so a /start payload cannot leak into the command
// match. Telegram sends "/start login" and "/start <deep-link>" as a single text field; treating
// the whole line as the command was why /start with extra words looked like "wrong text".
type inbound struct {
	cmd       string // no slash: "start", "login", …
	rest      string
	token     bool
	gemini    bool
	msgID     int64
	replyToID int64
}

// directMessage is a one-person chat. Group, supergroup, and channel ids are negative.
// main-bot must stay quiet there: it is admin in the request forum, so it sees every message,
// and answering would paste the /start screen into the staff inbox.
func directMessage(chatID int64) bool {
	return chatID > 0
}

func parseInbound(text string) inbound {
	text = strings.TrimSpace(text)
	if enroll.LooksLikeToken(text) {
		return inbound{token: true, rest: text}
	}
	if looksLikeGeminiKey(text) {
		return inbound{gemini: true, rest: text}
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return inbound{}
	}

	raw := strings.ToLower(fields[0])
	if at := strings.IndexByte(raw, '@'); at > 0 {
		raw = raw[:at]
	}
	cmd := strings.TrimPrefix(raw, "/")
	if cmd == raw {
		// Prose, not a slash command. rest keeps the original so a support reply is still the text.
		return inbound{rest: text}
	}

	in := inbound{cmd: cmd}
	if len(fields) > 1 {
		in.rest = strings.Join(fields[1:], " ")
		if enroll.LooksLikeToken(in.rest) {
			in.rest = ""
		}
	}
	return in
}

// isAppCodeStart is the ?start= payload the app uses so Telegram re-sends /start every tap
// (a fixed "bot" payload is ignored once the chat already exists — the client just opens home).
func isAppCodeStart(rest string) bool {
	r := strings.ToLower(strings.TrimSpace(rest))
	return r == "bot" || r == "appcode" || strings.HasPrefix(r, "ac_")
}

// isDeleteAccountStart is the app's account-deletion deep link (docs/04 §6).
func isDeleteAccountStart(rest string) bool {
	return strings.EqualFold(strings.TrimSpace(rest), "delete_account")
}

// isDownloadStart is the in-app update fallback (`?start=download`). Same picker as /download.
func isDownloadStart(rest string) bool {
	return strings.EqualFold(strings.TrimSpace(rest), "download")
}
