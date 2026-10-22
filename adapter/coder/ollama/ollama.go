// Package ollama is the Coder port on a local Ollama server (E14), over its
// chat API. Thin: configuration for a standard HTTP protocol, no feature of
// Ollama beyond a JSON-schema constrained answer.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/briolink/briolink/port/coder"
)

// DefaultBaseURL is where Ollama listens on the ops host.
const DefaultBaseURL = "http://127.0.0.1:11434"

// maxBody caps what is read of one answer.
const maxBody = 32 << 20

var (
	// ErrAPI: Ollama answered with a status other than 200, or not at all.
	ErrAPI = errors.New("ollama: request failed")
	// ErrAnswer: the model answered, but not with files the runner can apply.
	ErrAnswer = errors.New("ollama: unusable answer")
)

// system is the first message of every call, so the prefix stays stable.
const system = `You write code for a task. Answer with one JSON object {"files": {"<path>": "<full new content>"}}: ` +
	`paths relative to the workspace root, each file complete, nothing else.`

// format is the JSON schema Ollama constrains the answer to.
var format = json.RawMessage(`{"type":"object","properties":{"files":{"type":"object","additionalProperties":{"type":"string"}}},"required":["files"]}`)

// Coder is the port on one Ollama server and model.
type Coder struct {
	Model   string       // the routing table's coder tier, e.g. qwen3-coder:30b
	BaseURL string       // DefaultBaseURL when empty
	Client  *http.Client // http.DefaultClient when nil; the context bounds a call
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []message       `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format"`
	Options  map[string]any  `json:"options"`
}

type chatResponse struct {
	Model   string  `json:"model"`
	Message message `json:"message"`
}

// Code sends the parts in order, stable first, as one user message.
func (c *Coder) Code(ctx context.Context, req coder.Request) (coder.Response, error) {
	if err := ctx.Err(); err != nil {
		return coder.Response{}, err
	}
	if len(req.Parts) == 0 {
		return coder.Response{}, coder.ErrInvalid
	}
	body, err := json.Marshal(chatRequest{
		Model: c.Model,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: strings.Join(req.Parts, "\n\n")},
		},
		Format:  format,
		Options: map[string]any{"temperature": 0},
	})
	if err != nil {
		return coder.Response{}, err
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return coder.Response{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return coder.Response{}, ctx.Err()
		}
		return coder.Response{}, fmt.Errorf("%w: %v", ErrAPI, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return coder.Response{}, fmt.Errorf("%w: %v", ErrAPI, err)
	}
	if res.StatusCode != http.StatusOK {
		return coder.Response{}, fmt.Errorf("%w: status %d: %s", ErrAPI, res.StatusCode, clip(raw))
	}
	var chat chatResponse
	if err := json.Unmarshal(raw, &chat); err != nil {
		return coder.Response{}, fmt.Errorf("%w: reply is not JSON", ErrAPI)
	}
	var answer struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(chat.Message.Content), &answer); err != nil {
		return coder.Response{}, fmt.Errorf("%w: not {\"files\": {…}}", ErrAnswer)
	}
	if len(answer.Files) == 0 {
		return coder.Response{}, fmt.Errorf("%w: no files", ErrAnswer)
	}
	for p := range answer.Files {
		if p == "" || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") {
			return coder.Response{}, fmt.Errorf("%w: path %q is not clean and relative", ErrAnswer, p)
		}
	}
	model := chat.Model
	if model == "" {
		model = c.Model
	}
	return coder.Response{Files: answer.Files, Model: model}, nil
}

// clip keeps an error short.
func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
