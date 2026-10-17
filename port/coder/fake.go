package coder

import (
	"context"
	"maps"
	"sync"
)

// Fake answers every request with a fixed Response and records the requests.
type Fake struct {
	Answer Response

	mu    sync.Mutex
	calls []Request
}

// Calls returns the requests received so far.
func (f *Fake) Calls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.calls...)
}

func (f *Fake) Code(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if len(req.Parts) == 0 {
		return Response{}, ErrInvalid
	}
	f.mu.Lock()
	f.calls = append(f.calls, Request{Parts: append([]string(nil), req.Parts...)})
	f.mu.Unlock()
	return Response{Files: maps.Clone(f.Answer.Files), Model: f.Answer.Model}, nil
}
