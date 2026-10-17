package codehost

import "testing"

func TestFakeContract(t *testing.T) {
	Contract(t, func(*testing.T) Host { return NewFake() })
}
