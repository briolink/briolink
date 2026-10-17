// Package reference is a hand-written implementation of acme.Funcs: what
// the coder's work must be equivalent to. The derived tests pass on it and
// fail on the generated stubs.
package reference

import (
	"strings"
	"time"

	"example.com/acme/gen/domain/acme"
)

// Impl implements acme.Funcs.
type Impl struct{}

var _ acme.Funcs = Impl{}

func total(l []acme.Line) int64 {
	var sum int64
	for _, x := range l {
		sum += x.Quantity * x.UnitPrice
	}
	return sum
}

func (Impl) LineAcceptable(_ acme.Invoice, line acme.Line) bool {
	return line.Quantity >= 1 && line.UnitPrice >= 0 && strings.TrimSpace(line.Description) != ""
}

func (Impl) HasLines(invoice acme.Invoice) bool { return len(invoice.Lines) > 0 }

func (Impl) SettlesBalance(invoice acme.Invoice, amount int64) bool {
	return amount > 0 && amount == total(invoice.Lines)-invoice.Paid
}

func (Impl) PartialPayment(invoice acme.Invoice, amount int64) bool {
	return amount > 0 && amount < total(invoice.Lines)-invoice.Paid
}

func (Impl) Unpaid(invoice acme.Invoice) bool { return invoice.Paid == 0 }

func (Impl) AddLine(invoice acme.Invoice, line acme.Line) acme.Invoice {
	invoice.Lines = append(append([]acme.Line(nil), invoice.Lines...), line)
	return invoice
}

func (Impl) Issue(invoice acme.Invoice, number string, now time.Time) acme.Invoice {
	invoice.Number = &number
	invoice.IssuedAt = &now
	return invoice
}

func (Impl) ApplyPayment(invoice acme.Invoice, amount int64) acme.Invoice {
	invoice.Paid += amount
	return invoice
}

func (Impl) Void(invoice acme.Invoice, reason string) acme.Invoice {
	invoice.VoidReason = &reason
	return invoice
}

func (Impl) PaidWithinTotal(invoice acme.Invoice) bool {
	return invoice.Paid >= 0 && invoice.Paid <= total(invoice.Lines)
}

func (Impl) LinesValid(invoice acme.Invoice) bool {
	for _, l := range invoice.Lines {
		if l.Quantity < 1 || l.UnitPrice < 0 {
			return false
		}
	}
	return true
}

func (Impl) NewInvoice(id string, customer string, currency acme.Currency) acme.Invoice {
	return acme.Invoice{Id: id, Customer: customer, Currency: currency}
}

func (Impl) Total(invoice acme.Invoice) int64 { return total(invoice.Lines) }
