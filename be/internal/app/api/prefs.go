package api

import (
	"context"
	"fmt"

	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// GeminiKeyStore is the sealed-key read Prefs needs. Declared here, by the consumer (R-GO-5).
type GeminiKeyStore interface {
	GeminiAPIKey(ctx context.Context, userID int64) (key string, ok bool, err error)
}

// Prefs returns playback prefs the client cannot keep on disk — today, the user's Gemini API key,
// sealed by main-bot and opened here (doc 15 §12). The key is the user's; it is never logged.
func (a *API) Prefs(ctx context.Context, p Principal, _ *sushiv1.PrefsReq) (*sushiv1.PrefsRes, error) {
	if a.geminiKeys == nil {
		return nil, fmt.Errorf("prefs: not configured")
	}
	key, ok, err := a.geminiKeys.GeminiAPIKey(ctx, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("prefs: %w", err)
	}
	if !ok {
		return &sushiv1.PrefsRes{}, nil
	}
	return &sushiv1.PrefsRes{HasGeminiKey: true, GeminiApiKey: key}, nil
}
