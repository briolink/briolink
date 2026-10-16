// Package weak holds a function whose tests miss the boundary.
package weak

// settles_balance is true exactly when a positive amount equals the balance.
func settles_balance(amount, balance int) bool {
	return amount > 0 && amount == balance
}
