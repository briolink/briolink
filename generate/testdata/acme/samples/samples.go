// Package samples is the hand-written half of the acme derived tests: the
// entities and event values at the boundaries the rules in
// spec/domain/acme.json name. It is not generated.
package samples

import (
	"time"

	"example.com/acme/gen/domain/acme"
	"example.com/acme/gen/domain/acme/acmetest"
)

var now = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func line(qty, price int64) acme.Line {
	return acme.Line{Description: "item", Quantity: qty, UnitPrice: price}
}

// invoice is a valid invoice whose lines total 1000 cents.
func invoice(paid int64, lines ...acme.Line) acme.Invoice {
	if len(lines) == 0 {
		lines = []acme.Line{line(2, 500)}
	}
	return acme.Invoice{Id: "i1", Customer: "c1", Currency: acme.CurrencyEur, Lines: lines, Paid: paid}
}

func str(s string) *string { return &s }

type c = acmetest.InvoiceCase

// Invoice implements acmetest.InvoiceSamples.
type Invoice struct{}

// Fire: transitions in the order of the spec; each rule's boundary from
// the inside, with the entity the step must return.
func (Invoice) Fire(i int) []c {
	switch i {
	case 0: // draft add_line: quantity 1 and a price of 0 are the lowest accepted; appended after the existing line
		return []c{{invoice(0), acme.InvoiceAddLine{Line: line(1, 0)}, invoice(0, line(2, 500), line(1, 0))}}
	case 1: // draft issue: exactly one line; number and issued_at set, nothing else
		e := invoice(0, line(1, 1))
		want := e
		want.Number, want.IssuedAt = str("n1"), &now
		return []c{{e, acme.InvoiceIssue{Number: "n1", Now: now}, want}}
	case 2: // draft void: no guard; the reason is kept
		want := invoice(0)
		want.VoidReason = str("mistake")
		return []c{{invoice(0), acme.InvoiceVoid{Reason: "mistake"}, want}}
	case 3: // issued record_payment: the amount equals the balance, with and without a part paid, and the smallest balance
		return []c{
			{invoice(0), acme.InvoiceRecordPayment{Amount: 1000, Now: now}, invoice(1000)},
			{invoice(400), acme.InvoiceRecordPayment{Amount: 600, Now: now}, invoice(1000)},
			{invoice(0, line(1, 1)), acme.InvoiceRecordPayment{Amount: 1, Now: now}, invoice(1, line(1, 1))},
		}
	case 4: // issued record_payment: one cent, and one cent below the balance, with and without a part paid
		return []c{
			{invoice(0), acme.InvoiceRecordPayment{Amount: 1, Now: now}, invoice(1)},
			{invoice(0), acme.InvoiceRecordPayment{Amount: 999, Now: now}, invoice(999)},
			{invoice(400), acme.InvoiceRecordPayment{Amount: 599, Now: now}, invoice(999)},
		}
	case 5: // issued void: nothing paid; the reason is kept
		want := invoice(0)
		want.VoidReason = str("mistake")
		return []c{{invoice(0), acme.InvoiceVoid{Reason: "mistake"}, want}}
	}
	panic("no such transition")
}

// Block: each guard false, one step outside each boundary.
func (Invoice) Block(i int) []c {
	switch i {
	case 0: // quantity 0, a price below 0, a blank description
		return []c{
			{Entity: invoice(0), Event: acme.InvoiceAddLine{Line: line(0, 0)}},
			{Entity: invoice(0), Event: acme.InvoiceAddLine{Line: line(1, -1)}},
			{Entity: invoice(0), Event: acme.InvoiceAddLine{Line: acme.Line{Description: " ", Quantity: 1, UnitPrice: 0}}},
		}
	case 1: // no lines
		e := invoice(0)
		e.Lines = nil
		return []c{{Entity: e, Event: acme.InvoiceIssue{Number: "n1", Now: now}}}
	case 3: // one cent below the balance, one above, zero; zero on a zero balance
		return []c{
			{Entity: invoice(0, line(1, 0)), Event: acme.InvoiceRecordPayment{Amount: 0, Now: now}},
			{Entity: invoice(0), Event: acme.InvoiceRecordPayment{Amount: 999, Now: now}},
			{Entity: invoice(0), Event: acme.InvoiceRecordPayment{Amount: 1001, Now: now}},
			{Entity: invoice(0), Event: acme.InvoiceRecordPayment{Amount: 0, Now: now}},
		}
	case 4: // equal to the balance, with and without a part paid, above it, zero
		return []c{
			{Entity: invoice(0), Event: acme.InvoiceRecordPayment{Amount: 1000, Now: now}},
			{Entity: invoice(400), Event: acme.InvoiceRecordPayment{Amount: 600, Now: now}},
			{Entity: invoice(0), Event: acme.InvoiceRecordPayment{Amount: 1001, Now: now}},
			{Entity: invoice(0), Event: acme.InvoiceRecordPayment{Amount: 0, Now: now}},
		}
	case 5: // one cent paid
		return []c{{Entity: invoice(1), Event: acme.InvoiceVoid{Reason: "mistake"}}}
	}
	panic("no such guarded transition")
}

// Forbid: a valid invoice and the event, in any state.
func (Invoice) Forbid(_ acme.InvoiceState, event string) c {
	e := invoice(0)
	switch event {
	case "add_line":
		return c{Entity: e, Event: acme.InvoiceAddLine{Line: line(1, 1)}}
	case "issue":
		return c{Entity: e, Event: acme.InvoiceIssue{Number: "n1", Now: now}}
	case "record_payment":
		return c{Entity: e, Event: acme.InvoiceRecordPayment{Amount: 1, Now: now}}
	case "void":
		return c{Entity: e, Event: acme.InvoiceVoid{Reason: "mistake"}}
	}
	panic("no such event")
}

// Violate: each invariant broken one step outside each boundary, the other
// invariants kept.
func (Invoice) Violate(invariant string) []acme.Invoice {
	switch invariant {
	case "paid_within_total": // one cent below 0, one above the total
		return []acme.Invoice{invoice(-1), invoice(1001)}
	case "lines_valid": // a quantity of 0, a price of -1 (beside a line that keeps the total above paid)
		return []acme.Invoice{
			invoice(0, line(2, 500), line(0, 1)),
			invoice(0, line(2, 500), line(1, -1)),
		}
	}
	panic("no such invariant")
}
