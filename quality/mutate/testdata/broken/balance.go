// Package broken holds a function whose tests fail before any mutation.
package broken

func settles_balance(amount, balance int) bool { return amount > 0 && amount == balance }
