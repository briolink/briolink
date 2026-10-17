package objectstore

import "testing"

func TestFakeContract(t *testing.T) {
	Contract(t, func(*testing.T) Store { return NewFake() })
}
