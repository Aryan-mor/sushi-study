package tg

import "strings"

// StartURL is a t.me deep link that re-sends /start with a payload (docs/07: every user-facing
// link is t.me). Empty username or payload yields empty — a URL button must not point nowhere.
func StartURL(username, payload string) string {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	payload = strings.TrimSpace(payload)
	if username == "" || payload == "" {
		return ""
	}
	return "https://t.me/" + username + "?start=" + payload
}
