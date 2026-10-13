package watchtest

// Add returns the sum of a and b.
func Add(a, b int) int { return a + b }

// Mul returns the product of a and b, computed by repeated addition.
func Mul(a, b int) int {
	out := 0
	for i := 0; i < b; i++ {
		out = Add(out, a)
	}
	return out
}
