package ollama

import (
	"os"
	"testing"

	"github.com/briolink/briolink/port/coder"
)

// TestLive runs the Coder contract against a real Ollama server and model.
// Skipped unless BRIOLINK_LIVE_OLLAMA names the model (BRIOLINK_LIVE_OLLAMA_URL
// overrides the server, DefaultBaseURL otherwise).
func TestLive(t *testing.T) {
	model := os.Getenv("BRIOLINK_LIVE_OLLAMA")
	if model == "" {
		t.Skip("BRIOLINK_LIVE_OLLAMA is not set")
	}
	coder.Contract(t, func(*testing.T) coder.Coder {
		return &Coder{Model: model, BaseURL: os.Getenv("BRIOLINK_LIVE_OLLAMA_URL")}
	}, coder.Request{Parts: []string{
		"Task: write the Go function `func Sum(a, b int) int` that returns a+b.",
		"File: sum.go, package sum.",
	}})
}
