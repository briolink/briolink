package reasoner

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
)

// Fake answers from a handler per tier and records the requests. A tier with no
// handler is unknown. CostUSD per call is fixed at Cost.
type Fake struct {
	// Tiers maps a tier to the output it returns for any prompt.
	Tiers map[Tier]json.RawMessage
	Cost  float64

	mu    sync.Mutex
	calls []Request
	n     int
}

// Calls returns the requests received so far.
func (f *Fake) Calls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.calls...)
}

func (f *Fake) Reason(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if req.Prompt == "" || (len(req.Schema) > 0 && !json.Valid(req.Schema)) {
		return Response{}, ErrInvalid
	}
	out, ok := f.Tiers[req.Tier]
	if !ok {
		return Response{}, ErrUnknownTier
	}
	if req.MaxUSD > 0 && f.Cost > req.MaxUSD {
		return Response{}, ErrBudget
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	f.n++
	return Response{
		Output:    append(json.RawMessage(nil), out...),
		Tier:      req.Tier,
		Model:     "fake-" + string(req.Tier),
		CostUSD:   f.Cost,
		SessionID: "fake-session-" + strconv.Itoa(f.n),
	}, nil
}
