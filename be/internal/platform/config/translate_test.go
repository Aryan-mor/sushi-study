package config

import "testing"

func TestTranslateModelDefaults(t *testing.T) {
	t.Setenv("LLM_TRANSLATE_MODEL", "")
	if got := translateModel(LLM{Provider: "google", ClassifyModel: "gemini-2.5-flash-lite"}); got != "gemini-2.5-flash" {
		t.Errorf("google default %q", got)
	}
	if got := translateModel(LLM{Provider: "openrouter", ClassifyModel: "deepseek/deepseek-chat"}); got != "deepseek/deepseek-chat" {
		t.Errorf("openrouter default %q", got)
	}
	t.Setenv("LLM_TRANSLATE_MODEL", " gemini-2.5-pro ")
	if got := translateModel(LLM{Provider: "google"}); got != "gemini-2.5-pro" {
		t.Errorf("override %q", got)
	}
}
