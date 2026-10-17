package reasoner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// Contract is the suite every Reasoner must pass. sample is a prompt the
// adapter's models can answer; tiers are the tiers the adapter is configured
// with (at least one).
func Contract(t *testing.T, newReasoner func(t *testing.T) Reasoner, tiers []Tier, sample string) {
	ctx := context.Background()

	for _, tier := range tiers {
		t.Run("answers on tier "+string(tier), func(t *testing.T) {
			resp, err := newReasoner(t).Reason(ctx, Request{Tier: tier, Prompt: sample})
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(resp.Output) {
				t.Errorf("output is not one JSON value: %q", resp.Output)
			}
			if resp.Tier != tier {
				t.Errorf("answered on %q, asked %q", resp.Tier, tier)
			}
			if resp.Model == "" {
				t.Error("model must be named for the run record")
			}
			if resp.CostUSD < 0 {
				t.Errorf("negative cost %v", resp.CostUSD)
			}
		})
	}

	t.Run("unknown tier", func(t *testing.T) {
		_, err := newReasoner(t).Reason(ctx, Request{Tier: "no-such-tier", Prompt: sample})
		if !errors.Is(err, ErrUnknownTier) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("invalid requests", func(t *testing.T) {
		r := newReasoner(t)
		if _, err := r.Reason(ctx, Request{Tier: tiers[0]}); !errors.Is(err, ErrInvalid) {
			t.Errorf("empty prompt: %v", err)
		}
		if _, err := r.Reason(ctx, Request{Tier: tiers[0], Prompt: sample, Schema: json.RawMessage("{")}); !errors.Is(err, ErrInvalid) {
			t.Errorf("bad schema: %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := newReasoner(t).Reason(c, Request{Tier: tiers[0], Prompt: sample}); err == nil {
			t.Fatal("want an error")
		}
	})
}
