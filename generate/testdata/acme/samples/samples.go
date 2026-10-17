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

type c = acmetest.InvoiceCase

// Invoice implements acmetest.InvoiceSamples.
type Invoice struct{}

// Fire: transitions in the order of the spec; each rule's boundary from
// the inside.
func (Invoice) Fire(i int) []c {
	switch i {
	case 0: // draft add_line: quantity 1 and a price of 0 are the lowest accepted
		return []c{{invoice(0), acme.InvoiceAddLine{Line: line(1, 0)}}}
	case 1: // draft issue: exactly one line
		return []c{{invoice(0, line(1, 1)), acme.InvoiceIssue{Number: "n1", Now: now}}}
	case 2: // draft void: no guard
		return []c{{invoice(0), acme.InvoiceVoid{Reason: "mistake"}}}
	case 3: // issued record_payment: the amount equals the balance, with and without a part paid
		return []c{
			{invoice(0), acme.InvoiceRecordPayment{Amount: 1000, Now: now}},
			{invoice(400), acme.InvoiceRecordPayment{Amount: 600, Now: now}},
		}
	case 4: // issued record_payment: one cent, and one cent below the balance
		return []c{
			{invoice(0), acme.InvoiceRecordPayment{Amount: 1, Now: now}},
			{invoice(0), acme.InvoiceRecordPayment{Amount: 999, Now: now}},
		}
	case 5: // issued void: nothing paid
		return []c{{invoice(0), acme.InvoiceVoid{Reason: "mistake"}}}
	}
	panic("no such transition")
}

// Block: each guard false, one step outside each boundary.
func (Invoice) Block(i int) []c {
	switch i {
	case 0: // quantity 0, a price below 0, a blank description
		return []c{
			{invoice(0), acme.InvoiceAddLine{Line: line(0, 0)}},
			{invoice(0), acme.InvoiceAddLine{Line: line(1, -1)}},
			{invoice(0), acme.InvoiceAddLine{Line: acme.Line{Description: " ", Quantity: 1, UnitPrice: 0}}},
		}
	case 1: // no lines
		e := invoice(0)
		e.Lines = nil
		return []c{{e, acme.InvoiceIssue{Number: "n1", Now: now}}}
	case 3: // one cent below the balance, one above, zero; zero on a zero balance
		return []c{
			{invoice(0, line(1, 0)), acme.InvoiceRecordPayment{Amount: 0, Now: now}},
			{invoice(0), acme.InvoiceRecordPayment{Amount: 999, Now: now}},
			{invoice(0), acme.InvoiceRecordPayment{Amount: 1001, Now: now}},
			{invoice(0), acme.InvoiceRecordPayment{Amount: 0, Now: now}},
		}
	case 4: // equal to the balance, above it, zero
		return []c{
			{invoice(0), acme.InvoiceRecordPayment{Amount: 1000, Now: now}},
			{invoice(0), acme.InvoiceRecordPayment{Amount: 1001, Now: now}},
			{invoice(0), acme.InvoiceRecordPayment{Amount: 0, Now: now}},
		}
	case 5: // one cent paid
		return []c{{invoice(1), acme.InvoiceVoid{Reason: "mistake"}}}
	}
	panic("no such guarded transition")
}

// Forbid: a valid invoice and the event, in any state.
func (Invoice) Forbid(_ acme.InvoiceState, event string) c {
	e := invoice(0)
	switch event {
	case "add_line":
		return c{e, acme.InvoiceAddLine{Line: line(1, 1)}}
	case "issue":
		return c{e, acme.InvoiceIssue{Number: "n1", Now: now}}
	case "record_payment":
		return c{e, acme.InvoiceRecordPayment{Amount: 1, Now: now}}
	case "void":
		return c{e, acme.InvoiceVoid{Reason: "mistake"}}
	}
	panic("no such event")
}
