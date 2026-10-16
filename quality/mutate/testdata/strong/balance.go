// Package strong holds a function whose tests catch every mutant.
package strong

// settles_balance is true exactly when a positive amount equals the balance.
func settles_balance(amount, balance int) bool {
	return amount > 0 && amount == balance
}

// label names a payment, "" when there is none.
func label(amount int) string {
	if amount <= 0 {
		return ""
	}
	return "payment"
}
