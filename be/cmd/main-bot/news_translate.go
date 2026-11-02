package main

import (
	"context"
	"log/slog"

	adapterllm "github.com/Aryan-mor/sushi-study/be/internal/adapter/llm"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// newsTranslator builds the Persian-synopsis translator (ADR 0032) on indexerd's LLM route
// (ADR 0025). Unconfigured or broken is not fatal: the news post keeps TMDB's text, and the
// warning says why.
func newsTranslator(ctx context.Context, cfg config.MainBot, log *slog.Logger) newsroom.Translator {
	if !cfg.LLM.Configured() || cfg.TranslateModel == "" {
		log.Info("LLM not configured; news posts keep TMDB's synopsis untranslated (ADR 0032)")
		return nil
	}
	root, err := adapterllm.New(ctx, adapterllm.Config{
		Provider:           cfg.LLM.Provider,
		FallbackModel:      cfg.LLM.FallbackModel,
		VertexProject:      cfg.LLM.VertexProject,
		VertexLocation:     cfg.LLM.VertexLocation,
		VertexSAJSON:       cfg.LLM.VertexSAJSON,
		OpenRouterAPIKey:   cfg.LLM.OpenRouterAPIKey,
		OpenRouterProvider: cfg.LLM.OpenRouterProvider,
	})
	if err != nil {
		log.Warn("news translator unavailable; synopses stay untranslated", "err", err)
		return nil
	}
	log.Info("news synopses are translated to Persian", "provider", cfg.LLM.Provider, "model", cfg.TranslateModel)
	return newsroom.NewLLMTranslator(root.Named(cfg.TranslateModel))
}
