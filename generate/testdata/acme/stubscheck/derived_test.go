package stubscheck_test

import (
	"testing"

	"example.com/acme/gen/domain/acme/acmetest"
	"example.com/acme/gen/domain/acme/stubs"
	"example.com/acme/samples"
)

// TestInvoice must fail: the stubs panic. The plugin's test runs it and
// checks that it does.
func TestInvoice(t *testing.T) { acmetest.Invoice(t, stubs.Impl{}, samples.Invoice{}) }
