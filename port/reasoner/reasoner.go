// Package reasoner is the Reasoner port: structured output, cost reported, a
// tier chosen per call (docs/2-architecture.md § Routing). A tier is a name
// from the routing table; which model fills it is the adapter's config. An
// adapter must pass Contract before it can be bound.
package reasoner

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	// ErrInvalid: an empty prompt or a schema that is not JSON.
	ErrInvalid = errors.New("reasoner: invalid request")
	// ErrUnknownTier: the adapter has no model for the requested tier.
	ErrUnknownTier = errors.New("reasoner: unknown tier")
	// ErrBudget: the call would exceed or did exceed MaxUSD; no output.
	ErrBudget = errors.New("reasoner: budget exceeded")
)

// Tier names a model slot of the routing table.
type Tier string

// The tiers of spec/routing.json that name a reasoner.
const (
	Routine Tier = "routine"
	Top     Tier = "top"
)

// Request is one reasoning call.
type Request struct {
	Tier   Tier
	Prompt string
	// Schema is a JSON Schema the output must satisfy; empty means any JSON value.
	Schema json.RawMessage
	// MaxUSD stops the call when its cost would pass it; zero means no cap.
	MaxUSD float64
}

// Response is the answer and what it cost.
type Response struct {
	Output    json.RawMessage // one valid JSON value
	Tier      Tier            // the tier that answered
	Model     string          // the model that answered, for the run record
	CostUSD   float64         // zero on a flat-rate plan, never negative
	SessionID string          // the provider's session or request ID, for audit
}

// Reasoner is the port.
type Reasoner interface {
	Reason(ctx context.Context, req Request) (Response, error)
}
