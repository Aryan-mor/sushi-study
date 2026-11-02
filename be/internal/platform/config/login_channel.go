package config

import (
	"fmt"
	"strconv"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/pairs"
)

// loginPostChannel parses LOGIN_POST_CHANNEL=username|-100… (exactly one entry, ADR 0013).
func loginPostChannel(raw string) (username string, id int64, problems []string) {
	if raw == "" {
		return "", 0, []string{"LOGIN_POST_CHANNEL is empty"}
	}
	entries, err := pairs.Parser{FieldNames: []string{"username", "channel id"}}.Parse("LOGIN_POST_CHANNEL", raw)
	if err != nil {
		return "", 0, []string{err.Error()}
	}
	if len(entries) != 1 {
		return "", 0, []string{fmt.Sprintf("LOGIN_POST_CHANNEL: %d entries, want 1", len(entries))}
	}
	id, err = strconv.ParseInt(entries[0][1], 10, 64)
	if err != nil {
		return "", 0, []string{"LOGIN_POST_CHANNEL: channel id is not an integer"}
	}
	if id >= 0 {
		return "", 0, []string{"LOGIN_POST_CHANNEL: channel id must be negative (a supergroup/channel)"}
	}
	return entries[0].Key(), id, nil
}
