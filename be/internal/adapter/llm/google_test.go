package llm

import (
	"context"
	"strings"
	"testing"
)

func TestGoogleRejectsOpenRouterModelID(t *testing.T) {
	t.Parallel()
	g := &googleBackend{project: "p", location: "us-central1"}
	_, err := g.complete(context.Background(), "google/gemini-3.1-flash-lite", "", "hi", nil)
	if err == nil || !strings.Contains(err.Error(), "cannot contain '/'") {
		t.Fatalf("err=%v, want slash rejection before HTTP", err)
	}
}
