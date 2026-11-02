package config_test

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

func TestLoadIndexerGoogleRequiresSA(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("TMDB_API_KEY", "tmdb")
	t.Setenv("LLM_PROVIDER", "google")

	_, err := config.LoadIndexer("")
	if err == nil || !strings.Contains(err.Error(), "GOOGLE_VERTEX_SA_JSON") {
		t.Fatalf("err = %v, want google SA required", err)
	}
}

func TestLoadIndexerGoogleOK(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("TMDB_API_KEY", "tmdb")
	t.Setenv("LLM_PROVIDER", "google")
	t.Setenv("GOOGLE_VERTEX_PROJECT", "proj")
	t.Setenv("GOOGLE_VERTEX_SA_JSON", `{"type":"service_account"}`)

	cfg, err := config.LoadIndexer("")
	if err != nil {
		t.Fatalf("LoadIndexer: %v", err)
	}
	if cfg.LLM.Provider != "google" || cfg.LLM.ClassifyModel != "gemini-2.5-flash-lite" {
		t.Fatalf("LLM = %+v", cfg.LLM)
	}
	if !cfg.LLM.Configured() || !cfg.LLM.JuryConfigured() {
		t.Fatalf("configured=%v jury=%v", cfg.LLM.Configured(), cfg.LLM.JuryConfigured())
	}
	if cfg.LLM.RefereeModel != "gemini-2.5-pro" || cfg.LLM.VerifierModel != "gemini-2.5-flash-lite" {
		t.Fatalf("jury models %+v", cfg.LLM)
	}
}

func TestLoadIndexerGoogleIgnoresOpenRouterFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("TMDB_API_KEY", "tmdb")
	t.Setenv("LLM_PROVIDER", "google")
	t.Setenv("GOOGLE_VERTEX_PROJECT", "proj")
	t.Setenv("GOOGLE_VERTEX_SA_JSON", `{"type":"service_account"}`)
	t.Setenv("OPENROUTER_FALLBACK_MODEL", "google/gemini-3.1-flash-lite")
	t.Setenv("LLM_FALLBACK_MODEL", "gemini-2.0-flash")

	cfg, err := config.LoadIndexer("")
	if err != nil {
		t.Fatalf("LoadIndexer: %v", err)
	}
	if cfg.LLM.FallbackModel != "gemini-2.0-flash" {
		t.Fatalf("FallbackModel = %q, want Vertex id from LLM_FALLBACK_MODEL", cfg.LLM.FallbackModel)
	}
}

func TestLoadIndexerGoogleDropsSlashFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("TMDB_API_KEY", "tmdb")
	t.Setenv("LLM_PROVIDER", "google")
	t.Setenv("GOOGLE_VERTEX_PROJECT", "proj")
	t.Setenv("GOOGLE_VERTEX_SA_JSON", `{"type":"service_account"}`)
	t.Setenv("LLM_FALLBACK_MODEL", "google/gemini-3.1-flash-lite")

	cfg, err := config.LoadIndexer("")
	if err != nil {
		t.Fatalf("LoadIndexer: %v", err)
	}
	if cfg.LLM.FallbackModel != "" {
		t.Fatalf("FallbackModel = %q, want empty (OpenRouter id on Vertex)", cfg.LLM.FallbackModel)
	}
}

func TestLoadIndexerOpenRouterOK(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("TMDB_API_KEY", "tmdb")
	t.Setenv("LLM_PROVIDER", "openrouter")
	t.Setenv("OPENROUTER_API_KEY", "sk-or")
	t.Setenv("OPENROUTER_MODEL", "deepseek/deepseek-v4-flash")

	cfg, err := config.LoadIndexer("")
	if err != nil {
		t.Fatalf("LoadIndexer: %v", err)
	}
	if cfg.LLM.Provider != "openrouter" || cfg.LLM.ClassifyModel != "deepseek/deepseek-v4-flash" {
		t.Fatalf("LLM = %+v", cfg.LLM)
	}
	if cfg.LLM.JuryConfigured() {
		t.Fatalf("openrouter without referee models should skip jury")
	}
}

func TestLoadIndexerNoLLMIsFilenameFirst(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("TMDB_API_KEY", "tmdb")

	cfg, err := config.LoadIndexer("")
	if err != nil {
		t.Fatalf("LoadIndexer: %v", err)
	}
	if cfg.LLM.Configured() {
		t.Fatalf("expected no LLM, got %+v", cfg.LLM)
	}
}
