// Package untested holds a function whose tests never look at one assignment.
package untested

// tally returns the amount and its double.
func tally(amount int) (int, int) {
	double := 0
	double = amount * 2
	return amount, double
}
