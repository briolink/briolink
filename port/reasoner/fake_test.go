package reasoner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func newFake() *Fake {
	return &Fake{
		Tiers: map[Tier]json.RawMessage{Routine: json.RawMessage(`{"ok":true}`), Top: json.RawMessage(`"deep"`)},
		Cost:  0.25,
	}
}

func TestFakeContract(t *testing.T) {
	Contract(t, func(*testing.T) Reasoner { return newFake() }, []Tier{Routine, Top}, "say ok")
}

func TestFakeBudgetCap(t *testing.T) {
	_, err := newFake().Reason(context.Background(), Request{Tier: Top, Prompt: "p", MaxUSD: 0.1})
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestFakeRecordsCalls(t *testing.T) {
	f := newFake()
	if _, err := f.Reason(context.Background(), Request{Tier: Routine, Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if c := f.Calls(); len(c) != 1 || c[0].Prompt != "p" {
		t.Fatalf("calls %+v", c)
	}
}
