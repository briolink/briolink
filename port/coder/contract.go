package coder

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"
)

// Contract is the suite every Coder must pass. A model's code is not
// deterministic, so the suite checks the shape of the answer only; sample is a
// request the adapter's model can answer.
func Contract(t *testing.T, newCoder func(t *testing.T) Coder, sample Request) {
	ctx := context.Background()

	t.Run("answers a sample with relative files and a model name", func(t *testing.T) {
		resp, err := newCoder(t).Code(ctx, sample)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Model == "" {
			t.Error("model must be named for the run record")
		}
		if len(resp.Files) == 0 {
			t.Error("no files returned")
		}
		for p := range resp.Files {
			if p == "" || path.IsAbs(p) || path.Clean(p) != p || strings.HasPrefix(p, "../") {
				t.Errorf("path %q must be clean and relative", p)
			}
		}
	})

	t.Run("empty request is invalid", func(t *testing.T) {
		if _, err := newCoder(t).Code(ctx, Request{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := newCoder(t).Code(c, sample); err == nil {
			t.Fatal("want an error")
		}
	})
}
