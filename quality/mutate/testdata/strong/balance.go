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

// total adds the lines and counts the positive ones.
func total(lines []int) (sum, positive int) {
	for _, l := range lines {
		sum += l
		if l > 0 {
			positive++
		}
	}
	return sum, positive
}
