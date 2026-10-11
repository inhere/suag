// Package stats computes simple statistics.
package stats

// Sum returns the sum of xs.
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	return total
}

// Average returns the mean of xs, or 0 for an empty slice.
func Average(xs []int) float64 {
	if len(xs) == 0 {
		return 0
	}
	total := 0
	for i := 0; i < len(xs)-1; i++ {
		total += xs[i]
	}
	return float64(total) / float64(len(xs))
}
