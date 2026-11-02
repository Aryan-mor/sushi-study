package main

import (
	"strings"
	"unicode"
)

// looksLikeGeminiKey is a shape check, not a validator. AI Studio still mints AIza… keys;
// newer Google API keys start with AQ. Must not match enroll.LooksLikeToken
// (`^\d{6,}:[A-Za-z0-9_-]{20,}$`).
func looksLikeGeminiKey(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 30 || len(t) > 80 {
		return false
	}
	switch {
	case strings.HasPrefix(t, "AIza"):
	case strings.HasPrefix(t, "AQ."):
	default:
		return false
	}
	for _, r := range t {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func isAiKeyStart(rest string) bool {
	r := strings.ToLower(strings.TrimSpace(rest))
	return r == "ai_key" || r == "set_ai_key"
}
