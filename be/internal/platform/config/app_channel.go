package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/pairs"
)

// appChannelID parses RELEASE_CHANNEL. Empty is allowed (Download copies nothing until CI posts).
//
// Accepted shapes: "-100…" or "label|-100…". A public username is optional; the private news
// dump has none.
func appChannelID(raw string) (int64, []string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if strings.Contains(raw, "|") {
		entries, err := pairs.Parser{FieldNames: []string{"name", "channel id"}}.Parse("RELEASE_CHANNEL", raw)
		if err != nil {
			return 0, []string{err.Error()}
		}
		if len(entries) != 1 {
			return 0, []string{fmt.Sprintf("RELEASE_CHANNEL: %d entries, want 1", len(entries))}
		}
		raw = entries[0][1]
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, []string{"RELEASE_CHANNEL: channel id is not an integer"}
	}
	if id >= 0 {
		return 0, []string{"RELEASE_CHANNEL: channel id must be negative (a channel)"}
	}
	return id, nil
}
