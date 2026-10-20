package reference_test

import (
	"testing"

	"example.com/acme/gen/domain/acme/acmetest"
	"example.com/acme/reference"
	"example.com/acme/samples"
)

func TestInvoice(t *testing.T) { acmetest.Invoice(t, reference.Impl{}, samples.Invoice{}) }

func TestPure(t *testing.T) { samples.Pure(t, reference.Impl{}) }
