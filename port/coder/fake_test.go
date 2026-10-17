package coder

import (
	"context"
	"testing"
)

func newFake() *Fake {
	return &Fake{Answer: Response{Files: map[string]string{"a/b.go": "package a\n"}, Model: "fake"}}
}

func TestFakeContract(t *testing.T) {
	Contract(t, func(*testing.T) Coder { return newFake() }, Request{Parts: []string{"contract", "tests"}})
}

func TestFakeRecordsRequestsInOrder(t *testing.T) {
	f := newFake()
	if _, err := f.Code(context.Background(), Request{Parts: []string{"x", "y"}}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Parts[0] != "x" || calls[0].Parts[1] != "y" {
		t.Fatalf("calls %+v", calls)
	}
}
