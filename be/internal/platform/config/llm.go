package config

import (
	"os"
	"strings"
)

func loadIndexerLLM() LLM {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER")))
	orKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	sa := strings.TrimSpace(os.Getenv("GOOGLE_VERTEX_SA_JSON"))
	if provider == "" {
		switch {
		case sa != "":
			provider = "google"
		case orKey != "":
			provider = "openrouter"
		}
	}

	classify := firstNonEmpty(os.Getenv("LLM_CLASSIFY_MODEL"), os.Getenv("OPENROUTER_MODEL"))
	referee := strings.TrimSpace(os.Getenv("LLM_REFEREE_MODEL"))
	verifier := strings.TrimSpace(os.Getenv("LLM_VERIFIER_MODEL"))
	fallback := firstNonEmpty(os.Getenv("LLM_FALLBACK_MODEL"), os.Getenv("OPENROUTER_FALLBACK_MODEL"))
	if provider == "google" {
		classify = defaultString(classify, "gemini-2.5-flash-lite")
		referee = defaultString(referee, "gemini-2.5-pro")
		verifier = defaultString(verifier, "gemini-2.5-flash-lite")
		// Vertex ids are bare (gemini-2.5-pro). OPENROUTER_FALLBACK_MODEL is org/model and 404s
		// if we send it to generateContent — ignore it unless LLM_FALLBACK_MODEL is a Vertex id.
		fallback = strings.TrimSpace(os.Getenv("LLM_FALLBACK_MODEL"))
		if strings.Contains(fallback, "/") {
			fallback = ""
		}
	}

	loc := strings.TrimSpace(os.Getenv("GOOGLE_VERTEX_LOCATION"))
	if loc == "" {
		loc = "us-central1"
	}
	return LLM{
		Provider:           provider,
		ClassifyModel:      classify,
		RefereeModel:       referee,
		VerifierModel:      verifier,
		FallbackModel:      fallback,
		VertexProject:      strings.TrimSpace(os.Getenv("GOOGLE_VERTEX_PROJECT")),
		VertexLocation:     loc,
		VertexSAJSON:       sa,
		OpenRouterAPIKey:   orKey,
		OpenRouterProvider: strings.TrimSpace(os.Getenv("OPENROUTER_PROVIDER")),
	}
}

func llmProblems(l LLM) []string {
	p := strings.ToLower(strings.TrimSpace(l.Provider))
	if p == "" {
		return nil
	}
	if p != "google" && p != "openrouter" {
		return []string{`LLM_PROVIDER must be "google" or "openrouter"`}
	}
	if l.Configured() {
		return nil
	}
	if p == "google" {
		return []string{"LLM_PROVIDER=google requires GOOGLE_VERTEX_SA_JSON, GOOGLE_VERTEX_PROJECT, and LLM_CLASSIFY_MODEL"}
	}
	return []string{"LLM_PROVIDER=openrouter requires OPENROUTER_API_KEY and LLM_CLASSIFY_MODEL (or OPENROUTER_MODEL)"}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
