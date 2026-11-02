package newsroom

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// translateTimeout bounds one LLM call. A preview that waits longer than this is worse than a
// preview in English: the next approve (or the next title) tries again.
const translateTimeout = 45 * time.Second

// Translator turns a TMDB synopsis into the Persian the post is written in (ADR 0032).
type Translator interface {
	TranslateOverview(ctx context.Context, a Announcement) (string, error)
}

// OverviewStore keeps a translation in the library beside the English it was made from
// (items.overview_fa / episodes.overview_fa), so one title is translated once. *pglib.Ops.
type OverviewStore interface {
	SaveOverviewFa(ctx context.Context, a Announcement, fa string) error
}

// Completer is one structured LLM exchange — adapter/llm.Client satisfies it (ADR 0025). Declared
// here so newsroom never imports a vendor (R-GO-5).
type Completer interface {
	Complete(ctx context.Context, systemPrompt, userContent string, schema map[string]any) (string, error)
}

// FillOverview swaps a.Overview for its Persian translation. The stored one wins; otherwise the
// translator is asked and a good answer is saved for next time. Every failure keeps the English:
// a post with an untranslated synopsis beats a post held back by an LLM outage.
func FillOverview(ctx context.Context, tr Translator, store OverviewStore, log *slog.Logger, a Announcement) Announcement {
	if fa := strings.TrimSpace(a.OverviewFa); fa != "" {
		a.Overview = fa
		return a
	}
	if tr == nil || strings.TrimSpace(a.Overview) == "" || isPersian(a.Overview) {
		return a
	}
	tctx, cancel := context.WithTimeout(ctx, translateTimeout)
	defer cancel()
	fa, err := tr.TranslateOverview(tctx, a)
	if err != nil {
		log.Warn("newsroom: translate overview", "err", err, "tmdb", a.TMDBID, "kind", a.Kind,
			"season", a.Season, "episode", a.Episode)
		return a
	}
	if store != nil {
		// Keyed on the English we translated; a.Overview is overwritten only after.
		if err := store.SaveOverviewFa(ctx, a, fa); err != nil {
			log.Warn("newsroom: save overview_fa", "err", err, "tmdb", a.TMDBID, "kind", a.Kind)
		}
	}
	a.OverviewFa = fa
	a.Overview = fa
	return a
}

// LLMTranslator is the Translator over the ADR 0025 transport.
type LLMTranslator struct {
	llm Completer
}

// NewLLMTranslator returns nil for a nil Completer, so an env with no LLM configured simply posts
// English without a nil check at every call site.
func NewLLMTranslator(llm Completer) Translator {
	if llm == nil {
		return nil
	}
	return &LLMTranslator{llm: llm}
}

const translatePrompt = `You translate film and TV synopses from TMDB into Persian (Farsi) for a Persian-language Telegram channel that announces new releases.

Rules:
- Write natural, fluent, contemporary Persian, the way Iranian film sites and dubbing studios write — not a word-for-word rendering.
- Translate the meaning faithfully. Add nothing: no opinions, no extra plot, no spoilers the source does not contain. Leave nothing out.
- Write person and place names in Persian script, spelled the way Iranian media usually spell them.
- Plain text only: no markdown, no quotes around the answer, no notes, no English.
- Answer as JSON: {"overview": "<the Persian synopsis>"}.`

var translateSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"overview": map[string]any{"type": "string"}},
	"required":             []any{"overview"},
	"additionalProperties": false,
}

// TranslateOverview implements Translator.
func (t *LLMTranslator) TranslateOverview(ctx context.Context, a Announcement) (string, error) {
	raw, err := t.llm.Complete(ctx, translatePrompt, translateInput(a), translateSchema)
	if err != nil {
		return "", err
	}
	return parseTranslation(raw)
}

// translateInput gives the model the title as context — names and genre decide how a line reads —
// but asks it to translate only the synopsis.
func translateInput(a Announcement) string {
	var b strings.Builder
	b.WriteString("Title: " + a.Title)
	if a.Year > 0 {
		fmt.Fprintf(&b, " (%d)", a.Year)
	}
	b.WriteString("\n")
	switch {
	case a.Kind == catalog.KindSeries && a.Episode > 0:
		fmt.Fprintf(&b, "Kind: TV series, season %d episode %d", a.Season, a.Episode)
		if s := strings.TrimSpace(a.Subtitle); s != "" {
			b.WriteString(" — " + s)
		}
	case a.Kind == catalog.KindSeries:
		b.WriteString("Kind: TV series")
	default:
		b.WriteString("Kind: movie")
	}
	if g := strings.TrimSpace(a.Genres); g != "" {
		b.WriteString("\nGenres: " + g)
	}
	b.WriteString("\n\nSynopsis to translate:\n" + strings.TrimSpace(a.Overview))
	return b.String()
}

func parseTranslation(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	var out struct {
		Overview string `json:"overview"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out); err != nil {
		return "", fmt.Errorf("translate: decode: %w", err)
	}
	fa := strings.TrimSpace(out.Overview)
	if fa == "" {
		return "", fmt.Errorf("translate: empty overview")
	}
	// A model that echoed the English back must not be stored as "the translation".
	if !isPersian(fa) {
		return "", fmt.Errorf("translate: answer is not Persian")
	}
	return fa, nil
}

// isPersian reports whether most letters are Arabic-script. TMDB sometimes already carries a
// Persian overview; translating it again would only cost money.
func isPersian(s string) bool {
	var arabic, letters int
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Arabic, r) {
			arabic++
		}
	}
	return letters > 0 && arabic*2 > letters
}
