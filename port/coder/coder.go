// Package coder is the Coder port: the cheap model that turns a task into
// code (docs/2-architecture.md § Runner). The runner applies the answer to the
// allowed files only, so the port just carries text in and files out. An
// adapter must pass Contract before it can be bound.
package coder

import (
	"context"
	"errors"
)

// ErrInvalid: the request has no parts.
var ErrInvalid = errors.New("coder: invalid request")

// Request is what the runner sends. Parts are ordered stable first (contract,
// signature, documentation, tests, last failure output) so an adapter can cache
// the prefix; the port never reorders them.
type Request struct {
	Parts []string
}

// Response is the coder's answer.
type Response struct {
	// Files maps a path relative to the workspace to its full new content.
	// The runner, not the coder, decides which paths are allowed.
	Files map[string]string
	// Model names the model that answered, for the run record.
	Model string
}

// Coder is the port.
type Coder interface {
	Code(ctx context.Context, req Request) (Response, error)
}
